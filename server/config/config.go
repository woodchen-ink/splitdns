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
}

// New 构造配置。
// 这个工具只有桌面版一种形态: 不监听端口、请求由 Wails 直接交给 handler,
// 所以既没有端口与监听地址, 也不需要鉴权相关的配置项。
func New(dataDir, staticRoot string) *Config {
	return &Config{
		DatabasePath: filepath.Join(dataDir, "splitdns.db"),
		Timezone:     envStr("TZ", "Asia/Shanghai"),
		StaticRoot:   staticRoot,
	}
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
