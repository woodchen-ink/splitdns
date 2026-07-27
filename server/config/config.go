package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Config 是进程级配置, 全部来自环境变量。业务数据 (域名 / 回源 / 凭据) 在数据库里, 不进这里。
type Config struct {
	Port int
	// DatabasePath SQLite 数据库文件路径, 容器内挂卷持久化
	DatabasePath string
	// Timezone 业务时区, 入口显式加载
	Timezone string
	// StaticRoot Next.js export 产物目录
	StaticRoot string

	// AccessTeamDomain Cloudflare Access 团队域名, 如 czl.cloudflareaccess.com;
	// 留空则不校验 Access JWT, 此时服务只允许绑定回环地址
	AccessTeamDomain string
	// AccessAUD Access 应用的 AUD tag
	AccessAUD string
	// BindHost 监听地址
	BindHost string
	// AllowInsecureBind 显式放行"没有 Access 却监听非回环地址"。
	// 容器跑在只对内网暴露的反代后面时才该打开, 打开即表示由部署方自己保证访问控制
	AllowInsecureBind bool
}

// Load 从环境变量读取配置并校验必填项与危险组合。
// 未配置 Access 却绑定了非回环地址时直接报错, 避免把带密钥的面板裸奔到公网。
func Load() (*Config, error) {
	c := &Config{
		Port:             envInt("PORT", 8080),
		DatabasePath:     envStr("DATABASE_PATH", "/data/splitdns.db"),
		Timezone:         envStr("TZ", "Asia/Shanghai"),
		StaticRoot:       envStr("STATIC_ROOT", "../web/out"),
		AccessTeamDomain: os.Getenv("CF_ACCESS_TEAM_DOMAIN"),
		AccessAUD:        os.Getenv("CF_ACCESS_AUD"),
		BindHost:         envStr("BIND_HOST", "127.0.0.1"),

		AllowInsecureBind: envStr("ALLOW_INSECURE_BIND", "") == "true",
	}

	if c.DatabasePath == "" {
		return nil, fmt.Errorf("DATABASE_PATH 不能为空")
	}
	if c.AccessEnabled() && c.AccessAUD == "" {
		return nil, fmt.Errorf("配置了 CF_ACCESS_TEAM_DOMAIN 就必须同时配置 CF_ACCESS_AUD")
	}
	if !c.AccessEnabled() && !isLoopback(c.BindHost) && !c.AllowInsecureBind {
		return nil, fmt.Errorf(
			"未配置 Cloudflare Access 时只允许监听回环地址 (当前 BIND_HOST=%s)。"+
				"确实跑在受控网络的反代后面时, 设 ALLOW_INSECURE_BIND=true 显式放行", c.BindHost)
	}
	return c, nil
}

// Desktop 构造桌面版配置。
// 桌面版没有环境变量可依赖, 也不需要监听端口与鉴权 —— 请求由 Wails 直接交给 handler,
// 根本不开 TCP 端口, 所以"未配 Access 就只能听回环"那条约束在这里不适用。
func Desktop(dataDir, staticRoot string) *Config {
	return &Config{
		DatabasePath: filepath.Join(dataDir, "splitdns.db"),
		Timezone:     envStr("TZ", "Asia/Shanghai"),
		StaticRoot:   staticRoot,
	}
}

// AccessEnabled 判定是否启用 Cloudflare Access JWT 校验。
func (c *Config) AccessEnabled() bool { return c.AccessTeamDomain != "" }

// Addr 返回监听地址。
func (c *Config) Addr() string { return fmt.Sprintf("%s:%d", c.BindHost, c.Port) }

func isLoopback(host string) bool {
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
