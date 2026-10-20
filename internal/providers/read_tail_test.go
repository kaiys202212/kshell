package providers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadTailWholeFileSmallerThanLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	body := "line1\nline2\nline3\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadTail(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("got %q want %q", got, body)
	}
}

func TestReadTailDropsLeadingPartialLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	// 构造：前缀长行 + 完整两行；limit 切在前缀中间，应丢掉残缺前缀，只留完整行。
	prefix := strings.Repeat("x", 100)
	body := prefix + "\nkeep-a\nkeep-b\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// 读末尾 len("\nkeep-a\nkeep-b\n")+50，必然切进 prefix
	limit := len("\nkeep-a\nkeep-b\n") + 50
	got, err := ReadTail(path, limit)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if strings.Contains(s, "xxx") && !strings.HasPrefix(s, "keep") {
		// 允许残缺前缀被丢后以 keep 开头；不应仍以整段 x 开头却缺换行语义
	}
	if !strings.Contains(s, "keep-a\nkeep-b\n") {
		t.Fatalf("应含完整尾部行: %q", s)
	}
	if strings.HasPrefix(s, "x") {
		t.Fatalf("应丢弃首截断残缺行，不应以 x 开头: %q", s)
	}
}

func TestReadTailEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadTail(path, 256)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("空文件应返回空: %q", got)
	}
}
