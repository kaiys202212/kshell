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

// Sync 后 availableModels 只剩外来条目；kshell-only 时整键删除（内置模型
// 不再被权威清单挤出 ACP 上报）。
func TestCodeBuddySyncAvailableModels(t *testing.T) {
	in := SyncInput{
		Enabled: true,
		Platforms: []config.ModelPlatform{{
			ID: "p1", Name: "P", OpenAIBaseURL: "https://oai/", APIKey: "k", Models: []string{"m"},
		}},
	}

	// 场景一：availableModels 仅 kshell 条目 → 整键删除。
	home := t.TempDir()
	dir := filepath.Join(home, ".codebuddy")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	seed := map[string]any{
		"models":          []any{},
		"availableModels": []any{"kshell-old/x"},
	}
	raw, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "models.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (CodeBuddy{}).Sync(home, in); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	doc := readCodeBuddyJSON(t, home)
	if _, ok := doc["availableModels"]; ok {
		t.Fatal("kshell-only 的 availableModels 应整键删除")
	}

	// 场景二：含外来条目 → 保留外来条目，清 kshell 条目。
	home2 := t.TempDir()
	dir2 := filepath.Join(home2, ".codebuddy")
	if err := os.MkdirAll(dir2, 0o700); err != nil {
		t.Fatal(err)
	}
	seed2 := map[string]any{
		"models":          []any{},
		"availableModels": []any{"foreign-model", "kshell-old/x"},
	}
	raw2, _ := json.MarshalIndent(seed2, "", "  ")
	if err := os.WriteFile(filepath.Join(dir2, "models.json"), raw2, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (CodeBuddy{}).Sync(home2, in); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	doc2 := readCodeBuddyJSON(t, home2)
	avail, ok := doc2["availableModels"].([]any)
	if !ok {
		t.Fatalf("外来条目存在时应保留键，got %T", doc2["availableModels"])
	}
	if len(avail) != 1 || avail[0] != "foreign-model" {
		t.Fatalf("应只剩 foreign-model，got %v", avail)
	}
}

// 当前平台仍有的 mid：清掉 vendor=user 裸名镜像（Custom），只留 kshell 条目；
// 与 claim 无关的用户自定义保留。
func TestCodeBuddySyncRemovesUserMirrorOfClaimedMids(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codebuddy")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	seed := map[string]any{
		"models": []any{
			map[string]any{"id": "glm-5.3-flash", "name": "glm-5.3-flash", "vendor": "user"},
			map[string]any{"id": "qwen-max", "name": "qwen-max", "vendor": "user"},
		},
	}
	raw, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "models.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	in := SyncInput{
		Enabled: true,
		Platforms: []config.ModelPlatform{{
			ID: "default", Name: "openai", OpenAIBaseURL: "https://api.example/v1",
			APIKey: "sk", Models: []string{"glm-5.3-flash"},
		}},
	}
	if err := (CodeBuddy{}).Sync(home, in); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	ids := map[string]bool{}
	for _, m := range sliceOfMaps(readCodeBuddyJSON(t, home)["models"]) {
		id, _ := m["id"].(string)
		ids[id] = true
	}
	if ids["glm-5.3-flash"] {
		t.Fatal("应清除与平台 mid 碰撞的 user 镜像 glm-5.3-flash")
	}
	if !ids["kshell-default/glm-5.3-flash"] {
		t.Fatal("应写入 kshell-default/glm-5.3-flash")
	}
	if !ids["qwen-max"] {
		t.Fatal("无关用户自定义 qwen-max 应保留")
	}
}

// 平台已删：旧 kshell 条目的裸 mid 仍 claim，清掉对应 user 孤儿镜像。
func TestCodeBuddySyncRemovesOrphanUserMirrorOfOldKshell(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codebuddy")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	seed := map[string]any{
		"models": []any{
			map[string]any{"id": "kshell-gone/old-mid", "name": "openai: old-mid", "vendor": "kshell"},
			map[string]any{"id": "old-mid", "name": "old-mid", "vendor": "user"},
			map[string]any{"id": "keep-me", "name": "keep-me", "vendor": "user"},
		},
	}
	raw, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "models.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	in := SyncInput{
		Enabled: true,
		Platforms: []config.ModelPlatform{{
			ID: "default", Name: "openai", OpenAIBaseURL: "https://api.example/v1",
			APIKey: "sk", Models: []string{"other"},
		}},
	}
	if err := (CodeBuddy{}).Sync(home, in); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	ids := map[string]bool{}
	for _, m := range sliceOfMaps(readCodeBuddyJSON(t, home)["models"]) {
		id, _ := m["id"].(string)
		ids[id] = true
	}
	if ids["kshell-gone/old-mid"] || ids["old-mid"] {
		t.Fatalf("旧平台 kshell 与 user 孤儿均应清除，got %v", ids)
	}
	if !ids["keep-me"] {
		t.Fatal("keep-me 应保留")
	}
	if !ids["kshell-default/other"] {
		t.Fatal("应写入当前平台模型")
	}
}

func TestCodeBuddyReconcileDefaultModel(t *testing.T) {
	dir := func(t *testing.T) string {
		t.Helper()
		d := filepath.Join(t.TempDir(), ".codebuddy")
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		return d
	}
	write := func(t *testing.T, dir, name string, doc map[string]any) {
		t.Helper()
		raw, _ := json.MarshalIndent(doc, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	candidates := []string{
		"qwen-max", "glm-5.3-flash",
		"kshell-default/glm-5.3-flash", "kshell-p2/ds-x",
	}

	t.Run("精确命中不动", func(t *testing.T) {
		d := dir(t)
		write(t, d, "models.json", map[string]any{"models": []any{}})
		write(t, d, "settings.json", map[string]any{"model": "kshell-default/glm-5.3-flash", "keep": 1})
		if err := (CodeBuddy{}).reconcileDefaultModel(d, candidates); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		got := readCodeBuddySettings(t, d)
		if got["model"] != "kshell-default/glm-5.3-flash" {
			t.Fatalf("不应改写，got %v", got["model"])
		}
	})

	t.Run("裸名降级回写", func(t *testing.T) {
		d := dir(t)
		write(t, d, "models.json", map[string]any{"models": []any{}})
		write(t, d, "settings.json", map[string]any{"model": "custom-local:glm-5.3-flash"})
		if err := (CodeBuddy{}).reconcileDefaultModel(d, candidates); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		got := readCodeBuddySettings(t, d)
		if got["model"] != "glm-5.3-flash" {
			t.Fatalf("应回写裸名命中条目，got %v", got["model"])
		}
	})

	t.Run("无命中回退首个kshell", func(t *testing.T) {
		d := dir(t)
		write(t, d, "models.json", map[string]any{"models": []any{}})
		write(t, d, "settings.json", map[string]any{"model": "old-plat:gone"})
		if err := (CodeBuddy{}).reconcileDefaultModel(d, candidates); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		got := readCodeBuddySettings(t, d)
		if got["model"] != "kshell-default/glm-5.3-flash" {
			t.Fatalf("应回退首个 kshell id，got %v", got["model"])
		}
	})

	t.Run("纯裸名无命中不动（内置模型保护）", func(t *testing.T) {
		d := dir(t)
		write(t, d, "models.json", map[string]any{"models": []any{}})
		write(t, d, "settings.json", map[string]any{"model": "gpt-4o"})
		if err := (CodeBuddy{}).reconcileDefaultModel(d, candidates); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		got := readCodeBuddySettings(t, d)
		if got["model"] != "gpt-4o" {
			t.Fatalf("CLI 内置模型 id 不应被覆盖，got %v", got["model"])
		}
	})

	t.Run("无候选不动", func(t *testing.T) {
		d := dir(t)
		write(t, d, "models.json", map[string]any{"models": []any{}})
		write(t, d, "settings.json", map[string]any{"model": "old-plat:gone"})
		if err := (CodeBuddy{}).reconcileDefaultModel(d, nil); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		got := readCodeBuddySettings(t, d)
		if got["model"] != "old-plat:gone" {
			t.Fatalf("无候选时不应改写，got %v", got["model"])
		}
	})

	t.Run("缺失与损坏跳过", func(t *testing.T) {
		d := dir(t)
		write(t, d, "models.json", map[string]any{"models": []any{}})
		// settings.json 缺失。
		if err := (CodeBuddy{}).reconcileDefaultModel(d, candidates); err != nil {
			t.Fatalf("缺失 settings 不应报错: %v", err)
		}
		// JSON 损坏。
		if err := os.WriteFile(filepath.Join(d, "settings.json"), []byte("{bad"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := (CodeBuddy{}).reconcileDefaultModel(d, candidates); err != nil {
			t.Fatalf("损坏 settings 不应报错: %v", err)
		}
		// model 非字符串 / 空。
		write(t, d, "settings.json", map[string]any{"model": ""})
		if err := (CodeBuddy{}).reconcileDefaultModel(d, candidates); err != nil {
			t.Fatalf("空 model 不应报错: %v", err)
		}
	})
}

func readCodeBuddySettings(t *testing.T, dir string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
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
