package main

import (
	"archive/zip"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
)

// waitPidFlag 是绿色版替换自身后拉起新进程时带的参数: 新进程先等老进程退干净,
// 否则单实例锁还在老进程手里, 新进程会把自己当成"第二个实例"转交参数后直接退出。
const waitPidFlag = "--wait-pid="

// platformUpdater 按安装方式决定更新包与安装手段。
//
// exe 就在安装根目录下 = 安装版, 用新安装器静默覆盖 (按用户安装, 不弹 UAC);
// 否则是绿色版, 就地替换 exe。发布只出 amd64, 其他架构只能手动下载。
func platformUpdater(dirs appDirs) (func(tag string) string, func(file string) error) {
	if runtime.GOARCH != "amd64" {
		return nil, nil
	}
	exe, err := currentExe()
	if err != nil {
		slog.Warn("取可执行文件路径失败, 自动安装不可用", "err", err)
		return nil, nil
	}
	if strings.EqualFold(filepath.Clean(filepath.Dir(exe)), filepath.Clean(dirs.Root)) {
		return func(tag string) string { return "splitdns-" + tag + "-windows-amd64-installer.exe" },
			applyInstaller
	}
	return func(tag string) string { return "splitdns-" + tag + "-windows-amd64-portable.zip" },
		func(file string) error { return applyPortable(file, exe) }
}

func currentExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// applyInstaller 静默运行新安装器后退出。
// 安装器会等本进程释放 exe 再覆盖, 装完带 /relaunch 把新版本拉起来 (见 project.nsi)。
func applyInstaller(file string) error {
	if err := exec.Command(file, "/S", "/relaunch").Start(); err != nil {
		return fmt.Errorf("启动安装器失败: %w", err)
	}
	return quitApp()
}

// applyPortable 就地替换绿色版的 exe 并重启。
//
// Windows 不许覆盖正在运行的 exe, 但允许给它改名: 先挪成 .old 腾出位置, 新文件写进去,
// 新进程启动后删掉 .old。写新文件失败时把 .old 挪回来, 不能让用户落得一个 exe 都没有。
func applyPortable(zipFile, exe string) error {
	newExe := exe + ".new"
	// zip 里固定叫 splitdns.exe, 与本地文件名无关: 用户可能把绿色版改过名
	if err := extractExe(zipFile, "splitdns.exe", newExe); err != nil {
		return err
	}
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		_ = os.Remove(newExe)
		return fmt.Errorf("无法替换程序文件 (目录可能不可写): %w", err)
	}
	if err := os.Rename(newExe, exe); err != nil {
		_ = os.Rename(old, exe)
		_ = os.Remove(newExe)
		return fmt.Errorf("写入新版本失败: %w", err)
	}
	cmd := exec.Command(exe, waitPidFlag+strconv.Itoa(os.Getpid()))
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("新版本已就位但启动失败, 请手动打开: %w", err)
	}
	return quitApp()
}

// extractExe 从绿色版 zip 里取出 exe。按 zip 内的文件名找, 不认目录层级。
func extractExe(zipFile, name, dst string) error {
	zr, err := zip.OpenReader(zipFile)
	if err != nil {
		return fmt.Errorf("打开更新包失败: %w", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if !strings.EqualFold(filepath.Base(f.Name), name) || f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			_ = os.Remove(dst)
			return err
		}
		return out.Close()
	}
	return fmt.Errorf("更新包里没有 %s", name)
}

// cleanupReplacedExe 删掉绿色版替换时留下的 .old。
func cleanupReplacedExe() {
	if exe, err := currentExe(); err == nil {
		_ = os.Remove(exe + ".old")
	}
}

// waitForPreviousInstance 处理 --wait-pid: 最多等 30 秒, 超时也照常启动 (最坏是被单实例锁转交后退出)。
func waitForPreviousInstance(args []string) {
	for _, arg := range args {
		pidText, ok := strings.CutPrefix(arg, waitPidFlag)
		if !ok {
			continue
		}
		pid, err := strconv.Atoi(pidText)
		if err != nil || pid <= 0 {
			return
		}
		h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
		if err != nil {
			return // 进程已经不在了
		}
		defer windows.CloseHandle(h)
		_, _ = windows.WaitForSingleObject(h, 30_000)
		return
	}
}
