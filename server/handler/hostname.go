package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/woodchen-ink/go-web-utils/resputil"
	"github.com/woodchen-ink/splitdns/server/model"
	"github.com/woodchen-ink/splitdns/server/service"
)

// ListHostnames GET /api/hostnames?keyword=&offset=&limit=
func ListHostnames(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	list, total, err := service.ListHostnames(q.Get("keyword"), offset, limit)
	if err != nil {
		resputil.Fail(w, 500, err.Error())
		return
	}
	resputil.OK(w, map[string]any{"list": list, "total": total})
}

// GetHostname GET /api/hostnames/{id}
func GetHostname(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	h, err := service.HostnameByID(id)
	if err != nil {
		resputil.Fail(w, 404, err.Error())
		return
	}
	resputil.OK(w, h)
}

// SaveHostname POST /api/hostnames  (带 id 即更新)
func SaveHostname(w http.ResponseWriter, r *http.Request) {
	// directDnspod 是请求专属的模式声明, 不落库: 空 ParentZone 在委派模式下是"待推导"、
	// 直托模式下是"就该为空", 不带这个标记后端没法区分这两种意图
	var req struct {
		model.Hostname
		DirectDNSPod bool `json:"directDnspod"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		resputil.Fail(w, 400, "请求体格式错误: "+err.Error())
		return
	}
	if req.Hostname.Hostname == "" {
		resputil.Fail(w, 400, "访问域名不能为空")
		return
	}
	// 不校验 CF 凭据: 留空时由 service 按访问域名反查出该用哪个账号
	if err := service.SaveHostname(r.Context(), &req.Hostname, req.DirectDNSPod); err != nil {
		resputil.Fail(w, 500, err.Error())
		return
	}
	resputil.OK(w, req.Hostname)
}

// DeleteHostname DELETE /api/hostnames/{id}
func DeleteHostname(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := service.DeleteHostname(id); err != nil {
		resputil.Fail(w, 500, err.Error())
		return
	}
	resputil.OKMsg(w, nil, "已删除")
}

// InspectHostname POST /api/hostnames/{id}/inspect 立即巡检一次
func InspectHostname(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	h, err := service.HostnameByID(id)
	if err != nil {
		resputil.Fail(w, 404, err.Error())
		return
	}
	resputil.OK(w, service.Inspect(r.Context(), *h))
}

// pathID 解析路径里的 id 段, 失败时已写好响应, 返回 false。
func pathID(w http.ResponseWriter, r *http.Request) (uint, bool) {
	raw := r.PathValue("id")
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		resputil.Fail(w, 400, "无效的 id")
		return 0, false
	}
	return uint(id), true
}
