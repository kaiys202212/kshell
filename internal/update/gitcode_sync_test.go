package update

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncGitCodeCreatesReleaseAndUploads(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, ZipName)
	if err := os.WriteFile(zipPath, []byte("ZIPDATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	sumsPath := filepath.Join(dir, SumsName)
	if err := os.WriteFile(sumsPath, []byte("abc  "+ZipName+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var created bool
	var uploaded []string
	var obs http.Handler
	mux := http.NewServeMux()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	mux.HandleFunc("/api/v5/repos/"+GitCodeOwner+"/kshell/releases/tags/v0.2.0", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"not found"}`))
	})
	mux.HandleFunc("/api/v5/repos/"+GitCodeOwner+"/kshell/releases", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		created = true
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.2.0"})
	})
	mux.HandleFunc("/api/v5/repos/"+GitCodeOwner+"/kshell/releases/v0.2.0/upload_url", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("file_name")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"url": srv.URL + "/obs/" + name,
			"headers": map[string]string{
				"Content-Type": "application/octet-stream",
			},
		})
	})
	mux.HandleFunc("/obs/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		uploaded = append(uploaded, r.URL.Path+"|"+string(b))
		w.WriteHeader(http.StatusOK)
	})
	_ = obs

	err := SyncGitCode(context.Background(), SyncOptions{
		BaseURL: srv.URL + "/api/v5",
		Token:   "tok",
		Owner:   GitCodeOwner,
		Repo:    "kshell",
		Tag:     "v0.2.0",
		Dir:     dir,
		HTTP:    srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("应创建 GitCode Release")
	}
	if len(uploaded) < 2 {
		t.Fatalf("上传数 = %d, want ≥2: %v", len(uploaded), uploaded)
	}
	joined := strings.Join(uploaded, "\n")
	if !strings.Contains(joined, "ZIPDATA") || !strings.Contains(joined, ZipName) {
		t.Fatalf("未上传 zip: %s", joined)
	}
}

func TestSyncGitCodeSkipsWithoutToken(t *testing.T) {
	if err := SyncGitCode(context.Background(), SyncOptions{Tag: "v0.1.0", Dir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
}

// dist 里的 DMG 安装包也要同步到 GitCode Release（国内用户的下载入口）。
func TestSyncGitCodeUploadsDmg(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ZipName), []byte("ZIPDATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	dmgName := "kshell-desktop-darwin-arm64.dmg"
	if err := os.WriteFile(filepath.Join(dir, dmgName), []byte("DMGDATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, SumsName), []byte("abc  "+ZipName+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var uploaded []string
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/api/v5/repos/"+GitCodeOwner+"/kshell/releases/tags/v0.2.0", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.2.0"})
	})
	mux.HandleFunc("/api/v5/repos/"+GitCodeOwner+"/kshell/releases/v0.2.0/upload_url", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("file_name")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"url":     srv.URL + "/obs/" + name,
			"headers": map[string]string{"Content-Type": "application/octet-stream"},
		})
	})
	mux.HandleFunc("/obs/", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		uploaded = append(uploaded, r.URL.Path+"|"+string(b))
		w.WriteHeader(http.StatusOK)
	})

	err := SyncGitCode(context.Background(), SyncOptions{
		BaseURL: srv.URL + "/api/v5",
		Token:   "tok",
		Owner:   GitCodeOwner,
		Repo:    "kshell",
		Tag:     "v0.2.0",
		Dir:     dir,
		HTTP:    srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(uploaded, "\n")
	if !strings.Contains(joined, dmgName) || !strings.Contains(joined, "DMGDATA") {
		t.Fatalf("DMG 未上传: %s", joined)
	}
}

// Release 已存在（创建遇 409/422）时，应补 PATCH 把 release_status 标为 latest，
// 避免重跑 CI/重建 tag 后 GitCode /releases/latest 仍指向旧版本。
func TestSyncGitCodePatchesLatestOnConflict(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ZipName), []byte("ZIPDATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, SumsName), []byte("abc  "+ZipName+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var patched bool
	var patchBody string
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/api/v5/repos/"+GitCodeOwner+"/kshell/releases/tags/v0.2.0", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			b, _ := io.ReadAll(r.Body)
			patched = true
			patchBody = string(b)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.2.0"})
			return
		}
		// releaseExists 查询返回 404，走 createRelease 分支
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"not found"}`))
	})
	mux.HandleFunc("/api/v5/repos/"+GitCodeOwner+"/kshell/releases", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"already exists"}`))
	})
	mux.HandleFunc("/api/v5/repos/"+GitCodeOwner+"/kshell/releases/v0.2.0/upload_url", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("file_name")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"url":     srv.URL + "/obs/" + name,
			"headers": map[string]string{"Content-Type": "application/octet-stream"},
		})
	})
	mux.HandleFunc("/obs/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	err := SyncGitCode(context.Background(), SyncOptions{
		BaseURL: srv.URL + "/api/v5",
		Token:   "tok",
		Owner:   GitCodeOwner,
		Repo:    "kshell",
		Tag:     "v0.2.0",
		Dir:     dir,
		HTTP:    srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !patched {
		t.Fatal("创建遇 409 后应 PATCH release_status=latest")
	}
	if !strings.Contains(patchBody, "latest") {
		t.Fatalf("PATCH body = %q, 应含 release_status=latest", patchBody)
	}
}

// Release 已存在（releaseExists GET 200，重跑 CI 的主路径）时同样要补 PATCH
// release_status=latest，否则重建 tag 后 /releases/latest 仍指旧版本。
func TestSyncGitCodePatchesLatestWhenExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ZipName), []byte("ZIPDATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, SumsName), []byte("abc  "+ZipName+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var patched bool
	var uploaded bool
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/api/v5/repos/"+GitCodeOwner+"/kshell/releases/tags/v0.2.0", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patched = true
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.2.0"})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.2.0"})
	})
	mux.HandleFunc("/api/v5/repos/"+GitCodeOwner+"/kshell/releases/v0.2.0/upload_url", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("file_name")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"url":     srv.URL + "/obs/" + name,
			"headers": map[string]string{"Content-Type": "application/octet-stream"},
		})
	})
	mux.HandleFunc("/obs/", func(w http.ResponseWriter, r *http.Request) {
		uploaded = true
		w.WriteHeader(http.StatusOK)
	})

	err := SyncGitCode(context.Background(), SyncOptions{
		BaseURL: srv.URL + "/api/v5",
		Token:   "tok",
		Owner:   GitCodeOwner,
		Repo:    "kshell",
		Tag:     "v0.2.0",
		Dir:     dir,
		HTTP:    srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !patched {
		t.Fatal("release 已存在时也应 PATCH release_status=latest")
	}
	if !uploaded {
		t.Fatal("PATCH 失败不应阻断附件上传")
	}
}

// PATCH 失败（非 2xx）不应阻断附件上传。
func TestSyncGitCodePatchFailureDoesNotBlockUpload(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ZipName), []byte("ZIPDATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, SumsName), []byte("abc  "+ZipName+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var uploaded bool
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/api/v5/repos/"+GitCodeOwner+"/kshell/releases/tags/v0.2.0", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.2.0"})
	})
	mux.HandleFunc("/api/v5/repos/"+GitCodeOwner+"/kshell/releases/v0.2.0/upload_url", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("file_name")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"url":     srv.URL + "/obs/" + name,
			"headers": map[string]string{"Content-Type": "application/octet-stream"},
		})
	})
	mux.HandleFunc("/obs/", func(w http.ResponseWriter, r *http.Request) {
		uploaded = true
		w.WriteHeader(http.StatusOK)
	})

	err := SyncGitCode(context.Background(), SyncOptions{
		BaseURL: srv.URL + "/api/v5",
		Token:   "tok",
		Owner:   GitCodeOwner,
		Repo:    "kshell",
		Tag:     "v0.2.0",
		Dir:     dir,
		HTTP:    srv.Client(),
	})
	if err != nil {
		t.Fatalf("PATCH 失败不应返回错误: %v", err)
	}
	if !uploaded {
		t.Fatal("PATCH 失败不应阻断附件上传")
	}
}

func TestSyncGitCodeRetriesUpload(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, ZipName)
	if err := os.WriteFile(zipPath, []byte("ZIPDATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	puts := 0
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/api/v5/repos/"+GitCodeOwner+"/kshell/releases/tags/v0.2.0", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.2.0"})
	})
	mux.HandleFunc("/api/v5/repos/"+GitCodeOwner+"/kshell/releases/v0.2.0/upload_url", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("file_name")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"url": srv.URL + "/obs/" + name,
			"headers": map[string]string{
				"Content-Type": "application/octet-stream",
			},
		})
	})
	mux.HandleFunc("/obs/", func(w http.ResponseWriter, r *http.Request) {
		puts++
		if puts == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	err := SyncGitCode(context.Background(), SyncOptions{
		BaseURL: srv.URL + "/api/v5",
		Token:   "tok",
		Owner:   GitCodeOwner,
		Repo:    "kshell",
		Tag:     "v0.2.0",
		Dir:     dir,
		HTTP:    srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if puts < 2 {
		t.Fatalf("PUT 次数 = %d, want ≥2（先失败再成功）", puts)
	}
}
