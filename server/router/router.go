package router

import (
	"log/slog"
	"net/http"

	"github.com/woodchen-ink/go-web-utils/nextstatic"
	"github.com/woodchen-ink/go-web-utils/resputil"
	"github.com/woodchen-ink/splitdns/server/config"
	"github.com/woodchen-ink/splitdns/server/handler"
)

// New 组装路由: /api 走业务处理器, 其余交给 Next.js 静态导出产物。
// 没有鉴权中间件 —— 桌面版不监听端口, 请求只可能来自自己的 webview。
func New(cfg *config.Config) http.Handler {
	api := http.NewServeMux()

	api.HandleFunc("GET /api/hostnames", handler.ListHostnames)
	api.HandleFunc("POST /api/hostnames", handler.SaveHostname)
	api.HandleFunc("GET /api/hostnames/{id}", handler.GetHostname)
	api.HandleFunc("DELETE /api/hostnames/{id}", handler.DeleteHostname)
	api.HandleFunc("POST /api/hostnames/{id}/inspect", handler.InspectHostname)
	api.HandleFunc("POST /api/hostnames/{id}/plan", handler.CreatePlan)

	api.HandleFunc("GET /api/plans/{id}", handler.GetPlan)
	api.HandleFunc("POST /api/plans/{id}/apply", handler.ApplyStep)
	api.HandleFunc("POST /api/plans/{id}/mark", handler.MarkStep)

	api.HandleFunc("GET /api/origins", handler.ListOrigins)
	api.HandleFunc("POST /api/origins", handler.SaveOrigin)
	api.HandleFunc("DELETE /api/origins/{id}", handler.DeleteOrigin)

	api.HandleFunc("GET /api/credentials", handler.ListCredentials)
	api.HandleFunc("POST /api/credentials", handler.SaveCredential)
	api.HandleFunc("POST /api/credentials/{id}/check", handler.CheckCredential)
	api.HandleFunc("DELETE /api/credentials/{id}", handler.DeleteCredential)

	api.HandleFunc("GET /api/export/db", handler.ExportDatabase)
	api.HandleFunc("POST /api/import/db", handler.ImportDatabase)

	api.HandleFunc("GET /api/discover/cf-zones", handler.CloudflareZones)
	api.HandleFunc("GET /api/discover/dnspod-domains", handler.DNSPodDomains)
	api.HandleFunc("GET /api/discover/parent-zone", handler.ParentZone)
	api.HandleFunc("GET /api/discover/saas-origins", handler.SaaSOrigins)

	api.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		resputil.OK(w, map[string]string{"status": "ok"})
	})

	// 未命中的 /api 路径统一返回 JSON 404, 不要回落到 HTML
	api.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		resputil.FailStatus(w, http.StatusNotFound, 404, "接口不存在")
	})

	root := http.NewServeMux()
	root.Handle("/api/", api)

	// 静态产物缺失时不让整个程序起不来: API 仍然可用, 页面路由给出明确提示
	static, err := nextstatic.New(nextstatic.Config{Root: cfg.StaticRoot, TrailingSlash: true})
	if err != nil {
		slog.Error("前端静态产物不可用, 仅 API 可访问", "root", cfg.StaticRoot, "err", err)
		root.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "前端产物未构建", http.StatusServiceUnavailable)
		})
	} else {
		root.Handle("/", static)
	}
	return root
}
