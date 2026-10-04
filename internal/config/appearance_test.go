package config

import (
	"path/filepath"
	"testing"
)

func TestAppearanceDefaultIsDark(t *testing.T) {
	if got := Default().Appearance.Mode; got != "dark" {
		t.Fatalf("default mode = %q, want dark", got)
	}
}

func TestAppearanceInvalidFallsBackToDark(t *testing.T) {
	c := Config{MaxDepth: 1, Appearance: Appearance{Mode: "bogus"}}
	if got := c.normalized().Appearance.Mode; got != "dark" {
		t.Fatalf("normalized mode = %q, want dark", got)
	}
}

func TestAppearanceSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	cfg := Default()
	cfg.Appearance.Mode = "dark"
	if err := Save(p, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Appearance.Mode != "dark" {
		t.Fatalf("loaded mode = %q, want dark", loaded.Appearance.Mode)
	}
}
