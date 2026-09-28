// docker.go Docker Hub 镜像加速：解析镜像引用 → 逐站 docker pull → tag 回原名 → 全失败回落 Hub 直连。
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// dockerRef 解析后的镜像引用：path 为库内路径（如 library/nginx），suffix 为 :tag / @digest 原样保留。
type dockerRef struct {
	original string
	path     string
	suffix   string
	hub      bool // 是否 Docker Hub（v1 只加速 Hub）
}

// parseDockerImage 解析镜像引用：
// nginx → library/nginx；user/img 保持；docker.io/ 前缀剥离；
// ghcr.io 等其他 registry 标记为非 Hub（v1 不加速，原样透传）。
func parseDockerImage(ref string) dockerRef {
	r := dockerRef{original: ref}
	name := ref
	if i := strings.Index(name, "@"); i >= 0 { // 剥离 @digest 后缀
		r.suffix = name[i:]
		name = name[:i]
	}
	// 剥离 :tag（冒号必须在最后一段名字里，避免误伤 registry:5000/img 的端口）
	if i := strings.LastIndex(name, ":"); i > strings.LastIndex(name, "/") {
		r.suffix = name[i:] + r.suffix
		name = name[:i]
	}
	parts := strings.Split(name, "/")
	r.hub = true
	if len(parts) > 1 {
		first := parts[0]
		// 首段含 . 或 : 或是 localhost → 它是 registry 而非用户名
		if first == "localhost" || strings.Contains(first, ".") || strings.Contains(first, ":") {
			if strings.EqualFold(first, "docker.io") {
				parts = parts[1:] // docker.io/nginx 等价于 nginx
			} else {
				r.hub = false
				r.path = name
				return r
			}
		}
	}
	if len(parts) == 1 {
		r.path = "library/" + parts[0] // 官方镜像省略 library/ 前缀，补回
	} else {
		r.path = strings.Join(parts, "/")
	}
	return r
}

// rewriteDockerImage 填充 docker 模板的 {image}（= path+suffix）。
func rewriteDockerImage(m Mirror, r dockerRef) string {
	return strings.ReplaceAll(m.DockerTemplate, "{image}", r.path+r.suffix)
}

// cmdDocker edgit docker pull 入口（v1 仅支持 pull）；未安装 docker 直接报错。
func cmdDocker(args []string) int {
	if len(args) < 1 {
		fatalf("用法: edgit docker pull <镜像>[:tag] [镜像...]")
	}
	if args[0] != "pull" {
		fatalf("edgit docker 暂仅支持 pull（如: edgit docker pull nginx:1.27）")
	}
	images := args[1:]
	if len(images) == 0 {
		fatalf("用法: edgit docker pull <镜像>[:tag] [镜像...]")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		fatalf("未找到 docker 命令，请先安装 Docker")
	}

	cfg, err := loadConfig()
	if err != nil {
		warnf("读取配置失败（%v），使用内置镜像", err)
		cfg = defaultConfig()
	}

	code := 0
	for _, img := range images {
		if c := dockerPullOne(cfg, img); c != 0 {
			code = c
		}
	}
	return code
}

// dockerPullOne 拉取单个镜像：非 Hub 透传；Hub 逐站尝试（拉取即探测），
// 成功后 tag 回原名保证 docker run 可直接引用；全失败回落 Hub 直连。
func dockerPullOne(cfg *Config, image string) int {
	ref := parseDockerImage(image)
	fmt.Printf("拉取 %s\n", ref.original)

	if !ref.hub {
		fmt.Println("非 Docker Hub 镜像，原样透传给 docker pull")
		return runDockerInherit("pull", ref.original)
	}

	var cands []downloadCandidate
	for _, m := range enabledMirrors(cfg.Mirrors) {
		if m.DockerTemplate == "" {
			continue
		}
		cands = append(cands, downloadCandidate{name: m.Name, url: rewriteDockerImage(m, ref)})
	}
	if len(cands) == 0 {
		fmt.Println("未启用任何 Docker 镜像站，直接拉取")
		return runDockerInherit("pull", ref.original)
	}

	for i, c := range cands {
		fmt.Printf("[%d/%d] %s 尝试拉取 %s\n", i+1, len(cands), c.name, c.url)
		if code := runDockerInherit("pull", c.url); code != 0 {
			fmt.Printf("  %s\n", colorYellow("失败，换下一个"))
			continue
		}
		// 拉下来的镜像名字是「镜像站路径」，tag 回用户写的原名才能 docker run nginx 直接用
		if err := dockerTag(c.url, ref.original); err != nil {
			warnf("镜像已拉取，但标记为 %s 失败，可手动执行: docker tag %s %s", ref.original, c.url, ref.original)
		} else {
			fmt.Printf("  %s\n", colorGreen("完成")+"，已标记为 "+ref.original)
		}
		return 0
	}

	fmt.Println(colorYellow("全部镜像站拉取失败，回落 Docker Hub 直连"))
	return runDockerInherit("pull", ref.original)
}

// runDockerInherit 执行 docker 命令并继承标准输入输出，返回退出码。
func runDockerInherit(args ...string) int {
	cmd := exec.Command("docker", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		return 1
	}
	return 0
}

// dockerTag 给镜像打别名：docker tag <镜像站路径> <用户原名>。
func dockerTag(src, dst string) error {
	cmd := exec.Command("docker", "tag", src, dst)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
