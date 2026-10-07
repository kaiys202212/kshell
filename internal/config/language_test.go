package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultLanguageIsEn(t *testing.T) {
	if got := Default().Language; got != "en" {
		t.Fatalf("Language = %q, want en", got)
	}
}

func TestLanguageNormalized(t *testing.T) {
	c := Config{Language: "zh-CN"}
	if got := c.normalized().Language; got != "zh-CN" {
		t.Fatalf("zh-CN 保留，got %q", got)
	}
	c = Config{Language: "system"}
	if got := c.normalized().Language; got != "system" {
		t.Fatalf("system 保留，got %q", got)
	}
}

func TestInvalidLanguageFallsBackToEn(t *testing.T) {
	c := Config{Language: "bogus"}
	if got := c.normalized().Language; got != "en" {
		t.Fatalf("非法值回落 en，got %q", got)
	}
}

func TestLanguageSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	cfg := Default()
	cfg.Language = "zh-CN"
	if err := Save(p, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Language != "zh-CN" {
		t.Fatalf("round trip = %q, want zh-CN", loaded.Language)
	}
}
