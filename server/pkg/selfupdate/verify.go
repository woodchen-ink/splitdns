package selfupdate

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// ParsePublicKey 解析 base64 编码的 32 字节 ed25519 公钥。
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("公钥不是合法的 base64: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("公钥长度应为 %d 字节, 实际 %d", ed25519.PublicKeySize, len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

// ParsePrivateKey 解析 base64 编码的 32 字节 seed。只给 CI 的签名工具用。
func ParsePrivateKey(s string) (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("私钥不是合法的 base64: %w", err)
	}
	if len(raw) != ed25519.SeedSize {
		return nil, fmt.Errorf("私钥 seed 长度应为 %d 字节, 实际 %d", ed25519.SeedSize, len(raw))
	}
	return ed25519.NewKeyFromSeed(raw), nil
}

// Sign 给 SHA256SUMS 签名, 返回签名文件内容 (base64 + 换行)。
func Sign(sums []byte, priv ed25519.PrivateKey) []byte {
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sums)) + "\n")
}

// VerifyChecksums 验证 SHA256SUMS 的签名并解析成 文件名 → 小写十六进制摘要。
//
// 先验签后解析: 没通过签名的内容一个字都不该被信任, 包括里面的文件名。
func VerifyChecksums(sums, sig []byte, pub ed25519.PublicKey) (map[string]string, error) {
	rawSig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil {
		return nil, fmt.Errorf("签名文件格式错误: %w", err)
	}
	if !ed25519.Verify(pub, sums, rawSig) {
		return nil, errors.New("校验文件签名不匹配, 拒绝安装")
	}

	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		// sha256sum 格式: "<hex>  <name>", 二进制模式下文件名前带 '*'
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		out[strings.TrimPrefix(fields[1], "*")] = strings.ToLower(fields[0])
	}
	return out, sc.Err()
}
