// help.go 交互式帮助 edgit help：询问意图后展示「命令用法」或「镜像维护指南」。
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// cmdHelp 无参数进交互菜单；usage|mirrors 直达对应章节；stdin 非终端时打印全量用法。
func cmdHelp(args []string) int {
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "usage", "用法":
			helpUsage()
		case "mirrors", "镜像":
			helpMirrors()
		default:
			fatalf("用法: edgit help [usage|mirrors]")
		}
		return 0
	}
	if !stdinIsTTY() {
		helpUsage()
		return 0
	}

	fmt.Println(colorCyan("edgit 帮助"))
	fmt.Println()
	fmt.Println("  1. 忘记命令怎么用 — 查看全部命令与示例")
	fmt.Println("  2. 镜像站挂了 — 镜像维护指南")
	fmt.Print("请选择 (1/2): ")
	in := bufio.NewReader(os.Stdin)
	line, _ := in.ReadString('\n')
	fmt.Println()
	switch strings.TrimSpace(line) {
	case "1":
		helpUsage()
	case "2":
		helpMirrors()
	default:
		fmt.Println(colorYellow("未识别的选择，已显示全部命令用法"))
		fmt.Println()
		helpUsage()
	}
	return 0
}

// stdinIsTTY 判断标准输入是否为终端（管道/重定向时不进交互菜单）。
func stdinIsTTY() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// pad 按终端显示宽度补空格（中文算 2 列），保证中英混排对齐。
func pad(s string, width int) string {
	w := 0
	for _, r := range s {
		if r > 0x2E80 {
			w += 2 // CJK 及全角字符占 2 列
		} else {
			w++
		}
	}
	if w >= width {
		return s + " "
	}
	return s + strings.Repeat(" ", width-w)
}

// helpUsage 全量命令用法：命令 + 一行说明 + 典型示例。
func helpUsage() {
	fmt.Println(colorCyan("用法速查"))
	fmt.Println()
	fmt.Printf("  %s %s\n", pad("edgit clone <url> [git参数...]", 40), "镜像加速克隆，git 参数原样透传")
	fmt.Println("      例: edgit clone https://github.com/octocat/Hello-World.git --depth 1")
	fmt.Println()
	fmt.Printf("  %s %s\n", pad("edgit get <release URL> [输出文件名]", 40), "加速下载 Release 附件（高校镜像优先）")
	fmt.Println("      例: edgit get https://github.com/xxx/yyy/releases/download/v1.0/tool.zip")
	fmt.Println()
	fmt.Printf("  %s %s\n", pad("edgit docker pull <镜像>[:tag]...", 40), "Docker Hub 加速拉取，自动 tag 回原名")
	fmt.Println("      例: edgit docker pull nginx:1.27")
	fmt.Println()
	fmt.Printf("  %s %s\n", pad("edgit mirrors list", 40), "列出全部镜像")
	fmt.Printf("  %s %s\n", pad("edgit mirrors add <url> [名称]", 40), "添加镜像（代理前缀或 {owner}/{repo} 模板）")
	fmt.Printf("  %s %s\n", pad("edgit mirrors disable|enable <id>", 40), "禁用 / 启用镜像")
	fmt.Printf("  %s %s\n", pad("edgit mirrors remove <id>", 40), "删除镜像")
	fmt.Println()
	fmt.Printf("  %s %s\n", pad("edgit init", 40), "初始化向导（镜像配置 / 安装到 PATH）")
	fmt.Printf("  %s %s\n", pad("edgit test [项]", 40), "自检全部功能，生成测试报告")
	fmt.Printf("  %s %s\n", pad("edgit version", 40), "显示版本")
	fmt.Printf("  %s %s\n", pad("edgit help [usage|mirrors]", 40), "本帮助（可直达章节）")
	fmt.Println()
	fmt.Println("  · 仅 github.com 地址走镜像加速，其他地址原样透传")
	fmt.Println("  · 克隆完成后 remote 自动还原为 GitHub 原地址，push/pull 不受影响")
	fmt.Println("  · 镜像站失效怎么维护？ edgit help mirrors")
}

// helpMirrors 镜像维护指南：排查 → 启停 → 换站，附完整示例。
func helpMirrors() {
	fmt.Println(colorCyan("镜像维护指南"))
	fmt.Println()
	fmt.Println("  1. 看看是哪个站的问题")
	fmt.Println("      edgit mirrors list                       列出全部镜像及启用状态")
	fmt.Println()
	fmt.Println("  2. 临时禁用 / 恢复失效站点")
	fmt.Println("      edgit mirrors disable <id>               禁用后不参与探测")
	fmt.Println("      edgit mirrors enable <id>                恢复启用")
	fmt.Println()
	fmt.Println("  3. 删除并换新站")
	fmt.Println("      edgit mirrors remove <id>                删除镜像")
	fmt.Println("      edgit mirrors add https://gh.example.com/           代理前缀型")
	fmt.Println("      edgit mirrors add 'https://gh.example.com/{owner}/{repo}.git'   模板型")
	fmt.Println()
	fmt.Println("  示例: gh-proxy.com 挂了")
	fmt.Println("      edgit mirrors disable gh-proxy.com")
	fmt.Println("      edgit mirrors add https://gh.example.com/")
	fmt.Println()
	fmt.Println("  · 全部镜像失效时 edgit 自动回落 GitHub 直连，功能不受影响，只是没有加速")
	fmt.Println("  · 镜像改动即时生效，10 分钟内的命中缓存自动失效，无需手动处理")
}
