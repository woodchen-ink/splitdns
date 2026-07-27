//go:build !windows

package main

import "log"

// alert 在非 Windows 平台上退回日志输出 —— 这些平台通常从终端启动, 看得到 stderr。
func alert(title, message string) {
	log.Printf("%s: %s", title, message)
}
