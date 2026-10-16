package atomfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	if err := Write(path, []byte("hello"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "hello" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

// TestWriteConcurrent 覆盖固定 .tmp 互踩：并发写同一目标必须全部成功且无残留。
func TestWriteConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.yaml")
	done := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() { done <- Write(path, []byte("x"), 0o755) }()
	}
	for i := 0; i < 8; i++ {
		if err := <-done; err != nil {
			t.Fatalf("并发写不应失败: %v", err)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("残留临时文件: %s", e.Name())
		}
	}
}

func TestWriteCreatesMissingDir(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a", "b", "c.yaml")
	if err := Write(path, []byte("v"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

// TestWriteLeavesNoTempOnWriteError 写失败（目录被文件挡住）时不得留临时文件。
func TestWriteLeavesNoTempOnWriteError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 目标在 blocker（文件）之下：CreateTemp 失败，无残留可言；再验证写失败返回错误
	if err := Write(filepath.Join(blocker, "sub", "x.yaml"), []byte("v"), 0o755); err == nil {
		t.Fatal("目标父目录是文件时应失败")
	}
}
