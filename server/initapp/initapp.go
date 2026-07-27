package initapp

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/woodchen-ink/go-web-utils/timex"
	"github.com/woodchen-ink/splitdns/server/config"
	"github.com/woodchen-ink/splitdns/server/database"
)

// Init 从环境变量加载配置并完成进程级初始化, 服务端用。
func Init() (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("加载配置失败: %w", err)
	}
	if err := InitWith(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// InitWith 用给定配置完成进程级初始化: 结构化日志、业务时区、数据库。
// 桌面版自己构造配置后走这里, 不经过环境变量。
// 时区加载失败直接退出, 不静默回退 UTC —— 等待计时和记录时间都依赖它。
func InitWith(cfg *config.Config) error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	timex.MustInit(cfg.Timezone)
	return database.Init(cfg.DatabasePath)
}
