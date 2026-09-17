package main

import (
	_ "embed"
	"errors"
	"log/slog"
	"os"
	"strings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/woodchen-ink/splitdns/server/pkg/selfupdate"
	"github.com/woodchen-ink/splitdns/server/service"
)

// version 由 CI 用 -ldflags "-X main.version=vX.Y.Z" 注入。本地构建保持 dev, 不参与自动更新 ——
// 否则开发时跑起来的程序会把自己"升级"成线上版本。
var version = "dev"

// updatePublicKey 是验证发布签名的 ed25519 公钥 (base64), 由 server/tools/updatesign -keygen 生成。
// 文件为空时自动更新关闭: 验不了签的包宁可不装。
//
//go:embed updatekey.pub
var updatePublicKey string

// startUpdater 把本机的安装方式告诉 service, 由它负责定时检查、下载与校验。
func startUpdater(dirs appDirs) {
	opts := service.UpdateOptions{
		CurrentVersion: version,
		DownloadDir:    dirs.Updates,
	}
	if key := strings.TrimSpace(updatePublicKey); key != "" {
		pub, err := selfupdate.ParsePublicKey(key)
		if err != nil {
			slog.Error("内置的更新公钥无效, 自动更新关闭", "err", err)
		} else {
			opts.PublicKey = pub
		}
	}
	opts.AssetName, opts.Apply = platformUpdater(dirs)
	service.StartUpdater(opts)
}

// cleanupAfterUpdate 清掉上一次更新的残留。安装器可能还没退出, 删不掉就留到下次, 不影响运行。
func cleanupAfterUpdate(dirs appDirs) {
	_ = os.RemoveAll(dirs.Updates)
	cleanupReplacedExe()
}

// quitApp 让 Wails 正常退出, 好让安装器 / 新进程接手。
func quitApp() error {
	ctxMu.RLock()
	ctx := appCtx
	ctxMu.RUnlock()
	if ctx == nil {
		return errors.New("窗口尚未就绪, 无法重启")
	}
	wailsruntime.Quit(ctx)
	return nil
}
