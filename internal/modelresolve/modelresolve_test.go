package modelresolve

import (
	"errors"
	"testing"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/workbuddy"
)

// testModelConfig 构造解析矩阵共用的配置：p1/p2 端点齐全，wb 是 workbuddy
// 特型平台（无端点，端点/密钥由本地代理提供）；全局激活平台为 p2，
// claude 映射到 p1。
func testModelConfig() config.ModelConfig {
	return config.ModelConfig{
		Enabled:          true,
		ActivePlatformID: "p2",
		Platforms: []config.ModelPlatform{
			{
				ID: "p1", Name: "P1",
				AnthropicBaseURL: "https://p1-ant/", OpenAIBaseURL: "https://p1-oai/",
				APIKey: "k1", Models: []string{"glm-5.3", "deepseek-v4"},
			},
			{
				ID: "p2", Name: "P2",
				AnthropicBaseURL: "https://p2-ant/", OpenAIBaseURL: "https://p2-oai/",
				APIKey: "k2", Models: []string{"glm-5.3", "kimi-k3"},
			},
			{
				ID: "wb", Name: "WorkBuddy", Preset: workbuddy.PresetID,
				Models: []string{"hy4free", "glm-5.3"},
			},
		},
		ToolSync: config.ToolSyncConfig{
			ToolPlatforms: map[string]string{"claude": "p1"},
		},
	}
}

// TestResolvePlatform 覆盖「显式模型前缀 → 会话模型反查 → 每工具平台 →
// 全局激活平台」的完整优先级矩阵。
func TestResolvePlatform(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.ModelConfig // 可选覆盖，零值表示用 testModelConfig()
		q    Query
		want string
	}{
		{
			name: "ModelID 前缀最高优先",
			q:    Query{ToolID: "claude", ModelID: "kshell:p1:glm-5.3"},
			want: "p1",
		},
		{
			name: "ModelID 前缀悬空兜底 ToolPlatforms",
			q:    Query{ToolID: "claude", ModelID: "kshell:gone:x"},
			want: "p1",
		},
		{
			name: "SessionModel 唯一命中",
			q:    Query{ToolID: "claude", SessionModel: "kimi-k3"},
			want: "p2",
		},
		{
			name: "SessionModel 重名时 ToolPlatforms 优先",
			q:    Query{ToolID: "claude", SessionModel: "glm-5.3"},
			want: "p1",
		},
		{
			name: "SessionModel 查不到回落 ToolPlatforms",
			q:    Query{ToolID: "claude", SessionModel: "no-such-model"},
			want: "p1",
		},
		{
			name: "ToolPlatforms 悬空回落 ActivePlatform",
			cfg: func() config.ModelConfig {
				c := testModelConfig()
				c.ToolSync.ToolPlatforms["claude"] = "gone"
				return c
			}(),
			q:    Query{ToolID: "claude"},
			want: "p2",
		},
		{
			name: "全空回落 ActivePlatform",
			q:    Query{},
			want: "p2",
		},
		{
			name: "宽松匹配大小写与点号连字符",
			q:    Query{ToolID: "claude", SessionModel: "Kimi-K3"},
			want: "p2",
		},
		{
			name: "宽松匹配点号归一后重名仍按 ToolPlatforms",
			q:    Query{ToolID: "claude", SessionModel: "glm-5-3"},
			want: "p1",
		},
		{
			name: "ToolID 命中对应映射键",
			cfg: func() config.ModelConfig {
				c := testModelConfig()
				c.ToolSync.ToolPlatforms["codex"] = "p2"
				return c
			}(),
			q:    Query{ToolID: "codex", SessionModel: "glm-5.3"},
			want: "p2",
		},
		{
			name: "未知 toolID 按 ActivePlatform 兜底不崩",
			q:    Query{ToolID: "gemini"},
			want: "p2",
		},
		{
			name: "ModelID 两侧空白先修剪再切前缀",
			q:    Query{ToolID: "claude", ModelID: " kshell:p2:kimi-k3 "},
			want: "p2",
		},
		{
			// 宽松别名平台排在配置前面：两遍匹配必须精确优先，而非首个别名命中。
			name: "精确匹配优先于顺序在前的宽松别名",
			cfg: config.ModelConfig{
				Enabled:          true,
				ActivePlatformID: "loose",
				Platforms: []config.ModelPlatform{
					{ID: "loose", Models: []string{"glm-5-3"}},
					{ID: "exact", Models: []string{"glm-5.3"}},
				},
				ToolSync: config.ToolSyncConfig{
					ToolPlatforms: map[string]string{"claude": "gone"},
				},
			},
			q:    Query{ToolID: "claude", SessionModel: "glm-5.3"},
			want: "exact",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			if len(cfg.Platforms) == 0 {
				cfg = testModelConfig()
			}
			got, ok := ResolvePlatform(cfg, tc.q)
			if !ok || got.ID != tc.want {
				t.Fatalf("ResolvePlatform() = %+v ok=%v, want %q", got, ok, tc.want)
			}
		})
	}
}

// fakeProxier 是 Proxier 的测试替身，记录 Ensure 调用次数并可注入错误。
type fakeProxier struct {
	base, token string
	err         error
	calls       int
}

func (f *fakeProxier) Ensure() (string, string, error) {
	f.calls++
	return f.base, f.token, f.err
}

// TestOptions 覆盖 launch.ModelOptions 的构造：普通平台端点/密钥、模型名优先级、
// 未启用短路，以及 workbuddy 平台改由本地代理提供端点。
func TestOptions(t *testing.T) {
	t.Run("前缀模型取裸名与目标平台端点", func(t *testing.T) {
		mo := Options(testModelConfig(), Query{ToolID: "claude", ModelID: "kshell:p1:glm-5.3"}, nil)
		if mo.Resolver == nil {
			t.Fatal("Resolver 不应为空")
		}
		mc, ok := mo.Resolver("claude")
		if !ok {
			t.Fatal("应命中平台")
		}
		want := (struct{ openAI, anthropic, key, model string }{
			"https://p1-oai/", "https://p1-ant/", "k1", "glm-5.3",
		})
		got := struct{ openAI, anthropic, key, model string }{
			mc.OpenAIBaseURL, mc.AnthropicBaseURL, mc.APIKey, mc.Model,
		}
		if got != want {
			t.Fatalf("resolver = %+v, want %+v", got, want)
		}
	})

	t.Run("无 ModelID 时用 SessionModel 作模型名", func(t *testing.T) {
		mo := Options(testModelConfig(), Query{ToolID: "claude", SessionModel: "kimi-k3"}, nil)
		mc, ok := mo.Resolver("claude")
		if !ok || mc.Model != "kimi-k3" || mc.OpenAIBaseURL != "https://p2-oai/" {
			t.Fatalf("resolver = %+v ok=%v", mc, ok)
		}
	})

	t.Run("模型名为空时不注入模型", func(t *testing.T) {
		mo := Options(testModelConfig(), Query{ToolID: "claude"}, nil)
		mc, ok := mo.Resolver("claude")
		if !ok || mc.Model != "" {
			t.Fatalf("resolver = %+v ok=%v, want 空模型", mc, ok)
		}
	})

	t.Run("未启用返回空 ModelOptions", func(t *testing.T) {
		cfg := testModelConfig()
		cfg.Enabled = false
		if mo := Options(cfg, Query{ToolID: "claude"}, nil); mo.Resolver != nil {
			t.Fatal("未启用时 Resolver 应为 nil")
		}
	})

	t.Run("workbuddy 平台改由代理提供端点", func(t *testing.T) {
		px := &fakeProxier{base: "http://127.0.0.1:1", token: "tok"}
		mo := Options(testModelConfig(), Query{ToolID: "claude", ModelID: "kshell:wb:hy4free"}, px)
		mc, ok := mo.Resolver("claude")
		if !ok {
			t.Fatal("workbuddy 平台应命中")
		}
		if mc.AnthropicBaseURL != "http://127.0.0.1:1" || mc.OpenAIBaseURL != "http://127.0.0.1:1" || mc.APIKey != "tok" || mc.Model != "hy4free" {
			t.Fatalf("resolver = %+v", mc)
		}
		if px.calls != 1 {
			t.Fatalf("Ensure 调用次数 = %d, want 1", px.calls)
		}
	})

	t.Run("workbuddy 平台无代理返回 false", func(t *testing.T) {
		mo := Options(testModelConfig(), Query{ToolID: "claude", ModelID: "kshell:wb:hy4free"}, nil)
		if _, ok := mo.Resolver("claude"); ok {
			t.Fatal("px 为 nil 时不应命中")
		}
	})

	t.Run("Query.ToolID 为空时用 Resolver 实参兜底每工具平台", func(t *testing.T) {
		mo := Options(testModelConfig(), Query{SessionModel: "no-such-model"}, nil)
		mc, ok := mo.Resolver("claude")
		if !ok {
			t.Fatal("应命 claude 的每工具平台 p1")
		}
		// p1 的 OpenAI 端点与全局激活平台 p2 不同，可判别是否用了实参 toolID。
		// 会话模型未命中任何平台：平台仍回落 p1，但模型名不得下发。
		if mc.OpenAIBaseURL != "https://p1-oai/" || mc.Model != "" {
			t.Fatalf("resolver = %+v, want p1 端点且空模型", mc)
		}
	})

	t.Run("claude 的 default 伪模型不注入模型名", func(t *testing.T) {
		// Claude ACP 自报的 "default" 表示「让 agent 自选」，不是具体模型名。
		// 作为 ANTHROPIC_MODEL 下发时 CLI 不做别名解析，网关会收到字面
		// "default" 并 400（OpenCode Go 实测）。必须按「未指定」处理。
		mo := Options(testModelConfig(), Query{ToolID: "claude", ModelID: "default"}, nil)
		mc, ok := mo.Resolver("claude")
		if !ok {
			t.Fatal("平台仍应命中")
		}
		if mc.Model != "" {
			t.Fatalf("claude default 不应注入模型名，got %q", mc.Model)
		}
		if mc.AnthropicBaseURL != "https://p1-ant/" {
			t.Fatalf("端点应仍按每工具平台解析，got %q", mc.AnthropicBaseURL)
		}
	})

	t.Run("非 claude 工具的 default 保持原样", func(t *testing.T) {
		// 同一字符串在其它工具里可能是真实模型名，不得被 claude 专用规则误伤。
		mo := Options(testModelConfig(), Query{ToolID: "opencode", ModelID: "default"}, nil)
		mc, ok := mo.Resolver("opencode")
		if !ok || mc.Model != "default" {
			t.Fatalf("resolver = %+v ok=%v, want 模型 default", mc, ok)
		}
	})

	t.Run("代理 Ensure 出错返回 false", func(t *testing.T) {
		px := &fakeProxier{err: errors.New("boom")}
		mo := Options(testModelConfig(), Query{ToolID: "claude", ModelID: "kshell:wb:hy4free"}, px)
		if _, ok := mo.Resolver("claude"); ok {
			t.Fatal("Ensure 出错时不应命中")
		}
	})
}

// TestSplitKshellModelID 锁定前缀解析边界：畸形 ID 一律 ok=false，模型名可含冒号。
func TestSplitKshellModelID(t *testing.T) {
	cases := []struct {
		id                  string
		wantPlat, wantModel string
		wantOK              bool
	}{
		{"kshell:p1:glm-5.3", "p1", "glm-5.3", true},
		{"kshell:", "", "", false},
		{"kshell::m", "", "", false},
		{"kshell:p:", "", "", false},
		{"glm-5.3", "", "", false},
		{"kshell:p:a:b", "p", "a:b", true},
	}
	for _, tc := range cases {
		plat, model, ok := SplitKshellModelID(tc.id)
		if plat != tc.wantPlat || model != tc.wantModel || ok != tc.wantOK {
			t.Errorf("SplitKshellModelID(%q) = (%q,%q,%v), want (%q,%q,%v)",
				tc.id, plat, model, ok, tc.wantPlat, tc.wantModel, tc.wantOK)
		}
	}
}

// TestModelIDLooseEqual 锁定导出函数的原语义：只做 '.'/'-' 归一，大小写敏感
// （大小写宽松是会话模型反查内部专用的 looseModelEqual）。
func TestModelIDLooseEqual(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"glm-5.3-flash", "glm-5.3-flash", true},
		{"glm-5.3-flash", "glm-5-3-flash", true},
		{"glm-5-3-flash", "glm-5.3-flash", true},
		{"a.b.c", "a-b-c", true},
		{"m1", "m2", false},
		{"", "", true},
		{"glm-5.3-flash", "glm-5.3", false},
		{"GPT-4O", "gpt-4o", false},
	}
	for _, tc := range cases {
		if got := ModelIDLooseEqual(tc.a, tc.b); got != tc.want {
			t.Errorf("ModelIDLooseEqual(%q,%q)=%v want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
