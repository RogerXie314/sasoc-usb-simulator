//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	shell32          = syscall.NewLazyDLL("shell32.dll")
	gdi32            = syscall.NewLazyDLL("gdi32.dll")
	procMessageBoxW  = user32.NewProc("MessageBoxW")
	procCreateMutexW = kernel32.NewProc("CreateMutexW")
	procGetLastError = kernel32.NewProc("GetLastError")
)

// MessageBox 显示 Windows 消息框（GUI 模式下替代 fmt.Printf）
func MessageBox(title, message string, flags uintptr) int {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	messagePtr, _ := syscall.UTF16PtrFromString(message)
	ret, _, _ := procMessageBoxW.Call(0, uintptr(unsafe.Pointer(messagePtr)), uintptr(unsafe.Pointer(titlePtr)), flags)
	return int(ret)
}

// MessageBoxInfo 显示信息提示框
func MessageBoxInfo(title, message string) {
	MessageBox(title, message, 0x40) // MB_ICONINFORMATION
}

// MessageBoxError 显示错误提示框
func MessageBoxError(title, message string) {
	MessageBox(title, message, 0x10) // MB_ICONERROR
}

// MessageBoxYesNo 显示是/否选择框，返回 true 表示用户点了"是"
func MessageBoxYesNo(title, message string) bool {
	ret := MessageBox(title, message, 0x04|0x20) // MB_YESNO | MB_ICONQUESTION
	return ret == 6 // IDYES
}

// CreateMutex 创建 Windows 命名互斥体，返回是否为新实例
// 如果互斥体已存在（说明已有实例在运行），返回 false
func CreateMutex(name string) bool {
	namePtr, _ := syscall.UTF16PtrFromString(name)
	procCreateMutexW.Call(0, 1, uintptr(unsafe.Pointer(namePtr)))
	lastErr, _, _ := procGetLastError.Call()
	// ERROR_ALREADY_EXISTS = 183
	return lastErr != 183
}

// IsGUIAvailable 检测当前是否运行在 Windows GUI 子系统
func IsGUIAvailable() bool {
	return user32.Load() == nil && shell32.Load() == nil
}
