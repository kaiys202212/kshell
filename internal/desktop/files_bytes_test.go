package desktop

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileBytesPNG(t *testing.T) {
	env := newFilesEnv(t)
	content := append([]byte("\x89PNG\r\n\x1a\n"), []byte("fake-png-body")...)
	path := filepath.Join(env.root, "logo.png")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := env.app.ReadFileBytes(env.root, path)
	if err != nil {
		t.Fatalf("ReadFileBytes: %v", err)
	}
	if got.Mime != "image/png" {
		t.Fatalf("Mime = %q, want image/png", got.Mime)
	}
	if got.Size != int64(len(content)) {
		t.Fatalf("Size = %d, want %d", got.Size, len(content))
	}
	decoded, err := base64.StdEncoding.DecodeString(got.Base64)
	if err != nil {
		t.Fatalf("Base64 decode: %v", err)
	}
	if len(decoded) != len(content) {
		t.Fatalf("decoded len = %d, want %d", len(decoded), len(content))
	}
}

func TestReadFileBytesPDFMime(t *testing.T) {
	env := newFilesEnv(t)
	content := []byte("%PDF-1.4 fake")
	path := filepath.Join(env.root, "doc.pdf")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := env.app.ReadFileBytes(env.root, path)
	if err != nil {
		t.Fatalf("ReadFileBytes: %v", err)
	}
	if got.Mime != "application/pdf" {
		t.Fatalf("Mime = %q, want application/pdf", got.Mime)
	}
}

func TestReadFileBytesUnknownMime(t *testing.T) {
	env := newFilesEnv(t)
	path := filepath.Join(env.root, "blob.bin")
	if err := os.WriteFile(path, []byte{1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := env.app.ReadFileBytes(env.root, path)
	if err != nil {
		t.Fatalf("ReadFileBytes: %v", err)
	}
	if got.Mime != "application/octet-stream" {
		t.Fatalf("Mime = %q, want application/octet-stream", got.Mime)
	}
}

func TestReadFileBytesPathOutsideWorkspace(t *testing.T) {
	env := newFilesEnv(t)
	outside := filepath.Join(env.root, "..", "escape.png")
	if _, err := env.app.ReadFileBytes(env.root, outside); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("越界应报 errPathOutsideWorkspace，got %v", err)
	}
}

func TestReadFileBytesExceedsLimit(t *testing.T) {
	env := newFilesEnv(t)
	path := filepath.Join(env.root, "big.png")
	// 写略超上限的文件代价高；直接测纯函数
	big := make([]byte, maxPreviewBytes+1)
	if err := os.WriteFile(path, big[:64], 0o600); err != nil {
		t.Fatal(err)
	}
	// 用极小上限验证超限错误是 wire key（前端按 key 翻译）
	_, err := readFileBytesLimited(path, 16)
	if err == nil {
		t.Fatal("超限应返回错误")
	}
	if !strings.HasPrefix(err.Error(), "err.files.too_big_preview|") {
		t.Fatalf("超限错误应为 wire key，got %v", err)
	}
}

func TestMimeByExt(t *testing.T) {
	cases := map[string]string{
		"a.png":  "image/png",
		"a.JPG":  "image/jpeg",
		"a.jpeg": "image/jpeg",
		"a.gif":  "image/gif",
		"a.webp": "image/webp",
		"a.svg":  "image/svg+xml",
		"a.bmp":  "image/bmp",
		"a.ico":  "image/x-icon",
		"a.pdf":  "application/pdf",
		"a.xyz":  "application/octet-stream",
	}
	for name, want := range cases {
		if got := mimeByExt(name); got != want {
			t.Errorf("mimeByExt(%q) = %q, want %q", name, got, want)
		}
	}
}
