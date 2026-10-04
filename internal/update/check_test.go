package update

import (
	"context"
	"errors"
	"testing"
)

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
		t.Fatalf("Source = %q, want 第二个代理", got.Source)
	}
	if got.ZipURL == "" {
		t.Fatal("ZipURL 为空")
	}
	wantFirst := DefaultSources()[0].Wrap(LatestAPIURL)
	if seen[0] != wantFirst {
		t.Fatalf("优先请求 = %q, want %q", seen[0], wantFirst)
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
