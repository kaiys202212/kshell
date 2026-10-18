package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/config"
)

func newModelApp(t *testing.T) (*App, config.Layout) {
	t.Helper()
	dir := t.TempDir()
	layout := config.Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	return NewAppWith(Options{Config: config.Default(), Layout: layout}), layout
}

func TestGetModelConfigMultiPlatformView(t *testing.T) {
	app, _ := newModelApp(t)
	app.mu.Lock()
	app.opts.Config.Model = config.ModelConfig{
		Enabled:         true,
		DefaultPlatform: "p2",
		ActiveModel:     "m",
		Platforms: []config.ModelPlatform{
			{ID: "p1", Name: "A", OpenAIBaseURL: "https://a/", APIKey: "secret"},
			{ID: "p2", Name: "B", Models: []string{"m"}},
		},
	}
	app.mu.Unlock()

	v, err := app.GetModelConfig()
	if err != nil {
		t.Fatalf("GetModelConfig: %v", err)
	}
	if !v.Enabled || v.ActiveModel != "m" || v.DefaultPlatform != "p2" {
		t.Fatalf("顶层字段错误: %+v", v)
	}
	if len(v.Platforms) != 2 {
		t.Fatalf("平台数 = %d", len(v.Platforms))
	}
	p1, p2 := v.Platforms[0], v.Platforms[1]
	if !p1.APIKeySet || p2.APIKeySet {
		t.Fatalf("APIKeySet 标记错误: %+v %+v", p1, p2)
	}
	if p1.IsDefault || !p2.IsDefault {
		t.Fatalf("IsDefault 标记错误: %+v %+v", p1, p2)
	}
	if p2.Models == nil || len(p2.Models) != 1 {
		t.Fatalf("nil Models 应返回空 slice: %#v", p2.Models)
	}
}

func TestGetModelConfigEmptyPlatforms(t *testing.T) {
	app, _ := newModelApp(t)
	app.mu.Lock()
	app.opts.Config.Model = config.ModelConfig{}
	app.mu.Unlock()

	v, err := app.GetModelConfig()
	if err != nil {
		t.Fatalf("GetModelConfig: %v", err)
	}
	// Platforms 为 nil 时应返回空 slice（前端 JSON 需要数组）。
	if v.Platforms == nil || len(v.Platforms) != 0 {
		t.Fatalf("Platforms 应为空 slice: %#v", v.Platforms)
	}
}

func TestSetModelConfigPlatformCRUD(t *testing.T) {
	app, layout := newModelApp(t)

	// 1. 新增平台：ID 必填语义下第一次保存生成默认平台。
	if err := app.SetModelConfig(ModelConfigInput{
		Enabled: true,
		Platforms: []ModelPlatformInput{{
			ID: "p1", Name: "DeepSeek", Preset: "deepseek",
			OpenAIBaseURL: "https://oai/", AnthropicBaseURL: "https://ant/",
			APIKey: "k1", Models: []string{" m ", "gpt-x", "m", ""},
		}},
		DefaultPlatform: "p1", ActiveModel: " gpt-x ",
	}); err != nil {
		t.Fatalf("Set 新增: %v", err)
	}
	loaded, _ := config.Load(layout)
	if len(loaded.Model.Platforms) != 1 {
		t.Fatalf("应有 1 个平台: %+v", loaded.Model.Platforms)
	}
	pl := loaded.Model.Platforms[0]
	if pl.ID != "p1" || pl.Name != "DeepSeek" || pl.Preset != "deepseek" || pl.APIKey != "k1" {
		t.Fatalf("平台字段错误: %+v", pl)
	}
	if pl.OpenAIBaseURL != "https://oai/" || pl.AnthropicBaseURL != "https://ant/" {
		t.Fatalf("端点错误: %+v", pl)
	}
	if len(pl.Models) != 2 || pl.Models[0] != "m" || pl.Models[1] != "gpt-x" {
		t.Fatalf("models 应 TrimSpace/去空/去重: %#v", pl.Models)
	}
	if loaded.Model.DefaultPlatform != "p1" || loaded.Model.ActiveModel != "gpt-x" {
		t.Fatalf("默认平台/活跃模型错误: %+v", loaded.Model)
	}
	// legacy 字段应全部清空。
	if loaded.Model.Preset != "" || loaded.Model.OpenAIBaseURL != "" ||
		loaded.Model.AnthropicBaseURL != "" || loaded.Model.BaseURL != "" ||
		loaded.Model.APIKey != "" || loaded.Model.Models != nil || loaded.Model.Agents != nil {
		t.Fatalf("legacy 字段未清空: %+v", loaded.Model)
	}

	// 2. 新增第二个平台（输入含 p1+p2），p1 留空 key 保持原值。
	if err := app.SetModelConfig(ModelConfigInput{
		Enabled: true,
		Platforms: []ModelPlatformInput{
			{ID: "p1", Name: "DeepSeek", OpenAIBaseURL: "https://oai/"},
			{ID: "p2", Name: "GLM", AnthropicBaseURL: "https://zhipu/", APIKey: "k2"},
		},
		DefaultPlatform: "p2",
	}); err != nil {
		t.Fatalf("Set 更新: %v", err)
	}
	loaded, _ = config.Load(layout)
	if len(loaded.Model.Platforms) != 2 {
		t.Fatalf("应有 2 个平台: %+v", loaded.Model.Platforms)
	}
	if got := loaded.Model.Platforms[0].APIKey; got != "k1" {
		t.Fatalf("留空应保持密钥，got %q", got)
	}
	if got := loaded.Model.Platforms[1].APIKey; got != "k2" {
		t.Fatalf("新平台应取传入密钥，got %q", got)
	}

	// 3. 更新：传入新 key 覆盖；更新 p1 端点但留空 key。
	if err := app.SetModelConfig(ModelConfigInput{
		Enabled: true,
		Platforms: []ModelPlatformInput{
			{ID: "p1", Name: "DeepSeek", OpenAIBaseURL: "https://oai2/", APIKey: "k1-new"},
		},
		DefaultPlatform: "p1",
	}); err != nil {
		t.Fatalf("Set 更新: %v", err)
	}
	loaded, _ = config.Load(layout)
	if len(loaded.Model.Platforms) != 1 {
		t.Fatalf("输入列表缺 p2 应删除 p2: %+v", loaded.Model.Platforms)
	}
	pl = loaded.Model.Platforms[0]
	if pl.APIKey != "k1-new" || pl.OpenAIBaseURL != "https://oai2/" {
		t.Fatalf("更新失败: %+v", pl)
	}
	// 3b. ClearAPIKey 显式清除。
	if err := app.SetModelConfig(ModelConfigInput{
		Enabled:   true,
		Platforms: []ModelPlatformInput{{ID: "p1", Name: "DeepSeek", ClearAPIKey: true}},
	}); err != nil {
		t.Fatalf("Set clear: %v", err)
	}
	loaded, _ = config.Load(layout)
	if loaded.Model.Platforms[0].APIKey != "" {
		t.Fatalf("ClearAPIKey 应清空，got %q", loaded.Model.Platforms[0].APIKey)
	}

	// 4. DefaultPlatform 非法回落第一个平台。
	if err := app.SetModelConfig(ModelConfigInput{
		Enabled:         true,
		Platforms:       []ModelPlatformInput{{ID: "p1", Name: "DeepSeek"}, {ID: "p2", Name: "GLM"}},
		DefaultPlatform: "nope",
	}); err != nil {
		t.Fatalf("Set 默认平台: %v", err)
	}
	loaded, _ = config.Load(layout)
	if loaded.Model.DefaultPlatform != "p1" {
		t.Fatalf("非法默认平台应回落第一个，got %q", loaded.Model.DefaultPlatform)
	}
}

func TestSetModelConfigValidation(t *testing.T) {
	cases := []struct {
		name    string
		input   ModelConfigInput
		wantKey string
	}{
		{
			name:    "平台名必填",
			input:   ModelConfigInput{Platforms: []ModelPlatformInput{{ID: "p1"}}},
			wantKey: "err.settings.model_platform_name_required",
		},
		{
			name:    "平台 ID 必填",
			input:   ModelConfigInput{Platforms: []ModelPlatformInput{{Name: "A"}}},
			wantKey: "err.settings.model_platform_id_required",
		},
		{
			name: "平台 ID 重复",
			input: ModelConfigInput{Platforms: []ModelPlatformInput{
				{ID: "p1", Name: "A"}, {ID: " p1 ", Name: "B"},
			}},
			wantKey: "err.settings.model_platform_id_duplicated",
		},
		{
			name: "OpenAI 端点非法",
			input: ModelConfigInput{Platforms: []ModelPlatformInput{
				{ID: "p1", Name: "A", OpenAIBaseURL: "ftp://bad"},
			}},
			wantKey: "err.settings.http_url_required",
		},
		{
			name: "Anthropic 端点非法",
			input: ModelConfigInput{Platforms: []ModelPlatformInput{
				{ID: "p1", Name: "A", AnthropicBaseURL: "not-a-url"},
			}},
			wantKey: "err.settings.http_url_required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, layout := newModelApp(t)
			err := app.SetModelConfig(tc.input)
			if err == nil || !strings.HasPrefix(err.Error(), tc.wantKey) {
				t.Fatalf("应报 %s，got %v", tc.wantKey, err)
			}
			loaded, _ := config.Load(layout)
			if len(loaded.Model.Platforms) != 0 {
				t.Fatalf("校验失败不应落盘: %+v", loaded.Model.Platforms)
			}
		})
	}
}

func TestSetModelConfigEmptyPlatformsClearsDefault(t *testing.T) {
	app, layout := newModelApp(t)
	if err := app.SetModelConfig(ModelConfigInput{
		Enabled: true,
		Platforms: []ModelPlatformInput{
			{ID: "p1", Name: "A", APIKey: "k"},
		},
		DefaultPlatform: "p1", ActiveModel: "m",
	}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// 全部删除后默认平台与活跃模型应清空。
	if err := app.SetModelConfig(ModelConfigInput{Enabled: true}); err != nil {
		t.Fatalf("Set 空: %v", err)
	}
	loaded, _ := config.Load(layout)
	if len(loaded.Model.Platforms) != 0 || loaded.Model.DefaultPlatform != "" {
		t.Fatalf("应为空: %+v", loaded.Model)
	}
	if loaded.Model.ActiveModel != "" {
		t.Fatalf("ActiveModel 应保留传入值: %q", loaded.Model.ActiveModel)
	}
}

func TestListModelPresetsNonEmpty(t *testing.T) {
	app, _ := newModelApp(t)
	list := app.ListModelPresets()
	if len(list) < 3 {
		t.Fatalf("预设过少: %d", len(list))
	}
}

func TestModelOptionsUsesActiveModel(t *testing.T) {
	app, _ := newModelApp(t)
	app.mu.Lock()
	app.opts.Config.Model = config.ModelConfig{
		Enabled: true,
		OpenAIBaseURL: "https://oai/", AnthropicBaseURL: "https://ant/",
		APIKey: "k", ActiveModel: "glm-5",
	}
	app.mu.Unlock()

	mo := app.modelOptions()
	for _, toolID := range []string{"claude", "codex", "anything"} {
		mc, ok := mo.Resolver(toolID)
		if !ok || mc.Model != "glm-5" || mc.OpenAIBaseURL != "https://oai/" || mc.APIKey != "k" {
			t.Fatalf("resolver(%s) = %+v ok=%v", toolID, mc, ok)
		}
	}
	app.mu.Lock()
	app.opts.Config.Model = config.ModelConfig{Enabled: false, ActiveModel: "glm-5"}
	app.mu.Unlock()
	if mo := app.modelOptions(); mo.Resolver != nil {
		t.Fatal("未启用时不应注入")
	}
}

// writeCodebuddySettings 在 t.TempDir 下造一个 ~/.codebuddy/settings.json。
func writeCodebuddySettings(t *testing.T, model string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".codebuddy")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := `{"model":"` + model + `"}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestModelOptionsCodebuddyPrefersLastModel(t *testing.T) {
	app, _ := newModelApp(t)
	app.mu.Lock()
	app.opts.Home = writeCodebuddySettings(t, "custom-local:deepseek-v4.1-flash")
	app.opts.Config.Model = config.ModelConfig{Enabled: true, ActiveModel: "glm-5"}
	app.mu.Unlock()

	mo := app.modelOptions()
	// codebuddy：沿用记住的上次选择
	mc, ok := mo.Resolver("codebuddy")
	if !ok || mc.Model != "custom-local:deepseek-v4.1-flash" {
		t.Fatalf("resolver(codebuddy) = %+v ok=%v", mc, ok)
	}
	// 其他工具：仍用 ActiveModel
	mc, ok = mo.Resolver("claude")
	if !ok || mc.Model != "glm-5" {
		t.Fatalf("resolver(claude) = %+v ok=%v", mc, ok)
	}
	// codebuddy 无记录：回落 ActiveModel
	app.mu.Lock()
	app.opts.Home = t.TempDir()
	app.mu.Unlock()
	mc, ok = mo.Resolver("codebuddy")
	if !ok || mc.Model != "glm-5" {
		t.Fatalf("resolver(codebuddy, no record) = %+v ok=%v", mc, ok)
	}
}

// writeCursorConfig 在 t.TempDir 下造一个 ~/.cursor/cli-config.json。
func writeCursorConfig(t *testing.T, modelID string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := `{"selectedModel":{"modelId":"` + modelID + `"}}`
	if err := os.WriteFile(filepath.Join(dir, "cli-config.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestCursorLastModel(t *testing.T) {
	// 有记录：返回该值
	home := writeCursorConfig(t, "grok-4.6")
	if got := cursorLastModel(home); got != "grok-4.6" {
		t.Fatalf("cursorLastModel = %q", got)
	}
	// "default"（Auto）：返回空串（回落 ActiveModel）
	home = writeCursorConfig(t, "default")
	if got := cursorLastModel(home); got != "" {
		t.Fatalf("default model: got %q", got)
	}
	// modelId 为空：空串
	home = writeCursorConfig(t, "")
	if got := cursorLastModel(home); got != "" {
		t.Fatalf("empty modelId: got %q", got)
	}
	// 无 cli-config.json：空串
	if got := cursorLastModel(t.TempDir()); got != "" {
		t.Fatalf("missing file: got %q", got)
	}
	// JSON 非法：空串
	home = t.TempDir()
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cli-config.json"), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := cursorLastModel(home); got != "" {
		t.Fatalf("bad json: got %q", got)
	}
}

func TestModelOptionsCursorPrefersLastModel(t *testing.T) {
	app, _ := newModelApp(t)
	app.mu.Lock()
	app.opts.Home = writeCursorConfig(t, "grok-4.7")
	app.opts.Config.Model = config.ModelConfig{Enabled: true, ActiveModel: "glm-5"}
	app.mu.Unlock()

	mo := app.modelOptions()
	// cursor：沿用记住的上次选择
	mc, ok := mo.Resolver("cursor")
	if !ok || mc.Model != "grok-4.7" {
		t.Fatalf("resolver(cursor) = %+v ok=%v", mc, ok)
	}
	// 其他工具：仍用 ActiveModel
	mc, ok = mo.Resolver("codebuddy")
	if !ok || mc.Model != "glm-5" {
		t.Fatalf("resolver(codebuddy) = %+v ok=%v", mc, ok)
	}
	// cursor 记录为 default（Auto）：回落 ActiveModel
	app.mu.Lock()
	app.opts.Home = writeCursorConfig(t, "default")
	app.mu.Unlock()
	mc, ok = mo.Resolver("cursor")
	if !ok || mc.Model != "glm-5" {
		t.Fatalf("resolver(cursor, default) = %+v ok=%v", mc, ok)
	}
}

func TestSessionAndPermissionModeRoundTrip(t *testing.T) {
	app, layout := newModelApp(t)
	if app.GetSessionMode() != config.SessionModeTUI {
		t.Fatalf("default session = %q", app.GetSessionMode())
	}
	if err := app.SetSessionMode(config.SessionModeACP); err != nil {
		t.Fatal(err)
	}
	if err := app.SetPermissionMode(config.PermissionModeBypass); err != nil {
		t.Fatal(err)
	}
	loaded, _ := config.Load(layout)
	if loaded.SessionMode != config.SessionModeACP || loaded.PermissionMode != config.PermissionModeBypass {
		t.Fatalf("loaded = %+v", loaded)
	}
	if err := app.SetSessionMode("bogus"); err == nil {
		t.Fatal("非法 session_mode 应报错")
	}
}
