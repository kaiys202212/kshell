package config

import "testing"

func TestCloseBehaviorDefaultIsTray(t *testing.T) {
	if got := Default().CloseBehavior; got != CloseBehaviorTray {
		t.Fatalf("default close_behavior = %q, want %q", got, CloseBehaviorTray)
	}
}

func TestCloseBehaviorNormalized(t *testing.T) {
	cases := map[string]string{
		"":      CloseBehaviorTray,
		"tray":  CloseBehaviorTray,
		"exit":  CloseBehaviorExit,
		"EXIT":  CloseBehaviorTray, // 只有精确小写合法，其余回落默认
		"bogus": CloseBehaviorTray,
	}
	for in, want := range cases {
		c := Config{MaxDepth: 1, CloseBehavior: in}
		if got := c.normalized().CloseBehavior; got != want {
			t.Fatalf("normalized(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCloseBehaviorSaveLoadRoundTrip(t *testing.T) {
	p := tempPaths(t)
	cfg := Default()
	cfg.CloseBehavior = CloseBehaviorExit
	if err := Save(p, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.CloseBehavior != CloseBehaviorExit {
		t.Fatalf("loaded = %q, want %q", loaded.CloseBehavior, CloseBehaviorExit)
	}
}
