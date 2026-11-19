//go:build !windows

package main

// platformUpdater 在 macOS / Linux 上只提示新版本, 由用户去下载页手动更新:
// 静默替换 .app 还没做 (要处理目录不可写、App Translocation、从 DMG 卷里运行这几种情况)。
func platformUpdater(appDirs) (func(tag string) string, func(file string) error) {
	return nil, nil
}

func cleanupReplacedExe() {}

func waitForPreviousInstance([]string) {}
