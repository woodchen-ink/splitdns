package handler

import (
	"net/http"
	"strconv"

	"github.com/woodchen-ink/go-web-utils/resputil"
	"github.com/woodchen-ink/splitdns/server/service"
)

// 这一组接口只读平台上的现有资源, 用来把配置表单里的手打项变成可选项。

// CloudflareZones GET /api/discover/cf-zones?credentialId=
func CloudflareZones(w http.ResponseWriter, r *http.Request) {
	id, ok := queryCredentialID(w, r)
	if !ok {
		return
	}
	zones, err := service.CloudflareZones(r.Context(), id)
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
// 推导逻辑只有后端一份, 前端拿它做预览而不是自己再算一遍
func ParentZone(w http.ResponseWriter, r *http.Request) {
	id, ok := queryCredentialID(w, r)
	if !ok {
		return
	}
	zone, err := service.DeriveParentZone(r.Context(), id, r.URL.Query().Get("hostname"))
	if err != nil {
		resputil.Fail(w, 400, err.Error())
		return
	}
	resputil.OK(w, map[string]string{"parentZone": zone})
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
