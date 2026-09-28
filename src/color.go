package main

import "os"

var colorOn = os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" && enableVT()

func colorize(code, s string) string {
	if !colorOn {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func colorGreen(s string) string  { return colorize("32", s) }
func colorYellow(s string) string { return colorize("33", s) }
func colorRed(s string) string    { return colorize("31", s) }
func colorCyan(s string) string   { return colorize("36", s) }
