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

// applyTeardownStep 让程序执行拆除流程里的某一步。
// 每一步都是不可逆删除, 未确认时一律只返回待删清单 (ErrNeedConfirm), 一条都不动;
// 要删的东西本来就不在时当成功, 拆到一半重来不会被卡住。
func applyTeardownStep(ctx context.Context, key string, h model.Hostname, confirm bool) (string, error) {
	switch key {
	case "teardown.cf_delegation":
		return teardownDelegation(ctx, h, confirm)
	case "teardown.dnspod_records":
		return teardownDNSPodRecords(ctx, h, confirm)
	case "teardown.dnspod_zone":
		return teardownDNSPodZone(ctx, h, confirm)
	case "teardown.saas_custom_hostname":
		return teardownCustomHostname(ctx, h, confirm)
	case "teardown.saas_fallback":
		return teardownFallbackOrigin(ctx, h, confirm)
	case "teardown.cf_leftovers":
		return teardownParentLeftovers(ctx, h, confirm)
	}
	return "", fmt.Errorf("拆除步骤 %s 只能手动完成", key)
}

// teardownDelegation 删掉父区里指向 DNSPod 的 NS 委派, 解析随即停止。
func teardownDelegation(ctx context.Context, h model.Hostname, confirm bool) (string, error) {
	cf, err := cloudflareClient(h.CFCredentialID)
	if err != nil {
		return "", err
	}
	zoneID, err := cf.ZoneIDByName(ctx, h.ParentZone)
	if err != nil {
		return "", err
	}
	records, err := cf.ListRecords(ctx, zoneID, h.Hostname, "NS")
	if err != nil {
		return "", err
	}
	if len(records) == 0 {
		return "父区已经没有委派记录了", nil
	}

	if !confirm {
		return "", needConfirm(
			fmt.Sprintf("将从 %s 删除以下委派记录, 删完 %s 立刻停止解析", h.ParentZone, h.Hostname),
			cfRecordLines(records))
	}
	for _, r := range records {
		if err := cf.DeleteRecord(ctx, zoneID, r.ID); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("已删掉 %d 条委派 NS, %s 不再走 DNSPod。父区其它记录没有任何改动", len(records), h.Hostname), nil
}

// teardownDNSPodRecords 删掉 DNSPod 上由本工具维护的记录, 其余记录原样保留。
func teardownDNSPodRecords(ctx context.Context, h model.Hostname, confirm bool) (string, error) {
	dp, err := dnspodClient(h.DNSPodCredentialID)
	if err != nil {
		return "", err
	}
	zone := h.DNSPodZone()

	records, err := dp.ListRecords(ctx, zone)
	if err != nil {
		if errors.Is(err, dnspod.ErrDomainNotFound) {
			return "DNSPod 上已经没有这个域名了, 记录跟着一起没了", nil
		}
		return "", err
	}

	var doomed []dnspod.Record
	var kept []string
	for _, r := range records {
		if isManagedRecord(h, r.Name, r.Line, r.Type) {
			doomed = append(doomed, r)
			continue
		}
		kept = append(kept, describeDNSPodRecord(r)+"  ← 不是本工具建的, 保留")
	}

	if len(doomed) == 0 {
		if len(kept) > 0 {
			return fmt.Sprintf("本工具维护的记录已经清空, %s 下另有 %d 条别的记录, 原样保留", zone, len(kept)), nil
		}
		return fmt.Sprintf("%s 下已经没有解析记录了", zone), nil
	}

	if !confirm {
		lines := make([]string, 0, len(doomed)+len(kept))
		for _, r := range doomed {
			lines = append(lines, describeDNSPodRecord(r))
		}
		lines = append(lines, kept...)
		return "", needConfirm(
			fmt.Sprintf("将从 DNSPod 的 %s 删除以下 %d 条本工具维护的记录", zone, len(doomed)), lines)
	}

	for _, r := range doomed {
		if err := dp.DeleteRecord(ctx, zone, r.ID); err != nil {
			return "", err
		}
	}
	msg := fmt.Sprintf("已删掉 %d 条线路 / 验证记录", len(doomed))
	if len(kept) > 0 {
		msg += fmt.Sprintf(", 另外 %d 条不是本工具建的, 原样保留", len(kept))
	}
	return msg, nil
}

// teardownDNSPodZone 从 DNSPod 删掉整个域名。
func teardownDNSPodZone(ctx context.Context, h model.Hostname, confirm bool) (string, error) {
	dp, err := dnspodClient(h.DNSPodCredentialID)
	if err != nil {
		return "", err
	}
	zone := h.DNSPodZone()

	if _, err := dp.DescribeDomain(ctx, zone); err != nil {
		if errors.Is(err, dnspod.ErrDomainNotFound) {
			return "DNSPod 上已经没有这个域名了", nil
		}
		return "", err
	}

	if !confirm {
		return "", needConfirm(
			fmt.Sprintf("将从 DNSPod 删除域名 %s, 它下面的记录会一起消失", zone),
			remainingRecordLines(ctx, dp, zone))
	}
	if err := dp.DeleteDomain(ctx, zone); err != nil {
		return "", err
	}
	return fmt.Sprintf("已从 DNSPod 删除域名 %s", zone), nil
}

// remainingRecordLines 列出域名下现存的记录, 供删域名前的确认清单展示。
// 拉不到不阻断确认 —— 域名删除本身不依赖这份清单, 但要如实说明没拿到。
func remainingRecordLines(ctx context.Context, dp *dnspod.Client, zone string) []string {
	records, err := dp.ListRecords(ctx, zone)
	if err != nil {
		return []string{"(没能读到现存记录: " + err.Error() + ")"}
	}
	if len(records) == 0 {
		return []string{"(域名下已经没有解析记录)"}
	}
	lines := make([]string, 0, len(records))
	for _, r := range records {
		lines = append(lines, describeDNSPodRecord(r))
	}
	return lines
}

// teardownCustomHostname 删掉 SaaS 区里的自定义主机名, 证书由 CF 一并吊销;
// 本工具专为它建的那条落点记录一并清掉。
func teardownCustomHostname(ctx context.Context, h model.Hostname, confirm bool) (string, error) {
	cf, zoneID, err := saasZone(ctx, h)
	if err != nil {
		return "", err
	}
	existing, err := cf.FindCustomHostname(ctx, zoneID, h.Hostname)
	if err != nil {
		return "", err
	}
	if existing == nil {
		return "SaaS 区里已经没有这个自定义主机名了", nil
	}
	doomed, err := customOriginLeftovers(ctx, cf, zoneID, existing)
	if err != nil {
		return "", err
	}

	if !confirm {
		lines := []string{fmt.Sprintf("自定义主机名 %s (主机名状态 %s / 证书状态 %s)",
			existing.Hostname, orNone(existing.Status), orNone(existing.SSL.Status))}
		return "", needConfirm(
			fmt.Sprintf("将从 %s 删除自定义主机名, 证书会一并吊销", h.SaaSZone),
			append(lines, cfRecordLines(doomed)...))
	}
	if err := cf.DeleteCustomHostname(ctx, zoneID, existing.ID); err != nil {
		return "", err
	}
	for _, r := range doomed {
		if err := cf.DeleteRecord(ctx, zoneID, r.ID); err != nil {
			return "", fmt.Errorf("自定义主机名已删掉, 但删记录 %s 失败: %w", r.Name, err)
		}
	}
	msg := fmt.Sprintf("已删掉自定义主机名 %s, 证书一并吊销", h.Hostname)
	if len(doomed) > 0 {
		msg += fmt.Sprintf(", 并删掉本工具为它建的 %d 条落点记录", len(doomed))
	}
	return msg, nil
}

// customOriginLeftovers 挑出"只为这个自定义主机名建的"落点记录。
//
// 三道闸全过才算数: 得是本工具建的 (认备注前缀), 区里不能有别的自定义主机名还指着同一个源服务器,
// 也不能是该区的回退源。后两者删掉会连累别人, 宁可留着让人自己判断 —— 少删一条只是残留,
// 多删一条是别人的线上流量。任何一路读不到就直接不删: 拉不到 ≠ 没有。
func customOriginLeftovers(ctx context.Context, cf *cloudflare.Client, zoneID string, ch *cloudflare.CustomHostname) ([]cloudflare.DNSRecord, error) {
	if ch.CustomOriginServer == "" {
		return nil, nil
	}
	hosts, err := cf.ListCustomHostnames(ctx, zoneID)
	if err != nil {
		return nil, err
	}
	for _, other := range hosts {
		if other.ID != ch.ID && sameName(other.CustomOriginServer, ch.CustomOriginServer) {
			return nil, nil
		}
	}
	fo, err := cf.GetFallbackOrigin(ctx, zoneID)
	if err != nil || sameName(fo.Origin, ch.CustomOriginServer) {
		return nil, nil
	}

	mine, err := cf.RecordsByComment(ctx, zoneID, cfCommentPrefix)
	if err != nil {
		return nil, err
	}
	var out []cloudflare.DNSRecord
	for _, r := range mine {
		if sameName(r.Name, ch.CustomOriginServer) {
			out = append(out, r)
		}
	}
	return out, nil
}

// teardownFallbackOrigin 清掉 SaaS 区的回退源, 顺带删掉当初由本工具建的那条橙云记录。
//
// 回退源是整个区共享的: 区里还有别的自定义主机名时删掉它, 那些主机名会全部失效。
// 所以动手前先数一遍, 有别人就直接拒绝, 让用户跳过这一步而不是"确认一下就删"。
func teardownFallbackOrigin(ctx context.Context, h model.Hostname, confirm bool) (string, error) {
	cf, zoneID, err := saasZone(ctx, h)
	if err != nil {
		return "", err
	}
	fo, err := cf.GetFallbackOrigin(ctx, zoneID)
	if err != nil {
		return "", err
	}
	if fo.Origin == "" {
		return h.SaaSZone + " 已经没有设置回退源了", nil
	}

	hosts, err := cf.ListCustomHostnames(ctx, zoneID)
	if err != nil {
		return "", err
	}
	var others []string
	for _, ch := range hosts {
		if sameName(ch.Hostname, h.Hostname) {
			continue
		}
		others = append(others, ch.Hostname)
	}
	if len(others) > 0 {
		return "", fmt.Errorf("%s 里还有 %d 个自定义主机名在用这个回退源 (%s), 删了它们会全部失效。这一步跳过即可",
			h.SaaSZone, len(others), summarizeNames(others, 3))
	}

	// 只删本工具建的那条橙云记录: 用户自己建的可能还挂着别的用途
	mine, err := cf.RecordsByComment(ctx, zoneID, cfCommentPrefix)
	if err != nil {
		return "", err
	}
	var doomed []cloudflare.DNSRecord
	for _, r := range mine {
		if sameName(r.Name, fo.Origin) {
			doomed = append(doomed, r)
		}
	}

	if !confirm {
		lines := append([]string{"回退源设置 → " + fo.Origin}, cfRecordLines(doomed)...)
		return "", needConfirm(
			fmt.Sprintf("将清掉 %s 的回退源, 该区已经没有别的自定义主机名", h.SaaSZone), lines)
	}

	// 先撤设置再删记录: 记录还被引用为回退源时 CF 可能拒绝删除
	if err := cf.DeleteFallbackOrigin(ctx, zoneID); err != nil {
		return "", err
	}
	for _, r := range doomed {
		if err := cf.DeleteRecord(ctx, zoneID, r.ID); err != nil {
			return "", fmt.Errorf("回退源设置已清掉, 但删记录 %s 失败: %w", r.Name, err)
		}
	}
	if len(doomed) == 0 {
		return fmt.Sprintf("已清掉 %s 的回退源设置; %s 这条记录不是本工具建的, 没有动", h.SaaSZone, fo.Origin), nil
	}
	return fmt.Sprintf("已清掉 %s 的回退源设置, 并删掉本工具建的 %d 条记录", h.SaaSZone, len(doomed)), nil
}

// teardownParentLeftovers 删掉父区里由本工具写下、现在已经没用的辅助记录。
func teardownParentLeftovers(ctx context.Context, h model.Hostname, confirm bool) (string, error) {
	cf, err := cloudflareClient(h.CFCredentialID)
	if err != nil {
		return "", err
	}
	zoneID, err := cf.ZoneIDByName(ctx, h.ParentZone)
	if err != nil {
		return "", err
	}
	records, err := cf.RecordsByComment(ctx, zoneID, cfCommentPrefix)
	if err != nil {
		return "", err
	}
	// 委派 NS 归上一步管, 混进来会让同一条记录被两个步骤各删一次
	records = parentLeftovers(records, h.Hostname)
	if len(records) == 0 {
		return "父区没有本工具留下的记录", nil
	}

	if !confirm {
		return "", needConfirm(
			fmt.Sprintf("将从 %s 删除以下由本工具写下的记录", h.ParentZone), cfRecordLines(records))
	}
	for _, r := range records {
		if err := cf.DeleteRecord(ctx, zoneID, r.ID); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("已从父区删除 %d 条本工具留下的记录", len(records)), nil
}

// saasZone 取该域名 SaaS 区的客户端与 zone ID, 没配 SaaS 区时给出明确错误。
func saasZone(ctx context.Context, h model.Hostname) (*cloudflare.Client, string, error) {
	if h.SaaSZone == "" {
		return nil, "", fmt.Errorf("这个域名没有配置 SaaS 区")
	}
	cf, err := cloudflareClient(h.SaaSCredential())
	if err != nil {
		return nil, "", err
	}
	zoneID, err := cf.ZoneIDByName(ctx, h.SaaSZone)
	if err != nil {
		return nil, "", err
	}
	return cf, zoneID, nil
}

// cfRecordLines 把 CF 记录渲染成确认清单里的行。
func cfRecordLines(records []cloudflare.DNSRecord) []string {
	lines := make([]string, 0, len(records))
	for _, r := range records {
		lines = append(lines, describeCFRecord(r))
	}
	return lines
}

// describeDNSPodRecord 把一条 DNSPod 记录渲染成给人核对的一行, 线路一并带上 —— 同名记录靠线路区分。
func describeDNSPodRecord(r dnspod.Record) string {
	return fmt.Sprintf("%s %s [%s] → %s", r.Name, r.Type, r.Line, r.Value)
}

// summarizeNames 列出前几个名字, 多余的折成"等 N 个", 免得错误信息被刷屏。
func summarizeNames(names []string, max int) string {
	if len(names) <= max {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s 等 %d 个", strings.Join(names[:max], ", "), len(names))
}
