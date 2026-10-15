package config

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func tempPaths(t *testing.T) Layout {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	p, err := Paths()
	if err != nil {
		t.Fatalf("Paths() error: %v", err)
	}
	return p
}

func TestPathsUnderHome(t *testing.T) {
	p := tempPaths(t)
	if filepath.Base(p.Root) != ".kshell" {
		t.Fatalf("root = %s, want .kshell", p.Root)
	}
	if p.Config != filepath.Join(p.Root, "config.yaml") {
		t.Fatalf("config = %s", p.Config)
	}
	if p.Connections != filepath.Join(p.Root, "connections.yaml") {
		t.Fatalf("connections = %s", p.Connections)
	}
	if p.Providers != filepath.Join(p.Root, "providers.yaml") {
		t.Fatalf("providers = %s", p.Providers)
	}
	if p.CacheIndex != filepath.Join(p.Root, "cache", "index.json") {
		t.Fatalf("cache index = %s", p.CacheIndex)
	}
}

func TestLoadDefaultWhenMissing(t *testing.T) {
	p := tempPaths(t)
	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !reflect.DeepEqual(got, Default()) {
		t.Fatalf("got %+v, want %+v", got, Default())
	}
}

func TestSaveIsAtomicAndRoundTrips(t *testing.T) {
	p := tempPaths(t)
	want := Default()
	want.MaxDepth = 3
	want.ScanRoots = []string{filepath.Join("D:", "data", "workspace")}

	if err := Save(p, want); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch:\ngot  %+v\nwant %+v", got, want)
	}

	entries, err := os.ReadDir(p.Root)
	if err != nil {
		t.Fatalf("ReadDir() error: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("atomic save left temp file: %s", e.Name())
		}
	}
}

// TestSaveConcurrentNoTempCollision 覆盖并发保存互踩：固定 config.yaml.tmp
// 文件名下，两个并发 Save 在 Windows 上第二个 Rename 会因文件被占用而失败。
// 改用 os.CreateTemp 唯一临时文件后并发保存必须全部成功、无 .tmp 残留。
func TestSaveConcurrentNoTempCollision(t *testing.T) {
	p := tempPaths(t)
	if err := os.MkdirAll(p.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func(i int) {
			c := Default()
			c.MaxDepth = 3 + i
			done <- Save(p, c)
		}(i)
	}
	for i := 0; i < 8; i++ {
		if err := <-done; err != nil {
			t.Fatalf("并发 Save 不应失败: %v", err)
		}
	}
	entries, err := os.ReadDir(p.Root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("并发保存后残留临时文件: %s", e.Name())
		}
	}
	// 最终文件必须可解析（任一次成功写的结果）
	if _, err := Load(p); err != nil {
		t.Fatalf("并发保存后配置应可加载: %v", err)
	}
}

func TestLoadRecoversFromCorruptConfig(t *testing.T) {
	p := tempPaths(t)
	if err := os.MkdirAll(p.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Config, []byte("scan_roots: [unterminated\n  - "), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Load(p)
	// 语法损坏时回退默认值，但必须把情况告诉调用方（由调用方降级为提示，不静默清空）。
	if err == nil {
		t.Fatal("corrupt config should report the recovery")
	}
	if !reflect.DeepEqual(got, Default()) {
		t.Fatalf("got %+v, want defaults", got)
	}

	entries, err := os.ReadDir(p.Root)
	if err != nil {
		t.Fatal(err)
	}
	backedUp := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".bak") {
			backedUp = true
		}
	}
	if !backedUp {
		t.Fatalf("expected corrupt config to be backed up, entries: %v", entries)
	}
	if _, err := os.Stat(p.Config); err != nil {
		t.Fatalf("expected a rebuilt config file: %v", err)
	}
}

func TestLoadKeepsParsedFieldsOnTypeMismatch(t *testing.T) {
	p := tempPaths(t)
	if err := os.MkdirAll(p.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "scan_roots:\n  - D:\\data\nmax_depth: \"abc\"\n"
	if err := os.WriteFile(p.Config, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Load(p)
	if err == nil {
		t.Fatal("type mismatch should be reported")
	}
	// 已解析成功的字段必须保留，不能因为一个字段写错就把整份配置清空。
	if len(got.ScanRoots) != 1 || got.ScanRoots[0] != "D:\\data" {
		t.Fatalf("scan_roots should survive a type error, got %+v", got.ScanRoots)
	}
	if got.MaxDepth != Default().MaxDepth {
		t.Fatalf("max_depth = %d, want default %d", got.MaxDepth, Default().MaxDepth)
	}

	entries, err := os.ReadDir(p.Root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".bak") {
			t.Fatalf("type mismatch must not wipe the config, found %s", e.Name())
		}
	}
}

func TestModelConfigDefaultsAndRoundTrip(t *testing.T) {
	p := tempPaths(t)

	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Model.Enabled || got.Model.OpenAIBaseURL != "" || got.Model.AnthropicBaseURL != "" || got.Model.APIKey != "" {
		t.Fatalf("默认 model 应为空，got %+v", got.Model)
	}
	if got.Model.Models == nil || len(got.Model.Models) != 0 {
		t.Fatalf("默认 Models 应为非 nil 空 slice，got %#v", got.Model.Models)
	}

	want := Default()
	want.Model = ModelConfig{
		Enabled:          true,
		OpenAIBaseURL:    "https://example.com/v1",
		AnthropicBaseURL: "https://example.com/anthropic",
		APIKey:           "tp-secret",
		Models:           []string{"mimo-v2.5", "kimi-k2"},
		ActiveModel:      "mimo-v2.5",
	}
	if err := Save(p, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(loaded.Model, want.Model) {
		t.Fatalf("model round trip:\ngot  %+v\nwant %+v", loaded.Model, want.Model)
	}
}

func TestModelConfigNormalizes(t *testing.T) {
	p := tempPaths(t)
	if err := os.MkdirAll(p.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "model:\n  enabled: true\n  base_url: \"not-a-url\"\n  active_model: \"  mimo-v2.5  \"\n  models: [\"  m1  \", \"\", \"m1\", \"m2\", \"  \"]\n"
	if err := os.WriteFile(p.Config, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Model.BaseURL != "" {
		t.Fatalf("非法 base_url 应被丢弃，got %q", got.Model.BaseURL)
	}
	if got.Model.ActiveModel != "mimo-v2.5" {
		t.Fatalf("active_model 应 TrimSpace，got %q", got.Model.ActiveModel)
	}
	want := []string{"m1", "m2"}
	if !reflect.DeepEqual(got.Model.Models, want) {
		t.Fatalf("models 应 TrimSpace/去空/去重：got %#v want %#v", got.Model.Models, want)
	}
}

func TestModelConfigLegacyAgentsMigration(t *testing.T) {
	p := tempPaths(t)
	if err := os.MkdirAll(p.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "model:\n  enabled: true\n  agents:\n    claude: \"m1\"\n    codex: \"  m2  \"\n    gemini: \"m1\"\n"
	if err := os.WriteFile(p.Config, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantModels := []string{"m1", "m2"}
	if !reflect.DeepEqual(got.Model.Models, wantModels) {
		t.Fatalf("旧 agents 应去重迁移进 Models：got %#v want %#v", got.Model.Models, wantModels)
	}
	if got.Model.ActiveModel != "m1" {
		t.Fatalf("迁移后 ActiveModel 应取第一个，got %q", got.Model.ActiveModel)
	}
	if len(got.Model.Agents) != 0 {
		t.Fatalf("迁移后 Agents 应清空，got %#v", got.Model.Agents)
	}
	// 保存后旧字段不再落盘。
	if err := Save(p, got); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(p.Config)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("agents")) {
		t.Fatalf("保存后不应再写 agents 字段：\n%s", data)
	}
}

func TestScannersPartiallySpecifiedKeepsDefaults(t *testing.T) {
	p := tempPaths(t)
	if err := os.MkdirAll(p.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Config, []byte("scanners:\n  sshconfig: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if got.Scanners["sshconfig"] {
		t.Fatal("sshconfig should stay disabled")
	}
	for _, name := range []string{"env", "spring", "deploy", "docs"} {
		if !got.Scanners[name] {
			t.Fatalf("scanner %s should keep its default (enabled)", name)
		}
	}
}
