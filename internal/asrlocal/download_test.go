package asrlocal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func shaHex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestFetchFallsBackToNextSource(t *testing.T) {
	payload := []byte(strings.Repeat("sherpa-", 1000))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			http.Error(w, "not found", http.StatusNotFound)
		case "/b":
			w.Write(payload)
		}
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "pkg.tar.bz2")
	var logLines []string
	used, err := fetchFile(context.Background(), http.DefaultClient,
		[]string{srv.URL + "/a", srv.URL + "/b"}, dst, shaHex(payload), int64(len(payload)),
		nil, func(s string) { logLines = append(logLines, s) })
	if err != nil {
		t.Fatalf("fetchFile: %v", err)
	}
	if used != srv.URL+"/b" {
		t.Fatalf("应回落到第二源, got %q", used)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("落盘内容不符: %v", err)
	}
	if _, err := os.Stat(dst + partSuffix); err == nil {
		t.Fatal("完成后不应残留半成品")
	}
	if len(logLines) == 0 {
		t.Fatal("应记录换源日志")
	}
}

func TestFetchResumesFromPart(t *testing.T) {
	payload := []byte(strings.Repeat("abc123", 5000))
	dst := filepath.Join(t.TempDir(), "pkg.bin")
	half := len(payload) / 2
	if err := os.WriteFile(dst+partSuffix, payload[:half], 0o644); err != nil {
		t.Fatal(err)
	}
	var gotRange string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRange = r.Header.Get("Range")
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", half, len(payload)-1, len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(payload[half:])
	}))
	defer srv.Close()

	used, err := fetchFile(context.Background(), http.DefaultClient,
		[]string{srv.URL}, dst, shaHex(payload), int64(len(payload)), nil, nil)
	if err != nil {
		t.Fatalf("fetchFile: %v", err)
	}
	if used != srv.URL {
		t.Fatalf("used = %q", used)
	}
	if gotRange != "bytes="+strconv.Itoa(half)+"-" {
		t.Fatalf("Range = %q, want bytes=%d-", gotRange, half)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != string(payload) {
		t.Fatal("续传后内容不符")
	}
}

func TestFetchChecksumMismatchRotatesSource(t *testing.T) {
	good := []byte("correct-bytes")
	bad := []byte("tampered")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			w.Write(bad)
			return
		}
		w.Write(good)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "pkg.bin")
	used, err := fetchFile(context.Background(), http.DefaultClient,
		[]string{srv.URL + "/bad", srv.URL + "/good"}, dst, shaHex(good), int64(len(good)), nil, nil)
	if err != nil {
		t.Fatalf("应换源成功: %v", err)
	}
	if used != srv.URL+"/good" {
		t.Fatalf("used = %q", used)
	}
}

func TestFetchAllSourcesFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("bad"))
	}))
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "pkg.bin")
	_, err := fetchFile(context.Background(), http.DefaultClient,
		[]string{srv.URL + "/1", srv.URL + "/2"}, dst, shaHex([]byte("good")), 4, nil, nil)
	if err == nil {
		t.Fatal("全部源失败应报错")
	}
	if !strings.Contains(err.Error(), "全部下载源失败") {
		t.Fatalf("错误应说明所有源失败: %v", err)
	}
	if _, statErr := os.Stat(dst + partSuffix); statErr == nil {
		t.Fatal("失败后应清理半成品")
	}
}

func TestFetchProgressMonotonic(t *testing.T) {
	payload := []byte(strings.Repeat("x", 40<<10))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}))
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "pkg.bin")
	var last, calls int64
	_, err := fetchFile(context.Background(), http.DefaultClient, []string{srv.URL}, dst,
		shaHex(payload), int64(len(payload)), func(done, total int64) {
			calls++
			if done < last {
				t.Fatalf("进度回退: %d -> %d", last, done)
			}
			last = done
		}, nil)
	if err != nil {
		t.Fatalf("fetchFile: %v", err)
	}
	if calls == 0 || last != int64(len(payload)) {
		t.Fatalf("进度回调不完整: calls=%d last=%d", calls, last)
	}
}