package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	// 把时区数据库嵌进二进制: Windows 自身没有 tzdata, Go 默认去 GOROOT 找,
	// 没装 Go 的机器上 time.LoadLocation 会失败, 进而在启动时 panic
	_ "time/tzdata"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
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
	dirs, err := resolveAppDirs()
	if err != nil {
		fatal("", err)
	}
	// GUI 程序没有控制台, 启动失败什么都看不到, 所以先把日志落到文件
	logPath := filepath.Join(dirs.Logs, "splitdns.log")
	if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		defer f.Close()
		log.SetOutput(f)
		os.Stdout = f
		os.Stderr = f
	}

	if err := migrateLegacyData(dirs.Data); err != nil {
		fatal(logPath, err)
	}

	// nextstatic 按目录读文件, 所以把嵌进二进制的产物先摊到磁盘再交给它,
	// 这样静态托管行为与服务端完全一致 (trailing slash / RSC 头 / 目录索引)
	staticRoot := filepath.Join(dirs.Cache, "web")
	if err := extractAssets(staticRoot); err != nil {
		fatal(logPath, fmt.Errorf("释放前端资源失败: %w", err))
	}

	cfg := config.New(dirs.Data, staticRoot)
	if err := initapp.Init(cfg); err != nil {
		fatal(logPath, err)
	}

	// 注册失败不拦启动: 登录页上还留着"手动粘贴回调地址"这条路, 为了一条注册表项
	// 让整个工具打不开不划算
	if scheme := callbackScheme(); scheme != "" {
		if err := registerURLScheme(scheme); err != nil {
			slog.Warn("注册回调协议失败, 授权回跳将无法自动回到应用", "scheme", scheme, "err", err)
		}
	}
	// 冷启动时系统会把回调地址当命令行参数塞进来
	callbackURL := callbackFromArgs(os.Args[1:])

	err = wails.Run(&options.App{
		Title:  "splitdns",
		Width:  1280,
		Height: 900,
		AssetServer: &assetserver.Options{
			// 不用 Wails 自带的静态服务: 我们的 handler 已经处理好 /api 与 Next 导出产物
			Handler: router.New(cfg),
		},
		OnStartup: onStartup(callbackURL),
		// 单实例锁是授权回跳的正常通路: 系统拉起的第二个进程只负责把回调地址转交给
		// 已经开着的这个 —— PKCE 的 verifier 在它内存里, 换不换得到令牌全看这一转交
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "splitdns-single-instance",
			OnSecondInstanceLaunch: onSecondInstanceLaunch,
		},
		Windows: &windows.Options{
			// 默认落在 %APPDATA%\<exe 名> (Roaming), 收回安装根目录; 登录态不在 webview 里, 丢了也无妨
			WebviewUserDataPath: filepath.Join(dirs.Cache, "webview2"),
		},
		Mac: &mac.Options{
			// macOS 不走命令行参数: 系统把 URL 直接送给正在运行的 app
			OnUrlOpen: handleAuthCallback,
		},
	})
	if err != nil {
		fatal(logPath, err)
	}
}

// stampFile 存着上次释放的那份产物的内容指纹。
const stampFile = ".asset-stamp"

// extractAssets 把嵌入的前端产物摊到磁盘。
//
// 是否需要重新释放, 按整份产物的内容哈希判断。别拿"首页大小变没变"之类的近似条件糊弄:
// 改了子页面而首页没动时它判不出来, 结果就是新版二进制配着上一版前端跑, 症状离病灶十万八千里。
func extractAssets(root string) error {
	sub, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		return err
	}
	// Wails 在产物目录缺失时会塞一个占位 index.html 让编译通过, 装出来就是个空壳。
	// 与其让用户对着一张占位页发懵, 不如启动即报错并指出是怎么构建坏的。
	if _, err := fs.Stat(sub, "_next"); err != nil {
		return fmt.Errorf("二进制里没有真正的前端产物, 这个包构建坏了 " +
			"(构建时必须让 wails 跑 frontend:build, 别带 -s 跳过)")
	}

	stamp, err := assetsStamp(sub)
	if err != nil {
		return err
	}
	if current, err := os.ReadFile(filepath.Join(root, stampFile)); err == nil && string(current) == stamp {
		return nil
	}

	if err := os.RemoveAll(root); err != nil {
		return err
	}
	err = fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
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
	if err != nil {
		return err
	}
	// 指纹最后写: 中途失败时下次启动会发现它对不上, 重新释放一遍
	return os.WriteFile(filepath.Join(root, stampFile), []byte(stamp), 0o644)
}

// assetsStamp 算出嵌入产物的内容指纹。
// 路径和内容都参与, 所以任何一个文件改了都会变。几 MB 的产物走一遍毫秒级, 不值得为它省。
func assetsStamp(sub fs.FS) (string, error) {
	h := sha256.New()
	err := fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		fmt.Fprintf(h, "%s\x00", path)
		data, err := fs.ReadFile(sub, path)
		if err != nil {
			return err
		}
		h.Write(data)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
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
