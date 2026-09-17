package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
)

// appName 是 CZL 厂商目录下本产品的子目录名, 与安装器产品名、可执行文件名一致, 定名后不改。
const appName = "splitdns"

// appDirs 是程序自有文件的全部落点, 都收敛在安装根目录下。
type appDirs struct {
	Root string // 安装根目录, Windows 安装版的 exe 也在这里
	Data string // 数据库
	Logs string
	// Cache 下的东西删了下次启动会自己重建: 摊开的前端产物、WebView2 用户数据
	Cache string
}

// resolveAppDirs 按平台约定解析安装根目录并建好子目录。
//
// 根目录固定, 与 exe 放在哪无关: 绿色版解压到哪都读写同一份数据, 与安装版共用。
// 解析失败直接报错, 不回退到工作目录或 exe 同级 —— 数据悄悄落到别处,
// 用户下次换个方式启动就"丢了全部配置"。
func resolveAppDirs() (appDirs, error) {
	root, err := installRoot()
	if err != nil {
		return appDirs{}, err
	}
	dirs := appDirs{
		Root:  root,
		Data:  filepath.Join(root, "data"),
		Logs:  filepath.Join(root, "logs"),
		Cache: filepath.Join(root, "cache"),
	}
	for _, dir := range []string{dirs.Data, dirs.Logs, dirs.Cache} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return appDirs{}, fmt.Errorf("创建目录 %s 失败: %w", dir, err)
		}
	}
	return dirs, nil
}

// installRoot 返回 Windows %LOCALAPPDATA%\CZL\<项目> / macOS ~/Library/Application Support/CZL/<项目> /
// Linux ~/.local/share/CZL/<项目>。
//
// Windows 不用 os.UserConfigDir: 它返回的是 Roaming, 域环境下会跟着漫游同步, 数据库和缓存不该去那儿。
func installRoot() (string, error) {
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", errors.New("环境变量 LOCALAPPDATA 为空, 无法确定数据目录")
		}
		return filepath.Join(base, "CZL", appName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("取用户主目录失败: %w", err)
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "CZL", appName), nil
	}
	return filepath.Join(home, ".local", "share", "CZL", appName), nil
}

// dbFiles 是一份 SQLite 库在磁盘上的全部文件: WAL 模式下未合并的事务还在 -wal 里, 只搬主文件会丢数据。
var dbFiles = []string{"splitdns.db", "splitdns.db-wal", "splitdns.db-shm"}

// migrateLegacyData 把旧版本放在别处的数据库搬进新数据目录。
//
// 旧版的落点: 绿色版在 exe 同级 data/, 安装版 (装在 Program Files, 旁边不可写) 与 macOS 在
// os.UserConfigDir()/splitdns。新目录里已经有库就什么都不做 —— 新库一定比旧库新, 不能拿旧的盖掉。
// 只搬数据库: 日志与摊开的前端产物留在原处也不影响运行, 删用户目录里的东西宁可少删。
func migrateLegacyData(dataDir string) error {
	if _, err := os.Stat(filepath.Join(dataDir, dbFiles[0])); err == nil {
		return nil
	}
	for _, dir := range legacyDataDirs() {
		if _, err := os.Stat(filepath.Join(dir, dbFiles[0])); err != nil {
			continue
		}
		if err := copyDBFiles(dir, dataDir); err != nil {
			return fmt.Errorf("从 %s 迁移数据库失败: %w", dir, err)
		}
		// 新目录里的副本齐了才删旧的; 删失败只是留了份残余, 下次启动新库已在, 不会再搬
		for _, name := range dbFiles {
			if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				slog.Warn("旧版数据库文件未能删除", "path", filepath.Join(dir, name), "err", err)
			}
		}
		slog.Info("已迁移旧版数据库", "from", dir, "to", dataDir)
		return nil
	}
	return nil
}

// legacyDataDirs 按优先级列出旧版可能放数据库的目录。exe 同级在前: 用户正在跑的就是那份绿色版。
func legacyDataDirs() []string {
	var dirs []string
	if runtime.GOOS != "darwin" {
		if exe, err := os.Executable(); err == nil {
			dirs = append(dirs, filepath.Join(filepath.Dir(exe), "data"))
		}
	}
	if base, err := os.UserConfigDir(); err == nil {
		dirs = append(dirs, filepath.Join(base, appName))
	}
	return dirs
}

// copyDBFiles 把库的全部文件复制到目标目录, 要么全到位要么一个不留。
// 只到了主文件没到 -wal 时, 下次启动会认定新库已存在而不再搬, 未合并的事务就永久丢了。
func copyDBFiles(srcDir, dstDir string) error {
	var copied []string
	for _, name := range dbFiles {
		src := filepath.Join(srcDir, name)
		if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
			continue // -wal / -shm 在干净关闭后可能没有
		}
		dst := filepath.Join(dstDir, name)
		if err := copyFile(src, dst); err != nil {
			for _, path := range copied {
				_ = os.Remove(path)
			}
			return err
		}
		copied = append(copied, dst)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}
