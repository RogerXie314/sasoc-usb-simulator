//go:build windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

func loadIconFromICO() syscall.Handle {
	exePath, err := os.Executable()
	if err != nil {
		return 0
	}
	exeDir := filepath.Dir(exePath)
	icoPath := filepath.Join(exeDir, "icon.ico")

	namePtr, err := syscall.UTF16PtrFromString(icoPath)
	if err != nil {
		return 0
	}

	// LoadImageW: hinst=0, name=filename, type=IMAGE_ICON(1), cx=0, cy=0, fuLoad=LR_LOADFROMFILE(0x10)
	// cx=0, cy=0 loads the first icon in the file at its default size
	hIcon, _, _ := user32.NewProc("LoadImageW").Call(
		0,
		uintptr(unsafe.Pointer(namePtr)),
		1,    // IMAGE_ICON
		0,    // cxDesired (0 = default)
		0,    // cyDesired (0 = default)
		0x10, // LR_LOADFROMFILE
	)
	if hIcon == 0 || hIcon == uintptr(^uint32(0)) {
		return 0
	}
	return syscall.Handle(hIcon)
}
