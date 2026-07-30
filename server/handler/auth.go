package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/pkg/browser"
	"github.com/woodchen-ink/go-web-utils/resputil"
	"github.com/woodchen-ink/splitdns/server/service"
)

// CodeNeedLogin 是"未登录"的业务码, 与 HTTP 401 一起返回。
// 前端据此把界面切回登录页, 而不是把它当成一次普通的接口失败弹个 toast。
const CodeNeedLogin = 401

// StartLogin POST /api/auth/login 发起授权, 并用系统浏览器打开授权页。
//
// 必须是系统浏览器而不是应用自己的 webview: 授权页要用到用户在浏览器里已有的 CZL Connect 登录态,
// 而且让第三方登录页跑在自家 webview 里, 用户没有地址栏可以核对域名。
func StartLogin(w http.ResponseWriter, r *http.Request) {
	authorizeURL, err := service.StartLogin()
	if err != nil {
		resputil.Fail(w, 500, err.Error())
		return
	}

	// 打不开浏览器不算失败: 地址照样给出去, 界面上可以复制了自己打开
	opened := browser.OpenURL(authorizeURL) == nil
	msg := "已在浏览器中打开授权页, 完成授权后会自动回到这里"
	if !opened {
		msg = "没能自动打开浏览器, 请复制下面的地址手动打开"
	}
	resputil.OKMsg(w, map[string]any{
		"authorizeUrl": authorizeURL,
		"opened":       opened,
	}, msg)
}

// GetSession GET /api/auth/session 读当前登录状态。
// 前端在等待浏览器回跳期间轮询它 —— 授权是在应用外面完成的, 没有别的路子能通知界面。
func GetSession(w http.ResponseWriter, r *http.Request) {
	session, err := service.LoadSession(r.Context())
	if err != nil {
		resputil.Fail(w, 500, err.Error())
		return
	}
	resputil.OK(w, session)
}

// AuthCallback POST /api/auth/callback 手动兜底: 用户把浏览器地址栏里的回跳地址粘回来。
//
// 正常路径不走这里 —— splitdns:// 由系统直接唤起桌面壳。
// 但协议没注册上时 (绿色版换过目录、被安全软件拦下), 这是唯一能自救的入口。
func AuthCallback(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		resputil.Fail(w, 400, "请求体格式错误: "+err.Error())
		return
	}
	if req.URL == "" {
		resputil.Fail(w, 400, "请粘贴浏览器里那条 splitdns:// 开头的回调地址")
		return
	}
	if err := service.CompleteLogin(r.Context(), req.URL); err != nil {
		resputil.Fail(w, 400, err.Error())
		return
	}
	session, err := service.LoadSession(r.Context())
	if err != nil {
		resputil.Fail(w, 500, err.Error())
		return
	}
	resputil.OKMsg(w, session, "登录成功")
}

// RefreshSession POST /api/auth/refresh 强制刷新令牌并重新拉用户信息。
func RefreshSession(w http.ResponseWriter, r *http.Request) {
	if err := service.RefreshSession(r.Context()); err != nil {
		if errors.Is(err, service.ErrNeedLogin) {
			resputil.FailStatus(w, http.StatusUnauthorized, CodeNeedLogin, err.Error())
			return
		}
		resputil.Fail(w, 500, err.Error())
		return
	}
	session, err := service.LoadSession(r.Context())
	if err != nil {
		resputil.Fail(w, 500, err.Error())
		return
	}
	resputil.OKMsg(w, session, "登录状态已刷新")
}

// Logout POST /api/auth/logout 退出登录。
// 只清本地令牌; CZL Connect 侧的授权记录要用户自己去后台取消, 这里不谎称已经撤销。
func Logout(w http.ResponseWriter, r *http.Request) {
	if err := service.Logout(); err != nil {
		resputil.Fail(w, 500, err.Error())
		return
	}
	resputil.OKMsg(w, nil, "已退出登录")
}
