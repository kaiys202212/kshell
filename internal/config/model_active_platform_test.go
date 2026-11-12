package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func decodeModelConfig(t *testing.T, raw string) Config {
	t.Helper()
	var c Config
	if err := yaml.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return c.normalized()
}

func TestActivePlatformMigratesFromDefaultPlatform(t *testing.T) {
	c := decodeModelConfig(t, `
model:
  platforms:
    - id: p1
      name: A
      openai_base_url: https://a.example.com
      models: [m1]
    - id: p2
      name: B
      openai_base_url: https://b.example.com
      models: [m2]
  default_platform: p2
  active_model: m2
`)
	if c.Model.ActivePlatformID != "p2" {
		t.Fatalf("ActivePlatformID = %q, want p2", c.Model.ActivePlatformID)
	}
	if c.Model.DefaultPlatform != "" {
		t.Fatalf("legacy DefaultPlatform should be cleared, got %q", c.Model.DefaultPlatform)
	}
	if got, ok := c.Model.ActivePlatform(); !ok || got.ID != "p2" {
		t.Fatalf("ActivePlatform() = %+v, %v", got, ok)
	}
}

func TestActivePlatformDanglingFallsBackToFirst(t *testing.T) {
	c := decodeModelConfig(t, `
model:
  platforms:
    - id: p1
      name: A
      models: [m1]
  active_platform: gone
`)
	if c.Model.ActivePlatformID != "p1" {
		t.Fatalf("ActivePlatformID = %q, want p1", c.Model.ActivePlatformID)
	}
}

func TestToolPlatformMigratesFromDefaultPlatform(t *testing.T) {
	c := decodeModelConfig(t, `
model:
  platforms:
    - id: p1
      name: A
      anthropic_base_url: https://a.example.com
      models: [m1]
    - id: p2
      name: B
      anthropic_base_url: https://b.example.com
      models: [m2]
  default_platform: p2
  tool_sync:
    enabled: {claude: true, codebuddy: true}
    claude: {model: m2}
    codebuddy: [m2]
`)
	if c.Model.ToolSync.ClaudePlatform != "p2" {
		t.Fatalf("ClaudePlatform = %q, want p2", c.Model.ToolSync.ClaudePlatform)
	}
	if c.Model.ToolSync.CodeBuddyPlatform != "p2" {
		t.Fatalf("CodeBuddyPlatform = %q, want p2", c.Model.ToolSync.CodeBuddyPlatform)
	}
	if got, ok := c.Model.ToolPlatform("claude"); !ok || got.ID != "p2" {
		t.Fatalf("ToolPlatform(claude) = %+v, %v", got, ok)
	}
}

func TestNoPlatformsClearsAllSelections(t *testing.T) {
	c := decodeModelConfig(t, `
model:
  active_platform: p1
  active_model: m1
  tool_sync:
    claude_platform: p1
    codebuddy_platform: p1
`)
	if c.Model.ActivePlatformID != "" || c.Model.ActiveModel != "" {
		t.Fatalf("Active selection not cleared: %+v", c.Model)
	}
	if c.Model.ToolSync.ClaudePlatform != "" || c.Model.ToolSync.CodeBuddyPlatform != "" {
		t.Fatalf("tool platform not cleared: %+v", c.Model.ToolSync)
	}
}
