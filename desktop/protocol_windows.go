package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

// registerURLScheme 把 splitdns:// 注册成由本程序处理。
//
// 写 HKCU 而不是 HKLM: 不需要管理员权限, 绿色版双击就能用, 也不会影响同机器上的其他用户。
//
// 每次启动都核一遍 exe 路径。绿色版被挪过目录、或者装到了新位置之后, 注册表里那条会指向
// 一个不存在的文件 —— 症状是浏览器点完授权"什么都没发生", 离病灶十万八千里。
func registerURLScheme(scheme string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("取可执行文件路径失败: %w", err)
	}
	// %1 是系统传进来的完整回调地址, 必须带引号: 查询串里的 & 不引起来会被命令行拆断
	command := `"` + exe + `" "%1"`

	root := `Software\Classes\` + scheme
	key, _, err := registry.CreateKey(registry.CURRENT_USER, root, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("创建注册表项失败: %w", err)
	}
	defer key.Close()
	if err := key.SetStringValue("", "URL:"+scheme+" Protocol"); err != nil {
		return fmt.Errorf("写协议描述失败: %w", err)
	}
	// 这个空值就是"我是一个 URL 协议"的标记, 少了它系统不会把 splitdns:// 交过来
	if err := key.SetStringValue("URL Protocol", ""); err != nil {
		return fmt.Errorf("写协议标记失败: %w", err)
	}

	cmdKey, _, err := registry.CreateKey(
		registry.CURRENT_USER, root+`\shell\open\command`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("创建注册表命令项失败: %w", err)
	}
	defer cmdKey.Close()
	if current, _, err := cmdKey.GetStringValue(""); err == nil && current == command {
		return nil
	}
	if err := cmdKey.SetStringValue("", command); err != nil {
		return fmt.Errorf("写协议处理命令失败: %w", err)
	}
	return nil
}
