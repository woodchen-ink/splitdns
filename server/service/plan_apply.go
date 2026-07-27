package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/woodchen-ink/splitdns/server/model"
	"github.com/woodchen-ink/splitdns/server/pkg/cloudflare"
	"github.com/woodchen-ink/splitdns/server/pkg/dnspod"
)

// ErrNeedConfirm 表示该步骤会造成不可逆变更, 需要调用方带确认标记再来一次。
var ErrNeedConfirm = fmt.Errorf("需要确认")

// ApplyStep 让程序替用户执行某一步。执行完不直接判成功, 仍然走一次验证 ——
// 平台接口返回 200 不等于配置已经生效, 生效与否只认巡检结果。
func ApplyStep(ctx context.Context, planID, stepID uint, confirm bool) (string, error) {
	plan, err := loadPlan(planID)
	if err != nil {
		return "", err
	}
	h, err := HostnameByID(plan.HostnameID)
	if err != nil {
		return "", err
	}

	var step *model.Step
	for i := range plan.Steps {
		if plan.Steps[i].ID == stepID {
			step = &plan.Steps[i]
			break
		}
	}
	if step == nil {
		return "", fmt.Errorf("流程 %d 下没有步骤 %d", planID, stepID)
	}

	snap := Inspect(ctx, *h).Snapshot

	switch step.Key {
	case "saas.fallback_origin":
		return applyFallbackOrigin(ctx, *h, snap)
	case "saas.custom_hostname":
		return applyCustomHostname(ctx, *h)
	case "dnspod.zone":
		return applyDNSPodZone(ctx, *h)
	case "dnspod.dcv_txt":
		return applyDCVRecords(ctx, *h, snap)
	case "dnspod.routes":
		return applyRouteRecords(ctx, *h, snap)
	case "cf.delegation":
		return applyDelegation(ctx, *h, snap)
	case "cf.cleanup":
		return applyCleanup(ctx, *h, confirm)
	case "saas.cert":
		return "", fmt.Errorf("这一步只需要等待, CF 会自己签发证书")
	}
	return "", fmt.Errorf("步骤 %s 只能手动完成", step.Key)
}

// applyFallbackOrigin 确保 SaaS 区里有那条橙云记录, 并把它设为回退源。
func applyFallbackOrigin(ctx context.Context, h model.Hostname, snap model.Snapshot) (string, error) {
	origin := findOrigin(h, model.OriginSaaSFallback)
	if origin == nil {
		return "", fmt.Errorf("这个域名没有绑定「SaaS 回退源」类型的回源, 先去回源配置里加一个")
	}
	if h.SaaSZone == "" {
		return "", fmt.Errorf("这个域名没有配置 SaaS 区")
	}

	cf, err := cloudflareClient(h.SaaSCredential())
	if err != nil {
		return "", err
	}
	zoneID, err := cf.ZoneIDByName(ctx, h.SaaSZone)
	if err != nil {
		return "", err
	}

	var actions []string
	existing, err := cf.ListRecords(ctx, zoneID, origin.Value, "")
	if err != nil {
		return "", err
	}
	if len(existing) == 0 {
		if origin.Address == "" {
			return "", fmt.Errorf("SaaS 区里还没有 %s 这条记录, 请先在回源配置里填上源站 IP, 或自己去 CF 建好这条橙云记录", origin.Value)
		}
		_, err = cf.CreateRecord(ctx, zoneID, cloudflare.NewRecord{
			Type:    "A",
			Name:    origin.Value,
			Content: origin.Address,
			Proxied: true,
			Comment: "splitdns 自动创建的回退源",
		})
		if err != nil {
			return "", err
		}
		actions = append(actions, fmt.Sprintf("建了橙云记录 %s → %s", origin.Value, origin.Address))
	} else if !existing[0].Proxied {
		return "", fmt.Errorf("%s 这条记录是灰云的, 回退源必须是橙云记录, 请先在 CF 里打开代理", origin.Value)
	}

	if !sameName(snap.FallbackOrigin, origin.Value) {
		if _, err := cf.SetFallbackOrigin(ctx, zoneID, origin.Value); err != nil {
			return "", err
		}
		actions = append(actions, fmt.Sprintf("已把回退源设为 %s", origin.Value))
	}
	if len(actions) == 0 {
		return "回退源已经是期望值, 无需改动", nil
	}
	return strings.Join(actions, "; ") + "。回退源状态转为有效通常要一分钟左右", nil
}

// applyCustomHostname 在 SaaS 区创建自定义主机名, 带上自定义源服务器 (如果配了)。
func applyCustomHostname(ctx context.Context, h model.Hostname) (string, error) {
	if h.SaaSZone == "" {
		return "", fmt.Errorf("这个域名没有配置 SaaS 区")
	}
	cf, err := cloudflareClient(h.SaaSCredential())
	if err != nil {
		return "", err
	}
	zoneID, err := cf.ZoneIDByName(ctx, h.SaaSZone)
	if err != nil {
		return "", err
	}
	var customOrigin, sni string
	if o := findOrigin(h, model.OriginSaaSCustom); o != nil {
		customOrigin, sni = o.Value, o.SNI
		if sni == "" {
			sni = o.Value
		}
	}

	// 已经存在时不重复创建, 但要把源服务器纠正到配置值 ——
	// 这个字段配在自定义主机名上而不是 DNS 里, 只能在这里改
	if existing, err := cf.FindCustomHostname(ctx, zoneID, h.Hostname); err == nil && existing != nil {
		if sameName(existing.CustomOriginServer, customOrigin) {
			return "自定义主机名已存在且源服务器正确, 无需改动", nil
		}
		if err := cf.UpdateCustomOrigin(ctx, zoneID, existing.ID, customOrigin, sni); err != nil {
			return "", err
		}
		if customOrigin == "" {
			return "已把源服务器改回默认回退源", nil
		}
		return fmt.Sprintf("已把源服务器改成 %s (SNI %s), 记得那台机器上要有这个 SNI 的 router", customOrigin, sni), nil
	}

	ch, err := cf.CreateCustomHostname(ctx, zoneID, h.Hostname, customOrigin, sni)
	if err != nil {
		return "", err
	}
	msg := fmt.Sprintf("已创建自定义主机名 %s (TXT 验证, 最低 TLS 1.2)", ch.Hostname)
	if customOrigin != "" {
		msg += fmt.Sprintf("; 源服务器 %s, 记得在那台机器上给这个 SNI 挂一个 router, 否则回源 403", customOrigin)
	}
	return msg + "。刷新后就能看到要写的验证 TXT", nil
}

// applyDNSPodZone 在 DNSPod 添加域名。
// 主域名不在这个腾讯云账号下时, DNSPod 要求先证明归属 —— 而它要的那条 TXT 恰好加在父区,
// 父区就在 CF 上且凭据我们有, 所以这一步能连着做完, 不用把人踢去两个面板之间来回跑。
func applyDNSPodZone(ctx context.Context, h model.Hostname) (string, error) {
	dp, err := dnspodClient(h.DNSPodCredentialID)
	if err != nil {
		return "", err
	}
	zone := h.DNSPodZone()

	err = dp.CreateDomain(ctx, zone)
	if errors.Is(err, dnspod.ErrNeedOwnershipTXT) {
		msg, verifyErr := verifyDNSPodOwnership(ctx, h, dp, zone)
		if verifyErr != nil {
			return "", verifyErr
		}
		return msg, nil
	}
	if err != nil {
		return "", err
	}
	d, err := dp.DescribeDomain(ctx, zone)
	if err != nil {
		return fmt.Sprintf("已添加域名 %s", zone), nil
	}
	return fmt.Sprintf("已添加域名 %s, 分配到的 NS: %s", zone, strings.Join(d.Nameservers, ", ")), nil
}

// verifyDNSPodOwnership 取 DNSPod 要求的归属验证 TXT, 写进父区, 再重试添加域名。
// DNSPod 读到这条记录有延迟, 重试失败不算错 —— 记录已经落地, 过一会儿再点一次就行。
func verifyDNSPodOwnership(ctx context.Context, h model.Hostname, dp *dnspod.Client, zone string) (string, error) {
	txt, err := dp.SubdomainOwnershipTXT(ctx, zone)
	if err != nil {
		return "", err
	}

	cf, err := cloudflareClient(h.CFCredentialID)
	if err != nil {
		return "", fmt.Errorf("要把验证 TXT 写进父区, 但 %w", err)
	}
	zoneID, err := cf.ZoneIDByName(ctx, txt.Domain)
	if err != nil {
		return "", fmt.Errorf("DNSPod 要求把验证 TXT 加在 %s 上, 但这份凭据看不到这个 zone: %w", txt.Domain, err)
	}

	// 已经有同名同值的记录就不重复建, 这一步可能被点很多次
	existing, err := cf.ListRecords(ctx, zoneID, txt.FQDN, "TXT")
	if err != nil {
		return "", err
	}
	found := false
	for _, r := range existing {
		if strings.Trim(r.Content, `"`) == txt.Value {
			found = true
			break
		}
	}
	if !found {
		_, err = cf.CreateRecord(ctx, zoneID, cloudflare.NewRecord{
			Type:    "TXT",
			Name:    txt.FQDN,
			Content: txt.Value,
			Comment: "splitdns: DNSPod 域名归属验证",
		})
		if err != nil {
			return "", fmt.Errorf("在 %s 写验证 TXT 失败: %w", txt.Domain, err)
		}
	}

	if err := dp.CreateDomain(ctx, zone); err != nil {
		return fmt.Sprintf(
			"已把归属验证 TXT %s = %s 写进 %s, 但 DNSPod 还没读到。等一两分钟再点一次这一步。",
			txt.FQDN, txt.Value, txt.Domain), nil
	}
	return fmt.Sprintf("已通过归属验证 (TXT 写在 %s) 并添加域名 %s", txt.Domain, zone), nil
}

// applyDCVRecords 把 CF 要求的全部验证 TXT 写进 DNSPod。
func applyDCVRecords(ctx context.Context, h model.Hostname, snap model.Snapshot) (string, error) {
	missing := missingTXT(h, snap)
	if len(missing) == 0 {
		return "验证 TXT 已经齐了, 无需改动", nil
	}
	dp, err := dnspodClient(h.DNSPodCredentialID)
	if err != nil {
		return "", err
	}
	zone := h.DNSPodZone()
	for _, req := range missing {
		err := dp.CreateRecord(ctx, zone, dnspod.NewRecord{
			SubDomain: relativeName(req.Name, zone),
			Type:      "TXT",
			Line:      defaultLine,
			Value:     req.Value,
		})
		if err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("已写入 %d 条验证 TXT, 等 CF 轮询到就会签发证书", len(missing)), nil
}

// applyRouteRecords 按线路配置把解析记录写进 DNSPod。
// 只补缺失的, 已存在但值不对的不自动改 —— 那是线上正在生效的解析, 覆盖前得让人看见。
func applyRouteRecords(ctx context.Context, h model.Hostname, snap model.Snapshot) (string, error) {
	dp, err := dnspodClient(h.DNSPodCredentialID)
	if err != nil {
		return "", err
	}
	zone := h.DNSPodZone()
	apex := apexRecords(snap.Records)

	var created, conflicts []string
	for _, route := range h.Routes {
		want, err := expectedValue(route, snap)
		if err != nil {
			return "", fmt.Errorf("线路 %s: %w", route.Line, err)
		}
		if rec, ok := apex[route.Line]; ok {
			if !sameName(rec.Value, want) {
				conflicts = append(conflicts, fmt.Sprintf("线路 %s 现在指向 %s, 期望 %s", route.Line, rec.Value, want))
			}
			continue
		}
		err = dp.CreateRecord(ctx, zone, dnspod.NewRecord{
			SubDomain: "@",
			Type:      recordTypeFor(want),
			Line:      route.Line,
			Value:     want,
		})
		if err != nil {
			return "", err
		}
		created = append(created, fmt.Sprintf("%s → %s", route.Line, want))
	}

	var msg []string
	if len(created) > 0 {
		msg = append(msg, "已创建: "+strings.Join(created, "; "))
	}
	if len(conflicts) > 0 {
		msg = append(msg, "以下线路已有记录且与配置不符, 未自动覆盖, 请自行确认: "+strings.Join(conflicts, "; "))
	}
	if len(msg) == 0 {
		return "线路记录已经齐了, 无需改动", nil
	}
	return strings.Join(msg, "。"), nil
}

// applyDelegation 在 CF 父区创建 NS 委派记录。
func applyDelegation(ctx context.Context, h model.Hostname, snap model.Snapshot) (string, error) {
	if len(snap.DNSPodNameservers) == 0 {
		return "", fmt.Errorf("还没拿到 DNSPod 分配的 NS, 先完成添加域名那一步")
	}
	cf, err := cloudflareClient(h.CFCredentialID)
	if err != nil {
		return "", err
	}
	zoneID, err := cf.ZoneIDByName(ctx, h.ParentZone)
	if err != nil {
		return "", err
	}

	have := map[string]bool{}
	for _, ns := range snap.Delegation {
		have[normalizeName(ns)] = true
	}

	var created []string
	for _, ns := range snap.DNSPodNameservers {
		if have[normalizeName(ns)] {
			continue
		}
		_, err := cf.CreateRecord(ctx, zoneID, cloudflare.NewRecord{
			Type:    "NS",
			Name:    h.Hostname,
			Content: ns,
			Comment: "splitdns 委派",
		})
		if err != nil {
			return "", err
		}
		created = append(created, ns)
	}
	if len(created) == 0 {
		return "委派记录已经齐了, 无需改动", nil
	}
	return fmt.Sprintf("已加委派 NS: %s。父区其它记录没有任何改动", strings.Join(created, ", ")), nil
}

// applyCleanup 删除父区里被委派遮蔽的记录。
// 这是本流程里唯一不可逆的操作, 未确认时只返回待删清单, 不动手。
func applyCleanup(ctx context.Context, h model.Hostname, confirm bool) (string, error) {
	cf, err := cloudflareClient(h.CFCredentialID)
	if err != nil {
		return "", err
	}
	zoneID, err := cf.ZoneIDByName(ctx, h.ParentZone)
	if err != nil {
		return "", err
	}
	shadowed, err := cf.ShadowedRecords(ctx, zoneID, h.Hostname)
	if err != nil {
		return "", err
	}
	if len(shadowed) == 0 {
		return "父区没有被遮蔽的记录, 无需清理", nil
	}

	list := make([]string, 0, len(shadowed))
	for _, r := range shadowed {
		list = append(list, fmt.Sprintf("%s %s → %s", r.Name, r.Type, r.Content))
	}
	if !confirm {
		return "", fmt.Errorf("%w: 将从 %s 删除以下 %d 条记录\n  %s",
			ErrNeedConfirm, h.ParentZone, len(shadowed), strings.Join(list, "\n  "))
	}

	for _, r := range shadowed {
		if err := cf.DeleteRecord(ctx, zoneID, r.ID); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("已删除 %d 条被遮蔽的记录", len(shadowed)), nil
}

// findOrigin 在该域名的线路里找出指定类型的落点, 引用回源与内联填值一视同仁。
func findOrigin(h model.Hostname, kind string) *model.Origin {
	for _, r := range h.Routes {
		if t := r.Target(); t != nil && t.Kind == kind {
			return t
		}
	}
	return nil
}
