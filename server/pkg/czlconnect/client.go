// Package czlconnect 是 CZL Connect (https://connect.czl.net) 的 OAuth2 客户端。
//
// 只实现 Authorization Code + PKCE 一条路径: splitdns 是装在用户机器上的桌面程序,
// 藏不住 client_secret, 文档里那两种 client_secret_basic / client_secret_post 在这里没有使用场景,
// 留着只会诱导以后有人把密钥塞进二进制。
package czlconnect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 端点写死在这里: 这是接入的那一个身份服务, 不是可以指来指去的上游。
const (
	authorizeEndpoint = "https://connect.czl.net/oauth2/authorize"
	tokenEndpoint     = "https://connect.czl.net/api/oauth2/token"
	userInfoEndpoint  = "https://connect.czl.net/api/oauth2/userinfo"
)

// defaultExpiresIn 是服务端没给 expires_in 时的兜底有效期。
// 宁可短: 判早了顶多多刷一次令牌, 判晚了就是拿着废令牌去请求, 错误还发生在别处。
const defaultExpiresIn = time.Hour

// Config 是这个应用在 CZL Connect 后台登记的接入参数。
type Config struct {
	// ClientID 后台「接入参数」里的 Client ID
	ClientID string
	// RedirectURI 必须与后台登记的回调地址完全一致, 差一个斜杠都会被拒
	RedirectURI string
	// Scope 授权范围, 留空则不带这个参数
	Scope string
}

// Client 对应一份接入参数。它自己不持有任何用户令牌 —— 令牌属于会话, 由 service 层保管。
type Client struct {
	cfg  Config
	http *http.Client
}

// New 创建客户端。超时给到 20s: 授权服务器偶发慢响应比失败更常见。
func New(cfg Config) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: 20 * time.Second},
	}
}

// RedirectURI 返回登记的回调地址, 桌面壳按它的 scheme 注册协议处理器。
func (c *Client) RedirectURI() string { return c.cfg.RedirectURI }

// AuthorizeURL 拼出授权页地址。
//
// challenge 由 Challenge(verifier) 算出; verifier 必须留在本地直到换令牌那一步 ——
// 这正是 PKCE 的意义: 授权码被别的程序截走也换不出令牌, 因为它没有 verifier。
func (c *Client) AuthorizeURL(state, challenge string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", c.cfg.ClientID)
	q.Set("redirect_uri", c.cfg.RedirectURI)
	if c.cfg.Scope != "" {
		q.Set("scope", c.cfg.Scope)
	}
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	// 服务端只认 S256, plain 不要发
	q.Set("code_challenge_method", challengeMethod)
	return authorizeEndpoint + "?" + q.Encode()
}

// Token 是令牌端点的响应。
type Token struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
}

// Expiry 把 expires_in 换算成绝对时刻。
func (t *Token) Expiry(now time.Time) time.Time {
	if t.ExpiresIn <= 0 {
		return now.Add(defaultExpiresIn)
	}
	return now.Add(time.Duration(t.ExpiresIn) * time.Second)
}

// UserInfo 是用户信息端点的响应。
// 只声明用得上的字段: 接口以后加字段不该让这里解析失败。
type UserInfo struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Email    string `json:"email"`
	Avatar   string `json:"avatar"`
}

// Exchange 用授权码换令牌。
func (c *Client) Exchange(ctx context.Context, code, verifier string) (*Token, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", c.cfg.ClientID)
	// redirect_uri 这一步仍然要带: 服务端要拿它和授权请求里的那个比对
	form.Set("redirect_uri", c.cfg.RedirectURI)
	form.Set("code_verifier", verifier)

	var tok Token
	if err := c.postForm(ctx, form, &tok); err != nil {
		return nil, err
	}
	if tok.AccessToken == "" {
		return nil, errors.New("授权服务器没有返回 access_token")
	}
	return &tok, nil
}

// Refresh 用 refresh_token 换一套新令牌。
//
// 响应里的 refresh_token 可能为空 —— 那表示服务端这次不轮换, 调用方必须继续用手上那个旧的,
// 拿空值覆盖等于把自己的登录态清了。
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*Token, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", c.cfg.ClientID)

	var tok Token
	if err := c.postForm(ctx, form, &tok); err != nil {
		return nil, err
	}
	if tok.AccessToken == "" {
		return nil, errors.New("授权服务器没有返回 access_token")
	}
	return &tok, nil
}

// UserInfo 拉当前令牌对应的用户信息。
func (c *Client) UserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userInfoEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	var info UserInfo
	if err := c.do(req, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// postForm 向令牌端点发一次表单请求。
// 令牌端点只吃 application/x-www-form-urlencoded, 发 JSON 会被当成缺参数。
func (c *Client) postForm(ctx context.Context, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return c.do(req, out)
}

// do 发请求并把响应体解到 out。
func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("请求 CZL Connect 失败: %w", err)
	}
	//nolint:errcheck // 响应体关闭失败不影响业务结论
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("读取 CZL Connect 响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseError(resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("解析 CZL Connect 响应失败 (HTTP %d): %w", resp.StatusCode, err)
	}
	return nil
}

// OAuth2 错误码里需要区别对待的那两个。其余取值一律按"这次失败了"处理, 不逐个写分支。
const (
	// ErrCodeInvalidGrant 授权码或 refresh_token 已经不可用 (过期 / 被吊销 / 用过了)
	ErrCodeInvalidGrant = "invalid_grant"
	// ErrCodeInvalidToken access_token 不可用, 刷新一次还有救
	ErrCodeInvalidToken = "invalid_token"
)

// Error 是授权服务器返回的错误。
//
// HTTP 状态码和 body 里的 error 都留着: 前者用来判"要不要刷新令牌再试",
// 后者用来判"这个凭证是不是彻底废了"。只看其中一个都会误判 ——
// invalid_grant 常常挂在 400 上, 而 401 也可能只是 access_token 到点了。
type Error struct {
	Status      int
	Code        string
	Description string
}

func (e *Error) Error() string {
	detail := e.Code
	if e.Description != "" {
		if detail != "" {
			detail += ": "
		}
		detail += e.Description
	}
	if detail == "" {
		detail = "未提供错误详情"
	}
	return fmt.Sprintf("CZL Connect 返回错误 (HTTP %d): %s", e.Status, detail)
}

// IsInvalidGrant 判定 refresh_token / 授权码是否已经彻底不可用。
// 为 true 时重试没有意义, 只能让用户重新走一遍授权。
func IsInvalidGrant(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == ErrCodeInvalidGrant
}

// IsUnauthorized 判定是不是 access_token 不被认了 —— 刷新一次再试通常就好。
func IsUnauthorized(err error) bool {
	var e *Error
	if !errors.As(err, &e) {
		return false
	}
	return e.Status == http.StatusUnauthorized || e.Code == ErrCodeInvalidToken
}

// parseError 解析错误响应。
//
// 非 JSON 的响应体也要留一段原文: 网关返回的 HTML 错误页如果被抹成"未提供错误详情",
// 排查时就只剩一个状态码, 完全看不出是自己发错了还是根本没打到授权服务器。
func parseError(status int, body []byte) error {
	var payload struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
		Message          string `json:"message"`
		Msg              string `json:"msg"`
	}
	_ = json.Unmarshal(body, &payload)

	e := &Error{Status: status, Code: payload.Error, Description: payload.ErrorDescription}
	if e.Description == "" {
		e.Description = firstNonEmpty(payload.Message, payload.Msg)
	}
	if e.Code == "" && e.Description == "" {
		e.Description = snippet(body)
	}
	return e
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// snippet 截一段响应体原文用于报错, 顺手压掉换行, 免得一屏日志被一张 HTML 页面占满。
func snippet(body []byte) string {
	text := strings.Join(strings.Fields(string(body)), " ")
	const limit = 200
	if len(text) > limit {
		return text[:limit] + "…"
	}
	return text
}
