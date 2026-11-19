//go:build !windows && !darwin

package main

// platformUpdater 在 Linux 上只提示新版本: 这个工具没有 Linux 发行形态, 没有包可装。
func platformUpdater(appDirs) platformUpdate { return platformUpdate{} }

func cleanupReplacedExe() {}

func waitForPreviousInstance([]string) {}
