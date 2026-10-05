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
