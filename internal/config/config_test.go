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
	if got.Model.Platforms == nil || len(got.Model.Platforms) != 0 {
		t.Fatalf("默认 Platforms 应为非 nil 空 slice，got %#v", got.Model.Platforms)
	}

	want := Default()
	want.Model = ModelConfig{
		Enabled: true,
		Platforms: []ModelPlatform{{
			ID:               "default",
			Name:             "示例平台",
			OpenAIBaseURL:    "https://example.com/v1",
			AnthropicBaseURL: "https://example.com/anthropic",
			APIKey:           "tp-secret",
			Models:           []string{"mimo-v2.5", "kimi-k2"},
		}},
		DefaultPlatform: "default",
		ActiveModel:     "mimo-v2.5",
		Models:          []string{},
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
	// legacy 单平台字段已迁移进 default 平台并清空。
	if len(got.Model.Models) != 0 {
		t.Fatalf("迁移后顶层 Models 应清空，got %#v", got.Model.Models)
	}
	if len(got.Model.Platforms) != 1 {
		t.Fatalf("应迁移出恰好一个平台，got %#v", got.Model.Platforms)
	}
	pl := got.Model.Platforms[0]
	if pl.ID != "default" || pl.Name != "默认平台" {
		t.Fatalf("迁移平台 ID/Name 不符，got %+v", pl)
	}
	want := []string{"m1", "m2"}
	if !reflect.DeepEqual(pl.Models, want) {
		t.Fatalf("平台 models 应 TrimSpace/去空/去重：got %#v want %#v", pl.Models, want)
	}
	if got.Model.DefaultPlatform != "default" {
		t.Fatalf("DefaultPlatform = %q, want default", got.Model.DefaultPlatform)
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
	if got.Model.ActiveModel != "m1" {
		t.Fatalf("迁移后 ActiveModel 应取第一个，got %q", got.Model.ActiveModel)
	}
	if len(got.Model.Agents) != 0 {
		t.Fatalf("迁移后 Agents 应清空，got %#v", got.Model.Agents)
	}
	// agents 迁入的 Models 连同其他 legacy 字段一起迁入 default 平台。
	if got.Model.DefaultPlatform != "default" {
		t.Fatalf("DefaultPlatform = %q, want default", got.Model.DefaultPlatform)
	}
	if len(got.Model.Platforms) != 1 || got.Model.Platforms[0].ID != "default" {
		t.Fatalf("应迁移出 default 平台，got %#v", got.Model.Platforms)
	}
	if !reflect.DeepEqual(got.Model.Platforms[0].Models, wantModels) {
		t.Fatalf("平台 models 应为迁移结果：got %#v want %#v", got.Model.Platforms[0].Models, wantModels)
	}
	if len(got.Model.Models) != 0 {
		t.Fatalf("迁移后顶层 Models 应清空，got %#v", got.Model.Models)
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

// TestModelConfigLegacyFullMigration 覆盖旧单平台完整迁移：
// preset/base_url/api_key/models/active_model 打包成 default 平台，legacy 字段清空，
// 且保存重载后平台保留、legacy 不复活。
func TestModelConfigLegacyFullMigration(t *testing.T) {
	p := tempPaths(t)
	if err := os.MkdirAll(p.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "model:\n  enabled: true\n  preset: \"自建中转\"\n  base_url: \"https://relay.example.com/v1\"\n  api_key: \"sk-1\"\n  active_model: \"  mimo-v2.5  \"\n  models: [\"mimo-v2.5\", \"kimi-k2\"]\n"
	if err := os.WriteFile(p.Config, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantPlatform := ModelPlatform{
		ID:               "default",
		Name:             "自建中转",
		Preset:           "自建中转",
		OpenAIBaseURL:    "https://relay.example.com/v1",
		AnthropicBaseURL: "https://relay.example.com/v1",
		APIKey:           "sk-1",
		Models:           []string{"mimo-v2.5", "kimi-k2"},
	}
	if len(got.Model.Platforms) != 1 {
		t.Fatalf("应迁移出恰好一个平台，got %#v", got.Model.Platforms)
	}
	if !reflect.DeepEqual(got.Model.Platforms[0], wantPlatform) {
		t.Fatalf("平台迁移结果不符:\ngot  %+v\nwant %+v", got.Model.Platforms[0], wantPlatform)
	}
	if got.Model.DefaultPlatform != "default" {
		t.Fatalf("DefaultPlatform = %q, want default", got.Model.DefaultPlatform)
	}
	if got.Model.ActiveModel != "mimo-v2.5" {
		t.Fatalf("ActiveModel 应保留（TrimSpace），got %q", got.Model.ActiveModel)
	}
	if got.Model.Preset != "" || got.Model.OpenAIBaseURL != "" || got.Model.AnthropicBaseURL != "" ||
		got.Model.BaseURL != "" || got.Model.APIKey != "" || len(got.Model.Models) != 0 || got.Model.Agents != nil {
		t.Fatalf("迁移后 legacy 字段应全部清空，got %+v", got.Model)
	}

	// 往返：再次保存加载，平台保留、legacy 不复活。
	if err := Save(p, got); err != nil {
		t.Fatalf("Save: %v", err)
	}
	again, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(again.Model, got.Model) {
		t.Fatalf("往返后 model 不一致:\ngot  %+v\nwant %+v", again.Model, got.Model)
	}
}

// TestModelConfigLegacyEmptyStaysEmpty 覆盖 legacy 全空时不生成平台。
func TestModelConfigLegacyEmptyStaysEmpty(t *testing.T) {
	p := tempPaths(t)
	if err := os.MkdirAll(p.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Config, []byte("model:\n  enabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Model.Platforms == nil || len(got.Model.Platforms) != 0 {
		t.Fatalf("无 legacy 内容时不应生成平台，got %#v", got.Model.Platforms)
	}
	if got.Model.DefaultPlatform != "" || got.Model.ActiveModel != "" {
		t.Fatalf("DefaultPlatform/ActiveModel 应为空，got %q/%q", got.Model.DefaultPlatform, got.Model.ActiveModel)
	}
}

// TestModelConfigPlatformsNormalize 覆盖平台级归一化：
// ID/Name 去空白、ID 缺失按序补 p1/p2…（冲突顺延跳号）、非法 URL 丢弃、models 去空去重。
func TestModelConfigPlatformsNormalize(t *testing.T) {
	p := tempPaths(t)
	if err := os.MkdirAll(p.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "model:\n  platforms:\n    - name: \"  A  \"\n      openai_base_url: \"  https://a.example.com/v1  \"\n      models: [\" m1 \", \"\", \"m1\", \"m2\", \"  \"]\n    - id: \"p2\"\n      name: \"B\"\n      openai_base_url: \"not-a-url\"\n    - name: \"C\"\n"
	if err := os.WriteFile(p.Config, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ps := got.Model.Platforms
	if len(ps) != 3 {
		t.Fatalf("应保留 3 个平台，got %#v", ps)
	}
	if ps[0].ID != "p1" || ps[0].Name != "A" {
		t.Fatalf("第一个平台 ID/Name 归一不符，got %+v", ps[0])
	}
	if ps[0].OpenAIBaseURL != "https://a.example.com/v1" {
		t.Fatalf("平台 URL 应 TrimSpace，got %q", ps[0].OpenAIBaseURL)
	}
	if want := []string{"m1", "m2"}; !reflect.DeepEqual(ps[0].Models, want) {
		t.Fatalf("平台 models 应 TrimSpace/去空/去重：got %#v want %#v", ps[0].Models, want)
	}
	if ps[1].ID != "p2" {
		t.Fatalf("显式 ID 应保留，got %q", ps[1].ID)
	}
	if ps[1].OpenAIBaseURL != "" {
		t.Fatalf("非法 URL 应丢弃，got %q", ps[1].OpenAIBaseURL)
	}
	if ps[1].Models == nil || len(ps[1].Models) != 0 {
		t.Fatalf("平台 models 应归一为空 slice，got %#v", ps[1].Models)
	}
	if ps[2].ID != "p3" || ps[2].Name != "C" {
		t.Fatalf("ID 缺失应顺延跳号补齐（p1/p2 已占用），got %+v", ps[2])
	}
}

// TestModelConfigDefaultPlatformFallback 覆盖 DefaultPlatform 校验。
func TestModelConfigDefaultPlatformFallback(t *testing.T) {
	t.Run("指向不存在ID回落第一个", func(t *testing.T) {
		p := tempPaths(t)
		if err := os.MkdirAll(p.Root, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "model:\n  platforms:\n    - id: a\n    - id: b\n  default_platform: ghost\n"
		if err := os.WriteFile(p.Config, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := Load(p)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got.Model.DefaultPlatform != "a" {
			t.Fatalf("DefaultPlatform 应回落第一个平台，got %q", got.Model.DefaultPlatform)
		}
	})
	t.Run("合法ID去空白后保持", func(t *testing.T) {
		p := tempPaths(t)
		if err := os.MkdirAll(p.Root, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "model:\n  platforms:\n    - id: a\n    - id: b\n  default_platform: \"  b  \"\n"
		if err := os.WriteFile(p.Config, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := Load(p)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got.Model.DefaultPlatform != "b" {
			t.Fatalf("DefaultPlatform 应 TrimSpace 后保持，got %q", got.Model.DefaultPlatform)
		}
	})
	t.Run("无平台时连同ActiveModel清空", func(t *testing.T) {
		p := tempPaths(t)
		if err := os.MkdirAll(p.Root, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "model:\n  default_platform: ghost\n  active_model: m1\n"
		if err := os.WriteFile(p.Config, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := Load(p)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got.Model.DefaultPlatform != "" || got.Model.ActiveModel != "" {
			t.Fatalf("无平台时应清空 DefaultPlatform/ActiveModel，got %q/%q",
				got.Model.DefaultPlatform, got.Model.ActiveModel)
		}
	})
}

// TestActivePlatform 覆盖默认平台取值：命中、回落第一个、空列表返回 false。
func TestActivePlatform(t *testing.T) {
	ps := []ModelPlatform{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}}
	cases := []struct {
		name   string
		m      ModelConfig
		wantID string
		wantOK bool
	}{
		{"命中DefaultPlatform", ModelConfig{Platforms: ps, DefaultPlatform: "b"}, "b", true},
		{"未指定回落第一个", ModelConfig{Platforms: ps}, "a", true},
		{"指向缺失回落第一个", ModelConfig{Platforms: ps, DefaultPlatform: "zzz"}, "a", true},
		{"空列表返回false", ModelConfig{}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.m.ActivePlatform()
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if got.ID != tc.wantID {
				t.Fatalf("ID = %q, want %q", got.ID, tc.wantID)
			}
		})
	}
}

// TestModelConfigPlatformsRoundTrip 覆盖多平台配置的保存/加载往返。
func TestModelConfigPlatformsRoundTrip(t *testing.T) {
	p := tempPaths(t)
	want := Default()
	want.Model = ModelConfig{
		Enabled: true,
		Platforms: []ModelPlatform{
			{ID: "p1", Name: "A", OpenAIBaseURL: "https://a.example.com/v1", Models: []string{"m1"}},
			{ID: "p2", Name: "B", AnthropicBaseURL: "https://b.example.com", APIKey: "k2", Models: []string{"m2", "m3"}},
		},
		DefaultPlatform: "p2",
		ActiveModel:     "m2",
		Models:          []string{},
	}
	if err := Save(p, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got.Model, want.Model) {
		t.Fatalf("平台往返不符:\ngot  %+v\nwant %+v", got.Model, want.Model)
	}
	if got.Model.Preset != "" || got.Model.OpenAIBaseURL != "" || got.Model.AnthropicBaseURL != "" ||
		got.Model.BaseURL != "" || got.Model.APIKey != "" || len(got.Model.Models) != 0 || got.Model.Agents != nil {
		t.Fatalf("往返后 legacy 字段不应复活，got %+v", got.Model)
	}
}
