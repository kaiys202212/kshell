package agentsync

import (
	"testing"

	"github.com/yangk/kshell/internal/config"
)

func TestClaudeUsesItsOwnPlatform(t *testing.T) {
	plats := []config.ModelPlatform{
		{ID: "p1", Name: "A", AnthropicBaseURL: "https://a.example.com", Models: []string{"m1"}},
		{ID: "p2", Name: "B", AnthropicBaseURL: "https://b.example.com", Models: []string{"m2"}},
	}
	in := SyncInput{
		Platforms:        plats,
		ClaudePlatformID: "p2",
		ToolSync:         config.ToolSyncConfig{Claude: config.ClaudeSync{Model: "m2"}},
	}
	if got := pickClaudePlatform(in); got == nil || got.ID != "p2" {
		t.Fatalf("pickClaudePlatform = %+v, want p2", got)
	}
}

func TestCodeBuddyUsesItsOwnPlatform(t *testing.T) {
	plats := []config.ModelPlatform{
		{ID: "p1", Name: "A", OpenAIBaseURL: "https://a.example.com", Models: []string{"m1"}},
		{ID: "p2", Name: "B", OpenAIBaseURL: "https://b.example.com", Models: []string{"m2"}},
	}
	in := SyncInput{
		Platforms:           plats,
		CodeBuddyPlatformID: "p2",
		ToolSync:            config.ToolSyncConfig{CodeBuddy: []string{"m2"}},
	}
	if got := pickCodeBuddyPlatform(in); got == nil || got.ID != "p2" {
		t.Fatalf("pickCodeBuddyPlatform = %+v, want p2", got)
	}
}

func TestToolPlatformDanglingFallsBackToFirst(t *testing.T) {
	plats := []config.ModelPlatform{{ID: "p1"}, {ID: "p2"}}
	in := SyncInput{Platforms: plats, ClaudePlatformID: "gone", CodeBuddyPlatformID: "gone"}
	if got := pickClaudePlatform(in); got == nil || got.ID != "p1" {
		t.Fatalf("claude fallback = %+v", got)
	}
	if got := pickCodeBuddyPlatform(in); got == nil || got.ID != "p1" {
		t.Fatalf("codebuddy fallback = %+v", got)
	}
}
