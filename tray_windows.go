//go:build windows

package main

import (
	"encoding/hex"
	"fmt"
	"syscall"
	"unsafe"
)

// 托盘图标相关常量
const (
	NIM_ADD    = 0x00000000
	NIM_MODIFY = 0x00000001
	NIM_DELETE = 0x00000002

	WM_TRAYICON      = 0x0400 + 1 // WM_USER + 1
	WM_DESTROY       = 0x0002
	WM_COMMAND       = 0x0111
	WM_CLOSE         = 0x0010
	WM_LBUTTONDBLCLK = 0x0203

	WS_OVERSPACE  = 0x80000000
	WS_OVERLAPPED = 0x00000000

	CW_USEDEFAULT = uintptr(^uint32(0x7fffffff))

	MB_OK        = 0x00000000
	MB_ICONERROR = 0x00000010
)

var (
	procRegisterClassExW  = user32.NewProc("RegisterClassExW")
	procCreateWindowExW   = user32.NewProc("CreateWindowExW")
	procDefWindowProcW    = user32.NewProc("DefWindowProcW")
	procShowWindow        = user32.NewProc("ShowWindow")
	procGetMessageW       = user32.NewProc("GetMessageW")
	procTranslateMessage  = user32.NewProc("TranslateMessage")
	procDispatchMessageW  = user32.NewProc("DispatchMessageW")
	procPostQuitMessage   = user32.NewProc("PostQuitMessage")
	procLoadIconW         = user32.NewProc("LoadIconW")
	procShellNotifyIconW  = shell32.NewProc("Shell_NotifyIconW")
	procAppendMenuW       = user32.NewProc("AppendMenuW")
	procCreatePopupMenu   = user32.NewProc("CreatePopupMenu")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procTrackPopupMenu    = user32.NewProc("TrackPopupMenu")
	procDestroyWindow     = user32.NewProc("DestroyWindow")
)

// NOTIFYICONDATAW 结构体
type NOTIFYICONDATAW struct {
	CbSize           uint32
	HWnd             syscall.Handle
	UIID             uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            syscall.Handle
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
}

// WNDCLASSEXW 结构体
type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   syscall.Handle
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     syscall.Handle
	HIcon         syscall.Handle
	HCursor       syscall.Handle
	HbrBackground syscall.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       syscall.Handle
}

// MSG 结构体
type MSG struct {
	HWnd    syscall.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

// TrayApp 系统托盘应用
type TrayApp struct {
	hwnd      syscall.Handle
	className string
	title     string
	url       string
	onOpen    func()
	onQuit    func()
	quitCh    chan struct{}
}

// NewTrayApp 创建系统托盘应用
func NewTrayApp(title, url string, onOpen, onQuit func()) *TrayApp {
	return &TrayApp{
		className: "UsbSimulatorTrayClass",
		title:     title,
		url:       url,
		onOpen:    onOpen,
		onQuit:    onQuit,
		quitCh:    make(chan struct{}),
	}
}

// 全局回调函数指针
var globalTrayWndProc uintptr

// Run 启动系统托盘（阻塞，应在独立 goroutine 中运行）
func (t *TrayApp) Run() {
	hInstance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)

	// 注册窗口类
	classNamePtr, _ := syscall.UTF16PtrFromString(t.className)
	
	wndProc := syscall.NewCallback(t.wndProc)
	globalTrayWndProc = wndProc

	wc := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		LpfnWndProc:   syscall.Handle(wndProc),
		HInstance:     syscall.Handle(hInstance),
		LpszClassName: classNamePtr,
	}
	
	// 加载自定义USB图标
	hIconVal := loadIconFromICO()
	if hIconVal == 0 {
		// 回退到系统盾牌图标
		iconVal, _, _ := procLoadIconW.Call(0, 32518)
		hIconVal = syscall.Handle(iconVal)
	}
	wc.HIcon = hIconVal
	wc.HIconSm = hIconVal

	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	// 创建隐藏窗口
	titlePtr, _ := syscall.UTF16PtrFromString(t.title)
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(classNamePtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		WS_OVERLAPPED,
		CW_USEDEFAULT, CW_USEDEFAULT, 0, 0,
		0, 0, hInstance, 0,
	)
	t.hwnd = syscall.Handle(hwnd)

	// 添加托盘图标
	t.addTrayIcon(syscall.Handle(hInstance))

	// 消息循环
	var msg MSG
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if ret == 0 || ret == ^uintptr(0) {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

	// 移除托盘图标
	t.removeTrayIcon()
}

// wndProc 窗口过程
func (t *TrayApp) wndProc(hWnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_TRAYICON:
		switch lParam {
		case WM_LBUTTONDBLCLK:
			// 双击打开浏览器
			if t.onOpen != nil {
				go t.onOpen()
			}
		case 0x0205: // WM_RBUTTONUP
			t.showContextMenu()
		}

	case WM_COMMAND:
		switch wParam {
		case 1001: // 打开控制台
			if t.onOpen != nil {
				go t.onOpen()
			}
		case 1002: // 退出
			t.removeTrayIcon()
			if t.onQuit != nil {
				go t.onQuit()
			}
			procPostQuitMessage.Call(0)
		}

	case WM_DESTROY:
		t.removeTrayIcon()
		procPostQuitMessage.Call(0)
	}

	ret, _, _ := procDefWindowProcW.Call(uintptr(hWnd), uintptr(msg), wParam, lParam)
	return ret
}

// addTrayIcon 添加托盘图标
func (t *TrayApp) addTrayIcon(hInstance syscall.Handle) {
	// 加载自定义USB图标
	hIconVal := loadIconFromICO()
	if hIconVal == 0 {
		// 回退到系统盾牌图标
		iconVal, _, _ := procLoadIconW.Call(0, 32518)
		hIconVal = syscall.Handle(iconVal)
	}

	// 使用 UTF16FromString 正确转换中文 tooltip
	tipUTF16, err := syscall.UTF16FromString(t.title)
	if err != nil {
		tipUTF16, _ = syscall.UTF16FromString("USB Simulator")
	}

	var tip [128]uint16
	copy(tip[:], tipUTF16)

	nid := NOTIFYICONDATAW{
		CbSize:           uint32(unsafe.Sizeof(NOTIFYICONDATAW{})),
		HWnd:             t.hwnd,
		UIID:             1,
		UFlags:           0x00000001 | 0x00000002 | 0x00000004, // NIF_MESSAGE | NIF_ICON | NIF_TIP
		UCallbackMessage: WM_TRAYICON,
		HIcon:            hIconVal,
	}
	nid.SzTip = tip

	procShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&nid)))
}

// removeTrayIcon 移除托盘图标
func (t *TrayApp) removeTrayIcon() {
	nid := NOTIFYICONDATAW{
		CbSize: uint32(unsafe.Sizeof(NOTIFYICONDATAW{})),
		HWnd:   t.hwnd,
		UIID:   1,
	}
	procShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&nid)))
}

// showContextMenu 显示右键菜单
func (t *TrayApp) showContextMenu() {
	hMenu, _, _ := procCreatePopupMenu.Call()

	openText, _ := syscall.UTF16PtrFromString("打开控制台")
	quitText, _ := syscall.UTF16PtrFromString("退出")

	procAppendMenuW.Call(hMenu, 0, 1001, uintptr(unsafe.Pointer(openText)))
	procAppendMenuW.Call(hMenu, 0x00000800, 0, 0) // MF_SEPARATOR
	procAppendMenuW.Call(hMenu, 0, 1002, uintptr(unsafe.Pointer(quitText)))

	// 需要设置前台窗口，否则菜单可能不会消失
	procSetForegroundWindow.Call(uintptr(t.hwnd))

	var pt struct{ X, Y int32 }
	user32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&pt)))

	procTrackPopupMenu.Call(
		hMenu,
		0x0002, // TPM_RIGHTALIGN
		uintptr(pt.X), uintptr(pt.Y),
		0, uintptr(t.hwnd), 0,
	)

	user32.NewProc("DestroyMenu").Call(hMenu)
}

// hex helpers for embed
var _ = hex.EncodeToString
var _ = fmt.Sprintf
