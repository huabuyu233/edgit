// console_other.go 非 Windows 平台：终端默认支持 ANSI 颜色，直接放行。
//go:build !windows

package main

// enableVT 非 Windows 终端原生支持 ANSI，返回 true。
func enableVT() bool { return true }
