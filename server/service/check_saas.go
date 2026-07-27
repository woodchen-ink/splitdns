package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/woodchen-ink/go-web-utils/timex"
	"github.com/woodchen-ink/splitdns/server/model"
)

// certExpiryWarnDays 证书剩余有效期低于该天数就提醒。
// CF 代签的证书是三个月有效期、自动续, 正常情况下不会掉到这个水位; 掉下来说明续期卡住了。
const certExpiryWarnDays = 21

// checkSaaS 校验 Cloudflare for SaaS 侧的状态: 回退源、自定义主机名、证书、自定义源服务器。
// 没配 SaaS 区的域名整段跳过 —— 那类域名可能纯走第三方 CDN。
func checkSaaS(h model.Hostname, snap model.Snapshot) []model.Finding {
	if h.SaaSZone == "" {
		return nil
	}
	var out []model.Finding

	if snap.FallbackOriginStatus != "active" {
		out = append(out, model.Finding{
			Level:  model.LevelError,
			Code:   "saas.fallback_inactive",
			Title:  "SaaS 区的回退源不是有效状态",
			Detail: fmt.Sprintf("回退源 %s, 状态 %s", orNone(snap.FallbackOrigin), orNone(snap.FallbackOriginStatus)),
			Fix:    "回退源必须是该区内一条橙云记录且状态有效, 否则这个区下所有自定义主机名都验证不过",
		})
	}

	ch := snap.CustomHostname
	if !ch.Exists {
		return append(out, model.Finding{
			Level: model.LevelError,
			Code:  "saas.custom_hostname_missing",
			Title: "SaaS 区里没有这个自定义主机名",
			Fix:   fmt.Sprintf("在 %s 区的自定义主机名页面添加 %s, 验证方式选 TXT", h.SaaSZone, h.Hostname),
		})
	}

	if ch.Status != "active" {
		out = append(out, model.Finding{
			Level:  model.LevelError,
			Code:   "saas.hostname_pending",
			Title:  "自定义主机名尚未通过验证",
			Detail: "当前状态 " + orNone(ch.Status),
			Fix:    "确认归属验证 TXT 已经写进权威 DNS 并已生效",
		})
	}
	if ch.SSLStatus != "active" {
		out = append(out, model.Finding{
			Level:  model.LevelError,
			Code:   "saas.cert_pending",
			Title:  "证书还没签发或部署完成",
			Detail: "当前状态 " + orNone(ch.SSLStatus),
			Fix:    "确认 DCV 的 TXT 一条不少地写进了权威 DNS",
		})
	}
	if strings.EqualFold(ch.ValidationMethod, "http") {
		out = append(out, model.Finding{
			Level: model.LevelWarn,
			Code:  "saas.http_validation",
			Title: "证书验证方式是 HTTP",
			Fix:   "这个域名的权威 DNS 已经委派出去, HTTP 验证在切换期间容易卡住, 建议改成 TXT",
		})
	}
	if ch.MinTLSVersion != "" && (ch.MinTLSVersion == "1.0" || ch.MinTLSVersion == "1.1") {
		out = append(out, model.Finding{
			Level:  model.LevelWarn,
			Code:   "saas.min_tls",
			Title:  "最低 TLS 版本偏低",
			Detail: "当前 " + ch.MinTLSVersion,
			Fix:    "自定义主机名默认给的是 TLS 1.0, 建议手动调到 1.2",
		})
	}
	if f, ok := checkCertExpiry(ch.CertExpiresAt); ok {
		out = append(out, f)
	}
	if ch.CustomOrigin != "" {
		out = append(out, model.Finding{
			Level:  model.LevelWarn,
			Code:   "saas.custom_origin_sni",
			Title:  "使用了自定义源服务器, 源站必须能路由这个 SNI",
			Detail: fmt.Sprintf("源服务器 %s, SNI %s", ch.CustomOrigin, orNone(ch.CustomOriginSNI)),
			Fix:    "在那台机器上给这个 SNI 名字挂一个 router/vhost, 指向哪个服务都行; 缺了会直接回源 403。这一项程序无法自动验证",
		})
	}
	return out
}

// checkCertExpiry 检查证书剩余有效期。解析不了的时间格式不报错, 只是不检查。
func checkCertExpiry(expiresAt string) (model.Finding, bool) {
	if expiresAt == "" {
		return model.Finding{}, false
	}
	layouts := []string{time.RFC3339, "2006-01-02T15:04:05.999999Z", "2006-01-02 15:04:05 -0700 MST"}
	var exp time.Time
	var err error
	for _, l := range layouts {
		if exp, err = time.Parse(l, expiresAt); err == nil {
			break
		}
	}
	if err != nil {
		return model.Finding{}, false
	}

	left := exp.Sub(timex.Now())
	if left > certExpiryWarnDays*24*time.Hour {
		return model.Finding{}, false
	}
	level := model.LevelWarn
	if left <= 0 {
		level = model.LevelError
	}
	return model.Finding{
		Level:  level,
		Code:   "saas.cert_expiring",
		Title:  "证书即将到期或已过期",
		Detail: fmt.Sprintf("到期时间 %s, 剩余 %d 天", expiresAt, int(left.Hours()/24)),
		Fix:    "CF 正常会自动续期, 到这一步说明续期卡住了, 检查 DCV 记录是否还在",
	}, true
}

// checkTXT 校验 CF 要求的验证 TXT 是否一条不少地落在权威 DNS 上。
// 证书带通配符 SAN 时 DCV 是多条同名不同值的记录, 少一条证书就签不出来, 所以逐条比对值。
func checkTXT(h model.Hostname, snap model.Snapshot) []model.Finding {
	if h.SaaSZone == "" || !snap.CustomHostname.Exists {
		return nil
	}
	missing := missingTXT(h, snap)
	if len(missing) == 0 {
		return nil
	}

	zone := h.DNSPodZone()
	lines := make([]string, 0, len(missing))
	for _, req := range missing {
		lines = append(lines, fmt.Sprintf("%s = %s", relativeName(req.Name, zone), req.Value))
	}
	return []model.Finding{{
		Level:  model.LevelError,
		Code:   "dcv.incomplete",
		Title:  fmt.Sprintf("还缺 %d 条 CF 要求的验证 TXT", len(missing)),
		Detail: strings.Join(lines, "; "),
		Fix:    fmt.Sprintf("到 DNSPod 的 %s 里按「默认」线路补齐这些 TXT", zone),
	}}
}
