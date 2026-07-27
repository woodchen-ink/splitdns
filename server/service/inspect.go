package service

import (
	"context"
	"fmt"

	"github.com/woodchen-ink/go-web-utils/timex"
	"github.com/woodchen-ink/splitdns/server/model"
	"github.com/woodchen-ink/splitdns/server/pkg/cloudflare"
)

// Inspect 巡检单个访问域名: 拉取三个平台的实际状态, 再交给规则层判定。
// 任何一处拉取失败都转成 Finding 而不是直接返回错误 —— 面板要展示"其它部分是好的",
// 一个平台挂了不该让整个域名的报告消失。
func Inspect(ctx context.Context, h model.Hostname) model.HostnameReport {
	snap := model.Snapshot{}
	var fetchFindings []model.Finding

	fetchFindings = append(fetchFindings, inspectParentZone(ctx, h, &snap)...)
	fetchFindings = append(fetchFindings, inspectSaaSZone(ctx, h, &snap)...)
	fetchFindings = append(fetchFindings, inspectDNSPod(ctx, h, &snap)...)

	findings := append(fetchFindings, evaluate(h, snap)...)

	report := model.HostnameReport{
		HostnameID: h.ID,
		Hostname:   h.Hostname,
		Snapshot:   snap,
		Findings:   findings,
		CheckedAt:  timex.FormatRFC3339(timex.Now()),
	}
	report.Level, report.Summary = summarize(findings)
	return report
}

// inspectParentZone 读 CF 父区: 委派 NS 记录, 以及被该委派遮蔽的记录。
func inspectParentZone(ctx context.Context, h model.Hostname, snap *model.Snapshot) []model.Finding {
	cf, err := cloudflareClient(h.CFCredentialID)
	if err != nil {
		return []model.Finding{fetchFailed("fetch.parent_credential", "父区凭据不可用", err)}
	}
	zoneID, err := cf.ZoneIDByName(ctx, h.ParentZone)
	if err != nil {
		return []model.Finding{fetchFailed("fetch.parent_zone", "读取父区失败", err)}
	}

	nsRecords, err := cf.ListRecords(ctx, zoneID, h.Hostname, "NS")
	if err != nil {
		return []model.Finding{fetchFailed("fetch.delegation", "读取委派记录失败", err)}
	}
	for _, r := range nsRecords {
		snap.Delegation = append(snap.Delegation, normalizeName(r.Content))
	}

	shadowed, err := cf.ShadowedRecords(ctx, zoneID, h.Hostname)
	if err != nil {
		// 遮蔽元数据是增强信息, 拉不到只降级提示, 不影响其它检查
		return []model.Finding{{
			Level:  model.LevelWarn,
			Code:   "fetch.shadowed",
			Title:  "无法读取被遮蔽记录",
			Detail: err.Error(),
			Fix:    "确认 API Token 有该 zone 的 DNS 读取权限",
		}}
	}
	for _, r := range shadowed {
		snap.ShadowedRecords = append(snap.ShadowedRecords, fmt.Sprintf("%s %s", r.Name, r.Type))
	}
	return nil
}

// inspectSaaSZone 读 CF SaaS 区: 回退源与自定义主机名状态。
// 未配置 SaaS 区的域名直接跳过, 这类域名可能是纯第三方 CDN 分流。
func inspectSaaSZone(ctx context.Context, h model.Hostname, snap *model.Snapshot) []model.Finding {
	if h.SaaSZone == "" {
		return nil
	}
	cf, err := cloudflareClient(h.SaaSCredential())
	if err != nil {
		return []model.Finding{fetchFailed("fetch.saas_credential", "SaaS 区凭据不可用", err)}
	}
	zoneID, err := cf.ZoneIDByName(ctx, h.SaaSZone)
	if err != nil {
		return []model.Finding{fetchFailed("fetch.saas_zone", "读取 SaaS 区失败", err)}
	}

	if fo, err := cf.GetFallbackOrigin(ctx, zoneID); err == nil {
		snap.FallbackOrigin = fo.Origin
		snap.FallbackOriginStatus = fo.Status
	} else {
		return []model.Finding{fetchFailed("fetch.fallback_origin", "读取回退源失败", err)}
	}

	ch, err := cf.FindCustomHostname(ctx, zoneID, h.Hostname)
	if err != nil {
		return []model.Finding{fetchFailed("fetch.custom_hostname", "读取自定义主机名失败", err)}
	}
	if ch != nil {
		snap.CustomHostname = mapCustomHostname(ch)
	}
	return nil
}

// mapCustomHostname 把 CF 的原始响应收敛成前端可直接渲染的状态结构。
func mapCustomHostname(ch *cloudflare.CustomHostname) model.CustomHostnameState {
	state := model.CustomHostnameState{
		Exists:           true,
		Status:           ch.Status,
		SSLStatus:        ch.SSL.Status,
		ValidationMethod: ch.SSL.Method,
		CertAuthority:    ch.SSL.CertificateAuthority,
		CustomOrigin:     ch.CustomOriginServer,
		CustomOriginSNI:  ch.CustomOriginSNI,
		MinTLSVersion:    ch.SSL.Settings.MinTLSVersion,
		OwnershipTXT: model.TXTRequirement{
			Name:  ch.OwnershipVerification.Name,
			Value: ch.OwnershipVerification.Value,
		},
	}
	if len(ch.SSL.Certificates) > 0 {
		state.CertExpiresAt = ch.SSL.Certificates[0].ExpiresOn
	}
	for _, vr := range ch.SSL.ValidationRecords {
		if vr.TxtName == "" {
			continue
		}
		state.DCVTXT = append(state.DCVTXT, model.TXTRequirement{Name: vr.TxtName, Value: vr.TxtValue})
	}
	return state
}

// inspectDNSPod 读 DNSPod: 域名分配到的 NS 与全部解析记录。
func inspectDNSPod(ctx context.Context, h model.Hostname, snap *model.Snapshot) []model.Finding {
	dp, err := dnspodClient(h.DNSPodCredentialID)
	if err != nil {
		return []model.Finding{fetchFailed("fetch.dnspod_credential", "DNSPod 凭据不可用", err)}
	}
	zone := h.DNSPodZone()

	if d, err := dp.DescribeDomain(ctx, zone); err == nil {
		snap.DNSPodNameservers = d.Nameservers
		snap.DNSPodEnabled = d.Enabled
	} else {
		return []model.Finding{fetchFailed("fetch.dnspod_domain", "读取 DNSPod 域名失败", err)}
	}

	records, err := dp.ListRecords(ctx, zone)
	if err != nil {
		return []model.Finding{fetchFailed("fetch.dnspod_records", "读取 DNSPod 解析记录失败", err)}
	}
	for _, r := range records {
		snap.Records = append(snap.Records, model.DNSRecord{
			Name:    r.Name,
			Type:    r.Type,
			Line:    r.Line,
			Value:   r.Value,
			TTL:     r.TTL,
			Enabled: r.Enabled,
		})
	}
	return nil
}

// fetchFailed 把一次拉取失败包装成统一形态的 Finding。
func fetchFailed(code, title string, err error) model.Finding {
	return model.Finding{
		Level:  model.LevelError,
		Code:   code,
		Title:  title,
		Detail: err.Error(),
		Fix:    "检查凭据权限与域名归属, 修好后重新巡检",
	}
}

// summarize 聚合出该域名的最高级别与一句话总结。
func summarize(findings []model.Finding) (model.Level, string) {
	level := model.LevelOK
	errCount, warnCount := 0, 0
	for _, f := range findings {
		if f.Level.Rank() > level.Rank() {
			level = f.Level
		}
		switch f.Level {
		case model.LevelError:
			errCount++
		case model.LevelWarn:
			warnCount++
		}
	}
	switch {
	case errCount > 0:
		return level, fmt.Sprintf("%d 项错误, %d 项提醒", errCount, warnCount)
	case warnCount > 0:
		return level, fmt.Sprintf("%d 项提醒", warnCount)
	default:
		return level, "配置完整"
	}
}
