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

// 本工具写进 CF 的记录都带这个备注前缀。
// 记录的名字和类型各不相同, 拆除时只有备注能稳定认出"这条是我建的",
// 因此下面几个备注文案必须都以 cfCommentPrefix 开头。
const (
	cfCommentPrefix   = "splitdns"
	commentOwnership  = cfCommentPrefix + ": DNSPod 域名归属验证"
	commentDelegation = cfCommentPrefix + " 委派"
)

// ApplyStep 让程序替用户执行某一步。执行完不直接判成功, 仍然走一次验证 ——
// 平台接口返回 200 不等于配置已经生效, 生效与否只认巡检结果。
// action 目前只有 cf.cleanup 用到: 传 "migrate" 表示先把还在服务的记录搬到 DNSPod 再删。
func ApplyStep(ctx context.Context, planID, stepID uint, confirm bool, action string) (string, error) {
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

	// 拆除步骤全部按删除时的实时状态动手 (要拿记录 ID), 不吃巡检快照, 省一轮拉取
	if strings.HasPrefix(step.Key, teardownPrefix) {
		return applyTeardownStep(ctx, step.Key, *h, confirm)
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
		return applyRouteRecords(ctx, *h, snap, confirm)
	case "cf.delegation":
		return applyDelegation(ctx, *h, snap)
	case "cf.cleanup":
		return applyCleanup(ctx, *h, confirm, action == "migrate")
	case "saas.cert":
		return "", fmt.Errorf("这一步只需要等待, CF 会自己签发证书")
	}
	return "", fmt.Errorf("步骤 %s 只能手动完成", step.Key)
}

// applyFallbackOrigin 确保 SaaS 区里有那条橙云记录, 并把它设为回退源。
// 多条线路都是 saas_fallback 类型时优先认「默认」线那条 —— 优选场景下别的线可能挂着
// 优选域名, 它不在本区, 拿它当回退源会把整个区的回源打断。
func applyFallbackOrigin(ctx context.Context, h model.Hostname, snap model.Snapshot) (string, error) {
	origin := fallbackOriginTarget(h)
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

	// 回退源与自定义源服务器对那条橙云记录的要求完全一样, 建 / 校验都走同一处
	var actions []string
	created, err := ensureOriginRecord(ctx, cf, zoneID, h.SaaSZone, *origin)
	if err != nil {
		return "", err
	}
	if created != "" {
		actions = append(actions, created)
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
		// 归属验证 TXT 要写在当前的权威 DNS 上。委派模式那就是 CF 父区, 凭据在手能代做;
		// 直托模式没有父区可写, 只能明确告知, 不能拿空凭据去撞出一个费解的错误
		if !h.Delegated() {
			return "", fmt.Errorf(
				"DNSPod 要求先验证 %s 的归属 (通常是它已被别的账号添加)。"+
					"请到当前能改这个域名解析的地方按 DNSPod 提示加验证 TXT, 或在 DNSPod 控制台完成找回, 再回来重试",
				zone)
		}
		msg, verifyErr := verifyDNSPodOwnership(ctx, h, dp, zone)
		if verifyErr != nil {
			return "", verifyErr
		}
		return msg, nil
	}
	if err != nil {
		return "", err
	}
	// DNSPod 新加的域名默认是暂停状态, 记录配得再对也不生效, 顺手开掉
	if err := dp.EnableDomain(ctx, zone); err != nil {
		return "", err
	}
	d, err := dp.DescribeDomain(ctx, zone)
	if err != nil {
		return fmt.Sprintf("已添加域名 %s 并启用解析", zone), nil
	}
	return fmt.Sprintf("已添加域名 %s 并启用解析, 分配到的 NS: %s", zone, strings.Join(d.Nameservers, ", ")), nil
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
			Comment: commentOwnership,
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
	if err := dp.EnableDomain(ctx, zone); err != nil {
		return "", err
	}
	return fmt.Sprintf("已通过归属验证 (TXT 写在 %s), 添加域名 %s 并启用解析", txt.Domain, zone), nil
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
// 缺失的直接补; 已存在但值不对的是线上正在生效的解析, 未确认时只退回差异清单 (ErrNeedConfirm),
// 带确认再来一次才就地覆盖成期望值。
func applyRouteRecords(ctx context.Context, h model.Hostname, snap model.Snapshot, confirm bool) (string, error) {
	dp, err := dnspodClient(h.DNSPodCredentialID)
	if err != nil {
		return "", err
	}
	zone := h.DNSPodZone()
	recordName := routeRecordName(h)
	apex := routeRecords(snap.Records, recordName)

	type mismatch struct{ line, have, want string }
	var created []string
	var conflicts []mismatch
	for _, route := range h.Routes {
		want, err := expectedValue(route, snap)
		if err != nil {
			return "", fmt.Errorf("线路 %s: %w", route.Line, err)
		}
		if rec, ok := apex[route.Line]; ok {
			if !sameName(rec.Value, want) {
				conflicts = append(conflicts, mismatch{line: route.Line, have: rec.Value, want: want})
			}
			continue
		}
		err = dp.CreateRecord(ctx, zone, dnspod.NewRecord{
			SubDomain: recordName,
			Type:      recordTypeFor(want),
			Line:      route.Line,
			Value:     want,
		})
		if err != nil {
			return "", err
		}
		created = append(created, fmt.Sprintf("%s → %s", route.Line, want))
	}

	// 补缺失不需要确认, 也不该被冲突拦住, 所以上面已经先补完了;
	// 覆盖动的是线上正在生效的解析, 未确认时逐条列出差异, 一条不动
	if len(conflicts) > 0 && !confirm {
		header := "以下线路已有记录且与配置不符, 确认后会就地覆盖成期望值 (旧值会丢)"
		if len(created) > 0 {
			header = fmt.Sprintf("缺失的 %d 条已先补上。%s", len(created), header)
		}
		lines := make([]string, 0, len(conflicts))
		for _, c := range conflicts {
			lines = append(lines, fmt.Sprintf("线路「%s」: %s → %s", c.line, c.have, c.want))
		}
		return "", needConfirm(header, lines)
	}

	var overwritten []string
	if len(conflicts) > 0 {
		// 快照里没有记录 ID, 覆盖只能按动手时的实时状态定位, 与拆除步骤不吃快照是同一个理由
		live, err := dp.ListRecords(ctx, zone)
		if err != nil {
			return "", err
		}
		for _, c := range conflicts {
			targets := overwriteTargets(live, recordName, c.line, c.want)
			switch len(targets) {
			case 0:
				// 确认的间隙里已经被改对或删掉了, 是否生效交给巡检核实
				continue
			case 1:
				rec := targets[0]
				err := dp.ModifyRecord(ctx, zone, rec.ID, dnspod.NewRecord{
					SubDomain: recordName,
					Type:      recordTypeFor(c.want),
					Line:      c.line,
					Value:     c.want,
					TTL:       rec.TTL,
				})
				if err != nil {
					return "", err
				}
				overwritten = append(overwritten, fmt.Sprintf("%s: %s → %s", c.line, rec.Value, c.want))
			default:
				return "", fmt.Errorf("线路「%s」下有 %d 条落点记录, 说不清该覆盖哪条, 请到 DNSPod 里人工处理", c.line, len(targets))
			}
		}
	}

	var msg []string
	if len(created) > 0 {
		msg = append(msg, "已创建: "+strings.Join(created, "; "))
	}
	if len(overwritten) > 0 {
		msg = append(msg, "已覆盖: "+strings.Join(overwritten, "; "))
	}
	if len(msg) == 0 {
		return "线路记录已经齐了, 无需改动", nil
	}
	return strings.Join(msg, "。"), nil
}

// overwriteTargets 在实时记录里找出某线路下待覆盖的落点记录 (值不等于期望的 A / AAAA / CNAME)。
// 快照按线路只留一条, 实时状态可能不止: 同线路多条都不符时说不清该改哪条, 由调用方拒绝执行。
func overwriteTargets(records []dnspod.Record, name, line, want string) []dnspod.Record {
	var out []dnspod.Record
	for _, r := range records {
		if !sameName(r.Name, name) || r.Line != line {
			continue
		}
		if r.Type != "CNAME" && r.Type != "A" && r.Type != "AAAA" {
			continue
		}
		if sameName(r.Value, want) {
			continue
		}
		out = append(out, r)
	}
	return out
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
			Comment: commentDelegation,
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

// applyCleanup 处理父区里被委派遮蔽的记录。
//
// migrate 为真时先把还在服务的记录搬到 DNSPod 再删 —— 被遮蔽不等于该扔,
// 这些记录本来在正常工作, 只是委派之后待错了地方, 直接删会把线上打断。
// 这是本流程里唯一不可逆的操作, 未确认时只返回清单, 不动手。
func applyCleanup(ctx context.Context, h model.Hostname, confirm, migrate bool) (string, error) {
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

	if !migrate {
		if !confirm {
			list := make([]string, 0, len(shadowed))
			for _, r := range shadowed {
				list = append(list, fmt.Sprintf("%s %s → %s", r.Name, r.Type, r.Content))
			}
			return "", fmt.Errorf("%w: 将从 %s 直接删除以下 %d 条记录\n  %s\n\n"+
				"其中如果有还在服务的记录, 请改用「先迁移到 DNSPod 再删」",
				ErrNeedConfirm, h.ParentZone, len(shadowed), strings.Join(list, "\n  "))
		}
		return deleteShadowed(ctx, cf, zoneID, shadowed)
	}

	dp, err := dnspodClient(h.DNSPodCredentialID)
	if err != nil {
		return "", err
	}
	snap := Inspect(ctx, h).Snapshot
	plans := planShadowedMigration(h, shadowed, snap.Records)

	if !confirm {
		return "", fmt.Errorf("%w: 将按下面的方式处理 %s 里这 %d 条被遮蔽的记录, 搬完即从父区删除\n%s",
			ErrNeedConfirm, h.ParentZone, len(shadowed), describeShadowedPlan(plans, h.DNSPodZone()))
	}

	moved, err := migrateShadowedRecords(ctx, dp, h.DNSPodZone(), plans)
	if err != nil {
		// 搬到一半失败就停手, 父区的记录一条都不动 —— 已经搬过去的是幂等的, 重试不会重复建
		return "", fmt.Errorf("%w (已搬 %d 条, 父区未做任何删除)", err, moved)
	}
	msg, err := deleteShadowed(ctx, cf, zoneID, shadowed)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("已搬 %d 条到 %s, %s", moved, h.DNSPodZone(), msg), nil
}

// deleteShadowed 从父区删掉这批记录。
func deleteShadowed(ctx context.Context, cf *cloudflare.Client, zoneID string, shadowed []cloudflare.DNSRecord) (string, error) {
	for _, r := range shadowed {
		if err := cf.DeleteRecord(ctx, zoneID, r.ID); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("已从父区删除 %d 条被遮蔽的记录", len(shadowed)), nil
}

// needConfirm 把待删清单包装成需要二次确认的错误。
// 清单必须原样回给用户: 删除都是不可逆的, 只报"有 N 条"等于让人闭眼点确认。
func needConfirm(header string, lines []string) error {
	if len(lines) == 0 {
		return fmt.Errorf("%w: %s", ErrNeedConfirm, header)
	}
	return fmt.Errorf("%w: %s\n  %s", ErrNeedConfirm, header, strings.Join(lines, "\n  "))
}

// fallbackOriginTarget 找出该域名声明的「SaaS 回退源」落点, 默认线优先 ——
// 优选场景下别的线挂的是优选域名, 拿它当回退源会把整个区的回源打断。
// 设置 / 巡检 / 验证三处对"期望的回退源"必须是同一个答案, 都走这里。
func fallbackOriginTarget(h model.Hostname) *model.Origin {
	if o := findLineOrigin(h, defaultLine, model.OriginSaaSFallback); o != nil {
		return o
	}
	return findOrigin(h, model.OriginSaaSFallback)
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

// findLineOrigin 找指定线路上指定类型的落点, 没有则返回 nil。
func findLineOrigin(h model.Hostname, line, kind string) *model.Origin {
	for _, r := range h.Routes {
		if r.Line != line {
			continue
		}
		if t := r.Target(); t != nil && t.Kind == kind {
			return t
		}
	}
	return nil
}
