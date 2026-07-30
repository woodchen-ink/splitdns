package service

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/glebarez/go-sqlite" // 校验上传文件时要用纯 Go 的 sqlite 驱动直接打开
	"github.com/woodchen-ink/go-web-utils/timex"
	"github.com/woodchen-ink/splitdns/server/database"
)

// ExportDatabase 导出一份数据库快照, 返回临时文件路径与清理函数。
//
// 用 VACUUM INTO 而不是直接拷 .db: WAL 模式下未合并的事务还在 -wal 文件里,
// 直接拷会丢掉最近的改动, 而且拷的瞬间可能正好落在一次写入中间。
func ExportDatabase() (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "splitdns-export")
	if err != nil {
		return "", nil, fmt.Errorf("创建临时目录失败: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(dir) }

	target := filepath.Join(dir, "splitdns.db")
	// SQLite 的 VACUUM INTO 只接受字符串字面量, 单引号需要按 SQL 规则翻倍转义
	quoted := "'" + strings.ReplaceAll(target, "'", "''") + "'"
	if err := database.DB.Exec("VACUUM INTO " + quoted).Error; err != nil {
		cleanup()
		return "", nil, fmt.Errorf("导出数据库失败: %w", err)
	}
	if err := stripAccount(target); err != nil {
		cleanup()
		return "", nil, err
	}
	return target, cleanup, nil
}

// stripAccount 把导出副本里的登录态删掉。
//
// 备份是业务数据, 不是身份: 库里那份 refresh_token 拿到手就能以本人身份调 CZL Connect,
// 而备份文件是拿来换机器、发给人看的。平台密钥留着 (换机器就是要它们), 登录态换台机器重登一次即可。
func stripAccount(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("打开导出副本失败: %w", err)
	}
	defer db.Close()

	// 早于登录功能的库里没有这张表, 那种情况本来就没什么可删
	var name string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='account'").Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("检查导出副本失败: %w", err)
	}
	if _, err := db.Exec("DELETE FROM account"); err != nil {
		return fmt.Errorf("清除导出副本里的登录态失败: %w", err)
	}
	return nil
}

// ExportFileName 生成带日期的下载文件名, 便于区分不同时间导出的备份。
func ExportFileName() string {
	return fmt.Sprintf("splitdns-%s.db", timex.Now().Format("20060102-1504"))
}

// importTables 是导入文件必须具备的表。缺任何一张都说明这不是本工具的库,
// 与其导进去之后一路报错, 不如在这里就挡掉。
var importTables = []string{"credential", "origin", "hostname", "route"}

// ImportDatabase 用上传的库文件替换当前库。
//
// 替换前会把现有库另存一份 (同目录, 带时间戳), 导错了还能自己换回来 ——
// 这是唯一一处会整体覆盖用户数据的操作, 不留后路不合适。
func ImportDatabase(uploadPath string) (backupPath string, err error) {
	if err := validateImport(uploadPath); err != nil {
		return "", err
	}

	current := database.Path()
	if current == "" {
		return "", fmt.Errorf("当前没有打开的数据库")
	}
	backupPath = current + timex.Now().Format(".20060102-1504") + ".bak"
	if err := copyFile(current, backupPath); err != nil {
		return "", fmt.Errorf("备份现有数据库失败: %w", err)
	}

	if err := database.Close(); err != nil {
		return "", fmt.Errorf("关闭数据库失败: %w", err)
	}
	// WAL / SHM 是旧库的附属文件, 不清掉会和新库对不上
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Remove(current + suffix)
	}
	if err := copyFile(uploadPath, current); err != nil {
		// 覆盖失败时把备份换回去, 不能让用户既丢了新的也没了旧的
		_ = copyFile(backupPath, current)
		_ = database.Init(current)
		return "", fmt.Errorf("写入新数据库失败: %w", err)
	}

	// 重新打开顺带跑一次迁移: 导入的库可能来自旧版本, 缺列缺表在这里补上
	if err := database.Init(current); err != nil {
		return "", fmt.Errorf("导入的库打不开: %w", err)
	}
	return backupPath, nil
}

// sqliteMagic 是 SQLite 文件头。先认它能把"这压根不是数据库"和"是数据库但不是我们的"
// 分开报, 否则空文件会被当成合法空库, 报出来的是"缺 credential 表", 方向完全带偏。
var sqliteMagic = append([]byte("SQLite format 3"), 0)

// validateImport 确认这是一个本工具的库文件, 而不是随便一个 SQLite 文件。
func validateImport(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("读不到上传的文件: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("上传的文件是空的")
	}

	head := make([]byte, len(sqliteMagic))
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打不开上传的文件: %w", err)
	}
	n, _ := io.ReadFull(f, head)
	f.Close()
	if n < len(sqliteMagic) || !bytes.Equal(head, sqliteMagic) {
		return fmt.Errorf("这不是一个 SQLite 数据库文件 (大小 %d 字节)", info.Size())
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("打不开上传的文件: %w", err)
	}
	defer db.Close()

	for _, table := range importTables {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name = ?", table,
		).Scan(&name)
		if err != nil {
			return fmt.Errorf("这个文件里没有 %s 表, 不像是 splitdns 的数据库", table)
		}
	}
	return nil
}

// copyFile 整文件拷贝。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
