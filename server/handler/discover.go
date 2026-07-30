package handler

import (
	"net/http"
	"strconv"

	"github.com/woodchen-ink/go-web-utils/resputil"
	"github.com/woodchen-ink/splitdns/server/service"
)

// 这一组接口只读平台上的现有资源, 用来把配置表单里的手打项变成可选项。

// CloudflareZones GET /api/discover/cf-zones?credentialId=
// credentialId 可以不带: 不带就聚合全部 CF 凭据可见的 zone, 给"不绑凭据"的表单 (如回源) 挑后缀用
func CloudflareZones(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.URL.Query().Get("credentialId"), 10, 64)
	zones, err := service.CloudflareZones(r.Context(), uint(id))
	if err != nil {
		resputil.Fail(w, 500, err.Error())
		return
	}
	resputil.OK(w, zones)
}

// DNSPodDomains GET /api/discover/dnspod-domains?credentialId=
func DNSPodDomains(w http.ResponseWriter, r *http.Request) {
	id, ok := queryCredentialID(w, r)
	if !ok {
		return
	}
	domains, err := service.DNSPodDomains(r.Context(), id)
	if err != nil {
		resputil.Fail(w, 500, err.Error())
		return
	}
	resputil.OK(w, domains)
}

// ParentZone GET /api/discover/parent-zone?credentialId=&hostname=
// 推导逻辑只有后端一份, 前端拿它做预览而不是自己再算一遍。
// credentialId 可以不带: 不带就在全部 CF 账号里找, 顺带告诉前端该用哪份凭据
func ParentZone(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.URL.Query().Get("credentialId"), 10, 64)
	zone, err := service.DeriveParentZone(r.Context(), uint(id), r.URL.Query().Get("hostname"))
	if err != nil {
		resputil.Fail(w, 400, err.Error())
		return
	}
	resputil.OK(w, map[string]any{
		"parentZone":     zone.Zone,
		"credentialId":   zone.CredentialID,
		"credentialName": zone.CredentialName,
	})
}

// SaaSOrigins GET /api/discover/saas-origins?credentialId=&zone=
func SaaSOrigins(w http.ResponseWriter, r *http.Request) {
	id, ok := queryCredentialID(w, r)
	if !ok {
		return
	}
	options, err := service.SaaSOrigins(r.Context(), id, r.URL.Query().Get("zone"))
	if err != nil {
		resputil.Fail(w, 500, err.Error())
		return
	}
	resputil.OK(w, options)
}

// queryCredentialID 解析 credentialId 查询参数。
func queryCredentialID(w http.ResponseWriter, r *http.Request) (uint, bool) {
	raw := r.URL.Query().Get("credentialId")
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		resputil.Fail(w, 400, "缺少有效的 credentialId")
		return 0, false
	}
	return uint(id), true
}
