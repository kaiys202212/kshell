package config

import (
	"path/filepath"
	"testing"
)

func TestAppearanceDefaultIsSystem(t *testing.T) {
	if got := Default().Appearance.Mode; got != "system" {
		t.Fatalf("default mode = %q, want system", got)
	}
}

func TestAppearanceInvalidFallsBackToSystem(t *testing.T) {
	c := Config{MaxDepth: 1, Appearance: Appearance{Mode: "bogus"}}
	if got := c.normalized().Appearance.Mode; got != "system" {
		t.Fatalf("normalized mode = %q, want system", got)
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
