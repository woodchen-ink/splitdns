//go:build !windows

package main

// registerURLScheme 在非 Windows 平台什么都不做。
//
// macOS 的协议注册是打包信息的一部分, 写在 build/darwin/Info.plist 的 CFBundleURLTypes 里,
// 由系统在安装 .app 时登记, 运行时改不了。
// Linux 需要装一份 .desktop 文件, 而这个工具没有 Linux 发行形态。
//
// 两种情况下协议都可能不通, 所以登录页始终留着"手动粘贴回调地址"那条路。
func registerURLScheme(string) error { return nil }
