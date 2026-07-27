package middleware

import (
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"crypto"

	"github.com/woodchen-ink/go-web-utils/resputil"
)

// AccessVerifier 校验 Cloudflare Access 下发的身份 JWT。
// 应用自身不实现登录: 认证由 Access 在边缘完成, 这里只确认请求确实经过了 Access
// 并且签发给本应用, 防止有人绕过 Access 直连源站。
type AccessVerifier struct {
	teamDomain string
	aud        string

	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

// NewAccessVerifier 构造校验器。teamDomain 形如 czl.cloudflareaccess.com。
func NewAccessVerifier(teamDomain, aud string) *AccessVerifier {
	return &AccessVerifier{
		teamDomain: strings.TrimSuffix(strings.TrimPrefix(teamDomain, "https://"), "/"),
		aud:        aud,
		keys:       map[string]*rsa.PublicKey{},
	}
}

// Middleware 返回校验中间件。校验失败一律 403, 不透露失败细节给调用方。
func (v *AccessVerifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Cf-Access-Jwt-Assertion")
		if token == "" {
			if c, err := r.Cookie("CF_Authorization"); err == nil {
				token = c.Value
			}
		}
		if token == "" {
			resputil.FailStatus(w, http.StatusForbidden, 403, "缺少 Cloudflare Access 身份凭证")
			return
		}
		if err := v.verify(token); err != nil {
			slog.Warn("Access JWT 校验失败", "err", err, "path", r.URL.Path)
			resputil.FailStatus(w, http.StatusForbidden, 403, "身份校验未通过")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// verify 校验签名、签发方、受众与有效期。
func (v *AccessVerifier) verify(token string) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return fmt.Errorf("token 结构不合法")
	}

	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := decodeSegment(parts[0], &header); err != nil {
		return err
	}
	if header.Alg != "RS256" {
		return fmt.Errorf("不支持的签名算法 %s", header.Alg)
	}

	var claims struct {
		Aud json.RawMessage `json:"aud"`
		Iss string          `json:"iss"`
		Exp int64           `json:"exp"`
		Nbf int64           `json:"nbf"`
	}
	if err := decodeSegment(parts[1], &claims); err != nil {
		return err
	}

	now := time.Now().Unix()
	if claims.Exp != 0 && now > claims.Exp {
		return fmt.Errorf("token 已过期")
	}
	if claims.Nbf != 0 && now < claims.Nbf-60 {
		return fmt.Errorf("token 尚未生效")
	}
	if !strings.Contains(claims.Iss, v.teamDomain) {
		return fmt.Errorf("签发方 %s 与团队域名不符", claims.Iss)
	}
	if !audienceMatches(claims.Aud, v.aud) {
		return fmt.Errorf("受众与本应用 AUD 不符")
	}

	key, err := v.publicKey(header.Kid)
	if err != nil {
		return err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("签名解码失败: %w", err)
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	return rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig)
}

// audienceMatches 兼容 aud 为字符串与字符串数组两种形态。
func audienceMatches(raw json.RawMessage, want string) bool {
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return single == want
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		for _, a := range list {
			if a == want {
				return true
			}
		}
	}
	return false
}

// publicKey 取签名公钥, 带 10 分钟缓存。
// 遇到未知 kid 会立即强制刷新一次, 以便 Cloudflare 轮换密钥时不至于整段时间不可用。
func (v *AccessVerifier) publicKey(kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	key, ok := v.keys[kid]
	fresh := time.Since(v.fetchedAt) < 10*time.Minute
	v.mu.RUnlock()
	if ok && fresh {
		return key, nil
	}
	if err := v.refreshKeys(); err != nil {
		if ok {
			return key, nil
		}
		return nil, err
	}

	v.mu.RLock()
	defer v.mu.RUnlock()
	if key, ok := v.keys[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("找不到 kid %s 对应的公钥", kid)
}

// refreshKeys 拉取团队的 JWKS。
func (v *AccessVerifier) refreshKeys() error {
	url := fmt.Sprintf("https://%s/cdn-cgi/access/certs", v.teamDomain)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("拉取 JWKS 失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &jwks); err != nil {
		return fmt.Errorf("解析 JWKS 失败: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		n, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		e, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{
			N: new(big.Int).SetBytes(n),
			E: int(new(big.Int).SetBytes(e).Int64()),
		}
	}
	if len(keys) == 0 {
		return fmt.Errorf("JWKS 里没有可用公钥")
	}

	v.mu.Lock()
	v.keys = keys
	v.fetchedAt = time.Now()
	v.mu.Unlock()
	return nil
}

// decodeSegment 解 base64url 段并反序列化。
func decodeSegment(seg string, out any) error {
	raw, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return fmt.Errorf("token 段解码失败: %w", err)
	}
	return json.Unmarshal(raw, out)
}
