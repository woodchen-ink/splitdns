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

// currentPath 记住当前库文件位置, 导入时要先关掉它再换文件。
var currentPath string

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
		// 不建外键约束: route.origin_id 为 0 表示这条线路用内联落点、不引用回源库,
		// 有约束的话这个 0 会被直接挡掉。引用完整性由 service 层在删除时自己把关
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}

	// 新增 model 必须同步登记到这里, 漏了不会有编译错误, 只会在运行时报 no such table
	migrate := func() error {
		return db.AutoMigrate(
			&model.Account{},
			&model.Credential{},
			&model.Origin{},
			&model.Hostname{},
			&model.Route{},
			&model.Plan{},
			&model.Step{},
		)
	}
	if err := dropLegacyRouteFK(db, migrate); err != nil {
		return err
	}
	if err := migrate(); err != nil {
		return fmt.Errorf("自动迁移失败: %w", err)
	}

	DB = db
	currentPath = path
	return nil
}

// Path 返回当前库文件路径。
func Path() string { return currentPath }

// Close 关闭当前连接。换库文件前必须先关, 否则 Windows 上文件被占用改不动,
// 而且已打开的句柄还指着旧 inode。
func Close() error {
	if DB == nil {
		return nil
	}
	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}
	DB = nil
	return sqlDB.Close()
}
