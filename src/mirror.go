// mirror.go 镜像定义：镜像表结构、内置清单、URL 模板改写、GitHub 地址解析。
package main

import (
	"regexp"
	"sort"
	"strings"
)

// Mirror 单个镜像站。三种能力模板相互独立，空 = 不支持该能力：
// Template 走 clone，ReleaseTemplate 走 Release 下载，DockerTemplate 走 docker 拉取。
type Mirror struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Template        string `json:"template"`         // clone 改写模板，空 = 不支持 clone
	ReleaseTemplate string `json:"release_template"` // release 下载模板，空 = 不支持 release
	DockerTemplate  string `json:"docker_template"`  // docker 拉取模板，空 = 不支持 docker
	Enabled         bool   `json:"enabled"`
	Priority        int    `json:"priority"`
}

// builtinMirrors 内置镜像按权威性排序：知名高校镜像在前，其次经久不衰、
// 社区口碑好的镜像。高校镜像（TUNA/USTC）只镜像 Release 文件和特定仓库，
// 不支持任意仓库 git clone，故仅参与 Release 下载。
func builtinMirrors() []Mirror {
	return []Mirror{
		{ID: "tuna", Name: "TUNA（清华）", Template: "", ReleaseTemplate: "https://mirrors.tuna.tsinghua.edu.cn/github-release/{owner}/{repo}/{tag}/{file}", Enabled: true, Priority: 1},
		{ID: "ustc", Name: "USTC（中科大）", Template: "", ReleaseTemplate: "https://mirrors.ustc.edu.cn/github-release/{owner}/{repo}/{tag}/{file}", Enabled: true, Priority: 2},
		{ID: "gitclone.com", Name: "gitclone.com", Template: "https://gitclone.com/github.com/{owner}/{repo}.git", ReleaseTemplate: "", Enabled: true, Priority: 3},
		{ID: "gh-proxy.com", Name: "gh-proxy.com", Template: "https://gh-proxy.com/https://github.com/{owner}/{repo}.git", ReleaseTemplate: "https://gh-proxy.com/{url}", Enabled: true, Priority: 4},
		{ID: "ghproxy.net", Name: "ghproxy.net", Template: "https://ghproxy.net/https://github.com/{owner}/{repo}.git", ReleaseTemplate: "https://ghproxy.net/{url}", Enabled: true, Priority: 5},
		{ID: "ghfast.top", Name: "ghfast.top", Template: "https://ghfast.top/https://github.com/{owner}/{repo}.git", ReleaseTemplate: "https://ghfast.top/{url}", Enabled: true, Priority: 6},
		{ID: "moeyy.xyz", Name: "moeyy.xyz", Template: "https://gh.moeyy.xyz/https://github.com/{owner}/{repo}.git", ReleaseTemplate: "https://gh.moeyy.xyz/{url}", Enabled: true, Priority: 7},
		{ID: "gh.llkk.cc", Name: "gh.llkk.cc", Template: "https://gh.llkk.cc/https://github.com/{owner}/{repo}.git", ReleaseTemplate: "https://gh.llkk.cc/{url}", Enabled: true, Priority: 8},
		// Docker Hub 加速站（仅 docker 拉取；高校镜像均已停服，社区/厂商镜像按权威性排序）
		{ID: "docker.m.daocloud.io", Name: "DaoCloud", DockerTemplate: "docker.m.daocloud.io/{image}", Enabled: true, Priority: 9},
		{ID: "docker.1ms.run", Name: "毫秒镜像", DockerTemplate: "docker.1ms.run/{image}", Enabled: true, Priority: 10},
		{ID: "docker.xuanyuan.me", Name: "轩辕镜像", DockerTemplate: "docker.xuanyuan.me/{image}", Enabled: true, Priority: 11},
		{ID: "docker.1panel.live", Name: "1Panel", DockerTemplate: "docker.1panel.live/{image}", Enabled: true, Priority: 12},
		{ID: "dockerproxy.net", Name: "dockerproxy", DockerTemplate: "dockerproxy.net/{image}", Enabled: true, Priority: 13},
		{ID: "hub.rat.dev", Name: "rat.dev", DockerTemplate: "hub.rat.dev/{image}", Enabled: true, Priority: 14},
		{ID: "proxy.vvvv.ee", Name: "vvvv.ee", DockerTemplate: "proxy.vvvv.ee/{image}", Enabled: true, Priority: 15},
	}
}

// rewriteURL 填充 clone 模板的 {owner}/{repo}，得到完整镜像仓库地址。
func rewriteURL(m Mirror, owner, repo string) string {
	t := strings.ReplaceAll(m.Template, "{owner}", owner)
	return strings.ReplaceAll(t, "{repo}", repo)
}

// 匹配 github.com 的 HTTPS / SSH 仓库地址（允许省略协议和 .git 后缀）。
var (
	reHTTPS = regexp.MustCompile(`^(?:https?://)?(?:www\.)?github\.com/([^/]+)/([^/]+?)(?:\.git)?$`)
	reSSH   = regexp.MustCompile(`^(?:ssh://)?git@github\.com[:/]([^/]+)/([^/]+?)(?:\.git)?$`)
)

// parseGitHubURL 解析 github.com 仓库地址，返回 owner/repo；非 GitHub 地址 ok=false。
func parseGitHubURL(raw string) (owner, repo string, ok bool) {
	s := strings.TrimRight(strings.TrimSpace(raw), "/")
	if x := reHTTPS.FindStringSubmatch(s); x != nil {
		return x[1], x[2], true
	}
	if x := reSSH.FindStringSubmatch(s); x != nil {
		return x[1], x[2], true
	}
	return "", "", false
}

// enabledMirrors 返回启用中的镜像并按优先级升序排序。
func enabledMirrors(ms []Mirror) []Mirror {
	var out []Mirror
	for _, m := range ms {
		if m.Enabled {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Priority < out[j].Priority })
	return out
}

// mirrorIDFromTemplate 从模板 URL 提取域名作为镜像 ID（小写）。
func mirrorIDFromTemplate(tpl string) string {
	s := tpl
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/:"); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(s)
}
