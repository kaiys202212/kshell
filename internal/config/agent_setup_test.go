package config

import "testing"

func TestAgentSetupDismissedRoundTrip(t *testing.T) {
	p := tempPaths(t)
	cfg := Default()
	if cfg.AgentSetupDismissed {
		t.Fatal("默认不应已标记")
	}
	cfg.AgentSetupDismissed = true
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.AgentSetupDismissed {
		t.Fatal("应保留 agent_setup_dismissed")
	}
}
