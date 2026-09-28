package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const sponsorRepoURL = "https://github.com/your-name/edgit"

func cmdInit(_ []string) int {
	in := bufio.NewReader(os.Stdin)
	readLine := func() string {
		line, _ := in.ReadString('\n')
		return strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
	}

	fmt.Println(colorGreen("欢迎使用 edgit！"))
	fmt.Println(`edgit 是 GitHub 仓库镜像加速克隆工具。

用法:
  edgit clone <url> [git参数...]       镜像加速克隆（--depth 1 等 git 参数原样透传）
  edgit mirrors list|add|remove|...    镜像管理
  edgit init                           本初始化向导（可随时重跑）

说明: 仅 github.com 地址走镜像加速，其他地址直接透传给 git；
克隆完成后 remote 自动还原为原始 GitHub 地址，日常 pull/push 不受影响。`)

	fmt.Println()
	fmt.Println(colorCyan("[1/3] 镜像站配置"))
	cfg, err := loadConfig()
	if err != nil {
		cfg = defaultConfig()
	}
	fmt.Println("内置镜像站（按优先级探测，命中即用）:")
	for _, m := range enabledMirrors(cfg.Mirrors) {
		c, r := "-", "-"
		if m.Template != "" {
			c = "clone"
		}
		if m.ReleaseTemplate != "" {
			r = "release"
		}
		fmt.Printf("  %-16s [%s,%s]\n", m.ID, c, r)
	}
	fmt.Print("直接回车使用默认配置；或输入要追加的镜像站 URL（多个用逗号分隔）: ")
	line := readLine()
	if line != "" {
		added := 0
		for _, part := range strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == '，' }) {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			m, err := addMirrorToConfig(cfg, part, "")
			if err != nil {
				warnf("跳过 %s: %v", part, err)
				continue
			}
			fmt.Printf("  已追加 %s（优先级 %d）\n", m.ID, m.Priority)
			added++
		}
		if added > 0 {
			fmt.Println("  （镜像将在保存配置时写入 ~/.edgit/config.json）")
		}
	}

	fmt.Println()
	fmt.Println(colorCyan("[2/3] 环境变量配置"))
	ensurePATH()

	fmt.Println()
	fmt.Println(colorCyan("[3/3] 赞助（可选）"))
	fmt.Print("edgit 完全免费开源。如果它帮到了你，欢迎赞助支持！要查看赞助方式吗？(y/N): ")
	ans := strings.ToLower(readLine())
	if ans == "y" || ans == "yes" {
		fmt.Println("感谢支持！收款码仓库地址:")
		fmt.Println("  " + sponsorRepoURL)
		fmt.Print("回车结束初始化...")
		readLine()
	}

	if err := cfg.save(); err != nil {
		warnf("保存配置失败: %v", err)
	} else {
		p, _ := configPath()
		fmt.Println("配置已保存到 " + p)
	}

	fmt.Println()
	fmt.Println(colorGreen("初始化完成！快速开始:"))
	fmt.Println("  edgit clone https://github.com/algerkong/AlgerMusicPlayer.git --depth 1")
	return 0
}

func ensurePATH() {
	exe, err := os.Executable()
	if err != nil {
		warnf("无法确定 edgit 路径: %v", err)
		return
	}
	dir := filepath.Dir(exe)
	if dirInPATH(dir) {
		fmt.Println("  " + dir + " 已在 PATH 中，无需修改。")
		return
	}
	fmt.Println("  检测到 " + dir + " 不在 PATH 中，正在添加用户级环境变量...")
	if runtime.GOOS == "windows" {
		script := fmt.Sprintf(
			"$p=[Environment]::GetEnvironmentVariable('Path','User'); if(-not $p){$p=''}; "+
				"if((';'+$p+';') -notlike ('*;%s;*')){ "+
				"$np=if($p){$p.TrimEnd(';')+';%s'}else{'%s'}; "+
				"[Environment]::SetEnvironmentVariable('Path',$np,'User')}", dir, dir, dir)
		cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
		if out, err := cmd.CombinedOutput(); err != nil {
			warnf("添加 PATH 失败: %v %s", err, out)
			fmt.Println("  请手动把以下目录加入用户级 PATH:")
			fmt.Println("    " + dir)
			return
		}
		fmt.Println("  已添加到用户级 PATH（重开终端后生效）:")
		fmt.Println("    " + dir)
		return
	}
	rc := shellRCFile()
	if rc == "" {
		warnf("找不到 shell 配置文件，请手动把 %s 加入 PATH", dir)
		return
	}
	if data, err := os.ReadFile(rc); err == nil && strings.Contains(string(data), dir) {
		fmt.Println("  " + rc + " 中已存在，跳过。")
		return
	}
	f, err := os.OpenFile(rc, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		warnf("写入 %s 失败: %v", rc, err)
		return
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "\nexport PATH=\"$PATH:%s\"\n", dir); err != nil {
		warnf("写入 %s 失败: %v", rc, err)
		return
	}
	fmt.Println("  已追加到 " + rc + "（重开终端后生效）:")
	fmt.Println("    " + dir)
}

func dirInPATH(dir string) bool {
	dir = filepath.Clean(dir)
	for _, p := range filepath.SplitList(os.Getenv("Path")) {
		if p == "" {
			continue
		}
		cmp := filepath.Clean(p)
		if runtime.GOOS == "windows" {
			if strings.EqualFold(cmp, dir) {
				return true
			}
		} else if cmp == dir {
			return true
		}
	}
	return false
}

func shellRCFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	shell := os.Getenv("SHELL")
	switch {
	case strings.Contains(shell, "zsh"):
		return filepath.Join(home, ".zshrc")
	case strings.Contains(shell, "bash"):
		return filepath.Join(home, ".bashrc")
	default:
		return filepath.Join(home, ".profile")
	}
}
