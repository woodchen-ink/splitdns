package handler

import (
	"errors"
	"net/http"

	"github.com/woodchen-ink/go-web-utils/resputil"
	"github.com/woodchen-ink/splitdns/server/service"
)

// GetUpdate GET /api/update 读更新状态。只读内存, 不发网络请求, 前端可以放心轮询。
func GetUpdate(w http.ResponseWriter, r *http.Request) {
	resputil.OK(w, service.GetUpdateStatus())
}

// CheckUpdate POST /api/update/check 立即查一次 GitHub。
func CheckUpdate(w http.ResponseWriter, r *http.Request) {
	if err := service.CheckUpdate(r.Context()); err != nil {
		resputil.Fail(w, 502, "检查更新失败: "+err.Error())
		return
	}
	status := service.GetUpdateStatus()
	msg := "已是最新版本"
	if status.State == service.UpdateAvailable && status.Latest != nil {
		msg = "发现新版本 " + status.Latest.Tag
	}
	resputil.OKMsg(w, status, msg)
}

// InstallUpdate POST /api/update/install 开始下载安装, 立即返回, 进度走 GET /api/update。
func InstallUpdate(w http.ResponseWriter, r *http.Request) {
	if err := service.StartInstallUpdate(); err != nil {
		code := 500
		if errors.Is(err, service.ErrNoUpdate) {
			code = 400
		}
		resputil.Fail(w, code, err.Error())
		return
	}
	resputil.OKMsg(w, service.GetUpdateStatus(), "开始下载新版本")
}
