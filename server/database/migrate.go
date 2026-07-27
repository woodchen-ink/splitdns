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
// 全程不开事务: AutoMigrate 用的是自己的连接, 包在事务里会看不见事务内未提交的 DDL。
// 代价是中途失败会留下 route_legacy, 因此本函数设计成可重入 —— 下次启动检测到
// route_legacy 就直接走收尾流程, 不会重复重命名, 也不会丢行。
func dropLegacyRouteFK(db *gorm.DB, migrate func() error) error {
	legacy, err := tableExists(db, "route_legacy")
	if err != nil {
		return err
	}

	if !legacy {
		ddl, err := tableDDL(db, "route")
		if err != nil {
			return err
		}
		// 表还不存在, 或者已经是不带外键的新结构
		if ddl == "" || !strings.Contains(strings.ToLower(ddl), "references") {
			return nil
		}
		slog.Info("检测到 route 表带外键约束, 就地重建以支持内联落点")
		if err := db.Exec("ALTER TABLE route RENAME TO route_legacy").Error; err != nil {
			return fmt.Errorf("重命名旧表失败: %w", err)
		}
	} else {
		slog.Info("发现上次未完成的 route 表重建, 继续收尾")
	}

	if err := migrate(); err != nil {
		return fmt.Errorf("重建 route 表失败: %w", err)
	}
	// 旧表没有内联落点那几列, 只搬公共列; OR IGNORE 让重入时不会撞主键
	err = db.Exec(`INSERT OR IGNORE INTO route (id, hostname_id, line, origin_id)
		SELECT id, hostname_id, line, origin_id FROM route_legacy`).Error
	if err != nil {
		return fmt.Errorf("迁移旧数据失败: %w", err)
	}
	if err := db.Exec("DROP TABLE route_legacy").Error; err != nil {
		return fmt.Errorf("删除旧表失败: %w", err)
	}
	return nil
}

// tableExists 判断表是否存在。
func tableExists(db *gorm.DB, name string) (bool, error) {
	var count int64
	err := db.Raw("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&count).Error
	if err != nil {
		return false, fmt.Errorf("查询表 %s 是否存在失败: %w", name, err)
	}
	return count > 0, nil
}

// tableDDL 取建表语句, 表不存在时返回空串。
func tableDDL(db *gorm.DB, name string) (string, error) {
	var ddl string
	err := db.Raw("SELECT COALESCE(sql, '') FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&ddl).Error
	if err != nil {
		return "", fmt.Errorf("读取表 %s 结构失败: %w", name, err)
	}
	return ddl, nil
}
