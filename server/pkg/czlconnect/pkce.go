package czlconnect

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// challengeMethod 是唯一可用的 PKCE 算法。CZL Connect 不支持 plain, 别留退路。
const challengeMethod = "S256"

// GenerateVerifier 生成 PKCE 的 code_verifier。
// 32 字节随机数编成 base64url 是 43 个字符, 正好落在 RFC 7636 要求的 43~128 之间。
func GenerateVerifier() (string, error) {
	return randomURLSafe(32)
}

// GenerateState 生成授权请求的 state。
//
// 它不只是防 CSRF: 桌面版的回调走 splitdns:// 自定义协议, 机器上任何一个程序都能唤起它并塞一个
// 授权码进来。state 对不上就拒绝, 是这条入口唯一能自证"这次回跳确实来自我刚发起的那次登录"的东西。
func GenerateState() (string, error) {
	return randomURLSafe(16)
}

// Challenge 按 BASE64URL(SHA256(verifier)) 算出 code_challenge, 不带 padding。
func Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// randomURLSafe 生成 n 字节的密码学随机数并编成 URL 安全形式。
func randomURLSafe(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成随机数失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
