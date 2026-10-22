package agentsync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/config"
)

func TestCodeBuddySyncMergesNamespace(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codebuddy")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	seed := map[string]any{
		"models": []any{
			map[string]any{"id": "user-m", "name": "User", "vendor": "openai"},
			map[string]any{"id": "kshell-old/x", "name": "Old", "vendor": "kshell"},
		},
		"availableModels": []any{"user-m", "kshell-old/x"},
	}
	raw, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "models.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	in := SyncInput{
		Enabled: true,
		Platforms: []config.ModelPlatform{{
			ID: "p1", Name: "智谱", OpenAIBaseURL: "https://api.example/v1",
			APIKey: "sk", Models: []string{"glm-x"},
		}},
	}
	if err := (CodeBuddy{}).Sync(home, in); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	doc := readCodeBuddyJSON(t, home)
	models := sliceOfMaps(doc["models"])
	ids := map[string]bool{}
	for _, m := range models {
		id, _ := m["id"].(string)
		ids[id] = true
		if id == "kshell-p1/glm-x" {
			if m["url"] != "https://api.example/v1/chat/completions" {
				t.Fatalf("url = %v", m["url"])
			}
			if m["apiKey"] != "sk" || m["vendor"] != "kshell" {
				t.Fatalf("model = %#v", m)
			}
			if m["name"] != "智谱: glm-x" {
				t.Fatalf("name = %v", m["name"])
			}
		}
	}
	if !ids["user-m"] {
		t.Fatal("应保留 user-m")
	}
	if ids["kshell-old/x"] {
		t.Fatal("应删除过期 kshell-old/x")
	}
	if !ids["kshell-p1/glm-x"] {
		t.Fatal("应写入 kshell-p1/glm-x")
	}
}

func TestCodeBuddyChatCompletionsURL(t *testing.T) {
	if got := chatCompletionsURL("https://x/v1/"); got != "https://x/v1/chat/completions" {
		t.Fatalf("got %q", got)
	}
	if got := chatCompletionsURL("https://x/v1/chat/completions"); got != "https://x/v1/chat/completions" {
		t.Fatalf("got %q", got)
	}
}

func TestSyncAllSkipsUninstalledWithoutConfig(t *testing.T) {
	home := t.TempDir()
	in := SyncInput{
		Enabled: true,
		Platforms: []config.ModelPlatform{{
			ID: "p1", Name: "P", OpenAIBaseURL: "https://oai/", APIKey: "k", Models: []string{"m"},
		}},
	}
	if err := SyncAll(home, in, nil); err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	if (OpenCode{}).ConfigExists(home) || (CodeBuddy{}).ConfigExists(home) {
		t.Fatal("未安装且无配置时不应创建文件")
	}
	if err := SyncAll(home, in, []string{"opencode"}); err != nil {
		t.Fatalf("SyncAll installed: %v", err)
	}
	if !(OpenCode{}).ConfigExists(home) {
		t.Fatal("已安装 opencode 时应写入")
	}
	if (CodeBuddy{}).ConfigExists(home) {
		t.Fatal("未安装 codebuddy 不应写入")
	}
}

func readCodeBuddyJSON(t *testing.T, home string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(home, ".codebuddy", "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}
