package providers

import (
	"os"
	"path/filepath"
	"testing"
)

// 声明了 ResidueDirs：任一存在即算残留。
func TestHasResidueDeclaredDirExists(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".cursor", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := DetectSpec{ResidueDirs: []string{"~/.cursor/projects", "~/.cursor/chats"}}
	if !HasResidue(spec, home) {
		t.Fatal("projects 存在应算有残留")
	}
}

// 声明了 ResidueDirs 但全部不存在 → 无残留。
func TestHasResidueDeclaredAllMissing(t *testing.T) {
	home := t.TempDir()
	spec := DetectSpec{ResidueDirs: []string{"~/.cursor/projects"}}
	if HasResidue(spec, home) {
		t.Fatal("目录不存在不应算残留")
	}
}

// 未声明 ResidueDirs：退化为 ConfigDirs 非空目录判定。
func TestHasResidueFallbackConfigNonEmpty(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(filepath.Join(cfg, "chats"), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := DetectSpec{ConfigDirs: []string{"~/.cursor"}}
	if !HasResidue(spec, home) {
		t.Fatal("ConfigDirs 非空应算残留")
	}
	// 清空后（只剩空目录）不算残留
	if err := os.Remove(filepath.Join(cfg, "chats")); err != nil {
		t.Fatal(err)
	}
	if HasResidue(spec, home) {
		t.Fatal("空配置目录不应算残留")
	}
}

// 既无 ResidueDirs 也无 ConfigDirs → 无残留。
func TestHasResidueNothingDeclared(t *testing.T) {
	if HasResidue(DetectSpec{}, t.TempDir()) {
		t.Fatal("无声明应无残留")
	}
}
