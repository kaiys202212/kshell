package agentsync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/yangk/kshell/internal/config"
)

func TestInputFromModelCarriesDefaultPlatform(t *testing.T) {
	in := InputFromModel(config.ModelConfig{Enabled: true, DefaultPlatform: "p1"})
	if in.DefaultPlatformID != "p1" {
		t.Fatalf("DefaultPlatformID = %q, want p1", in.DefaultPlatformID)
	}
}

// Managed 决定是否允许改写各工具自有模型配置：只有 kshell 留有配置痕迹
//（启用、平台、默认平台或 ActiveModel）才算接管过；从未配置过必须原样保留。
func TestSyncInputManaged(t *testing.T) {
	cases := []struct {
		name string
		in   SyncInput
		want bool
	}{
		{"空配置", SyncInput{}, false},
		{"仅空白", SyncInput{DefaultPlatformID: " ", ActiveModel: "  "}, false},
		{"启用", SyncInput{Enabled: true}, true},
		{"有平台", SyncInput{Platforms: []config.ModelPlatform{{ID: "p1"}}}, true},
		{"仅 ActiveModel 残留", SyncInput{ActiveModel: "m"}, true},
		{"仅 DefaultPlatform 残留", SyncInput{DefaultPlatformID: "p1"}, true},
	}
	for _, c := range cases {
		if got := c.in.Managed(); got != c.want {
			t.Fatalf("%s: Managed = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestInputFromModelCarriesActiveModel(t *testing.T) {
	in := InputFromModel(config.ModelConfig{Enabled: true, ActiveModel: "glm-5.3-flash"})
	if in.ActiveModel != "glm-5.3-flash" {
		t.Fatalf("ActiveModel = %q", in.ActiveModel)
	}
}

// cleanupFor：命中 ToolOff 的目标得到「只清不建」输入（Enabled 关、平台保留
// 以维持 Managed、默认平台与 ActiveModel 清空）；未命中原样返回。
func TestSyncInputCleanupFor(t *testing.T) {
	in := SyncInput{
		Enabled:           true,
		Platforms:         []config.ModelPlatform{{ID: "p1", Name: "P1"}},
		DefaultPlatformID: "p1",
		ActiveModel:       "m1",
		ToolOff:           []string{"codebuddy"},
	}
	got := in.cleanupFor("codebuddy")
	if got.Enabled {
		t.Fatal("命中 ToolOff 应关闭 Enabled")
	}
	if len(got.Platforms) != 1 {
		t.Fatal("平台必须保留以维持 Managed（走只清不建分支）")
	}
	if got.DefaultPlatformID != "" || got.ActiveModel != "" {
		t.Fatalf("静态注入键应清空, got %q/%q", got.DefaultPlatformID, got.ActiveModel)
	}
	if same := in.cleanupFor("opencode"); !same.Enabled || same.ActiveModel != "m1" {
		t.Fatalf("未命中应返回原输入, got %+v", same)
	}
}

// ToolOff 命中的目标被「只清不建」：既有 kshell 注入物清除、用户自有内容保留；
// 未命中的目标照常注入。
func TestSyncAllToolOffCleansOnlyTarget(t *testing.T) {
	home := t.TempDir()
	plat := []config.ModelPlatform{{
		ID: "p1", Name: "P1",
		OpenAIBaseURL: "https://api.example.com",
		Models:        []string{"m1"},
	}}
	in := SyncInput{Enabled: true, Platforms: plat}
	if err := SyncAll(home, in, []string{"opencode", "codebuddy"}); err != nil {
		t.Fatalf("预置注入失败: %v", err)
	}
	// 用户在 codebuddy models.json 里放一条自有模型（非 kshell、不与平台撞名）。
	cbPath := filepath.Join(home, ".codebuddy", "models.json")
	raw, err := os.ReadFile(cbPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["models"] = append(sliceOfMaps(doc["models"]), map[string]any{"id": "my-own", "name": "My Own"})
	userRaw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cbPath, userRaw, 0o644); err != nil {
		t.Fatal(err)
	}

	off := SyncInput{Enabled: true, Platforms: plat, ToolOff: []string{"codebuddy"}}
	if err := SyncAll(home, off, []string{"opencode", "codebuddy"}); err != nil {
		t.Fatalf("ToolOff 同步失败: %v", err)
	}

	// codebuddy：kshell 条目清零，用户条目保留。
	raw, err = os.ReadFile(cbPath)
	if err != nil {
		t.Fatal(err)
	}
	var cb map[string]any
	if err := json.Unmarshal(raw, &cb); err != nil {
		t.Fatalf("models.json 应仍是完整 JSON: %v", err)
	}
	models := sliceOfMaps(cb["models"])
	if len(models) != 1 || models[0]["id"] != "my-own" {
		t.Fatalf("codebuddy 应只剩用户条目, got %v", models)
	}
	// opencode：注入仍在。
	ocRaw, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "opencode.json"))
	if err != nil {
		t.Fatalf("opencode.json 应存在: %v", err)
	}
	if !strings.Contains(string(ocRaw), "kshell-p1") {
		t.Fatalf("未命中 ToolOff 的 opencode 应照常注入, got %s", ocRaw)
	}
}

// SyncTool 同样遵守 ToolOff：命中即只清不建。
func TestSyncToolHonorsToolOff(t *testing.T) {
	home := t.TempDir()
	plat := []config.ModelPlatform{{
		ID: "p1", Name: "P1",
		OpenAIBaseURL: "https://api.example.com",
		Models:        []string{"m1"},
	}}
	if err := SyncTool(home, "opencode", SyncInput{Enabled: true, Platforms: plat}); err != nil {
		t.Fatalf("预置注入失败: %v", err)
	}
	ocPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	if err := SyncTool(home, "opencode", SyncInput{Enabled: true, Platforms: plat, ToolOff: []string{"opencode"}}); err != nil {
		t.Fatalf("SyncTool: %v", err)
	}
	raw, err := os.ReadFile(ocPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "kshell-p1") {
		t.Fatalf("ToolOff 命中后 kshell provider 应被清掉, got %s", raw)
	}
}

// 未配置过模型时 SyncAll 不得创建任何工具配置文件：Claude 只写登录标记
//（~/.claude.json），settings.json / opencode.json / models.json 都不应出现。
func TestSyncAllUnmanagedDoesNotCreateModelFiles(t *testing.T) {
	home := t.TempDir()
	if err := SyncAll(home, SyncInput{}, []string{"opencode", "codebuddy", "claude"}); err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	for _, rel := range []string{
		filepath.Join(".claude", "settings.json"),
		filepath.Join(".config", "opencode", "opencode.json"),
		filepath.Join(".codebuddy", "models.json"),
	} {
		if _, err := os.Stat(filepath.Join(home, rel)); !os.IsNotExist(err) {
			t.Fatalf("不应创建 %s: err=%v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); err != nil {
		t.Fatalf("应写入登录标记: %v", err)
	}
}

// 并发写同一目标（TUI 与桌面端/多个 goroutine 同时同步）必须全部成功，
// 文件保持完整 JSON 且不留下临时文件。Windows 上 rename 会因 sharing violation
// 失败，writeAtomic 需要退避重试；这里用真实并发锁住该行为。
func TestWriteAtomicConcurrentWritersAllSucceed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	const writers = 4
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload := fmt.Sprintf(`{"writer":%d,"pad":%q}`, i, strings.Repeat("x", 1024))
			errs <- writeAtomic(path, []byte(payload))
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("并发写不应失败: %v", err)
		}
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("并发写后文件应仍是完整 JSON: %v (%q)", err, raw)
	}
	if _, ok := doc["writer"]; !ok {
		t.Fatalf("缺少 writer 字段: %v", doc)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".settings.json.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("不应残留临时文件: %v", leftovers)
	}
}
