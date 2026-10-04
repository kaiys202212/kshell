//go:build windows

package desktop

import "testing"

func TestRevealExplorerArgsWindows(t *testing.T) {
	got := explorerArgs(`D:\proj\a.go`, false)
	if len(got) != 1 || got[0] != `/select,D:\proj\a.go` {
		t.Fatalf("file args: %v", got)
	}
	got = explorerArgs(`D:\proj\pkg`, true)
	if len(got) != 1 || got[0] != `D:\proj\pkg` {
		t.Fatalf("dir args: %v", got)
	}
}
