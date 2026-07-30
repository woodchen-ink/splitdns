package main

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/options"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/woodchen-ink/splitdns/server/service"
)

// CZL Connect 的授权回跳落在 splitdns://callback 上, 由系统按协议名把它交回给这个程序。
//
// 为什么不用 http 回环端口: 桌面版一个端口都不监听, 为了收一次回调专门开个本地监听,
// 既多一个网络入口, 又要处理端口被占用。自定义协议的代价是要在系统里注册处理器,
// 但那件事本来就要做一次 (安装时或首次启动时), 之后不需要任何运行时资源。

// appCtx 由 Wails 在启动时给出, 用来把窗口调到前台。
var (
	ctxMu  sync.RWMutex
	appCtx context.Context
)

// onStartup 记下 Wails 的上下文, 并补做冷启动时收到的授权回调。
//
// 冷启动是指应用没开着的时候点了授权: 系统会拿回调地址当命令行参数把程序拉起来。
// 这种情况下 PKCE 的 verifier 早随上个进程一起没了, 换令牌必定失败 ——
// 但仍然要走一遍, 好让界面上给出"请重新登录"而不是停在一个什么都不说的登录页。
func onStartup(callbackURL string) func(context.Context) {
	return func(ctx context.Context) {
		ctxMu.Lock()
		appCtx = ctx
		ctxMu.Unlock()

		if callbackURL != "" {
			// 起协程: OnStartup 阻塞着窗口显示, 而这里要发两次网络请求
			go handleAuthCallback(callbackURL)
		}
	}
}

// onSecondInstanceLaunch 是正常路径: 应用已经开着, 系统又拿回调地址拉起了第二个进程,
// Wails 的单实例锁把参数转交过来, 第二个进程随即退出。
//
// 参数里没有回调地址时说明用户只是又双击了一次图标, 那就把已经开着的窗口调到前台 ——
// 单实例应用最让人困惑的行为就是"点了没反应"。
func onSecondInstanceLaunch(data options.SecondInstanceData) {
	if url := callbackFromArgs(data.Args); url != "" {
		go handleAuthCallback(url)
		return
	}
	raiseWindow()
}

// handleAuthCallback 把回跳地址交给 service 完成换令牌。
//
// 失败不弹窗: 原因已经记进登录状态, 前端那边正在轮询, 由界面统一呈现。
//
// 窗口在换令牌**之前**就调到前台。浏览器跳到 splitdns:// 之后那个标签页会停在原地不动
// (外部协议跳转不会把页面带走), 用户盯着的是一张不动的页面; 换令牌又要打两次网络请求。
// 先抢焦点, 用户的视线立刻回到应用上, 剩下那一两秒有界面陪着他。
func handleAuthCallback(rawURL string) {
	raiseWindow()
	if err := service.CompleteLogin(context.Background(), rawURL); err != nil {
		slog.Error("处理授权回调失败", "err", err)
	} else {
		slog.Info("授权回调处理完成")
	}
	// 再抢一次: 冷启动时上面那次窗口还没建出来, 什么都没发生
	raiseWindow()
}

// callbackFromArgs 从命令行参数里挑出回调地址。
// 逐个看而不是只认第一个: 参数里可能还夹着系统或安全软件塞进来的东西。
func callbackFromArgs(args []string) string {
	prefix := callbackScheme() + ":"
	for _, arg := range args {
		if strings.HasPrefix(strings.ToLower(arg), prefix) {
			return arg
		}
	}
	return ""
}

// callbackScheme 从登记的回调地址里取出协议名 (splitdns://callback → splitdns)。
// 不写死: 回调地址改了, 注册的协议和识别的参数要跟着一起改, 少改一处就是一条死路。
func callbackScheme() string {
	scheme, _, found := strings.Cut(service.RedirectURI(), ":")
	if !found {
		return ""
	}
	return strings.ToLower(scheme)
}

// raiseWindow 把窗口从最小化 / 后台调回前台。
func raiseWindow() {
	ctxMu.RLock()
	ctx := appCtx
	ctxMu.RUnlock()
	if ctx == nil {
		return
	}
	wailsruntime.WindowUnminimise(ctx)
	wailsruntime.Show(ctx)
}
