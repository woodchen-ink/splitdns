package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	// 把时区数据库嵌进二进制: Windows 自身没有 tzdata, Go 默认去 GOROOT 找,
	// 没装 Go 的机器上 time.LoadLocation 会失败, 进而在启动时 panic
	_ "time/tzdata"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/woodchen-ink/splitdns/server/config"
	"github.com/woodchen-ink/splitdns/server/initapp"
	"github.com/woodchen-ink/splitdns/server/router"
)

// assets 是 web/ 的静态导出产物, 构建时由 web 侧的 copy-to-desktop 脚本拷进来。
//
//go:embed all:frontend/dist
var assets embed.FS

// main 起一个原生窗口, 里面跑的还是服务端那套 handler ——
// 桌面版不监听任何端口, Wails 直接把 webview 的请求交给 handler, 因此也不需要鉴权。
func main() {
	dataDir, err := resolveDataDir()
	if err != nil {
		fatal("", err)
	}
	// GUI 程序没有控制台, 启动失败什么都看不到, 所以先把日志落到文件
	logPath := filepath.Join(dataDir, "splitdns.log")
	if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		defer f.Close()
		log.SetOutput(f)
		os.Stdout = f
		os.Stderr = f
	}

	// nextstatic 按目录读文件, 所以把嵌进二进制的产物先摊到数据目录再交给它,
	// 这样静态托管行为与服务端完全一致 (trailing slash / RSC 头 / 目录索引)
	staticRoot := filepath.Join(dataDir, "web")
	if err := extractAssets(staticRoot); err != nil {
		fatal(logPath, fmt.Errorf("释放前端资源失败: %w", err))
	}

	cfg := config.Desktop(dataDir, staticRoot)
	if err := initapp.InitWith(cfg); err != nil {
		fatal(logPath, err)
	}

	err = wails.Run(&options.App{
		Title:  "splitdns",
		Width:  1280,
		Height: 900,
		AssetServer: &assetserver.Options{
			// 不用 Wails 自带的静态服务: 我们的 handler 已经处理好 /api 与 Next 导出产物
			Handler: router.New(cfg),
		},
	})
	if err != nil {
		fatal(logPath, err)
	}
}

// resolveDataDir 选数据目录。
// 绿色版优先放在 exe 同级的 data/ 下, 拷走整个文件夹就能带走全部配置;
// exe 落在 Program Files 这类只读位置时回退到用户配置目录。
func resolveDataDir() (string, error) {
	exe, err := os.Executable()
	if err == nil {
		beside := filepath.Join(filepath.Dir(exe), "data")
		if writable(beside) {
			return beside, nil
		}
	}

	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("找不到可写的数据目录: %w", err)
	}
	dir := filepath.Join(base, "splitdns")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// writable 试着创建目录并写一个探针文件, 判断该位置能不能落数据。
func writable(dir string) bool {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	probe := filepath.Join(dir, ".write-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return false
	}
	_ = os.Remove(probe)
	return true
}

// extractAssets 把嵌入的前端产物摊到磁盘。
// 已经摊过且内容没变时直接跳过 —— 靠文件数量与索引页大小做个便宜的判断,
// 每次启动全量重写既慢又会让磁盘白白转一遍。
func extractAssets(root string) error {
	sub, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		return err
	}
	if assetsUpToDate(sub, root) {
		return nil
	}
	if err := os.RemoveAll(root); err != nil {
		return err
	}

	return fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(root, filepath.FromSlash(path))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(sub, path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// assetsUpToDate 比较嵌入产物与磁盘上已释放内容的首页大小, 相同就认为不用重来。
func assetsUpToDate(sub fs.FS, root string) bool {
	embedded, err := fs.Stat(sub, "index.html")
	if err != nil {
		return false
	}
	onDisk, err := os.Stat(filepath.Join(root, "index.html"))
	if err != nil {
		return false
	}
	return embedded.Size() == onDisk.Size()
}

// fatal 记录启动失败并弹窗告知, 而不是让窗口一闪而过 ——
// GUI 程序退出得无声无息时, 用户能拿到的信息是零。
func fatal(logPath string, err error) {
	log.Printf("splitdns 启动失败: %v", err)
	msg := fmt.Sprintf("splitdns 启动失败:\n\n%v", err)
	if logPath != "" {
		msg += fmt.Sprintf("\n\n详细日志: %s", logPath)
	}
	alert("splitdns 启动失败", msg)
	os.Exit(1)
}
