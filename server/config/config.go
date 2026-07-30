package config

import (
	"os"
	"path/filepath"
)

// Config 是进程级配置。业务数据 (域名 / 回源 / 凭据) 在数据库里, 不进这里。
type Config struct {
	// DatabasePath SQLite 数据库文件路径
	DatabasePath string
	// Timezone 业务时区, 入口显式加载
	Timezone string
	// StaticRoot Next.js export 产物目录
	StaticRoot string
	// OAuth CZL Connect 接入参数
	OAuth OAuthConfig
}

// OAuthConfig 是这个应用在 CZL Connect 后台登记的接入参数。
//
// 这里没有 client_secret: 后台把客户端认证方式登记成了 Public Client,
// 换令牌靠 PKCE。桌面程序里的任何"密钥"用户都能从二进制里挖出来, 那不叫密钥。
type OAuthConfig struct {
	ClientID    string
	RedirectURI string
	Scope       string
}

// New 构造配置。
// 这个工具只有桌面版一种形态: 不监听端口、请求由 Wails 直接交给 handler,
// 因此没有端口与监听地址; 登录态是本机的, 也不涉及会话密钥之类的服务端配置。
func New(dataDir, staticRoot string) *Config {
	return &Config{
		DatabasePath: filepath.Join(dataDir, "splitdns.db"),
		Timezone:     envStr("TZ", "Asia/Shanghai"),
		StaticRoot:   staticRoot,
		OAuth: OAuthConfig{
			ClientID: envStr("CZL_CLIENT_ID", "client_50469374"),
			// 回调地址必须与后台登记的完全一致。自定义协议而不是 http 回环端口:
			// 桌面版一个端口都不监听, 为了收一次回调专门开一个本地监听不划算
			RedirectURI: envStr("CZL_REDIRECT_URI", "splitdns://callback"),
			Scope:       envStr("CZL_SCOPE", "read"),
		},
	}
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
