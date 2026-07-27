package handler

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/pkg/browser"
	"github.com/woodchen-ink/go-web-utils/resputil"
)

// OpenExternal POST /api/open 用系统默认浏览器打开一个链接。
//
// webview 里 target="_blank" 的行为不可靠 (要么被吞, 要么开出一个没有地址栏的窗口),
// 外链一律交给 Go 去调系统浏览器。
func OpenExternal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		resputil.Fail(w, 400, "请求体格式错误: "+err.Error())
		return
	}

	// 只放行 http/https: 别让界面上的一个链接变成任意命令的入口
	parsed, err := url.Parse(req.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		resputil.Fail(w, 400, "只能打开 http / https 链接")
		return
	}
	if err := browser.OpenURL(parsed.String()); err != nil {
		resputil.Fail(w, 500, "打不开浏览器: "+err.Error())
		return
	}
	resputil.OKMsg(w, nil, "已在浏览器中打开")
}
