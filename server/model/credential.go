package model

import (
	"time"

	"gorm.io/gorm"
)

// CredentialKind 是凭据所属平台。开放式取值: 新增平台只加常量与对应 pkg 客户端,
// 不在各处硬编码分支穷举, 未识别的 kind 由调用方兜底跳过并显式提示。
const (
	CredCloudflare = "cloudflare"
	CredDNSPod     = "dnspod"
)

// Credential 是一份平台 API 凭据。密钥字段一律 json:"-", 不出现在任何接口响应里,
// 前端只能写入、不能读回。
type Credential struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// Kind 平台标识, 见上方常量
	Kind string `gorm:"column:kind;size:32;index;not null" json:"kind"`
	// Name 人类可读的备注名, 如 "CF 主账号" / "腾讯云 wood"
	Name string `gorm:"column:name;size:128;not null" json:"name"`

	// APIToken Cloudflare 用: API Token
	APIToken string `gorm:"column:api_token;size:512" json:"-"`
	// SecretID 腾讯云用
	SecretID string `gorm:"column:secret_id;size:256" json:"-"`
	// SecretKey 腾讯云用
	SecretKey string `gorm:"column:secret_key;size:256" json:"-"`

	// HasSecret 派生字段, 告诉前端这份凭据是否已填过密钥, 不暴露密钥本身
	HasSecret bool `gorm:"-" json:"hasSecret"`
}

// TableName 显式声明表名, 不依赖 GORM 自动复数化。
func (Credential) TableName() string { return "credential" }

// AfterFind 填充派生字段, 让列表接口能直接渲染"已配置 / 未配置"。
// 签名必须带 *gorm.DB, 否则 GORM 不认这个钩子, 只会打一条警告然后静默跳过。
func (c *Credential) AfterFind(_ *gorm.DB) error {
	c.HasSecret = c.APIToken != "" || c.SecretKey != ""
	return nil
}
