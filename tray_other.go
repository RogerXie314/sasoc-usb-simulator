//go:build !windows

package main

import "fmt"

// TrayApp 非 Windows 平台的空实现
type TrayApp struct {
	onQuit func()
}

// NewTrayApp 创建托盘应用
func NewTrayApp(title, url string, onOpen, onQuit func()) *TrayApp {
	return &TrayApp{onQuit: onQuit}
}

// Run 非 Windows 平台不做任何事
func (t *TrayApp) Run() {
	fmt.Println("system tray is only supported on Windows")
	// 阻塞，等待 quit
	select {}
}
