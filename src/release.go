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

var reRelease = regexp.MustCompile(`^(?:https?://)?(?:www\.)?github\.com/([^/]+)/([^/]+)/releases/download/(.+)/([^/]+)$`)

type releaseAsset struct {
	owner, repo, tag, file, raw string
}

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

type downloadCandidate struct {
	name string
	url  string
}

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

func rewriteReleaseURL(m Mirror, a releaseAsset) string {
	t := m.ReleaseTemplate
	t = strings.ReplaceAll(t, "{owner}", a.owner)
	t = strings.ReplaceAll(t, "{repo}", a.repo)
	t = strings.ReplaceAll(t, "{tag}", escapePath(a.tag))
	t = strings.ReplaceAll(t, "{file}", escapePath(a.file))
	t = strings.ReplaceAll(t, "{url}", a.raw)
	return t
}

func escapePath(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		s = u
	}
	return url.PathEscape(s)
}

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

func printProgress(written, total int64) {
	if total > 0 {
		fmt.Printf("\r  已下载 %s / %s（%d%%）", humanSize(written), humanSize(total), written*100/total)
	} else {
		fmt.Printf("\r  已下载 %s", humanSize(written))
	}
}

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
			_ = os.Remove(out)
			continue
		}
		fmt.Printf("%s（%s）\n", colorGreen("完成"), formatDur(time.Since(start)))
		fmt.Printf("  已保存为 %s\n", filepath.Clean(out))
		return 0
	}
	fmt.Println(colorRed("所有镜像均下载失败"))
	return 1
}
