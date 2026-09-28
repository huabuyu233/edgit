// console_windows.go Windows 专用：开启控制台 VT 转义序列支持，让 ANSI 颜色生效。
//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

// enableVT 给 stdout 开启 ENABLE_VIRTUAL_TERMINAL_PROCESSING；
// 输出被重定向到文件/管道时会失败，返回 false（自动关闭颜色）。
func enableVT() bool {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getConsoleMode := kernel32.NewProc("GetConsoleMode")
	setConsoleMode := kernel32.NewProc("SetConsoleMode")
	h := os.Stdout.Fd()
	var mode uint32
	r, _, _ := getConsoleMode.Call(uintptr(h), uintptr(unsafe.Pointer(&mode)))
	if r == 0 {
		return false
	}
	const enableVTProcessing = 0x0004
	r, _, _ = setConsoleMode.Call(uintptr(h), uintptr(mode|enableVTProcessing))
	return r != 0
}
