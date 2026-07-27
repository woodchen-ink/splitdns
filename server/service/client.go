package service

import (
	"fmt"

	"github.com/woodchen-ink/splitdns/server/database"
	"github.com/woodchen-ink/splitdns/server/model"
	"github.com/woodchen-ink/splitdns/server/pkg/cloudflare"
	"github.com/woodchen-ink/splitdns/server/pkg/dnspod"
)

// credentialRecord 按 ID 读一份凭据, 不校验平台。
func credentialRecord(id uint, out *model.Credential) error {
	if err := database.DB.First(out, id).Error; err != nil {
		return fmt.Errorf("读取凭据 %d 失败: %w", id, err)
	}
	return nil
}

// credentialByID 读一份凭据, 并校验它确实属于期望的平台。
// 配错平台 (把腾讯云凭据挂到 CF 字段上) 是很容易犯的错, 这里直接拦掉而不是等 API 报鉴权失败。
func credentialByID(id uint, wantKind string) (*model.Credential, error) {
	if id == 0 {
		return nil, fmt.Errorf("未绑定 %s 凭据", wantKind)
	}
	var c model.Credential
	if err := database.DB.First(&c, id).Error; err != nil {
		return nil, fmt.Errorf("读取凭据 %d 失败: %w", id, err)
	}
	if c.Kind != wantKind {
		return nil, fmt.Errorf("凭据 %s 属于 %s, 不能当作 %s 使用", c.Name, c.Kind, wantKind)
	}
	return &c, nil
}

// cloudflareClient 按凭据 ID 构建 CF 客户端。客户端本身很轻, 每次巡检重建, 不做缓存以免凭据更新后仍用旧值。
func cloudflareClient(id uint) (*cloudflare.Client, error) {
	c, err := credentialByID(id, model.CredCloudflare)
	if err != nil {
		return nil, err
	}
	if c.APIToken == "" {
		return nil, fmt.Errorf("凭据 %s 未填写 API Token", c.Name)
	}
	return cloudflare.New(c.APIToken), nil
}

// dnspodClient 按凭据 ID 构建 DNSPod 客户端。
func dnspodClient(id uint) (*dnspod.Client, error) {
	c, err := credentialByID(id, model.CredDNSPod)
	if err != nil {
		return nil, err
	}
	if c.SecretID == "" || c.SecretKey == "" {
		return nil, fmt.Errorf("凭据 %s 未填写 SecretId / SecretKey", c.Name)
	}
	return dnspod.New(c.SecretID, c.SecretKey)
}
