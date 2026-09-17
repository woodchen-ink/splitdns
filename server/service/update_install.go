package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/woodchen-ink/splitdns/server/pkg/selfupdate"
)

// ErrNoUpdate 表示当前没有可以自动安装的新版本。
var ErrNoUpdate = errors.New("没有可自动安装的新版本")

// StartInstallUpdate 开始下载并安装新版本, 立即返回; 进度通过 GetUpdateStatus 观察。
//
// 异步是因为下载可能要几分钟, 而 webview 里一个挂着的请求什么反馈都给不了。
func StartInstallUpdate() error {
	updater.mu.Lock()
	defer updater.mu.Unlock()
	st := &updater.status
	if st.State == UpdateDownloading || st.State == UpdateInstalling {
		return nil
	}
	if st.State != UpdateAvailable || !st.CanAutoInstall || st.Latest == nil {
		return ErrNoUpdate
	}
	st.State = UpdateDownloading
	st.Received, st.Total, st.Error = 0, st.Latest.Find(updater.opts.AssetName(st.Latest.Tag)).Size, ""
	go runInstall(updater.opts, st.Latest)
	return nil
}

func runInstall(opts UpdateOptions, rel *selfupdate.Release) {
	file, err := downloadVerified(context.Background(), opts, rel)
	if err == nil {
		setUpdateState(UpdateInstalling, "")
		slog.Info("新版本校验通过, 开始安装", "version", rel.Tag, "file", file)
		err = opts.Apply(file)
	}
	if err != nil {
		slog.Error("自动更新失败", "version", rel.Tag, "err", err)
		// 回到 available: 用户可以再试, 或者改走手动下载
		setUpdateState(UpdateAvailable, err.Error())
	}
}

// downloadVerified 先拿签名过的校验文件, 再下安装包比对摘要。
// 顺序不能反: 校验文件验不过时, 几十 MB 的安装包一个字节都不该落盘。
func downloadVerified(ctx context.Context, opts UpdateOptions, rel *selfupdate.Release) (string, error) {
	name := opts.AssetName(rel.Tag)
	asset := rel.Find(name)
	sumsAsset, sigAsset := rel.Find(selfupdate.ChecksumsAsset), rel.Find(selfupdate.SignatureAsset)
	if asset == nil || sumsAsset == nil || sigAsset == nil {
		return "", ErrNoUpdate
	}

	sums, err := selfupdate.Fetch(ctx, sumsAsset.URL)
	if err != nil {
		return "", err
	}
	sig, err := selfupdate.Fetch(ctx, sigAsset.URL)
	if err != nil {
		return "", err
	}
	digests, err := selfupdate.VerifyChecksums(sums, sig, opts.PublicKey)
	if err != nil {
		return "", err
	}
	want, ok := digests[name]
	if !ok {
		return "", fmt.Errorf("校验文件里没有 %s", name)
	}

	// 下载前清空目录: 上一次失败或已装完的残留没有用处
	if err := os.RemoveAll(opts.DownloadDir); err != nil {
		slog.Warn("清理更新下载目录失败", "dir", opts.DownloadDir, "err", err)
	}
	if err := os.MkdirAll(opts.DownloadDir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(opts.DownloadDir, name)
	got, err := selfupdate.Download(ctx, asset.URL, dst, func(received, total int64) {
		updater.mu.Lock()
		updater.status.Received = received
		if total > 0 {
			updater.status.Total = total
		}
		updater.mu.Unlock()
	})
	if err != nil {
		return "", err
	}
	if got != want {
		_ = os.Remove(dst)
		return "", fmt.Errorf("安装包摘要与签名清单不符, 拒绝安装 (期望 %s, 实际 %s)", want, got)
	}
	return dst, nil
}

func setUpdateState(state UpdateState, errMsg string) {
	updater.mu.Lock()
	defer updater.mu.Unlock()
	updater.status.State = state
	updater.status.Error = errMsg
}
