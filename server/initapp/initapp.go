package initapp

import (
	"log/slog"
	"os"

	"github.com/woodchen-ink/go-web-utils/timex"
	"github.com/woodchen-ink/splitdns/server/config"
	"github.com/woodchen-ink/splitdns/server/database"
)

// Init 完成进程级初始化: 结构化日志、业务时区、数据库。
// 时区加载失败直接退出, 不静默回退 UTC —— 等待计时和记录时间都依赖它。
func Init(cfg *config.Config) error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	timex.MustInit(cfg.Timezone)
	return database.Init(cfg.DatabasePath)
}
