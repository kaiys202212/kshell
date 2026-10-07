package desktop

import (
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/discovery"
)

func TestListSkillTargetsFiltersInstalled(t *testing.T) {
	home := t.TempDir()
	a := &App{opts: Options{Home: home}}
	a.tools = []discovery.Tool{
		{ID: "claude", Installed: true},
		{ID: "cursor", Installed: true, Broken: true},
		{ID: "codex", Installed: false},
		{ID: "unknown-tool", Installed: true},
	}
	got := a.ListSkillTargets()
	if len(got) != 1 || got[0].ToolID != "claude" {
		t.Fatalf("%+v", got)
	}
	wantRoot := filepath.Join(home, ".claude", "skills")
	if got[0].Root != wantRoot || !got[0].DefaultChecked {
		t.Fatalf("%+v", got[0])
	}
}
