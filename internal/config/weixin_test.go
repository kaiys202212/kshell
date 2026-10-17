package config

import (
	"path/filepath"
	"testing"
)

// TestWeixinDefault：缺失配置走默认值，Weixin 保持零值。
func TestWeixinDefault(t *testing.T) {
	dir := t.TempDir()
	p := Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Weixin != (WeixinConfig{}) {
		t.Fatalf("Weixin = %+v, want 零值", cfg.Weixin)
	}
}

// TestWeixinRoundTrip：Save 后 Load，Weixin 三字段须一致。
func TestWeixinRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	cfg := Default()
	cfg.Weixin = WeixinConfig{Enabled: true, Token: "tok", Wxid: "wxid_abc"}
	if err := Save(p, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Weixin != cfg.Weixin {
		t.Fatalf("round trip Weixin = %+v, want %+v", loaded.Weixin, cfg.Weixin)
	}
}
