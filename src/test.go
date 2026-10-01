// test.go 自检命令 edgit test：环境预检 → 逐项测试（失败继续）→ 清理测试产物 → 汇总报告。
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// 测试目标：get 下载用 GitHub 官方仓库 cli/cli 发行的校验文件（稳定、体积小）。
const (
	testGetURL   = "https://github.com/cli/cli/releases/download/v2.101.0/gh_2.101.0_checksums.txt"
	testGetFile  = "gh_2.101.0_checksums.txt"
	testCloneURL = "https://github.com/octocat/Hello-World.git"
)

// skipError 测试跳过原因（如缺少可选环境）。
type skipError struct{ reason string }

func (e skipError) Error() string { return e.reason }

// testItem 一项测试。
type testItem struct {
	name string
	run  func() (string, error) // 返回补充信息（成功时展示）或错误
}

// testResult 单项测试结果。
type testResult struct {
	name, note string
	dur        time.Duration
	status     int // 0 通过 / 1 失败 / 2 跳过
}

// testCtx 测试上下文：临时目录、自身路径、环境标记、待清理的产物。
type testCtx struct {
	dir         string
	exe         string
	docker      bool
	dockerNew   []string // 测试拉取的新镜像，结束时删除
	cfgBackup   fileBackup
	cacheBackup fileBackup
}

// fileBackup 文件备份（原始字节 + 是否存在），用于测试后恢复。
type fileBackup struct {
	path   string
	data   []byte
	exists bool
}

// backupFile 备份文件内容。
func backupFile(path string) fileBackup {
	b := fileBackup{path: path}
	if data, err := os.ReadFile(path); err == nil {
		b.data, b.exists = data, true
	}
	return b
}

// restore 恢复备份；原本不存在则删除。
func (b fileBackup) restore() error {
	if b.exists {
		return os.WriteFile(b.path, b.data, 0o644)
	}
	if err := os.Remove(b.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// cmdTest 自检入口：无参数跑全部；带参数跑单项（cli/clone/get/docker/mirrors）。
func cmdTest(args []string) int {
	t := &testCtx{}
	items := t.allItems()

	if len(args) > 0 {
		name := strings.ToLower(args[0])
		var picked []testItem
		for _, it := range items {
			if it.name == name {
				picked = append(picked, it)
			}
		}
		if picked == nil {
			fatalf("未知测试项 %q（支持 cli/clone/get/docker/mirrors）", args[0])
		}
		if name == "get" && len(args) > 1 { // 支持自定义下载地址: edgit test get <url>
			url := args[1]
			picked[0].run = func() (string, error) { return t.testGet(url) }
		}
		items = picked
	}

	// [1/3] 环境预检：git 必需；docker 可选（缺失跳过相关测试）；网络仅提示
	fmt.Println(colorCyan("环境检查"))
	gitVer := probeGit()
	if gitVer == "" {
		fmt.Printf("  %-8s %s\n", "git", colorYellow("未安装"))
		fmt.Println()
		fmt.Println(colorYellow("环境未安装 git，无法测试。请先安装 Git ≥ 2.x 后重试。"))
		return 1
	}
	fmt.Printf("  %-8s %s %s\n", "git", colorGreen("✓"), gitVer)

	dockerVer := probeDocker()
	t.docker = dockerVer != ""
	if t.docker {
		fmt.Printf("  %-8s %s %s\n", "docker", colorGreen("✓"), dockerVer)
	} else {
		fmt.Printf("  %-8s %s\n", "docker", colorYellow("⚠ 未安装，docker 测试将跳过"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	reachable, _ := checkReachable(ctx, "github.com:443", 3*time.Second)
	cancel()
	if reachable {
		fmt.Printf("  %-8s %s %s\n", "network", colorGreen("✓"), "github.com 可达")
	} else {
		fmt.Printf("  %-8s %s\n", "network", colorYellow("⚠ github.com 不可达，测试结果仅供参考"))
	}

	// [2/3] 逐项测试：失败继续；先准备临时目录与配置备份（清理兜底）
	exe, err := os.Executable()
	if err != nil {
		fatalf("无法确定 edgit 路径: %v", err)
	}
	t.exe = exe
	t.dir, err = os.MkdirTemp("", "edgit-test-*")
	if err != nil {
		fatalf("创建临时目录失败: %v", err)
	}
	if cp, err := configPath(); err == nil {
		t.cfgBackup = backupFile(cp)
	}
	if cp, err := cachePath(); err == nil {
		t.cacheBackup = backupFile(cp)
	}

	fmt.Println()
	fmt.Println(colorCyan("测试"))
	results := make([]testResult, 0, len(items))
	for _, it := range items {
		start := time.Now()
		note, err := it.run()
		res := testResult{name: it.name, note: note, dur: time.Since(start)}
		switch {
		case err == nil:
			res.status = 0
			fmt.Printf("  %s %-8s %-7s %s\n", colorGreen("✓"), it.name, formatDur(res.dur), note)
		default:
			var se skipError
			if errors.As(err, &se) {
				res.status = 2
				res.note = se.reason
				fmt.Printf("  %s %-8s %-7s %s\n", colorYellow("⚠"), it.name, "—", res.note)
			} else {
				res.status = 1
				res.note = err.Error()
				fmt.Printf("  %s %-8s %-7s %s\n", colorRed("✗"), it.name, formatDur(res.dur), res.note)
			}
		}
		results = append(results, res)
	}

	// [3/3] 清理测试产物（无论成败都执行）
	fmt.Println()
	fmt.Println(colorCyan("清理"))
	for _, line := range t.cleanup() {
		fmt.Println("  " + line)
	}

	// 汇总：任一失败退出码 1
	pass, fail, skip := 0, 0, 0
	for _, r := range results {
		switch r.status {
		case 0:
			pass++
		case 1:
			fail++
		default:
			skip++
		}
	}
	var total time.Duration
	for _, r := range results {
		total += r.dur
	}
	fmt.Println()
	fmt.Println(colorCyan("结果"))
	fmt.Printf("  %s · %s · %s   %s\n",
		colorGreen(fmt.Sprintf("%d 通过", pass)),
		colorRed(fmt.Sprintf("%d 失败", fail)), colorYellow(fmt.Sprintf("%d 跳过", skip)),
		"总耗时 "+formatDur(total))
	if fail > 0 {
		return 1
	}
	return 0
}

// allItems 定义全部测试项及执行顺序。
func (t *testCtx) allItems() []testItem {
	return []testItem{
		{"cli", t.testCLI},
		{"clone", t.testClone},
		{"get", func() (string, error) { return t.testGet(testGetURL) }},
		{"docker", t.testDocker},
		{"mirrors", t.testMirrors},
	}
}

// testCLI 自检 version / help 子命令输出正常。
func (t *testCtx) testCLI() (string, error) {
	out, err := exec.Command(t.exe, "version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("version 执行失败: %v", err)
	}
	if !strings.Contains(string(out), version) {
		return "", fmt.Errorf("版本输出异常: %s", strings.TrimSpace(string(out)))
	}
	hout, err := exec.Command(t.exe, "help", "usage").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("help 执行失败: %v", err)
	}
	if len(strings.TrimSpace(string(hout))) < 50 {
		return "", fmt.Errorf("help 输出为空")
	}
	return "version / help 正常", nil
}

// testClone 克隆测试仓库，校验 .git 存在且 remote 已还原为 GitHub 原地址。
func (t *testCtx) testClone() (string, error) {
	dst := filepath.Join(t.dir, "clone-test")
	out, err := exec.Command(t.exe, "clone", testCloneURL, "--depth", "1", dst).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("克隆失败: %s", tailLine(out))
	}
	if _, err := os.Stat(filepath.Join(dst, ".git")); err != nil {
		return "", fmt.Errorf("克隆目录缺少 .git")
	}
	remote, err := exec.Command("git", "-C", dst, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", fmt.Errorf("读取 remote 失败: %v", err)
	}
	if got := strings.TrimSpace(string(remote)); got != testCloneURL {
		return "", fmt.Errorf("remote 未还原: %s", got)
	}
	return "remote 已还原为 GitHub 原地址", nil
}

// testGet 下载 GitHub 官方测试文件，校验文件存在且非空。
func (t *testCtx) testGet(url string) (string, error) {
	out := filepath.Join(t.dir, testGetFile)
	cmdOut, err := exec.Command(t.exe, "get", url, out).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("下载失败: %s", tailLine(cmdOut))
	}
	fi, err := os.Stat(out)
	if err != nil {
		return "", fmt.Errorf("文件不存在: %v", err)
	}
	if fi.Size() == 0 {
		return "", fmt.Errorf("文件为空")
	}
	return fmt.Sprintf("%s 已下载（%s）", testGetFile, humanSize(fi.Size())), nil
}

// testDocker 拉取 hello-world 并校验原名 tag；未安装 docker 时跳过。
func (t *testCtx) testDocker() (string, error) {
	if !t.docker {
		return "", skipError{"环境未安装 docker，无法测试"}
	}
	before := dockerImages()
	out, err := exec.Command(t.exe, "docker", "pull", "hello-world").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("拉取失败: %s", tailLine(out))
	}
	if err := exec.Command("docker", "image", "inspect", "hello-world").Run(); err != nil {
		return "", fmt.Errorf("原名 tag 校验失败: %v", err)
	}
	t.dockerNew = newImages(before, dockerImages())
	return "hello-world 拉取成功，tag 已还原", nil
}

// testMirrors 校验 add/disable/enable/remove 及配置持久化，结束后恢复原配置。
func (t *testCtx) testMirrors() (string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return "", fmt.Errorf("读取配置失败: %v", err)
	}
	n0 := len(cfg.Mirrors)
	m, err := addMirrorToConfig(cfg, "https://edgit-test.invalid/", "测试镜像")
	if err != nil {
		return "", fmt.Errorf("add 失败: %v", err)
	}
	if err := cfg.save(); err != nil {
		return "", fmt.Errorf("保存配置失败: %v", err)
	}

	cfg2, err := loadConfig() // 重新加载，验证持久化
	if err != nil {
		return "", fmt.Errorf("重载配置失败: %v", err)
	}
	if findMirror(cfg2.Mirrors, m.ID) == nil {
		return "", fmt.Errorf("add 后未找到 %s", m.ID)
	}
	mm := findMirror(cfg2.Mirrors, m.ID)
	mm.Enabled = false
	if mm.Enabled {
		return "", fmt.Errorf("disable 失败")
	}
	mm.Enabled = true
	if !mm.Enabled {
		return "", fmt.Errorf("enable 失败")
	}

	kept := cfg2.Mirrors[:0]
	for _, x := range cfg2.Mirrors {
		if x.ID != m.ID {
			kept = append(kept, x)
		}
	}
	cfg2.Mirrors = kept
	if err := cfg2.save(); err != nil {
		return "", fmt.Errorf("保存配置失败: %v", err)
	}
	cfg3, err := loadConfig()
	if err != nil {
		return "", fmt.Errorf("重载配置失败: %v", err)
	}
	if findMirror(cfg3.Mirrors, m.ID) != nil {
		return "", fmt.Errorf("remove 失败，%s 仍存在", m.ID)
	}
	if len(cfg3.Mirrors) != n0 {
		return "", fmt.Errorf("镜像数量不一致: %d → %d", n0, len(cfg3.Mirrors))
	}
	return "add / disable / enable / remove 正常", nil
}

// cleanup 清理测试产物：临时目录、配置/缓存备份、docker 测试镜像。
func (t *testCtx) cleanup() []string {
	var lines []string
	if t.dir != "" {
		if err := os.RemoveAll(t.dir); err != nil {
			lines = append(lines, colorYellow("⚠ 临时目录删除失败: ")+err.Error())
		} else {
			lines = append(lines, colorGreen("✓")+" 临时目录已删除")
		}
	}
	if err := t.cfgBackup.restore(); err != nil {
		lines = append(lines, colorYellow("⚠ 配置恢复失败: ")+err.Error())
	} else {
		lines = append(lines, colorGreen("✓")+" 配置已恢复原状")
	}
	if err := t.cacheBackup.restore(); err != nil {
		lines = append(lines, colorYellow("⚠ 缓存恢复失败: ")+err.Error())
	} else {
		lines = append(lines, colorGreen("✓")+" 命中缓存已恢复原状")
	}
	if len(t.dockerNew) > 0 {
		removed := 0
		for _, img := range t.dockerNew {
			if exec.Command("docker", "rmi", img).Run() == nil {
				removed++
			}
		}
		lines = append(lines, fmt.Sprintf("%s docker 测试镜像已删除（%d/%d）", colorGreen("✓"), removed, len(t.dockerNew)))
	}
	return lines
}

// probeGit 探测 git 版本；未安装返回空串。
func probeGit() string {
	out, err := exec.Command("git", "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(string(out), "git version "))
}

// probeDocker 探测 docker 版本；未安装返回空串。
func probeDocker() string {
	out, err := exec.Command("docker", "--version").Output()
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(out))
	if i := strings.Index(s, ","); i > 0 {
		s = s[:i] // 只保留 "Docker version 27.3.1"，去掉 build 信息
	}
	return s
}

// dockerImages 当前全部镜像（repository:tag 集合）。
func dockerImages() map[string]bool {
	out, err := exec.Command("docker", "images", "--format", "{{.Repository}}:{{.Tag}}").Output()
	if err != nil {
		return nil
	}
	m := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasSuffix(line, ":<none>") {
			m[line] = true
		}
	}
	return m
}

// newImages 计算测试新增的镜像（after - before），用于清理。
func newImages(before, after map[string]bool) []string {
	var out []string
	for img := range after {
		if !before[img] {
			out = append(out, img)
		}
	}
	return out
}

// tailLine 取命令输出的最后一行（错误摘要）。
func tailLine(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 {
		return "无输出"
	}
	return strings.TrimSpace(lines[len(lines)-1])
}
