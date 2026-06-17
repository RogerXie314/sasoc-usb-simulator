//go:build !windows

package main

import "fmt"

// MessageBox 显示消息框（非 Windows 平台降级为 stdout）
func MessageBox(title, message string, flags uintptr) int {
	fmt.Printf("[%s] %s\n", title, message)
	return 0
}

// MessageBoxInfo 显示信息提示框
func MessageBoxInfo(title, message string) {
	fmt.Printf("[%s] %s\n", title, message)
}

// MessageBoxError 显示错误提示框
func MessageBoxError(title, message string) {
	fmt.Printf("[%s] %s\n", title, message)
}

// MessageBoxYesNo 显示是/否选择框
func MessageBoxYesNo(title, message string) bool {
	fmt.Printf("[%s] %s (assuming yes)\n", title, message)
	return true
}

// CreateMutex 创建互斥体（非 Windows 平台总是返回 true）
func CreateMutex(name string) bool {
	return true
}

// IsGUIAvailable 非 Windows 平台总是返回 false
func IsGUIAvailable() bool {
	return false
}
