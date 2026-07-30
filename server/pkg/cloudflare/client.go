package cloudflare

import (
	"bytes"
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

const apiBase = "https://api.cloudflare.com/client/v4"

// Client 是 Cloudflare API v4 的只读客户端。一个 Client 对应一份 API Token,
// 不同账号各建一个实例, 不做全局单例。
type Client struct {
	token string
	http  *http.Client
}

// New 创建客户端。超时给到 20s, CF 侧偶发慢响应比失败更常见。
func New(token string) *Client {
	return &Client{
		token: token,
		http:  &http.Client{Timeout: 20 * time.Second},
	}
}

// envelope 是 CF API 的统一响应外壳。
type envelope struct {
	Success bool            `json:"success"`
	Errors  []apiError      `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// APIError 是一次 CF 请求的失败结果, 带上 HTTP 状态码。
// 删除类操作要能把"这东西本来就不在了"当成功, 靠匹配错误文案判定太脆, 所以留出状态码。
type APIError struct {
	Status int
	Errors []apiError
}

func (e *APIError) Error() string {
	return fmt.Sprintf("CF 返回错误 (HTTP %d): %s", e.Status, joinErrors(e.Errors))
}

// IsNotFound 判定错误是不是"要操作的东西不存在"。
func IsNotFound(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Status == http.StatusNotFound
}

// get 发起一次 GET 请求并把 result 解到 out。
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, query, nil, out)
}

// do 发起一次请求并把 result 解到 out。
// CF 的错误既可能体现在 HTTP 状态码上, 也可能是 200 + success:false, 两种都要拦。
func (c *Client) do(ctx context.Context, method, path string, query url.Values, payload any, out any) error {
	u := apiBase + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var reqBody io.Reader
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("序列化请求体失败: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("请求 CF 失败: %w", err)
	}
	//nolint:errcheck // 响应体关闭失败不影响业务结论
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("读取 CF 响应失败: %w", err)
	}

	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("解析 CF 响应失败 (HTTP %d): %w", resp.StatusCode, err)
	}
	if !env.Success {
		return &APIError{Status: resp.StatusCode, Errors: env.Errors}
	}
	if out == nil || len(env.Result) == 0 || string(env.Result) == "null" {
		return nil
	}
	return json.Unmarshal(env.Result, out)
}

// VerifyToken 校验 API Token 是否有效, 返回 CF 给的状态 (active / disabled / expired)。
func (c *Client) VerifyToken(ctx context.Context) (string, error) {
	var out struct {
		Status string `json:"status"`
	}
	if err := c.get(ctx, "/user/tokens/verify", nil, &out); err != nil {
		return "", err
	}
	return out.Status, nil
}

// ListZoneNames 列出该 Token 可见的 zone 名。
// 用来确认权限范围确实覆盖了要操作的父区与 SaaS 区 —— Token 有效不代表范围够。
func (c *Client) ListZoneNames(ctx context.Context) ([]string, error) {
	zones, err := c.ListZones(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(zones))
	for _, z := range zones {
		names = append(names, z.Name)
	}
	return names, nil
}

func joinErrors(errs []apiError) string {
	if len(errs) == 0 {
		return "未提供错误详情"
	}
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, fmt.Sprintf("%d %s", e.Code, e.Message))
	}
	return strings.Join(parts, "; ")
}
