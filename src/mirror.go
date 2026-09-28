package main

import (
	"regexp"
	"sort"
	"strings"
)

type Mirror struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Template        string `json:"template"`         // clone 改写模板，空 = 不支持 clone
	ReleaseTemplate string `json:"release_template"` // release 下载模板，空 = 不支持 release
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
	}
}

func rewriteURL(m Mirror, owner, repo string) string {
	t := strings.ReplaceAll(m.Template, "{owner}", owner)
	return strings.ReplaceAll(t, "{repo}", repo)
}

var (
	reHTTPS = regexp.MustCompile(`^(?:https?://)?(?:www\.)?github\.com/([^/]+)/([^/]+?)(?:\.git)?$`)
	reSSH   = regexp.MustCompile(`^(?:ssh://)?git@github\.com[:/]([^/]+)/([^/]+?)(?:\.git)?$`)
)

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
