package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/woodchen-ink/splitdns/server/model"
)

// CredentialCheck 是一次凭据可用性检测的结果。
// 只回结论与范围提示, 不回任何密钥内容。
type CredentialCheck struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	// Scope 该凭据实际能看到的资源, 用来确认权限范围够不够
	Scope []string `json:"scope"`
}

// CheckCredential 拿凭据真去调一次平台接口。
// "能连上"和"范围够用"是两件事: CF Token 有效但没授权到目标 zone 一样干不了活,
// 所以顺带把可见 zone 列出来交给人判断。
func CheckCredential(ctx context.Context, id uint) (*CredentialCheck, error) {
	var c model.Credential
	if err := credentialRecord(id, &c); err != nil {
		return nil, err
	}

	switch c.Kind {
	case model.CredCloudflare:
		return checkCloudflare(ctx, c)
	case model.CredDNSPod:
		return checkDNSPod(ctx, c)
	}
	return &CredentialCheck{
		OK:      false,
		Message: fmt.Sprintf("不认识的平台 %q, 没有对应的检测方式", c.Kind),
	}, nil
}

// checkCloudflare 校验 Token 状态并列出可见 zone。
func checkCloudflare(ctx context.Context, c model.Credential) (*CredentialCheck, error) {
	if c.APIToken == "" {
		return &CredentialCheck{OK: false, Message: "还没填 API Token"}, nil
	}
	cf, err := cloudflareClient(c.ID)
	if err != nil {
		return &CredentialCheck{OK: false, Message: err.Error()}, nil
	}

	status, err := cf.VerifyToken(ctx)
	if err != nil {
		return &CredentialCheck{OK: false, Message: "Token 校验失败: " + err.Error()}, nil
	}
	if status != "active" {
		return &CredentialCheck{OK: false, Message: fmt.Sprintf("Token 状态是 %q, 不可用", status)}, nil
	}

	zones, err := cf.ListZoneNames(ctx)
	if err != nil {
		return &CredentialCheck{
			OK:      false,
			Message: "Token 有效, 但列不出 zone, 多半缺 Zone:Read 权限: " + err.Error(),
		}, nil
	}
	if len(zones) == 0 {
		return &CredentialCheck{
			OK:      false,
			Message: "Token 有效, 但一个 zone 都看不到, 检查 Token 的作用范围",
		}, nil
	}
	return &CredentialCheck{
		OK:      true,
		Message: fmt.Sprintf("可用, 能看到 %d 个 zone", len(zones)),
		Scope:   zones,
	}, nil
}

// checkDNSPod 用一次域名列表调用验证密钥可用。
func checkDNSPod(ctx context.Context, c model.Credential) (*CredentialCheck, error) {
	if c.SecretID == "" || c.SecretKey == "" {
		return &CredentialCheck{OK: false, Message: "还没填 SecretId / SecretKey"}, nil
	}
	dp, err := dnspodClient(c.ID)
	if err != nil {
		return &CredentialCheck{OK: false, Message: err.Error()}, nil
	}

	count, err := dp.CountDomains(ctx)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "AuthFailure") {
			msg += " (SecretId / SecretKey 不匹配或已停用)"
		}
		return &CredentialCheck{OK: false, Message: "调用失败: " + msg}, nil
	}
	return &CredentialCheck{
		OK:      true,
		Message: fmt.Sprintf("可用, 账号下有 %d 个域名", count),
	}, nil
}
