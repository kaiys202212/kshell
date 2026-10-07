package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallTargetLinkOrCopy(t *testing.T) {
	base := t.TempDir()
	entity := filepath.Join(base, "entity", "demo")
	if err := os.MkdirAll(entity, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entity, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "agents", "skills", "demo")
	mode, err := InstallTarget(entity, target)
	if err != nil {
		t.Fatal(err)
	}
	if mode != modeLink && mode != modeCopy {
		t.Fatalf("mode %q", mode)
	}
	if _, err := os.Stat(filepath.Join(target, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	// 链接模式下再次安装应 no-op；副本模式目标已存在会冲突，先清再装。
	if mode == modeLink {
		mode2, err := InstallTarget(entity, target)
		if err != nil || mode2 != modeLink {
			t.Fatalf("relink: mode=%q err=%v", mode2, err)
		}
	}
}

func TestInstallTargetConflict(t *testing.T) {
	base := t.TempDir()
	entity := filepath.Join(base, "entity")
	other := filepath.Join(base, "other")
	target := filepath.Join(base, "target")
	for _, d := range []string{entity, other, target} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("foreign"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallTarget(entity, target); err != errTargetConf {
		t.Fatalf("got %v", err)
	}
}

func TestRemoveTarget(t *testing.T) {
	base := t.TempDir()
	entity := filepath.Join(base, "entity")
	target := filepath.Join(base, "tgt")
	if err := os.MkdirAll(entity, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallTarget(entity, target); err != nil {
		t.Fatal(err)
	}
	if err := RemoveTarget(target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("still exists: %v", err)
	}
}
