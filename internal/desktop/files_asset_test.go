package desktop

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/discovery"
)

// assetURL 构造通道 URL：前缀 + base64url(目录) [+ "/" + 相对名]。
func assetURL(dir, rel string) string {
	u := assetFilePrefix + base64.RawURLEncoding.EncodeToString([]byte(dir))
	if rel != "" {
		u += "/" + rel
	}
	return u
}

// getAssetFile 以目录+相对名请求通道，返回 recorder。
func getAssetFile(t *testing.T, app *App, dir, rel string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, assetURL(dir, rel), nil)
	rec := httptest.NewRecorder()
	app.WorkspaceFileHandler().ServeHTTP(rec, req)
	return rec
}

func TestWorkspaceFileHandlerServesSubresource(t *testing.T) {
	env := newFilesEnv(t)
	env.app.rawWorkspaces = []discovery.Workspace{{Path: env.root, Kind: discovery.KindLocal}}
	css := filepath.Join(env.root, "assets", "style.css")
	if err := os.MkdirAll(filepath.Dir(css), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(css, []byte("body{color:red}"), 0o600); err != nil {
		t.Fatal(err)
	}

	rec := getAssetFile(t, env.app, env.root, filepath.Join("assets", "style.css"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/css; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("应带 no-store")
	}
	if rec.Body.String() != "body{color:red}" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestWorkspaceFileHandlerRejectsTraversalAndOutside(t *testing.T) {
	env := newFilesEnv(t)
	env.app.rawWorkspaces = []discovery.Workspace{{Path: env.root, Kind: discovery.KindLocal}}
	outside := filepath.Join(filepath.Dir(env.root), "outside.css")
	if err := os.WriteFile(outside, []byte("h1{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outside)
	if err := os.WriteFile(filepath.Join(env.root, "ok.css"), []byte("p{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	// 相对名带 .. 穿越：Clean 后不在根内，拒绝
	if rec := getAssetFile(t, env.app, env.root, "../"+filepath.Base(outside)); rec.Code == http.StatusOK {
		t.Fatal(".. 穿越不应放行")
	}
	// 目录段直接指向工作区外：拒绝
	if rec := getAssetFile(t, env.app, outside, "outside.css"); rec.Code == http.StatusOK {
		t.Fatal("工作区外目录不应放行")
	}
	// 对照：工作区内文件正常放行
	if rec := getAssetFile(t, env.app, env.root, "ok.css"); rec.Code != http.StatusOK {
		t.Fatalf("工作区内文件应放行，got %d", rec.Code)
	}
}

func TestWorkspaceFileHandlerRejectsDeniedAndUnknownExt(t *testing.T) {
	env := newFilesEnv(t)
	env.app.rawWorkspaces = []discovery.Workspace{{Path: env.root, Kind: discovery.KindLocal}}
	page := filepath.Join(env.root, "page.html")
	if err := os.WriteFile(page, []byte("<h1>x</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if rec := getAssetFile(t, env.app, env.root, "page.html"); rec.Code != http.StatusForbidden {
		t.Fatalf("html 应拒绝，got %d", rec.Code)
	}

	exe := filepath.Join(env.root, "tool.exe")
	if err := os.WriteFile(exe, []byte{0x4d, 0x5a}, 0o600); err != nil {
		t.Fatal(err)
	}
	if rec := getAssetFile(t, env.app, env.root, "tool.exe"); rec.Code != http.StatusForbidden {
		t.Fatalf("白名单外应拒绝，got %d", rec.Code)
	}
}

func TestWorkspaceFileHandlerRejectsSSHRootAndDir(t *testing.T) {
	env := newFilesEnv(t)
	// 仅登记 SSH 工作区：本地路径即使拼出来也不在允许根内
	env.app.rawWorkspaces = []discovery.Workspace{{
		Path: "ssh://c1/home/u/proj", Kind: discovery.KindSSH, ConnID: "c1", RemotePath: "/home/u/proj",
	}}
	if rec := getAssetFile(t, env.app, env.root, "main.go"); rec.Code == http.StatusOK {
		t.Fatal("SSH 工作区不应放行本地文件")
	}

	// 目录本身不可读（rel 为空时解出目录）
	env.app.rawWorkspaces = []discovery.Workspace{{Path: env.root, Kind: discovery.KindLocal}}
	if rec := getAssetFile(t, env.app, env.root, ""); rec.Code == http.StatusOK {
		t.Fatal("目录不应放行")
	}
}

func TestWorkspaceFileHandlerRejectsBadRequests(t *testing.T) {
	env := newFilesEnv(t)
	env.app.rawWorkspaces = []discovery.Workspace{{Path: env.root, Kind: discovery.KindLocal}}
	h := env.app.WorkspaceFileHandler()

	// 非 GET
	post := httptest.NewRequest(http.MethodPost, assetURL(env.root, "main.go"), nil)
	recPost := httptest.NewRecorder()
	h.ServeHTTP(recPost, post)
	if recPost.Code == http.StatusOK {
		t.Fatal("POST 不应放行")
	}
	// 缺前缀 / 非法 base64 / 相对路径
	for _, p := range []string{"/other", assetFilePrefix + "!!!", assetFilePrefix + base64.RawURLEncoding.EncodeToString([]byte("rel/a.css"))} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatalf("请求 %q 不应放行", p)
		}
	}
}
