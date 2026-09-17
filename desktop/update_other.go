//go:build !windows

package main

// platformUpdater 在 macOS / Linux 上只提示新版本, 由用户去下载页手动更新:
// .app 没有签名公证, 自己替换包内容容易被 Gatekeeper 拦成打不开。
func platformUpdater(appDirs) (func(tag string) string, func(file string) error) {
	return nil, nil
}

func cleanupReplacedExe() {}

func waitForPreviousInstance([]string) {}
