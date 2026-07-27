package database

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"github.com/woodchen-ink/splitdns/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB 是全局数据库句柄, 由 Init 赋值。
var DB *gorm.DB

// Init 打开 SQLite 数据库文件并执行自动迁移。
// 目录不存在时自动创建, 便于容器里直接挂一个空卷。
func Init(path string) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建数据目录失败: %w", err)
		}
	}

	// _pragma 参数: WAL 提升并发读写表现, busy_timeout 避免写锁瞬时冲突直接报错
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	if err := db.AutoMigrate(
		&model.Credential{},
		&model.Origin{},
		&model.Hostname{},
		&model.Route{},
	); err != nil {
		return fmt.Errorf("自动迁移失败: %w", err)
	}
	DB = db
	return nil
}
