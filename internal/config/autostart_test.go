package config

import (
	"os"
	"testing"
)

// withRelease 注入发布态并保证测试结束恢复，避免污染同包其它用例。
func withRelease(t *testing.T, release bool) {
	t.Helper()
	orig := isReleaseFn
	isReleaseFn = func() bool { return release }
	t.Cleanup(func() { isReleaseFn = orig })
}

func TestDefaultAutostart(t *testing.T) {
	// 缺省值跟随发布态：release 默认开、dev 默认关。
	if !defaultAutostart(true) {
		t.Fatal("release 缺省应为 true")
	}
	if defaultAutostart(false) {
		t.Fatal("dev 缺省应为 false")
	}
}

func TestAutostartMissingFieldFollowsRelease(t *testing.T) {
	p := tempPaths(t)
	// 旧版 config.yaml（无 autostart 字段）。
	if err := os.MkdirAll(p.Root, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(p.Config, []byte("max_depth: 4\n"), 0o644); err != nil {
		t.Fatalf("写配置: %v", err)
	}

	// release 构建：缺省开。
	withRelease(t, true)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !c.Autostart {
		t.Fatal("release 下旧配置缺字段应缺省为 true")
	}

	// dev 构建：缺省关。
	withRelease(t, false)
	c, err = Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Autostart {
		t.Fatal("dev 下旧配置缺字段应缺省为 false")
	}
}

func TestAutostartExplicitFalsePreserved(t *testing.T) {
	p := tempPaths(t)
	withRelease(t, true) // 即便 release 缺省开，显式 false 必须保留
	if err := os.MkdirAll(p.Root, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(p.Config, []byte("autostart: false\n"), 0o644); err != nil {
		t.Fatalf("写配置: %v", err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Autostart {
		t.Fatal("显式 autostart: false 应保留，不得被缺省值覆盖")
	}
}

func TestAutostartExplicitTruePreserved(t *testing.T) {
	p := tempPaths(t)
	withRelease(t, false) // dev 下显式 true 也保留
	if err := os.MkdirAll(p.Root, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(p.Config, []byte("autostart: true\n"), 0o644); err != nil {
		t.Fatalf("写配置: %v", err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !c.Autostart {
		t.Fatal("显式 autostart: true 应保留")
	}
}

func TestAutostartMissingFileDefaultsByRelease(t *testing.T) {
	p := tempPaths(t)
	if err := os.Remove(p.Config); err != nil && !os.IsNotExist(err) {
		t.Fatalf("Remove: %v", err)
	}

	withRelease(t, true)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !c.Autostart {
		t.Fatal("配置文件缺失时 release 应缺省为 true")
	}
}

func TestAutostartSaveLoadRoundTrip(t *testing.T) {
	p := tempPaths(t)
	withRelease(t, false)
	cfg := Default()
	cfg.Autostart = true
	if err := Save(p, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.Autostart {
		t.Fatal("Save/Load 往返应保留 autostart: true")
	}
}
