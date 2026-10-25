//go:build darwin

package desktop

import "testing"

func TestRevealArgsDarwin(t *testing.T) {
	if got := revealArgs("/tmp/a/b.txt", false); len(got) != 2 || got[0] != "-R" || got[1] != "/tmp/a/b.txt" {
		t.Fatalf("文件应 open -R 选中，got %v", got)
	}
	if got := revealArgs("/tmp/a", true); len(got) != 1 || got[0] != "/tmp/a" {
		t.Fatalf("目录应 open <dir>，got %v", got)
	}
}
