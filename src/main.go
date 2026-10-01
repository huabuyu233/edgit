// main.go 程序入口：命令分发 + clone/mirrors 命令实现 + 通用小工具。
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const version = "0.3.1"

// main 解析命令行，分发到 clone / get / docker / mirrors / init 子命令。
func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "clone":
		os.Exit(cmdClone(os.Args[2:]))
	case "get":
		os.Exit(cmdGet(os.Args[2:]))
	case "docker":
		os.Exit(cmdDocker(os.Args[2:]))
	case "mirrors":
		os.Exit(cmdMirrors(os.Args[2:]))
	case "init":
		os.Exit(cmdInit(os.Args[2:]))
	case "test":
		os.Exit(cmdTest(os.Args[2:]))
	case "help":
		os.Exit(cmdHelp(os.Args[2:]))
	case "-h", "--help":
		printUsage()
	case "version", "-v", "--version":
		fmt.Println("edgit " + version)
	default:
		fmt.Fprintf(os.Stderr, "edgit: 未知命令 %q\n\n", os.Args[1])
		printUsage()
		os.Exit(2)
	}
}

// printUsage 打印帮助信息。
func printUsage() {
	fmt.Println(`edgit - GitHub 仓库镜像加速克隆

用法:
  edgit clone <url> [git参数...]       镜像加速克隆，git 参数原样透传
  edgit get <release URL> [输出文件名] 镜像加速下载 Release 附件
  edgit docker pull <镜像>[:tag]       Docker Hub 镜像加速拉取（自动标记回原名）
  edgit init                           初始化向导（欢迎页/镜像/PATH/赞助）
  edgit mirrors list                   列出全部镜像
  edgit mirrors add <url> [名称]   添加镜像（URL 为代理前缀，或含 {owner}/{repo} 的模板）
  edgit mirrors remove <id>        删除镜像
  edgit mirrors disable <id>       禁用镜像
  edgit mirrors enable <id>        启用镜像
  edgit test [项]                    自检全部功能（cli/clone/get/docker/mirrors）
  edgit version                    显示版本
  edgit help [usage|mirrors]       交互式帮助（命令用法 / 镜像维护指南）

说明:
  仅 github.com 的 HTTPS/SSH 地址走镜像加速，其他地址原样透传给 git clone。
  克隆完成后 remote origin 自动还原为原始 GitHub 地址，不影响后续 pull/push。`)
}

// fatalf 打印错误并以退出码 2 结束程序。
func fatalf(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "edgit: "+format+"\n", a...)
	os.Exit(2)
}

// warnf 打印黄色警告，不退出。
func warnf(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, colorYellow("警告: ")+format+"\n", a...)
}

// cmdClone 镜像加速克隆主流程：
// 非 github 地址透传 → 命中缓存免探测 → 并行探测+优先级择优 → 克隆并还原 remote，全失败回落 GitHub。
func cmdClone(args []string) int {
	if len(args) < 1 {
		fatalf("用法: edgit clone <url> [git参数...]")
	}
	rawURL := args[0]
	userArgs := args[1:]

	if _, err := exec.LookPath("git"); err != nil {
		fatalf("未找到 git 命令，请先安装 Git")
	}

	owner, repo, ok := parseGitHubURL(rawURL)
	if !ok {
		fmt.Println("非 github.com 地址，原样透传给 git clone")
		return runGitInherit(append([]string{"clone", rawURL}, userArgs...)...)
	}

	cfg, err := loadConfig()
	if err != nil {
		warnf("读取配置失败（%v），使用内置镜像", err)
		cfg = defaultConfig()
	}
	var mirrors []Mirror
	for _, m := range enabledMirrors(cfg.Mirrors) {
		if m.Template != "" {
			mirrors = append(mirrors, m)
		}
	}
	if len(mirrors) == 0 {
		fmt.Println("未启用任何可用镜像，直接使用原始 GitHub 地址")
		return runGitInherit(append([]string{"clone", rawURL}, userArgs...)...)
	}

	// 10 分钟内命中过的镜像直接复用，跳过探测
	if m := loadCacheHit(cfg.Mirrors); m != nil {
		fmt.Printf("命中缓存镜像 %s（%s 内免探测），直接克隆...\n", m.Name, cacheTTL)
		if code := runGitInherit(append([]string{"clone", rewriteURL(*m, owner, repo)}, userArgs...)...); code == 0 {
			finishClone(userArgs, repo, rawURL, *m)
			return 0
		}
		warnf("缓存镜像 %s 克隆失败，重新探测", m.Name)
	}

	fmt.Printf("并行探测 %d 个镜像...\n", len(mirrors))
	done := 0
	selected, hits := probeAndSelect(context.Background(), mirrors, owner, repo, func(r probeResult) {
		done++
		printProbeResult(r, done, len(mirrors))
	})

	if selected == nil {
		fmt.Println(colorYellow("全部镜像未命中，回落到原始 GitHub 地址"))
		return runGitInherit(append([]string{"clone", rawURL}, userArgs...)...)
	}

	// hits 已按优先级排序：选中者失败时顺延下一个命中镜像
	for _, r := range hits {
		fmt.Printf("  从 %s 克隆...\n", r.mirror.Name)
		if code := runGitInherit(append([]string{"clone", r.url}, userArgs...)...); code == 0 {
			finishClone(userArgs, repo, rawURL, r.mirror)
			return 0
		}
		warnf("从 %s 克隆失败，尝试下一个镜像", r.mirror.Name)
	}

	fmt.Println(colorYellow("镜像克隆均失败，回落到原始 GitHub 地址"))
	return runGitInherit(append([]string{"clone", rawURL}, userArgs...)...)
}

// finishClone 克隆成功收尾：还原 remote origin 为原 GitHub 地址，并写入命中缓存。
func finishClone(userArgs []string, repo, rawURL string, m Mirror) {
	if err := restoreOrigin(userArgs, repo, rawURL); err != nil {
		warnf("还原 remote origin 失败（%v），请手动执行: git remote set-url origin %s", err, rawURL)
	} else {
		fmt.Printf("  remote origin 已还原为 %s\n", rawURL)
	}
	saveCacheHit(m)
}

// printProbeResult 打印单个镜像探测结果（命中 / 站点不可达 / 仓库不可用）。
func printProbeResult(r probeResult, n, total int) {
	switch r.stage {
	case "hit":
		fmt.Printf("[%d/%d] %s %s（%s）\n", n, total, r.mirror.Name, colorGreen("命中"), formatDur(r.d))
	case "unreachable":
		fmt.Printf("[%d/%d] %s %s（%s）\n", n, total, r.mirror.Name, colorYellow("跳过：站点不可达"), formatDur(r.d))
	default:
		fmt.Printf("[%d/%d] %s %s（%s）\n", n, total, r.mirror.Name, colorYellow("跳过：仓库不可用"), formatDur(r.d))
	}
}

// formatDur 耗时格式化到毫秒。
func formatDur(d time.Duration) string {
	return d.Round(time.Millisecond).String()
}

// restoreOrigin 在克隆出的目录里把 remote origin 还原为原 GitHub 地址。
func restoreOrigin(userArgs []string, repo, rawURL string) error {
	for _, dir := range cloneDirCandidates(userArgs, repo) {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		if err := gitSetRemoteURL(dir, rawURL); err == nil {
			return nil
		}
	}
	return fmt.Errorf("未找到克隆目录")
}

// cloneOptsWithValue git clone 中「带参数值」的选项，解析参数时跳过其值才能找到目录参数。
var cloneOptsWithValue = map[string]bool{
	"-b": true, "--branch": true, "-c": true, "--config": true, "--depth": true,
	"-o": true, "--origin": true, "--reference": true, "--reference-if-able": true,
	"--separate-git-dir": true, "--shallow-since": true, "--shallow-exclude": true,
	"--template": true, "--filter": true, "-j": true, "--jobs": true,
	"--recurse-submodules": true, "--server-option": true,
}

// cloneDirCandidates 从 git 参数推断克隆目标目录的候选列表：
// 最后一个位置参数（显式指定的目录）优先，兜底用仓库名。
func cloneDirCandidates(userArgs []string, repo string) []string {
	var pos []string
	skip := false
	for _, a := range userArgs {
		if skip {
			skip = false
			continue
		}
		if strings.HasPrefix(a, "-") && a != "--" {
			if !strings.Contains(a, "=") && cloneOptsWithValue[a] {
				skip = true
			}
			continue
		}
		if a != "--" {
			pos = append(pos, a)
		}
	}
	var out []string
	for i := len(pos) - 1; i >= 0; i-- {
		out = append(out, pos[i])
	}
	return append(out, repo)
}

// cmdMirrors 镜像管理：list / add / remove / disable / enable，改动写回配置文件。
func cmdMirrors(args []string) int {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}
	cfg, err := loadConfig()
	if err != nil {
		fatalf("读取配置失败: %v", err)
	}

	switch sub {
	case "list":
		ms := append([]Mirror(nil), cfg.Mirrors...)
		sort.SliceStable(ms, func(i, j int) bool { return ms[i].Priority < ms[j].Priority })
		fmt.Printf("%-20s %-16s %-4s %-4s %-6s %-6s %-6s %s\n", "ID", "名称", "状态", "优先级", "clone", "release", "docker", "URL模板")
		for _, m := range ms {
			state := "启用"
			if !m.Enabled {
				state = "禁用"
			}
			c, r, d := "-", "-", "-"
			if m.Template != "" {
				c = "✓"
			}
			if m.ReleaseTemplate != "" {
				r = "✓"
			}
			if m.DockerTemplate != "" {
				d = "✓"
			}
			tpl := firstNonEmpty(m.Template, m.ReleaseTemplate, m.DockerTemplate)
			fmt.Printf("%-20s %-16s %-4s %-4d %-6s %-6s %-6s %s\n", m.ID, m.Name, state, m.Priority, c, r, d, tpl)
		}
		return 0

	case "add":
		if len(args) < 2 {
			fatalf("用法: edgit mirrors add <url> [名称]")
		}
		name := ""
		if len(args) > 2 {
			name = args[2]
		}
		m, err := addMirrorToConfig(cfg, args[1], name)
		if err != nil {
			fatalf("%v", err)
		}
		saveConfig(cfg)
		fmt.Printf("已添加镜像 %s（优先级 %d）\n", m.ID, m.Priority)
		return 0

	case "remove":
		id := requireMirrorID(args, sub)
		idx := -1
		for i, m := range cfg.Mirrors {
			if strings.EqualFold(m.ID, id) || strings.EqualFold(m.Name, id) {
				idx = i
				break
			}
		}
		if idx < 0 {
			fatalf("未找到镜像 %s", id)
		}
		removed := cfg.Mirrors[idx]
		cfg.Mirrors = append(cfg.Mirrors[:idx], cfg.Mirrors[idx+1:]...)
		saveConfig(cfg)
		fmt.Printf("已删除镜像 %s\n", removed.ID)
		return 0

	case "disable", "enable":
		id := requireMirrorID(args, sub)
		m := findMirror(cfg.Mirrors, id)
		if m == nil {
			fatalf("未找到镜像 %s", id)
		}
		m.Enabled = sub == "enable"
		saveConfig(cfg)
		verb := "禁用"
		if m.Enabled {
			verb = "启用"
		}
		fmt.Printf("已%s镜像 %s\n", verb, m.ID)
		return 0

	default:
		fatalf("未知子命令: edgit mirrors %s（支持 list/add/remove/disable/enable）", sub)
		return 2
	}
}

// requireMirrorID 取出子命令里的镜像 id 参数，缺失则报用法错误。
func requireMirrorID(args []string, sub string) string {
	if len(args) < 2 {
		fatalf("用法: edgit mirrors %s <id>", sub)
	}
	return strings.TrimSpace(args[1])
}

// findMirror 按 ID 或名称查找镜像（不区分大小写），找不到返回 nil。
func findMirror(ms []Mirror, id string) *Mirror {
	for i := range ms {
		if strings.EqualFold(ms[i].ID, id) || strings.EqualFold(ms[i].Name, id) {
			return &ms[i]
		}
	}
	return nil
}

// addMirrorToConfig 添加镜像，按模板变量自动判定能力：
// {url}=release 模板、{image}=docker 模板、{owner}=clone 模板；
// 纯代理前缀则自动生成 clone+release 双模板。
func addMirrorToConfig(cfg *Config, raw, name string) (Mirror, error) {
	tpl := strings.TrimSpace(raw)
	m := Mirror{Enabled: true, Priority: maxPriority(cfg.Mirrors) + 1}
	switch {
	case strings.Contains(tpl, "{url}"):
		m.ReleaseTemplate = tpl
	case strings.Contains(tpl, "{image}"):
		m.DockerTemplate = tpl
	case strings.Contains(tpl, "{owner}"):
		m.Template = tpl
	default:
		if !strings.HasPrefix(tpl, "http://") && !strings.HasPrefix(tpl, "https://") {
			return Mirror{}, fmt.Errorf("镜像 URL 需以 http(s):// 开头，或使用含 {owner}/{repo}、{url} 的模板")
		}
		base := strings.TrimRight(tpl, "/")
		m.Template = base + "/https://github.com/{owner}/{repo}.git"
		m.ReleaseTemplate = base + "/{url}"
	}
	m.ID = mirrorIDFromTemplate(firstNonEmpty(m.Template, m.ReleaseTemplate, m.DockerTemplate))
	if findMirror(cfg.Mirrors, m.ID) != nil {
		return Mirror{}, fmt.Errorf("镜像 %s 已存在", m.ID)
	}
	if name == "" {
		m.Name = m.ID
	} else {
		m.Name = name
	}
	cfg.Mirrors = append(cfg.Mirrors, m)
	return m, nil
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// maxPriority 返回当前最大优先级，新镜像排在最后。
func maxPriority(ms []Mirror) int {
	max := 0
	for _, m := range ms {
		if m.Priority > max {
			max = m.Priority
		}
	}
	return max
}
