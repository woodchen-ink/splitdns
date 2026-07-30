package router

import (
	"net/http"
	"strings"

	"github.com/woodchen-ink/go-web-utils/resputil"
	"github.com/woodchen-ink/splitdns/server/handler"
	"github.com/woodchen-ink/splitdns/server/service"
)

// publicPrefixes 是不需要登录就能访问的 /api 路径。
//
// /api/open 也在里面: 登录页上的帮助链接要靠它调系统浏览器, 而那时候本来就还没登录。
// 它只放行 http / https, 不会因为豁免而变成什么入口。
var publicPrefixes = []string{
	"/api/auth/",
	"/api/healthz",
	"/api/open",
}

// requireLogin 拦下未登录的 /api 请求。
//
// 桌面版没有网络入口, 这一层不是用来挡外人的, 而是让"未登录"在后端也是一个确定状态:
// 界面上的闸门可以被绕开 (开一下 devtools 就行), 后端不认的话, 一个没有登录的会话
// 照样能去改生产 DNS。
//
// 全程不碰网络, 只查本地那一行登录态 —— 每个请求都去问一次授权服务器, 断网就等于整个工具不可用。
func requireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicPath(r.URL.Path) || service.IsLoggedIn() {
			next.ServeHTTP(w, r)
			return
		}
		resputil.FailStatus(w, http.StatusUnauthorized, handler.CodeNeedLogin, "请先登录 CZL Connect 账号")
	})
}

func isPublicPath(path string) bool {
	for _, prefix := range publicPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
