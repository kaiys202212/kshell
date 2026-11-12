package config

import (
	"reflect"
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
	if c.Model.ToolSync.ToolPlatforms["claude"] != "p2" {
		t.Fatalf("ToolPlatforms[claude] = %q, want p2", c.Model.ToolSync.ToolPlatforms["claude"])
	}
	if c.Model.ToolSync.ToolPlatforms["codebuddy"] != "p2" {
		t.Fatalf("ToolPlatforms[codebuddy] = %q, want p2", c.Model.ToolSync.ToolPlatforms["codebuddy"])
	}
	if c.Model.ToolSync.ClaudePlatform != "" || c.Model.ToolSync.CodeBuddyPlatform != "" {
		t.Fatalf("旧字段应清空: %+v", c.Model.ToolSync)
	}
	if got, ok := c.Model.ToolPlatform("claude"); !ok || got.ID != "p2" {
		t.Fatalf("ToolPlatform(claude) = %+v, %v", got, ok)
	}
}

func TestToolPlatformMigratesForEnabledToolsWithEmptySlots(t *testing.T) {
	c := decodeModelConfig(t, `
model:
  platforms:
    - id: p1
      name: A
      anthropic_base_url: https://a.example.com
    - id: p2
      name: B
      anthropic_base_url: https://b.example.com
  default_platform: p2
  tool_sync:
    enabled: {claude: true, codebuddy: true}
`)
	if c.Model.ToolSync.ToolPlatforms["claude"] != "p2" {
		t.Fatalf("ToolPlatforms[claude] = %q, want p2", c.Model.ToolSync.ToolPlatforms["claude"])
	}
	if c.Model.ToolSync.ToolPlatforms["codebuddy"] != "p2" {
		t.Fatalf("ToolPlatforms[codebuddy] = %q, want p2", c.Model.ToolSync.ToolPlatforms["codebuddy"])
	}
	if c.Model.ToolSync.ClaudePlatform != "" || c.Model.ToolSync.CodeBuddyPlatform != "" {
		t.Fatalf("旧字段应清空: %+v", c.Model.ToolSync)
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
	if len(c.Model.ToolSync.ToolPlatforms) != 0 {
		t.Fatalf("无平台时 ToolPlatforms 应清空: %#v", c.Model.ToolSync.ToolPlatforms)
	}
}

// TestToolPlatformsNormalize 直测 normalized() 的映射归一块：空白 key/value
// 丢弃、key/value 去首尾空白、全空归一为 nil（不是空 map，保证 omitempty 不落盘）。
func TestToolPlatformsNormalize(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want map[string]string
	}{
		{
			name: "空白key丢弃",
			raw: `
model:
  platforms:
    - id: p1
  tool_sync:
    tool_platforms:
      " ": p1
`,
			want: nil,
		},
		{
			name: "空白value丢弃",
			raw: `
model:
  platforms:
    - id: p1
  tool_sync:
    tool_platforms:
      claude: " "
`,
			want: nil,
		},
		{
			name: "key与value去空白",
			raw: `
model:
  platforms:
    - id: p1
  tool_sync:
    tool_platforms:
      " claude ": " p1 "
`,
			want: map[string]string{"claude": "p1"},
		},
		{
			name: "空白key丢弃且有效值去空白",
			raw: `
model:
  platforms:
    - id: p1
  tool_sync:
    tool_platforms:
      " ": p1
      " claude ": " p1 "
`,
			want: map[string]string{"claude": "p1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := decodeModelConfig(t, tc.raw)
			if !reflect.DeepEqual(c.Model.ToolSync.ToolPlatforms, tc.want) {
				t.Fatalf("ToolPlatforms = %#v, want %#v", c.Model.ToolSync.ToolPlatforms, tc.want)
			}
		})
	}
}

// TestToolPlatformKnownAndUnknownTools 锁定 ToolPlatform 契约：已知工具
// （映射里显式出现的键，或旧字段兜底的 claude/codebuddy）空/悬空回落第一个；
// 拼错的未知 toolID 返回 false，不能静默拿到第一平台。
func TestToolPlatformKnownAndUnknownTools(t *testing.T) {
	m := ModelConfig{
		Platforms: []ModelPlatform{{ID: "p1"}, {ID: "p2"}},
		ToolSync: ToolSyncConfig{ToolPlatforms: map[string]string{
			"claude":    "p2",
			"codebuddy": "gone",
			"codex":     "p1",
		}},
	}
	cases := []struct {
		toolID string
		wantID string
		wantOK bool
	}{
		{"claude", "p2", true},
		{"codebuddy", "p1", true}, // 悬空回落第一个
		{"codex", "p1", true},
		{"claud", "", false}, // 拼错：未知工具
		{"gemini", "", false},
	}
	for _, tc := range cases {
		got, ok := m.ToolPlatform(tc.toolID)
		if ok != tc.wantOK || got.ID != tc.wantID {
			t.Fatalf("ToolPlatform(%q) = %+v, %v; want %q, %v", tc.toolID, got, ok, tc.wantID, tc.wantOK)
		}
	}
}
