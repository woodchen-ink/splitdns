package database

import (
	"fmt"
	"log/slog"
	"strings"

	"gorm.io/gorm"
)

// dropLegacyRouteFK 重建早期版本带外键约束的 route 表。
//
// route.origin_id 现在允许为 0 —— 表示这条线路用内联落点、不引用回源库。
// 但早期版本的表是带 FOREIGN KEY(origin_id) REFERENCES origin 建出来的,
// 0 指不到任何一行, 写入直接被约束挡掉。SQLite 不能单独 drop 约束, 只能整表重建。
//
// 只在检测到旧约束时才动手, 且先把数据搬到新表再删旧表, 不丢行。
func dropLegacyRouteFK(db *gorm.DB, migrate func() error) error {
	var ddl string
	err := db.Raw("SELECT sql FROM sqlite_master WHERE type='table' AND name='route'").Scan(&ddl).Error
	if err != nil {
		return fmt.Errorf("读取 route 表结构失败: %w", err)
	}
	// 表还不存在, 或者已经是不带外键的新结构
	if ddl == "" || !strings.Contains(strings.ToLower(ddl), "references") {
		return nil
	}

	slog.Info("检测到 route 表带外键约束, 就地重建以支持内联落点")
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("ALTER TABLE route RENAME TO route_legacy").Error; err != nil {
			return fmt.Errorf("重命名旧表失败: %w", err)
		}
		if err := migrate(); err != nil {
			return fmt.Errorf("重建 route 表失败: %w", err)
		}
		// 旧表没有内联落点那几列, 只搬公共列
		err := tx.Exec(`INSERT INTO route (id, hostname_id, line, origin_id)
			SELECT id, hostname_id, line, origin_id FROM route_legacy`).Error
		if err != nil {
			return fmt.Errorf("迁移旧数据失败: %w", err)
		}
		if err := tx.Exec("DROP TABLE route_legacy").Error; err != nil {
			return fmt.Errorf("删除旧表失败: %w", err)
		}
		return nil
	})
}
