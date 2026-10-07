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

func TestAppearanceDefaultFontSizeIs13(t *testing.T) {
	if got := Default().Appearance.FontSize; got != DefaultUIFontSize {
		t.Fatalf("default font size = %d, want %d", got, DefaultUIFontSize)
	}
}

func TestAppearanceDefaultShowWhitespaceIsFalse(t *testing.T) {
	if Default().Appearance.ShowWhitespace {
		t.Fatal("default show_whitespace should be false")
	}
}

func TestAppearanceShowWhitespaceNormalized(t *testing.T) {
	// 缺省字段归一后仍为 false（零值保留）
	if got := (Config{MaxDepth: 1}).normalized().Appearance.ShowWhitespace; got {
		t.Fatal("missing show_whitespace should normalize to false")
	}
	if got := (Config{MaxDepth: 1, Appearance: Appearance{ShowWhitespace: true}}).normalized().Appearance.ShowWhitespace; !got {
		t.Fatal("explicit true must survive normalized()")
	}
}

func TestAppearanceShowWhitespaceSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	cfg := Default()
	cfg.Appearance.ShowWhitespace = true
	if err := Save(p, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.Appearance.ShowWhitespace {
		t.Fatal("loaded show_whitespace = false, want true")
	}
}

func TestClampUIFontSize(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, 13},
		{-1, 13},
		{7, 10},
		{10, 10},
		{13, 13},
		{15, 15},
		{20, 20},
		{25, 20},
	}
	for _, tc := range cases {
		if got := ClampUIFontSize(tc.in); got != tc.want {
			t.Fatalf("ClampUIFontSize(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestAppearanceFontSizeNormalized(t *testing.T) {
	if got := (Config{MaxDepth: 1}).normalized().Appearance.FontSize; got != 13 {
		t.Fatalf("missing font_size = %d, want 13", got)
	}
	if got := (Config{MaxDepth: 1, Appearance: Appearance{FontSize: 7}}).normalized().Appearance.FontSize; got != 10 {
		t.Fatalf("too small = %d, want 10", got)
	}
	if got := (Config{MaxDepth: 1, Appearance: Appearance{FontSize: 25}}).normalized().Appearance.FontSize; got != 20 {
		t.Fatalf("too large = %d, want 20", got)
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
	if loaded.Appearance.FontSize != 13 {
		t.Fatalf("loaded font size = %d, want 13", loaded.Appearance.FontSize)
	}
}

func TestAppearanceFontSizeSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	cfg := Default()
	cfg.Appearance.FontSize = 16
	if err := Save(p, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Appearance.FontSize != 16 {
		t.Fatalf("loaded font size = %d, want 16", loaded.Appearance.FontSize)
	}
}
