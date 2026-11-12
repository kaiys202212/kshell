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

func TestInputFromModelCarriesToolPlatforms(t *testing.T) {
	// 旧单字段兜底：未归一化的程序内构造配置仍要能读到平台。
	in := InputFromModel(config.ModelConfig{
		Enabled: true,
		ToolSync: config.ToolSyncConfig{
			ClaudePlatform:    "p1",
			CodeBuddyPlatform: "p1",
		},
	})
	if in.ClaudePlatformID != "p1" || in.CodeBuddyPlatformID != "p1" {
		t.Fatalf("tool platforms = %q/%q, want p1/p1", in.ClaudePlatformID, in.CodeBuddyPlatformID)
	}
}

// 新格式唯一来源是 ToolPlatforms 映射：归一化后的真实配置旧字段已清空，
// InputFromModel 必须从映射取值，否则静态注入在真实路径永远落第一个平台。
func TestInputFromModelCarriesToolPlatformMap(t *testing.T) {
	in := InputFromModel(config.ModelConfig{
		Enabled: true,
		ToolSync: config.ToolSyncConfig{
			ToolPlatforms: map[string]string{"claude": "p2", "codebuddy": "p1"},
		},
	})
	if in.ClaudePlatformID != "p2" || in.CodeBuddyPlatformID != "p1" {
		t.Fatalf("tool platforms = %q/%q, want p2/p1", in.ClaudePlatformID, in.CodeBuddyPlatformID)
	}
}

// Managed 决定是否允许改写各工具自有模型配置：只有 kshell 留有配置痕迹
// （启用、平台、默认平台或 ActiveModel）才算接管过；从未配置过必须原样保留。
func TestSyncInputManaged(t *testing.T) {
	cases := []struct {
		name string
		in   SyncInput
		want bool
	}{
		{"空配置", SyncInput{}, false},
		{"仅空白", SyncInput{ClaudePlatformID: " ", CodeBuddyPlatformID: " ", ActiveModel: "  "}, false},
		{"启用", SyncInput{Enabled: true}, true},
		{"有平台", SyncInput{Platforms: []config.ModelPlatform{{ID: "p1"}}}, true},
		{"仅 ActiveModel 残留", SyncInput{ActiveModel: "m"}, true},
		{"仅工具平台残留", SyncInput{ClaudePlatformID: "p1"}, true},
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

func TestInputFromModelCarriesToolSync(t *testing.T) {
	ts := config.ToolSyncConfig{Enabled: map[string]bool{"codebuddy": true}}
	in := InputFromModel(config.ModelConfig{Enabled: true, ToolSync: ts})
	if !in.ToolSync.Enabled["codebuddy"] {
		t.Fatalf("ToolSync = %+v, want codebuddy 启用", in.ToolSync)
	}
}

// cleanupFor：ToolSync 未启用的目标得到「只清不建」输入（Enabled 关、平台保留
// 以维持 Managed、默认平台与 ActiveModel 清空）；已启用的目标原样返回。
func TestSyncInputCleanupFor(t *testing.T) {
	in := SyncInput{
		Enabled:             true,
		Platforms:           []config.ModelPlatform{{ID: "p1", Name: "P1"}},
		ClaudePlatformID:    "p1",
		CodeBuddyPlatformID: "p1",
		ActiveModel:         "m1",
		ToolSync:            config.ToolSyncConfig{Enabled: map[string]bool{"opencode": true}},
	}
	got := in.cleanupFor("codebuddy")
	if got.Enabled {
		t.Fatal("未启用的目标应关闭 Enabled")
	}
	if len(got.Platforms) != 1 {
		t.Fatal("平台必须保留以维持 Managed（走只清不建分支）")
	}
	if got.ClaudePlatformID != "" || got.CodeBuddyPlatformID != "" || got.ActiveModel != "" {
		t.Fatalf("静态注入键应清空, got %q/%q/%q", got.ClaudePlatformID, got.CodeBuddyPlatformID, got.ActiveModel)
	}
	if same := in.cleanupFor("opencode"); !same.Enabled || same.ActiveModel != "m1" {
		t.Fatalf("已启用的目标应返回原输入, got %+v", same)
	}
}

// ToolSync 未启用的目标被「只清不建」：既有 kshell 注入物清除、用户自有内容保留；
// 已启用的目标照常注入。
func TestSyncAllToolOffCleansOnlyTarget(t *testing.T) {
	home := t.TempDir()
	plat := []config.ModelPlatform{{
		ID: "p1", Name: "P1",
		OpenAIBaseURL: "https://api.example.com",
		Models:        []string{"m1"},
	}}
	allOn := config.ToolSyncConfig{
		Enabled:  map[string]bool{"opencode": true, "codebuddy": true},
		OpenCode: map[string][]string{"p1": {"m1"}},
	}
	in := SyncInput{Enabled: true, Platforms: plat, ToolSync: allOn}
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

	off := SyncInput{Enabled: true, Platforms: plat, ToolSync: config.ToolSyncConfig{
		Enabled:  map[string]bool{"opencode": true},
		OpenCode: map[string][]string{"p1": {"m1"}},
	}}
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
		t.Fatalf("已启用的 opencode 应照常注入, got %s", ocRaw)
	}
}

// SyncTool 同样遵守 ToolSync：未启用即只清不建。
func TestSyncToolHonorsToolOff(t *testing.T) {
	home := t.TempDir()
	plat := []config.ModelPlatform{{
		ID: "p1", Name: "P1",
		OpenAIBaseURL: "https://api.example.com",
		Models:        []string{"m1"},
	}}
	on := SyncInput{Enabled: true, Platforms: plat, ToolSync: config.ToolSyncConfig{Enabled: map[string]bool{"opencode": true}}}
	if err := SyncTool(home, "opencode", on); err != nil {
		t.Fatalf("预置注入失败: %v", err)
	}
	ocPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	if err := SyncTool(home, "opencode", SyncInput{Enabled: true, Platforms: plat}); err != nil {
		t.Fatalf("SyncTool: %v", err)
	}
	raw, err := os.ReadFile(ocPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "kshell-p1") {
		t.Fatalf("未启用后 kshell provider 应被清掉, got %s", raw)
	}
}

// 未配置过模型时 SyncAll 不得创建任何工具配置文件：Claude 只写登录标记
// （~/.claude.json），settings.json / opencode.json / models.json 都不应出现。
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
