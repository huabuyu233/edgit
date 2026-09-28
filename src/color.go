// color.go 彩色输出：NO_COLOR 环境变量、dumb 终端、不支持 VT 时自动降级为无色。
package main

import "os"

// colorOn 是否启用 ANSI 颜色。
var colorOn = os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" && enableVT()

// colorize 给字符串包裹 ANSI 颜色码（colorOn 为 false 时原样返回）。
func colorize(code, s string) string {
	if !colorOn {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func colorGreen(s string) string  { return colorize("32", s) } // 绿（成功/命中）
func colorYellow(s string) string { return colorize("33", s) } // 黄（警告/跳过）
func colorRed(s string) string    { return colorize("31", s) } // 红（失败）
func colorCyan(s string) string   { return colorize("36", s) } // 青（步骤标题）
