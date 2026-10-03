package desktop

import (
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/appearance"
	"github.com/yangk/kshell/internal/config"
)

func TestGetAppearanceDefaultsToSystem(t *testing.T) {
	a := NewAppWith(Options{Config: config.Default()})
	got := a.GetAppearance()
	if got.Mode != "system" {
		t.Fatalf("mode = %q, want system", got.Mode)
	}
	if got.Resolved != string(appearance.Resolve(appearance.System)) {
		t.Fatalf("resolved = %q", got.Resolved)
	}
}

func TestSetAppearanceModeWithoutLayoutIsNotReady(t *testing.T) {
	a := NewAppWith(Options{Config: config.Default()})
	if err := a.SetAppearanceMode("dark"); err != errNotReady {
		t.Fatalf("err = %v, want errNotReady", err)
	}
}

func TestSetAppearanceModePersistsAndEmits(t *testing.T) {
	dir := t.TempDir()
	layout := config.Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	var events []string
	a := NewAppWith(Options{
		Config: config.Default(),
		Layout: layout,
		Emit:   func(name string, _ ...any) { events = append(events, name) },
	})

	if err := a.SetAppearanceMode("dark"); err != nil {
		t.Fatalf("SetAppearanceMode: %v", err)
	}
	if a.GetAppearance().Mode != "dark" {
		t.Fatalf("mode = %q", a.GetAppearance().Mode)
	}
	if len(events) == 0 || events[len(events)-1] != "appearance:changed" {
		t.Fatalf("events = %v", events)
	}

	loaded, err := config.Load(layout)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Appearance.Mode != "dark" {
		t.Fatalf("persisted = %q", loaded.Appearance.Mode)
	}

	if err := a.SetAppearanceMode("bogus"); err != nil {
		t.Fatalf("SetAppearanceMode(bogus): %v", err)
	}
	if a.GetAppearance().Mode != "system" {
		t.Fatalf("invalid mode should fall back to system, got %q", a.GetAppearance().Mode)
	}
}
