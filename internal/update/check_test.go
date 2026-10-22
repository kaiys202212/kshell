package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func pinCheckWindowsPackage(t *testing.T) {
	t.Helper()
	prevOS, prevArch := currentGOOS, currentGOARCH
	currentGOOS, currentGOARCH = "windows", "amd64"
	t.Cleanup(func() { currentGOOS, currentGOARCH = prevOS, prevArch })
}

func TestPackageZipAndBinaryName(t *testing.T) {
	if got := PackageZipName("linux", "amd64"); got != "kshell-desktop-linux-amd64.zip" {
		t.Fatalf("linux zip = %q", got)
	}
	if got := PackageZipName("darwin", "arm64"); got != "kshell-desktop-darwin-arm64.zip" {
		t.Fatalf("darwin zip = %q", got)
	}
	if got := PackageZipName("windows", "amd64"); got != ZipName {
		t.Fatalf("windows zip = %q, want 与 ZipName 一致", got)
	}
	if PackageBinaryName("windows") != "kshell-desktop.exe" {
		t.Fatal("windows 二进制应带 .exe")
	}
	if PackageBinaryName("linux") != "kshell-desktop" {
		t.Fatal("unix 二进制不应带后缀")
	}
}

func TestCheckSelectsCurrentPlatformZip(t *testing.T) {
	prevOS, prevArch := currentGOOS, currentGOARCH
	currentGOOS, currentGOARCH = "linux", "amd64"
	t.Cleanup(func() { currentGOOS, currentGOARCH = prevOS, prevArch })

	winURL := "https://github.com/kaiys202212/kshell/releases/download/v0.2.0/" + ZipName
	linZip := PackageZipName("linux", "amd64")
	linURL := "https://github.com/kaiys202212/kshell/releases/download/v0.2.0/" + linZip
	body := `{
  "tag_name": "v0.2.0",
  "body": "多平台",
  "assets": [
    {"name": "` + ZipName + `", "browser_download_url": "` + winURL + `"},
    {"name": "` + linZip + `", "browser_download_url": "` + linURL + `"},
    {"name": "` + SumsName + `", "browser_download_url": "https://github.com/kaiys202212/kshell/releases/download/v0.2.0/` + SumsName + `"}
  ]
}`
	c := Client{
		Current: "v0.1.0",
		Sources: []Source{{Name: "github", Prefix: ""}},
		Get:     func(context.Context, string) ([]byte, error) { return []byte(body), nil },
	}
	got, err := c.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.ZipURL != linURL {
		t.Fatalf("ZipURL = %q, want linux 包 %q", got.ZipURL, linURL)
	}
}

func TestShouldSkip(t *testing.T) {
	for _, v := range []string{"", "dev", "DEV", "unknown"} {
		if !ShouldSkip(v) {
			t.Fatalf("ShouldSkip(%q) = false", v)
		}
	}
	if ShouldSkip("v0.1.0") {
		t.Fatal("正式版本不应跳过")
	}
}

func TestCheckReportsUpToDateReason(t *testing.T) {
	pinCheckWindowsPackage(t)
	body := githubLatestJSON("v0.1.0", "https://github.com/kaiys202212/kshell/releases/download/v0.1.0/"+ZipName)
	c := Client{
		Current: "v0.1.0",
		Sources: []Source{{Name: "github", Prefix: ""}},
		Get:     func(context.Context, string) ([]byte, error) { return []byte(body), nil },
	}
	got, err := c.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Available {
		t.Fatalf("同版本不应有更新: %+v", got)
	}
	if got.Reason != "update.reason.up_to_date" {
		t.Fatalf("Reason = %q, want update.reason.up_to_date", got.Reason)
	}
}

func TestCheckMissingAssetReturnsKey(t *testing.T) {
	pinCheckWindowsPackage(t)
	body := `{"tag_name":"v0.2.0","body":"","assets":[]}`
	c := Client{
		Current: "v0.1.0",
		Sources: []Source{{Name: "github", Prefix: ""}},
		Get:     func(context.Context, string) ([]byte, error) { return []byte(body), nil },
	}
	_, err := c.Check(context.Background())
	if err == nil {
		t.Fatal("缺资产应报错")
	}
	if want := "err.update.release_missing_asset|v0.2.0|" + ZipName; err.Error() != want {
		t.Fatalf("err = %q, want %q", err.Error(), want)
	}
}

func TestNewer(t *testing.T) {
	if !Newer("v0.1.0", "v0.2.0") {
		t.Fatal("0.2.0 应新于 0.1.0")
	}
	if Newer("v0.2.0", "v0.2.0") {
		t.Fatal("相等不应视为更新")
	}
	if Newer("v0.3.0", "v0.2.0") {
		t.Fatal("旧版本不应视为更新")
	}
	if !Newer("0.1.0", "v0.1.1") {
		t.Fatal("缺 v 前缀仍应能比较")
	}
}

func TestCheckPrefersFirstSuccessfulProxy(t *testing.T) {
	pinCheckWindowsPackage(t)
	body := githubLatestJSON("v0.2.0", "https://github.com/kaiys202212/kshell/releases/download/v0.2.0/"+ZipName)
	var seen []string
	c := Client{
		Current: "v0.1.0",
		Sources: DefaultSources(),
		Get: func(_ context.Context, url string) ([]byte, error) {
			seen = append(seen, url)
			if len(seen) == 1 {
				return nil, errors.New("超时")
			}
			return []byte(body), nil
		},
	}
	got, err := c.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Available {
		t.Fatalf("Available = false: %+v", got)
	}
	if got.Latest != "v0.2.0" {
		t.Fatalf("Latest = %q", got.Latest)
	}
	if got.Source != DefaultSources()[1].Name {
		t.Fatalf("Source = %q, want GitCode 失败后的下一源", got.Source)
	}
	if got.ZipURL == "" {
		t.Fatal("ZipURL 为空")
	}
	if seen[0] != GitCodeLatestURL() {
		t.Fatalf("优先请求 = %q, want GitCode latest %q", seen[0], GitCodeLatestURL())
	}
}

func TestCheckUsesGitCodeAttachWhenZipAssetMissing(t *testing.T) {
	pinCheckWindowsPackage(t)
	body := `{
  "tag_name": "v0.2.0",
  "body": "国内",
  "assets": [
    {"name": "kshell-v0.2.0.zip", "browser_download_url": "https://gitcode.com/x/-/archive/v0.2.0/x.zip"}
  ]
}`
	c := Client{
		Current: "v0.1.0",
		Sources: DefaultSources(),
		Get: func(_ context.Context, url string) ([]byte, error) {
			return []byte(body), nil
		},
	}
	got, err := c.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantZip := GitCodeAttachURL("v0.2.0", ZipName)
	if got.ZipURL != wantZip {
		t.Fatalf("ZipURL = %q, want attach %q", got.ZipURL, wantZip)
	}
	if got.SumsURL != GitCodeAttachURL("v0.2.0", SumsName) {
		t.Fatalf("SumsURL = %q", got.SumsURL)
	}
	if got.Source != "gitcode" {
		t.Fatalf("Source = %q", got.Source)
	}
}

// 跨源取最大：靠前的源返回过期 latest（镜像缓存滞后）时，应继续问后续源并取
// semver 最大的结果，而不是首胜短路。这是「一次只升一个小版本」问题的回归测试。
func TestCheckTakesMaxAcrossSources(t *testing.T) {
	pinCheckWindowsPackage(t)
	stale := githubLatestJSON("v0.1.17", "https://github.com/kaiys202212/kshell/releases/download/v0.1.17/"+ZipName)
	fresh := githubLatestJSON("v0.1.18", "https://github.com/kaiys202212/kshell/releases/download/v0.1.18/"+ZipName)
	c := Client{
		Current: "v0.1.10",
		Sources: []Source{
			{Name: "stale-mirror", Prefix: "https://stale.example/"},
			{Name: "github", Prefix: ""},
		},
		Get: func(_ context.Context, url string) ([]byte, error) {
			if strings.HasPrefix(url, "https://stale.example/") {
				return []byte(stale), nil
			}
			return []byte(fresh), nil
		},
	}
	got, err := c.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Latest != "v0.1.18" {
		t.Fatalf("Latest = %q, want v0.1.18（应跨源取最大而非首胜）", got.Latest)
	}
	if got.Source != "github" {
		t.Fatalf("Source = %q, want github", got.Source)
	}
	if !got.Available {
		t.Fatalf("Available = false: %+v", got)
	}
	if !strings.Contains(got.ZipURL, "v0.1.18") {
		t.Fatalf("ZipURL = %q, 应取自胜出源", got.ZipURL)
	}
}

// 胜出源（最大 tag）缺 zip 资产时，应退而取下一个有资产的次新源，而不是整体失败。
func TestCheckFallsBackWhenMaxSourceMissingAsset(t *testing.T) {
	pinCheckWindowsPackage(t)
	freshNoAsset := `{"tag_name":"v0.1.18","body":"","assets":[]}`
	stale := githubLatestJSON("v0.1.17", "https://github.com/kaiys202212/kshell/releases/download/v0.1.17/"+ZipName)
	c := Client{
		Current: "v0.1.10",
		Sources: []Source{
			{Name: "stale-mirror", Prefix: "https://stale.example/"},
			{Name: "github", Prefix: ""},
		},
		Get: func(_ context.Context, url string) ([]byte, error) {
			if strings.HasPrefix(url, "https://stale.example/") {
				return []byte(stale), nil
			}
			return []byte(freshNoAsset), nil
		},
	}
	got, err := c.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Latest != "v0.1.17" {
		t.Fatalf("Latest = %q, want v0.1.17（最大源缺资产时退回次新可用源）", got.Latest)
	}
	if !got.Available {
		t.Fatalf("Available = false: %+v", got)
	}
}

func TestCheckSendsNoCacheHeader(t *testing.T) {
	pinCheckWindowsPackage(t)
	body := githubLatestJSON("v0.2.0", "https://github.com/kaiys202212/kshell/releases/download/v0.2.0/"+ZipName)
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("Cache-Control")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c := Client{
		Current: "v0.1.0",
		Sources: []Source{{Name: "local", LatestURL: srv.URL + "/repos/x/releases/latest"}},
	}
	if _, err := c.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotHeader != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", gotHeader)
	}
}

// no-cache 只用于 latest 检查，zip/sums 大文件下载不应带头强制回源。
func TestDownloadDoesNotSendNoCacheHeader(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("Cache-Control")
		_, _ = w.Write([]byte("DATA"))
	}))
	defer srv.Close()
	c := Client{HTTP: srv.Client()}
	if _, err := c.getThroughSources(context.Background(), srv.URL+"/download/v0.2.0/"+ZipName); err != nil {
		t.Fatal(err)
	}
	if gotHeader != "" {
		t.Fatalf("下载请求不应带 Cache-Control, got %q", gotHeader)
	}
}

func TestCheckSkipsDev(t *testing.T) {
	c := Client{Current: "dev", Get: func(context.Context, string) ([]byte, error) {
		t.Fatal("dev 不应发网")
		return nil, nil
	}}
	got, err := c.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Skipped || got.Available {
		t.Fatalf("%+v", got)
	}
	if got.Reason != "update.reason.dev_build" {
		t.Fatalf("Reason = %q, want update.reason.dev_build", got.Reason)
	}
}

func TestCheckAllSourcesFail(t *testing.T) {
	c := Client{
		Current: "v0.1.0",
		Sources: DefaultSources(),
		Get:     func(context.Context, string) ([]byte, error) { return nil, errors.New("down") },
	}
	_, err := c.Check(context.Background())
	if err == nil {
		t.Fatal("期望错误")
	}
}

func githubLatestJSON(tag, zipURL string) string {
	return `{
  "tag_name": "` + tag + `",
  "body": "修复升级",
  "assets": [
    {"name": "` + ZipName + `", "browser_download_url": "` + zipURL + `"},
    {"name": "` + SumsName + `", "browser_download_url": "https://github.com/kaiys202212/kshell/releases/download/` + tag + `/` + SumsName + `"}
  ]
}`
}

func TestCheckSkipsDarwinWithoutNetwork(t *testing.T) {
	prevOS, prevArch := currentGOOS, currentGOARCH
	currentGOOS, currentGOARCH = "darwin", "arm64"
	t.Cleanup(func() { currentGOOS, currentGOARCH = prevOS, prevArch })

	called := false
	c := Client{
		Current: "v0.1.0",
		Sources: []Source{{Name: "github", Prefix: ""}},
		Get: func(context.Context, string) ([]byte, error) {
			called = true
			return nil, errors.New("darwin 门控不应发起网络请求")
		},
	}
	got, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("darwin 门控应返回 nil error: %v", err)
	}
	if called {
		t.Fatal("darwin 门控不应发起任何网络请求")
	}
	if !got.Skipped {
		t.Fatal("darwin 上 Check 应置 Skipped")
	}
	if got.Reason != "update.reason.platform_unsupported" {
		t.Fatalf("Reason = %q", got.Reason)
	}
	if got.Available {
		t.Fatal("darwin 上不应报有可用更新")
	}
}
