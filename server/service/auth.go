package service

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/woodchen-ink/go-web-utils/timex"
	"github.com/woodchen-ink/splitdns/server/database"
	"github.com/woodchen-ink/splitdns/server/model"
	"github.com/woodchen-ink/splitdns/server/pkg/czlconnect"
	"gorm.io/gorm"
)

// ErrNeedLogin 表示本地登录态已经不可用, 只能重新走一次浏览器授权。
// 与"这次没连上授权服务器"必须分开: 后者过一会儿自己就好了, 把它也当成需要重新登录,
// 用户会在断网时被自己的工具锁在门外。
var ErrNeedLogin = errors.New("登录已过期, 请重新登录")

// 登录状态。
const (
	// SessionGuest 本地没有登录态
	SessionGuest = "guest"
	// SessionActive 登录有效 (令牌临近过期时已经自动刷过)
	SessionActive = "active"
	// SessionExpired 账号信息还在, 但令牌被服务端判死, 必须重新授权
	SessionExpired = "expired"
)

// pendingTTL 是一次授权的等待上限。用户在浏览器里晾着不管, 超过这个时间回跳也不再认 ——
// 授权码本身寿命更短, 与其拿一个必然失败的码去换令牌, 不如直接说清楚是等太久了。
const pendingTTL = 10 * time.Minute

// pendingLogin 是一次已发起、尚未收尾的授权。
//
// 只放内存, 不落库: verifier 是这次流程的一次性秘密, 进程都退出了, 那次授权本来就该作废。
// 代价是"应用关掉后才在浏览器里点同意"这种情况必须重新发起, 提示里会讲清楚。
//
// **收到回调不等于流程结束**: 后面还要换令牌、拉用户信息, 那是两次网络请求。
// 这段时间里流程必须仍然算"进行中" —— 否则前端看到的是"既没在等待也没有错误",
// 会据此停掉轮询, 之后无论成功失败都没人再来看一眼, 界面就永远停在登录页。
type pendingLogin struct {
	state     string
	verifier  string
	startedAt time.Time
	// exchanging 表示回调已经到手, 正在换令牌
	exchanging bool
}

var (
	authClient *czlconnect.Client

	// authMu 保护 pending 与 lastLoginError
	authMu         sync.Mutex
	pending        *pendingLogin
	lastLoginError string
)

// InitAuth 装配 OAuth 客户端。入口调一次即可;
// 没调过时登录相关操作会明确报错, 而不是在某个空指针上崩掉。
func InitAuth(cfg czlconnect.Config) {
	authClient = czlconnect.New(cfg)
}

// RedirectURI 返回登记的回调地址, 桌面壳按它注册系统协议处理器。
func RedirectURI() string {
	if authClient == nil {
		return ""
	}
	return authClient.RedirectURI()
}

// StartLogin 发起一次授权, 返回该在系统浏览器里打开的授权页地址。
//
// 重复调用会作废上一次未完成的授权: 用户连点两次登录时, 只有最后那次的回跳算数,
// 否则两次流程的 state 互相覆盖, 先回来的那个反而会被判成"对不上"。
func StartLogin() (string, error) {
	if authClient == nil {
		return "", errors.New("登录功能未初始化")
	}
	verifier, err := czlconnect.GenerateVerifier()
	if err != nil {
		return "", err
	}
	state, err := czlconnect.GenerateState()
	if err != nil {
		return "", err
	}

	authMu.Lock()
	pending = &pendingLogin{state: state, verifier: verifier, startedAt: timex.Now()}
	lastLoginError = ""
	authMu.Unlock()

	return authClient.AuthorizeURL(state, czlconnect.Challenge(verifier)), nil
}

// CompleteLogin 处理授权回跳。
//
// 调用方有两个: 桌面壳收到 splitdns:// 唤起时, 以及用户手动把回跳地址粘回来时。
// 失败原因会记在会话里 —— 前一种情况下前端并没有一个正在等待的请求能接住这个错误,
// 不记下来的话界面就只是一直转圈, 什么都不说。
//
// **收尾必须是一次上锁完成的**: 清掉流程和记下失败原因中间不能留缝,
// 否则前端正好在那一瞬间轮询到, 看到的就是"没在等待、也没出错", 于是停掉轮询再也不问了。
func CompleteLogin(ctx context.Context, rawURL string) error {
	owned, err := completeLogin(ctx, rawURL)
	finishFlow(owned, err)
	return err
}

// completeLogin 返回它真正接手的那次流程 —— 没接手到就是 nil, 收尾时据此判断有没有资格清掉它。
func completeLogin(ctx context.Context, rawURL string) (*pendingLogin, error) {
	if authClient == nil {
		return nil, errors.New("登录功能未初始化")
	}
	code, state, err := parseCallback(rawURL)
	if err != nil {
		// 服务端明确拒绝时, 被拒的如果正是手上这次流程, 它就到此为止
		return flowByState(state), err
	}
	flow, err := beginExchange(state)
	if err != nil {
		return nil, err
	}

	tok, err := authClient.Exchange(ctx, code, flow.verifier)
	if err != nil {
		return flow, fmt.Errorf("用授权码换令牌失败: %w", err)
	}
	info, err := authClient.UserInfo(ctx, tok.AccessToken)
	if err != nil {
		return flow, fmt.Errorf("拉取用户信息失败: %w", err)
	}
	return flow, saveAccount(tok, info)
}

// parseCallback 从回跳地址里取出授权码与 state。
//
// 授权失败时服务端不会给 code, 而是把 error 挂在查询串上。这两种情况必须分开报:
// 一个是"流程没走通", 一个是"对方明确拒绝了", 用户要做的事完全不同。
// 出错时 state 照样往外带 —— 调用方要靠它认出被拒的到底是不是手上这次流程。
func parseCallback(raw string) (code, state string, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", fmt.Errorf("回调地址解析失败: %w", err)
	}
	q := u.Query()
	state = q.Get("state")
	if e := q.Get("error"); e != "" {
		return "", state, fmt.Errorf("授权未通过: %s", describeAuthError(e, q.Get("error_description")))
	}
	code = q.Get("code")
	if code == "" {
		return "", state, errors.New("回调地址里没有授权码, 这不像一次完整的授权回跳")
	}
	return code, state, nil
}

// flowByState 找出 state 对得上的那次流程, 对不上或者压根没带 state 时返回 nil。
func flowByState(state string) *pendingLogin {
	authMu.Lock()
	defer authMu.Unlock()
	if pending != nil && state != "" && subtle.ConstantTimeCompare([]byte(pending.state), []byte(state)) == 1 {
		return pending
	}
	return nil
}

// authErrorText 只翻译文档里明确列出的那几个。
// 取值是开放的, 认不出来的照原样带出去, 不要吞掉 —— 一个陌生的错误码总比"未知错误"有用。
var authErrorText = map[string]string{
	"login_required":       "还没有登录 CZL Connect",
	"consent_required":     "还没有授权过 splitdns, 首次授权必须在浏览器里点一次同意",
	"interaction_required": "需要先在 CZL Connect 上完成一些操作 (如验证邮箱、绑定上游账号)",
	"access_denied":        "没有访问这个应用的权限, 或者应用配额已满",
}

func describeAuthError(code, description string) string {
	text := authErrorText[code]
	if text == "" {
		text = code
	}
	if description != "" {
		text += " (" + description + ")"
	}
	return text
}

// beginExchange 校验 state 并把流程标成"换令牌中"。
//
// 标记而不是取走: 换令牌那一两秒里流程仍然要算进行中 (见 pendingLogin 上的说明)。
// 标记本身也是防重放 —— 同一个授权码再回来一次会撞上这里, 不会被换第二遍。
func beginExchange(state string) (*pendingLogin, error) {
	authMu.Lock()
	defer authMu.Unlock()

	flow := pending
	if flow == nil {
		return nil, errors.New("当前没有正在进行的登录, 请回到应用里重新点一次登录")
	}
	if flow.exchanging {
		return nil, errors.New("上一次回调正在处理中")
	}
	if timex.Now().Sub(flow.startedAt) > pendingTTL {
		return nil, errors.New("这次登录等待过久已作废, 请重新发起")
	}
	if subtle.ConstantTimeCompare([]byte(flow.state), []byte(state)) != 1 {
		return nil, errors.New("回调的 state 与本次登录对不上, 已拒绝")
	}
	flow.exchanging = true
	return flow, nil
}

// finishFlow 给一次授权收尾: 清掉流程, 失败的话把原因留下。
//
// 两件事必须在同一把锁里做完 —— 中间露出的那一瞬间在前端看来就是"没在等待也没出错",
// 它会据此停掉轮询, 于是后面无论成功失败都再没人来看一眼。
//
// **只有真正接手过这次流程的那一路才能清掉它**: 浏览器那个停住的标签页刷新一下就会把回调再发一次,
// 而那时用户可能已经重新点过登录了。拿一条过期的回调去清掉手上正跑着的流程,
// 等于把用户刚发起的那次登录判死。
func finishFlow(owned *pendingLogin, err error) {
	authMu.Lock()
	if owned != nil && pending == owned {
		pending = nil
	}
	if err != nil {
		lastLoginError = err.Error()
	}
	authMu.Unlock()
}

func clearPending() {
	authMu.Lock()
	pending = nil
	authMu.Unlock()
}

// saveAccount 落库登录态。
//
// 先删后插而不是按主键更新: 这样顺带清掉历史版本可能留下的多余行,
// 也不必操心 GORM 的 Save 在"主键给了但行不存在"时不会插入这件事。
func saveAccount(tok *czlconnect.Token, info *czlconnect.UserInfo) error {
	now := timex.Now()
	account := model.Account{
		ID:           model.AccountRowID,
		RemoteID:     info.ID,
		Username:     info.Username,
		Nickname:     info.Nickname,
		Email:        info.Email,
		Avatar:       info.Avatar,
		GroupsRaw:    info.Groups,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    tok.Expiry(now),
		Scope:        tok.Scope,
		LoggedInAt:   now,
	}
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&model.Account{}).Error; err != nil {
			return err
		}
		return tx.Create(&account).Error
	})
	if err != nil {
		return fmt.Errorf("保存登录状态失败: %w", err)
	}
	resetRefreshBackoff()
	return nil
}

// CurrentAccount 读当前登录账号, 没有登录时返回 nil 而不是错误。
//
// 用 Find 而不是 First: 单行表本来就常常是空的, 而 First 会为此报 ErrRecordNotFound 并打一条警告 ——
// 未登录时前端在按秒轮询登录状态, 那条警告会把日志刷满。
func CurrentAccount() (*model.Account, error) {
	var accounts []model.Account
	if err := database.DB.Limit(1).Find(&accounts).Error; err != nil {
		return nil, fmt.Errorf("读取登录状态失败: %w", err)
	}
	if len(accounts) == 0 {
		return nil, nil
	}
	return &accounts[0], nil
}

// IsLoggedIn 判断这次请求该不该放行。只查本地, 不碰网络 ——
// 它挂在每个 /api 请求前面, 一次网络抖动不该让整个界面变成不可用。
//
// refresh_token 为空表示服务端已经判死这份授权, 那种情况等同未登录。
func IsLoggedIn() bool {
	account, err := CurrentAccount()
	return err == nil && account != nil && account.RefreshToken != ""
}

// Logout 清掉本地登录态。
//
// 只清本地: CZL Connect 没有提供吊销端点, 服务端那侧的授权记录要用户自己去后台取消。
func Logout() error {
	if err := database.DB.Where("1 = 1").Delete(&model.Account{}).Error; err != nil {
		return fmt.Errorf("清除登录状态失败: %w", err)
	}
	clearPending()
	authMu.Lock()
	lastLoginError = ""
	authMu.Unlock()
	resetRefreshBackoff()
	return nil
}

// Session 是给前端的登录状态视图。
type Session struct {
	Status string         `json:"status"`
	User   *model.Account `json:"user"`
	// Waiting 表示有一次授权正在进行 (等浏览器回跳, 或回跳已到手正在换令牌)
	Waiting bool `json:"waiting"`
	// Exchanging 表示回调已经到手, 正在换令牌 —— 界面据此把话说准, 别还在喊"请到浏览器完成授权"
	Exchanging bool `json:"exchanging"`
	// LoginError 上一次授权回跳失败的原因。回跳是从进程外面进来的, 只能这样带给界面
	LoginError string `json:"loginError"`
	// TokenError 最近一次刷新令牌失败的原因。属于"暂时没刷上", 不影响继续使用
	TokenError string `json:"tokenError"`
}

// LoadSession 读登录状态, 顺便保证令牌是新鲜的。
//
// 刷新只在临近过期时才真的发请求, 所以前端等待回跳期间的轮询不会变成对授权服务器的连打。
func LoadSession(ctx context.Context) (*Session, error) {
	account, err := CurrentAccount()
	if err != nil {
		return nil, err
	}

	session := &Session{Status: SessionGuest}
	authMu.Lock()
	if pending != nil {
		// 换令牌中的流程不看 TTL: 那一步是我们自己在跑, 不是在等用户
		session.Exchanging = pending.exchanging
		session.Waiting = pending.exchanging || timex.Now().Sub(pending.startedAt) <= pendingTTL
	}
	session.LoginError = lastLoginError
	authMu.Unlock()

	if account == nil {
		return session, nil
	}
	session.User = account
	session.Status = SessionActive

	if _, err := AccessToken(ctx); err != nil {
		if errors.Is(err, ErrNeedLogin) {
			session.Status = SessionExpired
			// 令牌已被清掉, 重新读一次让界面显示的是清理后的状态
			if account, err := CurrentAccount(); err == nil && account != nil {
				session.User = account
			}
		} else {
			// 连不上授权服务器只是这次没刷上, 本地登录态照旧, 别把人挡在门外
			session.TokenError = err.Error()
		}
	}
	return session, nil
}
