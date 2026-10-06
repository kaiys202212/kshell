package config

import "testing"

func TestDesktopShortcutEnsuredRoundTrip(t *testing.T) {
	p := tempPaths(t)
	cfg := Default()
	if cfg.DesktopShortcutEnsured {
		t.Fatal("默认不应已标记")
	}
	cfg.DesktopShortcutEnsured = true
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.DesktopShortcutEnsured {
		t.Fatal("应保留 desktop_shortcut_ensured")
	}
}
