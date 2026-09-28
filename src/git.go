// git.go git 封装与镜像探测：两级探测（TCP 连通性预检 → ls-remote 仓库探测）+ 并行探测与优先级择优。
package main

import (
	"bytes"
	"context"
	"net"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"time"
)

const (
	probeTimeout = 8 * time.Second // 仓库探测（git ls-remote）超时
	connTimeout  = 3 * time.Second // 连通性预检（TCP 拨号）超时
	graceWindow  = 3 * time.Second // 命中后等待更高优先级镜像的宽限期
)

// probeResult 单镜像探测结果。stage: hit=命中 / unreachable=站点不可达 / repo=仓库不可用。
type probeResult struct {
	mirror Mirror
	url    string
	ok     bool
	stage  string
	d      time.Duration
}

// runGitInherit 执行 git 命令并继承标准输入输出（用户可见 git 原始输出），返回退出码。
func runGitInherit(args ...string) int {
	cmd := exec.Command("git", args...)
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

// gitSetRemoteURL 把目录 dir 下仓库的 remote origin 改为 url（克隆后还原用）。
func gitSetRemoteURL(dir, url string) error {
	cmd := exec.Command("git", "-C", dir, "remote", "set-url", "origin", url)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// checkReachable TCP 拨号 host:port 做 ping 式连通性预检，返回是否可达及耗时。
func checkReachable(ctx context.Context, hostport string, timeout time.Duration) (bool, time.Duration) {
	start := time.Now()
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", hostport)
	if err != nil {
		return false, time.Since(start)
	}
	conn.Close()
	return true, time.Since(start)
}

// mirrorHostPort 取镜像 URL 的 host:port（连通性预检用），非 http(s) 协议返回空跳过预检。
func mirrorHostPort(m Mirror, owner, repo string) string {
	u, err := url.Parse(rewriteURL(m, owner, repo))
	if err != nil || u.Host == "" {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "http" {
			port = "80"
		} else {
			port = "443"
		}
	}
	return net.JoinHostPort(u.Hostname(), port)
}

// probeOne 单镜像两级探测：连通性预检（连不上立即淘汰，不跑 git）→ git ls-remote 验证仓库可用。
func probeOne(ctx context.Context, m Mirror, owner, repo string) probeResult {
	res := probeResult{mirror: m, url: rewriteURL(m, owner, repo)}
	start := time.Now()
	if hp := mirrorHostPort(m, owner, repo); hp != "" {
		ok, d := checkReachable(ctx, hp, connTimeout)
		if !ok {
			res.stage = "unreachable"
			res.d = d
			return res
		}
	}
	res.ok = probeRepo(ctx, res.url, probeTimeout)
	res.d = time.Since(start)
	if res.ok {
		res.stage = "hit"
	} else {
		res.stage = "repo"
	}
	return res
}

// probeAndSelect 并行探测所有镜像。命中后若所有更高优先级镜像均已失败则立即返回；
// 否则最多等待 graceWindow 宽限期让更高优先级镜像命中，超时后返回当前优先级最高的命中者。
// onResult 每个结果返回时回调（实时打印）；返回选中命中者与全部命中列表（按优先级排序）。
func probeAndSelect(ctx context.Context, mirrors []Mirror, owner, repo string, onResult func(probeResult)) (*probeResult, []probeResult) {
	results := make([]probeResult, len(mirrors))
	resolved := make([]bool, len(mirrors))
	ch := make(chan int, len(mirrors))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // 提前返回时取消剩余探测
	for i, m := range mirrors {
		go func(i int, m Mirror) {
			results[i] = probeOne(ctx, m, owner, repo)
			ch <- i
		}(i, m)
	}
	hits := make(map[int]bool)
	best := -1
	pickBest := func() {
		best = -1
		for i := range results {
			if hits[i] && (best < 0 || results[i].mirror.Priority < results[best].mirror.Priority) {
				best = i
			}
		}
	}
	var graceC <-chan time.Time
	for n := 0; n < len(mirrors); n++ {
		select {
		case i := <-ch:
			resolved[i] = true
			if onResult != nil {
				onResult(results[i])
			}
			if results[i].ok {
				hits[i] = true
			}
			pickBest()
			if best < 0 {
				continue
			}
			// 已有命中：若更高优先级镜像都已失败则立即决策，否则启动宽限计时
			ready := true
			for j := range mirrors {
				if mirrors[j].Priority < mirrors[best].Priority && !resolved[j] {
					ready = false
					break
				}
			}
			if ready {
				return &results[best], collectHits(results, hits)
			}
			if graceC == nil {
				graceC = time.After(graceWindow)
			}
		case <-graceC: // 宽限期到：不再等更高优先级，用当前最佳命中
			pickBest()
			if best >= 0 {
				return &results[best], collectHits(results, hits)
			}
		case <-ctx.Done():
			return nil, collectHits(results, hits)
		}
	}
	pickBest()
	if best < 0 {
		return nil, collectHits(results, hits)
	}
	return &results[best], collectHits(results, hits)
}

// collectHits 收集全部命中结果并按优先级排序（克隆失败时顺延用）。
func collectHits(results []probeResult, hits map[int]bool) []probeResult {
	var out []probeResult
	for i := range results {
		if hits[i] {
			out = append(out, results[i])
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].mirror.Priority < out[j].mirror.Priority
	})
	return out
}

// probeRepo 用 git ls-remote 探测仓库是否可拉取。
// WaitDelay 防止孙进程 git-remote-https 拖住输出管道，导致超时失效（曾出现 8s 超时实际 21s）。
func probeRepo(ctx context.Context, repoURL string, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--symref", repoURL, "HEAD")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.WaitDelay = 2 * time.Second
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return false
	}
	return bytes.TrimSpace(out.Bytes()) != nil
}
