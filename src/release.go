// release.go Release 附件下载：解析下载 URL → 顺序尝试镜像（高校优先）→ 流式下载带进度 → GitHub 直连兜底。
package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// reRelease 匹配 github.com 的 Release 附件下载地址。
var reRelease = regexp.MustCompile(`^(?:https?://)?(?:www\.)?github\.com/([^/]+)/([^/]+)/releases/download/(.+)/([^/]+)$`)

// releaseAsset 解析后的 Release 附件信息。
type releaseAsset struct {
	owner, repo, tag, file, raw string
}

// parseReleaseURL 解析 Release 下载 URL，并把无协议头的输入补齐为 https。
func parseReleaseURL(raw string) (releaseAsset, bool) {
	s := strings.TrimSpace(raw)
	m := reRelease.FindStringSubmatch(s)
	if m == nil {
		return releaseAsset{}, false
	}
	if !strings.HasPrefix(s, "http") {
		s = "https://" + strings.TrimPrefix(strings.TrimPrefix(s, "//"), "http://")
		if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "//") {
			s = "https://" + raw[strings.Index(raw, "github.com"):]
		} else {
			s = "https://github.com/" + m[1] + "/" + m[2] + "/releases/download/" + m[3] + "/" + m[4]
		}
	}
	return releaseAsset{owner: m[1], repo: m[2], tag: m[3], file: m[4], raw: s}, true
}

// downloadCandidate 一个下载候选源（镜像站或 GitHub 直连）。
type downloadCandidate struct {
	name string
	url  string
}

// releaseCandidates 生成候选链：支持 release 的镜像按优先级排前面，GitHub 直连垫底。
func releaseCandidates(cfg *Config, a releaseAsset) []downloadCandidate {
	var out []downloadCandidate
	for _, m := range enabledMirrors(cfg.Mirrors) {
		if m.ReleaseTemplate == "" {
			continue
		}
		out = append(out, downloadCandidate{
			name: m.Name,
			url:  rewriteReleaseURL(m, a),
		})
	}
	out = append(out, downloadCandidate{name: "GitHub 直连", url: a.raw})
	return out
}

// rewriteReleaseURL 填充 release 模板变量；{tag}/{file} 是路径段，需 URL 转义。
func rewriteReleaseURL(m Mirror, a releaseAsset) string {
	t := m.ReleaseTemplate
	t = strings.ReplaceAll(t, "{owner}", a.owner)
	t = strings.ReplaceAll(t, "{repo}", a.repo)
	t = strings.ReplaceAll(t, "{tag}", escapePath(a.tag))
	t = strings.ReplaceAll(t, "{file}", escapePath(a.file))
	t = strings.ReplaceAll(t, "{url}", a.raw)
	return t
}

// escapePath 先解码再重新编码，统一处理用户粘贴的「已编码」URL（如 %20 的 tag）。
func escapePath(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		s = u
	}
	return url.PathEscape(s)
}

// downloadOnce 从 rawURL 流式下载保存到 out 文件；非 HTTP 200 直接报错。
func downloadOnce(rawURL, out string) error {
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: connTimeout}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second,
	}
	client := &http.Client{Transport: transport}
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "edgit/"+version)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()

	// 边读边写，每 200ms 刷新一次进度显示
	var written int64
	total := resp.ContentLength
	buf := make([]byte, 128*1024)
	last := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			written += int64(n)
			if time.Since(last) > 200*time.Millisecond {
				printProgress(written, total)
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	printProgress(written, total)
	fmt.Println()
	return nil
}

// printProgress 单行刷新下载进度（有总大小显示百分比，否则只显示已下载量）。
func printProgress(written, total int64) {
	if total > 0 {
		fmt.Printf("\r  已下载 %s / %s（%d%%）", humanSize(written), humanSize(total), written*100/total)
	} else {
		fmt.Printf("\r  已下载 %s", humanSize(written))
	}
}

// humanSize 字节数转人类可读大小（B/KB/MB/GB）。
func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.2f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.2f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// cmdGet edgit get 主流程：逐候选尝试下载，失败删掉半成品换下一个，全失败退出码 1。
func cmdGet(args []string) int {
	if len(args) < 1 {
		fatalf("用法: edgit get <release下载URL> [输出文件名]")
	}
	asset, ok := parseReleaseURL(args[0])
	if !ok {
		fatalf("无法识别的 Release 地址（需要形如 https://github.com/<owner>/<repo>/releases/download/<tag>/<文件>）")
	}
	out := asset.file
	if len(args) > 1 {
		out = strings.TrimSpace(args[1])
	}

	cfg, err := loadConfig()
	if err != nil {
		warnf("读取配置失败（%v），使用内置镜像", err)
		cfg = defaultConfig()
	}

	fmt.Printf("下载 %s/%s@%s → %s\n", asset.owner, asset.repo, asset.tag, out)
	cands := releaseCandidates(cfg, asset)
	for i, c := range cands {
		fmt.Printf("[%d/%d] %s 尝试下载 ", i+1, len(cands), c.name)
		start := time.Now()
		err := downloadOnce(c.url, out)
		if err != nil {
			fmt.Printf("%s（%s）\n", colorYellow("跳过"), formatDur(time.Since(start)))
			_ = os.Remove(out) // 清理下载失败留下的半成品
			continue
		}
		fmt.Printf("%s（%s）\n", colorGreen("完成"), formatDur(time.Since(start)))
		fmt.Printf("  已保存为 %s\n", filepath.Clean(out))
		return 0
	}
	fmt.Println(colorRed("所有镜像均下载失败"))
	return 1
}
