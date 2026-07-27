package initapp

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/woodchen-ink/go-web-utils/timex"
	"github.com/woodchen-ink/splitdns/server/config"
	"github.com/woodchen-ink/splitdns/server/database"
)

// Init 完成进程级初始化: 结构化日志、业务时区、数据库。
// 时区加载失败直接退出, 不静默回退 UTC —— 等待计时和记录时间都依赖它。
func Init() (*config.Config, error) {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("加载配置失败: %w", err)
	}
	timex.MustInit(cfg.Timezone)

	if err := database.Init(cfg.DatabasePath); err != nil {
		return nil, err
	}
	return cfg, nil
}
