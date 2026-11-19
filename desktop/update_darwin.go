//go:build darwin

package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/woodchen-ink/splitdns/server/service"
	"golang.org/x/sys/unix"
)

// stagePrefix 是原地替换时在 .app 同级建的临时目录前缀: 与 .app 同卷才能 rename, 也方便下次启动认回来清掉。
const stagePrefix = ".splitdns-update-"

// bundleNameInZip 是发布 zip 里 .app 的名字 (CI 用 ditto --keepParent 打包), 与用户本地改没改名无关。
const bundleNameInZip = "splitdns.app"

// platformUpdater 在 macOS 上用钉过公证票据的 universal zip 更新。
//
// .app 所在目录可写时原地替换并重启; 否则 (系统把 .app 挪到临时位置运行 / 在 DMG 卷里运行 / 目录无写权限)
// 只能把校验过的新版本在访达里显示出来, 让用户自己拖进「应用程序」。
func platformUpdater(appDirs) platformUpdate {
	app, err := currentBundle()
	if err != nil {
		slog.Warn("不在 .app 包里运行, 自动安装不可用", "err", err)
		return platformUpdate{}
	}
	pu := platformUpdate{
		assetName: func(tag string) string { return "splitdns-" + tag + "-macos-universal.zip" },
	}
	if reason := manualReason(app); reason != "" {
		slog.Info("当前位置无法原地替换, 自动更新改为交给用户", "app", app, "reason", reason)
		pu.manualReason = reason
		pu.apply = revealBundle
		return pu
	}
	pu.apply = func(file string) error { return replaceBundle(file, app) }
	return pu
}

// currentBundle 由可执行文件位置倒推 .app 根目录 (xxx.app/Contents/MacOS/<exe>)。
func currentBundle() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	macOS := filepath.Dir(exe)
	app := filepath.Dir(filepath.Dir(macOS))
	if filepath.Base(macOS) != "MacOS" || !strings.HasSuffix(app, ".app") {
		return "", fmt.Errorf("%s 不在 .app/Contents/MacOS 下", exe)
	}
	return app, nil
}

// manualReason 判定能不能原地替换, 不能时返回给用户看的原因。
func manualReason(app string) string {
	parent := filepath.Dir(app)
	switch {
	// App Translocation: 从「下载」等位置直接打开、没拖进「应用程序」的 .app 会被挪到只读的随机路径运行
	case strings.Contains(app, "/AppTranslocation/"):
		return "程序没有放进「应用程序」文件夹, macOS 把它放在临时位置运行, 无法原地替换。把 splitdns 拖进「应用程序」后再打开, 以后就能自动更新"
	case unix.Access(parent, unix.W_OK) != nil && strings.HasPrefix(app, "/Volumes/"):
		return "程序正在磁盘映像 (DMG) 里运行, 无法原地替换。把 splitdns 拖进「应用程序」后再打开, 以后就能自动更新"
	case unix.Access(parent, unix.W_OK) != nil:
		return "没有 " + parent + " 的写权限, 无法原地替换"
	}
	return ""
}

// replaceBundle 原地替换 .app 并重启。
//
// 新包先解到 .app 同级的临时目录 (同卷 rename 才是原子的), 验过签名再换:
// 旧 .app 挪进临时目录腾位置, 新 .app 挪进来; 挪新包失败就把旧的挪回去, 不能让用户落得一个 .app 都没有。
// 正在运行的程序被挪走不影响它继续跑, 临时目录 (连同旧 .app) 由新版本启动时清掉。
func replaceBundle(zipFile, app string) error {
	stage, err := os.MkdirTemp(filepath.Dir(app), stagePrefix)
	if err != nil {
		return fmt.Errorf("无法在 %s 旁边建临时目录: %w", app, err)
	}
	newApp := filepath.Join(stage, bundleNameInZip)
	if err := extractBundle(zipFile, stage, newApp); err != nil {
		_ = os.RemoveAll(stage)
		return err
	}
	// 摘要已经过 ed25519 签名清单校验; 这里验代码签名, 是为了不把一个 Gatekeeper 不放行的包换上去
	if out, err := exec.Command("/usr/bin/codesign", "--verify", "--deep", "--strict", newApp).CombinedOutput(); err != nil {
		_ = os.RemoveAll(stage)
		return fmt.Errorf("新版本代码签名校验失败: %v: %s", err, strings.TrimSpace(string(out)))
	}

	old := filepath.Join(stage, "old.app")
	if err := os.Rename(app, old); err != nil {
		_ = os.RemoveAll(stage)
		return fmt.Errorf("无法替换程序 (目录可能不可写): %w", err)
	}
	if err := os.Rename(newApp, app); err != nil {
		if rbErr := os.Rename(old, app); rbErr != nil {
			slog.Error("替换失败且旧版本挪不回去", "old", old, "err", rbErr)
			return fmt.Errorf("写入新版本失败, 旧版本留在 %s: %w", old, err)
		}
		_ = os.RemoveAll(stage)
		return fmt.Errorf("写入新版本失败: %w", err)
	}

	// -n: 老进程还开着, 不带它 open 只会把老窗口激活, 不会起新进程
	cmd := exec.Command("/usr/bin/open", "-n", app, "--args", waitPidFlag+strconv.Itoa(os.Getpid()))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("新版本已就位但启动失败, 请手动打开: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return quitApp()
}

// revealBundle 把新版本解到下载目录旁, 在访达里选中它, 由用户拖进「应用程序」替换。
// 解在更新下载目录 (Application Support 下) 而不是「下载」: 后者要过隐私授权弹窗。
func revealBundle(zipFile string) error {
	dir := strings.TrimSuffix(zipFile, filepath.Ext(zipFile))
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	newApp := filepath.Join(dir, bundleNameInZip)
	if err := extractBundle(zipFile, dir, newApp); err != nil {
		return err
	}
	if out, err := exec.Command("/usr/bin/open", "-R", newApp).CombinedOutput(); err != nil {
		return fmt.Errorf("新版本在 %s, 但打不开访达: %v: %s", newApp, err, strings.TrimSpace(string(out)))
	}
	return &service.ManualInstallError{
		Hint: "新版本已在访达中选中。退出 splitdns, 把它拖进「应用程序」替换旧版本后重新打开 (数据不受影响)",
	}
}

// extractBundle 用 ditto 解压: .app 里有符号链接、可执行位与扩展属性, Go 的 archive/zip 还原不全。
func extractBundle(zipFile, dir, wantApp string) error {
	if out, err := exec.Command("/usr/bin/ditto", "-x", "-k", zipFile, dir).CombinedOutput(); err != nil {
		return fmt.Errorf("解压更新包失败: %v: %s", err, strings.TrimSpace(string(out)))
	}
	if st, err := os.Stat(wantApp); err != nil || !st.IsDir() {
		return errors.New("更新包里没有 " + bundleNameInZip)
	}
	return nil
}

// cleanupReplacedExe 删掉上一次原地替换留下的临时目录 (里面是旧版本的 .app)。
func cleanupReplacedExe() {
	app, err := currentBundle()
	if err != nil {
		return
	}
	stages, _ := filepath.Glob(filepath.Join(filepath.Dir(app), stagePrefix+"*"))
	for _, dir := range stages {
		if err := os.RemoveAll(dir); err != nil {
			slog.Warn("清理更新残留失败", "dir", dir, "err", err)
		}
	}
}

// waitForPreviousInstance 处理 --wait-pid: 最多等 30 秒, 超时也照常启动 (最坏是被单实例锁转交后退出)。
func waitForPreviousInstance(args []string) {
	pid, ok := waitPidArg(args)
	if !ok {
		return
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if err := unix.Kill(pid, 0); errors.Is(err, unix.ESRCH) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}
