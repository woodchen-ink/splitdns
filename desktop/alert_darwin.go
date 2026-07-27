package main

import (
	"os/exec"
	"strings"
)

// alert 用系统对话框告知启动失败。
// macOS 上双击 .app 启动时没有终端, 日志和 stderr 用户都看不到,
// 所以这里借 osascript 弹一个框 —— 与 Windows 那边的行为对齐。
func alert(title, message string) {
	script := "display dialog " + quote(message) +
		" with title " + quote(title) +
		` buttons {"好"} default button 1 with icon stop`
	_ = exec.Command("osascript", "-e", script).Run()
}

// quote 把字符串转成 AppleScript 字面量, 转义反斜杠与引号。
func quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
