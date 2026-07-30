package model

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

// AccountRowID 是登录态固定占用的主键。
// 单行表: 这台机器前只坐着一个人, 允许多行只会让"哪一行才算当前登录"变成一个新问题。
const AccountRowID = 1

// Account 是当前登录的 CZL Connect 账号, 连同它的令牌一起落库 ——
// 桌面工具关掉再开是常态, 登录态不持久化的话每次启动都要重走一遍浏览器授权。
//
// 令牌字段一律 json:"-": 前端只需要知道"登录着谁", 令牌拿到手也没有用途。
type Account struct {
	ID        uint      `gorm:"primaryKey" json:"-"`
	UpdatedAt time.Time `json:"-"`

	// RemoteID 是 CZL Connect 侧的用户 ID
	RemoteID int64  `gorm:"column:remote_id;not null" json:"remoteId"`
	Username string `gorm:"column:username;size:128" json:"username"`
	Nickname string `gorm:"column:nickname;size:128" json:"nickname"`
	Email    string `gorm:"column:email;size:256" json:"email"`
	Avatar   string `gorm:"column:avatar;size:512" json:"avatar"`
	// GroupsRaw 是接口原样返回的逗号分隔分组串, 拆开后的形态走 Groups
	GroupsRaw string `gorm:"column:groups;size:512" json:"-"`

	AccessToken string `gorm:"column:access_token;size:2048" json:"-"`
	// RefreshToken 为空表示这份登录态已经救不回来了 (被吊销 / 过期), 只能重新授权
	RefreshToken string `gorm:"column:refresh_token;size:2048" json:"-"`
	// ExpiresAt 是 access_token 的过期时刻, 由拿到令牌那一刻加 expires_in 算出
	ExpiresAt time.Time `gorm:"column:expires_at" json:"expiresAt"`
	Scope     string    `gorm:"column:scope;size:256" json:"scope"`
	// LoggedInAt 是最近一次完整走完授权流程的时刻, 刷新令牌不会改它
	LoggedInAt time.Time `gorm:"column:logged_in_at" json:"loggedInAt"`

	// Groups 是 GroupsRaw 拆开后的派生字段, 只出现在响应里
	Groups []string `gorm:"-" json:"groups"`
}

// TableName 显式声明表名, 不依赖 GORM 自动复数化。
func (Account) TableName() string { return "account" }

// AfterFind 填充派生字段。
// 签名必须带 *gorm.DB, 否则 GORM 不认这个钩子, 只会打一条警告然后静默跳过。
func (a *Account) AfterFind(_ *gorm.DB) error {
	a.Groups = splitGroups(a.GroupsRaw)
	return nil
}

// DisplayName 挑一个用来称呼用户的名字, 昵称缺失时退到用户名, 再退到邮箱。
func (a *Account) DisplayName() string {
	for _, v := range []string{a.Nickname, a.Username, a.Email} {
		if v != "" {
			return v
		}
	}
	return "未知用户"
}

// splitGroups 拆分组串。空串拆出来是空切片而不是 [""], 免得前端渲染出一个空徽标。
func splitGroups(raw string) []string {
	parts := strings.Split(raw, ",")
	groups := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			groups = append(groups, p)
		}
	}
	return groups
}
