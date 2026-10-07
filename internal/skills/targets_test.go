package skills

import (
	"path/filepath"
	"testing"
)

func TestTargetRoot(t *testing.T) {
	home := `C:\Users\me`

	cases := []struct {
		id   string
		want string
		ok   bool
	}{
		{"claude", filepath.Join(home, ".claude", "skills"), true},
		{"cursor", filepath.Join(home, ".cursor", "skills"), true},
		{"codex", filepath.Join(home, ".agents", "skills"), true},
		{"gemini", filepath.Join(home, ".gemini", "skills"), true},
		{"opencode", filepath.Join(home, ".config", "opencode", "skills"), true},
		{"codebuddy", filepath.Join(home, ".codebuddy", "skills"), true},
		{"unknown", "", false},
	}
	for _, tc := range cases {
		got, ok := TargetRoot(tc.id, home)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("%s: got %q ok=%v, want %q ok=%v", tc.id, got, ok, tc.want, tc.ok)
		}
	}
}
