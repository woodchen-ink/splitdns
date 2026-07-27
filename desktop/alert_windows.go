package main

import (
	"syscall"
	"unsafe"
)

// alert 弹一个系统消息框。
// 启动阶段 Wails 还没跑起来, 用不了它的 dialog, 所以直接调 user32。
func alert(title, message string) {
	user32 := syscall.NewLazyDLL("user32.dll")
	messageBox := user32.NewProc("MessageBoxW")

	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	msgPtr, err := syscall.UTF16PtrFromString(message)
	if err != nil {
		return
	}
	// MB_OK | MB_ICONERROR
	const flags = 0x00000000 | 0x00000010
	messageBox.Call(0, uintptr(unsafe.Pointer(msgPtr)), uintptr(unsafe.Pointer(titlePtr)), flags)
}
