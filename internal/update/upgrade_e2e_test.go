package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestUpgradePredownloadThenOfflineApply 模拟一次完整升级：
// 从 release JSON 检查到新版本 → 预下载并校验当前平台包 → 断网安装（只用缓存）。
// 覆盖线上最关键的链路：缓存 tag/哈希匹配、提取可执行文件、调度替换。
func TestUpgradePredownloadThenOfflineApply(t *testing.T) {
	pinWindowsPackage(t)

	zipBytes := mustZip(t, map[string][]byte{"kshell-desktop.exe": []byte("NEWBIN")})
	zipName := CurrentPackageZip()
	sum := sha256.Sum256(zipBytes)
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + zipName + "\n")

	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		fmt.Fprintf(w, `{"tag_name":"v0.2.0","assets":[`+
			`{"name":%q,"browser_download_url":%q},`+
			`{"name":%q,"browser_download_url":%q}]}`,
			zipName, base+"/zip", SumsName, base+"/sums")
	})
	mux.HandleFunc("/zip", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(zipBytes) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(sums) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := Client{
		Current: "v0.1.0",
		Sources: []Source{{Name: "test", LatestURL: srv.URL + "/latest"}},
	}
	r, err := c.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !r.Available || r.Latest != "v0.2.0" {
		t.Fatalf("check = %+v", r)
	}

	cacheDir := t.TempDir()
	if err := Prepare(context.Background(), PrepareOptions{
		Tag:      r.Latest,
		ZipURL:   r.ZipURL,
		SumsURL:  r.SumsURL,
		CacheDir: cacheDir,
	}); err != nil {
		t.Fatal(err)
	}
	if !IsCached(cacheDir, r.Latest) {
		t.Fatal("预下载后应命中缓存")
	}

	dest := filepath.Join(t.TempDir(), "kshell-desktop.exe")
	if err := os.WriteFile(dest, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	var spawned [][2]string
	// 断网：Get 一旦被调用即失败，证明安装只依赖缓存。
	err = Apply(context.Background(), ApplyOptions{
		Tag:      r.Latest,
		ZipURL:   "https://offline.invalid/zip",
		SumsURL:  "https://offline.invalid/sums",
		DestExe:  dest,
		CacheDir: cacheDir,
		Get: func(context.Context, string) ([]byte, error) {
			t.Fatal("断网安装不应再发起下载")
			return nil, nil
		},
		SpawnReplace: func(current, next string) error {
			spawned = append(spawned, [2]string{current, next})
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest + ".new")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEWBIN" {
		t.Fatalf("新版本内容 = %q", got)
	}
	if len(spawned) != 1 || spawned[0][0] != dest || spawned[0][1] != dest+".new" {
		t.Fatalf("替换调度 = %#v", spawned)
	}
}

// 说明：完整的「下载→校验→解压→替换」由 Apply 测试覆盖；
// PowerShell 替换脚本另有真跑用例（replace_windows_test.go）。
