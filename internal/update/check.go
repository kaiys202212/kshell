package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	Owner         = "kaiys202212"
	Repo          = "kshell"
	GitCodeOwner  = "abraveheart2023"
	GitCodeRepo   = Repo
	ZipName       = "kshell-desktop-windows-amd64.zip"
	SumsName      = "SHA256SUMS"
	LatestAPIURL  = "https://api.github.com/repos/" + Owner + "/" + Repo + "/releases/latest"
	userAgent     = "kshell"
	maxGetBytes   = 64 << 20
	checkTimeout  = 12 * time.Second
	applyTimeout  = 3 * time.Minute
)

// GitCodeLatestURL 是国内主源的 latest API。
func GitCodeLatestURL() string {
	return "https://api.gitcode.com/api/v5/repos/" + GitCodeOwner + "/" + GitCodeRepo + "/releases/latest"
}

// GitCodeAttachURL 公开附件下载（无需登录的公开仓库）。
func GitCodeAttachURL(tag, fileName string) string {
	return "https://api.gitcode.com/api/v5/repos/" + GitCodeOwner + "/" + GitCodeRepo +
		"/releases/" + tag + "/attach_files/" + fileName + "/download"
}

// 测试可改 GOOS/GOARCH，生产用 runtime。
var (
	currentGOOS   = runtime.GOOS
	currentGOARCH = runtime.GOARCH
)

// PackageZipName 是桌面发布包文件名：kshell-desktop-{GOOS}-{GOARCH}.zip。
func PackageZipName(goos, goarch string) string {
	return "kshell-desktop-" + goos + "-" + goarch + ".zip"
}

// PackageBinaryName 是 zip 内可执行文件名。
func PackageBinaryName(goos string) string {
	if goos == "windows" {
		return "kshell-desktop.exe"
	}
	return "kshell-desktop"
}

// CurrentPackageZip 返回当前运行平台应对应的 Release zip。
func CurrentPackageZip() string {
	return PackageZipName(currentGOOS, currentGOARCH)
}

func currentBinaryName() string {
	return PackageBinaryName(currentGOOS)
}

// Source 是更新入口。LatestURL 非空则检查走该 API；Attach 表示 zip 缺失时填 GitCode 附件链。
type Source struct {
	Name      string
	Prefix    string
	LatestURL string
	Attach    bool
}

// Wrap 把官方 URL 转成经该源访问的地址。
func (s Source) Wrap(rawURL string) string {
	p := strings.TrimRight(s.Prefix, "/")
	if p == "" {
		return rawURL
	}
	return p + "/" + rawURL
}

func (s Source) checkURL() string {
	if s.LatestURL != "" {
		return s.LatestURL
	}
	return s.Wrap(LatestAPIURL)
}

// DefaultSources GitCode 国内主源，其后公共加速，官方最后。
func DefaultSources() []Source {
	return []Source{
		{Name: "gitcode", LatestURL: GitCodeLatestURL(), Attach: true},
		{Name: "ghfast", Prefix: "https://ghfast.top/"},
		{Name: "gh-proxy", Prefix: "https://gh-proxy.com/"},
		{Name: "ghproxy.net", Prefix: "https://ghproxy.net/"},
		{Name: "mirror.ghproxy", Prefix: "https://mirror.ghproxy.com/"},
		{Name: "github", Prefix: ""},
	}
}

// GetFunc 拉取 URL 正文；测试注入，生产用 HTTP。
type GetFunc func(ctx context.Context, url string) ([]byte, error)

// Client 按源顺序检查 GitHub latest release。
type Client struct {
	Current string
	Sources []Source
	Get     GetFunc
	HTTP    *http.Client
}

// CheckResult 是一次检查的结果。
type CheckResult struct {
	Current   string
	Latest    string
	Notes     string
	ZipURL    string
	SumsURL   string
	Available bool
	Skipped   bool
	Reason    string
	Source    string
}

type releaseJSON struct {
	TagName string `json:"tag_name"`
	Body    string `json:"body"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// ShouldSkip 开发/未知版本不查网。
func ShouldSkip(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "dev", "unknown":
		return true
	default:
		return false
	}
}

func canon(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

// Newer 报告 latest 是否严格新于 current。
func Newer(current, latest string) bool {
	a, b := canon(current), canon(latest)
	if !semver.IsValid(a) || !semver.IsValid(b) {
		return false
	}
	return semver.Compare(a, b) < 0
}

// Check 依次尝试各源，直到 latest release JSON 成功。
func (c Client) Check(ctx context.Context) (CheckResult, error) {
	cur := strings.TrimSpace(c.Current)
	out := CheckResult{Current: cur}
	if ShouldSkip(cur) {
		out.Skipped = true
		out.Reason = "update.reason.dev_build"
		return out, nil
	}
	src := c.Sources
	if len(src) == 0 {
		src = DefaultSources()
	}
	get := c.Get
	if get == nil {
		get = c.httpGet
	}
	var last error
	for _, s := range src {
		u := s.checkURL()
		raw, err := get(ctx, u)
		if err != nil {
			last = err
			continue
		}
		var rel releaseJSON
		if err := json.Unmarshal(raw, &rel); err != nil {
			last = err
			continue
		}
		out.Latest = rel.TagName
		out.Notes = rel.Body
		out.Source = s.Name
		wantZip := CurrentPackageZip()
		for _, a := range rel.Assets {
			switch a.Name {
			case wantZip:
				out.ZipURL = a.BrowserDownloadURL
			case SumsName:
				out.SumsURL = a.BrowserDownloadURL
			}
		}
		if out.ZipURL == "" && s.Attach && rel.TagName != "" {
			out.ZipURL = GitCodeAttachURL(rel.TagName, wantZip)
			if out.SumsURL == "" {
				out.SumsURL = GitCodeAttachURL(rel.TagName, SumsName)
			}
		}
		if out.ZipURL == "" {
			last = fmt.Errorf("err.update.release_missing_asset|%s|%s", rel.TagName, wantZip)
			continue
		}
		out.Available = Newer(cur, rel.TagName)
		if !out.Available {
			out.Reason = "update.reason.up_to_date"
		}
		return out, nil
	}
	if last == nil {
		last = fmt.Errorf("err.update.no_source")
	}
	return out, last
}

// errNoDownloadSource 是下载无候选源时的兜底（wire key，前端直译）。
// 调用方（apply.go）需原样返回，不可再包 `err.update.*|%w`，否则嵌套 wire key 无法翻译。
var errNoDownloadSource = errors.New("err.update.download_source_unavailable")

func (c Client) getThroughSources(ctx context.Context, official string) ([]byte, error) {
	src := c.Sources
	if len(src) == 0 {
		src = DefaultSources()
	}
	get := c.Get
	if get == nil {
		get = c.httpGet
	}
	var last error
	for _, u := range downloadCandidates(official, src) {
		b, err := get(ctx, u)
		if err != nil {
			last = err
			continue
		}
		return b, nil
	}
	if last == nil {
		last = errNoDownloadSource
	}
	return nil, last
}

func downloadCandidates(official string, src []Source) []string {
	if !strings.Contains(official, "github.com") {
		return []string{official}
	}
	seen := map[string]bool{}
	var out []string
	add := func(u string) {
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		out = append(out, u)
	}
	for _, s := range src {
		if s.Prefix != "" {
			add(s.Wrap(official))
		}
	}
	add(official)
	return out
}

func (c Client) httpGet(ctx context.Context, url string) ([]byte, error) {
	cli := c.HTTP
	if cli == nil {
		cli = &http.Client{Timeout: checkTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if strings.Contains(url, "api.github.com") {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxGetBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	return b, nil
}
