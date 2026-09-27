//go:build !windows

package launcher

import (
	"path/filepath"
	"testing"
)

func TestResolveIsPassthroughOnUnix(t *testing.T) {
	bin := filepath.Join("/usr", "local", "bin", "claude")
	spec := Resolve(bin, []string{"--resume", "abc"})
	if spec.Path != bin {
		t.Fatalf("path = %q", spec.Path)
	}
	if len(spec.Args) != 2 || spec.Args[1] != "abc" {
		t.Fatalf("args = %v", spec.Args)
	}
}
