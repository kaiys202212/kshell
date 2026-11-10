package agentsync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/workbuddy"
)

// wbDefaultInput 构造「默认平台为 WorkBuddy」的输入：workbuddy 平台无静态
// 端点/密钥（动态凭据），另一个普通平台不作为默认。
func wbDefaultInput() SyncInput {
	return SyncInput{
		Enabled: true,
		Platforms: []config.ModelPlatform{
			{ID: "wb", Name: "WorkBuddy", Preset: workbuddy.PresetID, Models: []string{"hy3"}},
			{ID: "p1", Name: "智谱", OpenAIBaseURL: "https://api.example/v1", APIKey: "sk", Models: []string{"glm-x"}},
		},
		DefaultPlatformID: "wb",
	}
}

// 三个目标的共同契约：默认平台是 WorkBuddy（无静态端点）时走「只清不建」——
// 清掉过期 kshell 注入物，不为 WorkBuddy 平台写任何条目（动态凭据无法被
// 外部 CLI 使用），用户自有内容保留。

func TestWorkbuddyDefaultOpenCodeClearOnly(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	seed := map[string]any{
		"provider": map[string]any{
			"kshell-p1": map[string]any{"name": "old"},
			"keep":      map[string]any{"name": "keep"},
		},
	}
	raw, _ := json.Marshal(seed)
	if err := os.WriteFile(filepath.Join(cfgDir, "opencode.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (OpenCode{}).Sync(home, wbDefaultInput()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	doc := readOpenCodeJSON(t, home)
	prov, _ := doc["provider"].(map[string]any)
	// p1 是普通平台且仍有端点：kshell-p1 属正常重建，不算过期注入。
	if _, ok := prov["kshell-p1"]; !ok {
		t.Fatal("普通平台 p1 应照常注入")
	}
	if _, ok := prov["kshell-wb"]; ok {
		t.Fatal("WorkBuddy 平台不得注入 opencode")
	}
	if _, ok := prov["keep"]; !ok {
		t.Fatal("用户自有 provider 应保留")
	}
}

func TestWorkbuddyDefaultCodeBuddyClearOnly(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codebuddy")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	seed := map[string]any{
		"models": []any{
			map[string]any{"id": "user-m", "name": "User", "vendor": "openai"},
			map[string]any{"id": "glm-x", "name": "智谱: glm-x", "vendor": "kshell"},
		},
	}
	raw, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "models.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (CodeBuddy{}).Sync(home, wbDefaultInput()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	doc := readCodeBuddyJSON(t, home)
	models := sliceOfMaps(doc["models"])
	for _, m := range models {
		if id, _ := m["id"].(string); id == "glm-x" {
			t.Fatal("过期 kshell 条目应被清除")
		}
		if id, _ := m["id"].(string); id == "hy3" {
			t.Fatal("WorkBuddy 平台不得注入 codebuddy")
		}
	}
	found := false
	for _, m := range models {
		if id, _ := m["id"].(string); id == "user-m" {
			found = true
		}
	}
	if !found {
		t.Fatal("用户自有条目应保留")
	}
}

func TestWorkbuddyDefaultClaudeClearOnly(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	seed := `{"env":{"ANTHROPIC_BASE_URL":"https://old.example","ANTHROPIC_AUTH_TOKEN":"sk-old","KEEP":"yes"},"theme":"dark"}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (Claude{}).Sync(home, wbDefaultInput()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	env := readClaudeSettings(t, home)["env"].(map[string]any)
	if _, ok := env["ANTHROPIC_BASE_URL"]; ok {
		t.Fatal("过期 ANTHROPIC_BASE_URL 应被清除")
	}
	if env["KEEP"] != "yes" {
		t.Fatal("用户 env 键应保留")
	}
}
