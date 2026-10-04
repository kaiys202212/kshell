package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewTextFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	write(t, path, "package main\nfunc main() {}\n")

	got := PreviewFile(path, 64*1024, 100)
	if got.Err != nil {
		t.Fatalf("unexpected error: %v", got.Err)
	}
	if got.Binary {
		t.Fatal("text file must not be flagged as binary")
	}
	if len(got.Lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(got.Lines))
	}
	if !strings.Contains(got.Lines[0], "package main") {
		t.Fatalf("line = %q", got.Lines[0])
	}
	// Text 为无行号原文；Lines 仍带行号前缀供 TUI
	if strings.Contains(got.Text, "│") {
		t.Fatalf("Text 不应含行号前缀 │，got %q", got.Text)
	}
	if !strings.Contains(got.Text, "package main") {
		t.Fatalf("Text = %q", got.Text)
	}
	if !strings.Contains(got.Lines[0], "│") {
		t.Fatalf("Lines[0] 应保留行号前缀，got %q", got.Lines[0])
	}
}

func TestPreviewTruncatesLargeFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "big.txt")
	write(t, path, strings.Repeat("x", 500)+"\nsecond\n")

	got := PreviewFile(path, 100, 10)
	if !got.Truncated {
		t.Fatal("expected truncation")
	}
	if len(got.Lines) == 0 {
		t.Fatal("truncated preview should still show something")
	}
}

func TestPreviewTextRespectsMaxLines(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "many.txt")
	var b strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&b, "line-%d\n", i)
	}
	write(t, path, b.String())

	const maxLines = 5
	got := PreviewFile(path, 64*1024, maxLines)
	if !got.Truncated {
		t.Fatal("expected line truncation")
	}
	if len(got.Lines) != maxLines {
		t.Fatalf("Lines = %d, want %d", len(got.Lines), maxLines)
	}
	textLines := strings.Split(got.Text, "\n")
	if len(textLines) > maxLines {
		t.Fatalf("Text 行数 = %d, 应 ≤ %d；Text=%q", len(textLines), maxLines, got.Text)
	}
	if len(textLines) != maxLines {
		t.Fatalf("Text 行数 = %d, want %d", len(textLines), maxLines)
	}
	if strings.Contains(got.Text, "line-5") {
		t.Fatalf("截断后 Text 不应含第 6 行，got %q", got.Text)
	}
}

func TestPreviewDetectsBinary(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "logo.png")
	content := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	got := PreviewFile(path, 64*1024, 100)
	if !got.Binary {
		t.Fatal("png should be detected as binary")
	}
	if got.Info == "" {
		t.Fatal("binary preview should carry file info")
	}
	if len(got.Lines) != 0 {
		t.Fatalf("binary must not emit content lines, got %v", got.Lines)
	}
	if got.Text != "" {
		t.Fatalf("binary Text 应为空，got %q", got.Text)
	}
}

func TestPreviewMissingFile(t *testing.T) {
	got := PreviewFile(filepath.Join(t.TempDir(), "nope.txt"), 1024, 10)
	if got.Err == nil {
		t.Fatal("missing file should report an error")
	}
}
