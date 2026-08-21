package model

import "time"

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

	AccessToken string `gorm:"column:access_token;size:2048" json:"-"`
	// RefreshToken 为空表示这份登录态已经救不回来了 (被吊销 / 过期), 只能重新授权
	RefreshToken string `gorm:"column:refresh_token;size:2048" json:"-"`
	// ExpiresAt 是 access_token 的过期时刻, 由拿到令牌那一刻加 expires_in 算出
	ExpiresAt time.Time `gorm:"column:expires_at" json:"expiresAt"`
	Scope     string    `gorm:"column:scope;size:256" json:"scope"`
	// LoggedInAt 是最近一次完整走完授权流程的时刻, 刷新令牌不会改它
	LoggedInAt time.Time `gorm:"column:logged_in_at" json:"loggedInAt"`
}

// TableName 显式声明表名, 不依赖 GORM 自动复数化。
func (Account) TableName() string { return "account" }

// DisplayName 挑一个用来称呼用户的名字, 昵称缺失时退到用户名, 再退到邮箱。
func (a *Account) DisplayName() string {
	for _, v := range []string{a.Nickname, a.Username, a.Email} {
		if v != "" {
			return v
		}
	}
	return "未知用户"
}
