package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/acp"
	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

// recChatConn 记录 SetSettingsMeta 并上报指定模型视图。
type recChatConn struct {
	fakeChatConn
	gotMeta string
}

func (c *recChatConn) SetSettingsMeta(raw string) { c.gotMeta = raw }

// recChatBackend 产出 recChatConn，并记录传入的 spec。
type recChatBackend struct {
	conn recChatConn  // Start 后的快照（仅字段初值参考）
	live *recChatConn // manager 实际持有的连接指针
	spec chat.Spec
}

func (b *recChatBackend) Start(spec chat.Spec, h acp.Handler) (chat.Conn, error) {
	b.spec = spec
	c := &recChatConn{fakeChatConn: fakeChatConn{waitCh: make(chan struct{})}}
	// 记录指针：Start 返回的指针即 manager 持有的连接，SetSettingsMeta 落在它上面。
	b.live = c
	return c, nil
}

// openChat 必须把 launch 层组装的 ACPSettingsJSON 直通 chat.Spec。
func TestOpenWorkspaceACPCarriesSettingsPayload(t *testing.T) {
	backend := &recChatBackend{}
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, backend)
	env.app.mu.Lock()
	env.app.opts.Config.SessionMode = config.SessionModeACP
	env.app.opts.Config.Model = config.ModelConfig{
		Enabled:     true,
		ActiveModel: "glm-5.3-flash",
		Platforms: []config.ModelPlatform{
			{ID: "p1", Name: "GLM", AnthropicBaseURL: "https://h/", APIKey: "k"},
		},
	}
	env.app.mu.Unlock()

	got, err := env.app.OpenWorkspaceACP(`D:\w`, "claude", "")
	if err != nil {
		t.Fatalf("OpenWorkspaceACP: %v", err)
	}
	if got.Kind != "chat" {
		t.Fatalf("应走聊天: %+v", got)
	}
	if backend.live.gotMeta == "" {
		t.Fatal("连接未收到 SetSettingsMeta")
	}
	if backend.spec.ACPSettingsJSON != backend.live.gotMeta {
		t.Fatalf("spec 与 conn 载荷不一致: %q vs %q", backend.spec.ACPSettingsJSON, backend.live.gotMeta)
	}
	var meta struct {
		ClaudeCode struct {
			Options struct {
				Settings map[string]any `json:"settings"`
			} `json:"options"`
		} `json:"claudeCode"`
	}
	if err := json.Unmarshal([]byte(backend.live.gotMeta), &meta); err != nil {
		t.Fatalf("载荷非法 JSON: %v (%q)", err, backend.live.gotMeta)
	}
	envWrap := meta.ClaudeCode.Options.Settings
	if envWrap["env"] == nil {
		t.Fatalf("settings 缺 env 块: %q", backend.live.gotMeta)
	}
	// 无显式 modelID：不得注入 ActiveModel（no-default-model）
	envMap, _ := envWrap["env"].(map[string]any)
	if _, ok := envMap["ANTHROPIC_MODEL"]; ok {
		t.Fatalf("无 modelID 时不应注入 ANTHROPIC_MODEL: %v", envMap)
	}
}

// 显式 modelID 的新建：该次 Claude settings 必须带 ANTHROPIC_MODEL（及 DEFAULT 别名），
// 与「设置页 ActiveModel 全局注入」无关——easynew/弹窗选定的模型要在 session/new 就生效。
func TestOpenWorkspaceACPInjectsCreateModel(t *testing.T) {
	backend := &recChatBackend{}
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, backend)
	env.app.mu.Lock()
	env.app.opts.Config.SessionMode = config.SessionModeACP
	env.app.opts.Config.Model = config.ModelConfig{
		Enabled: true,
		Platforms: []config.ModelPlatform{
			{ID: "p1", Name: "P", AnthropicBaseURL: "https://h/", APIKey: "k", Models: []string{"deepseek-v4.1-flash"}},
		},
	}
	env.app.mu.Unlock()

	if _, err := env.app.OpenWorkspaceACP(`D:\w`, "claude", "deepseek-v4.1-flash"); err != nil {
		t.Fatalf("OpenWorkspaceACP: %v", err)
	}
	var meta struct {
		ClaudeCode struct {
			Options struct {
				Settings map[string]any `json:"settings"`
			} `json:"options"`
		} `json:"claudeCode"`
	}
	if err := json.Unmarshal([]byte(backend.live.gotMeta), &meta); err != nil {
		t.Fatalf("载荷: %v", err)
	}
	envMap, _ := meta.ClaudeCode.Options.Settings["env"].(map[string]any)
	if envMap["ANTHROPIC_MODEL"] != "deepseek-v4.1-flash" {
		t.Fatalf("应注入创建模型: %v", envMap)
	}
}

// kshell 前缀 modelID 注入时剥成裸名。
func TestOpenWorkspaceACPInjectsBareModelFromPrefix(t *testing.T) {
	backend := &recChatBackend{}
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, backend)
	env.app.mu.Lock()
	env.app.opts.Config.SessionMode = config.SessionModeACP
	env.app.opts.Config.Model = config.ModelConfig{
		Enabled: true,
		Platforms: []config.ModelPlatform{
			{ID: "p1", Name: "P", AnthropicBaseURL: "https://h/", APIKey: "k"},
			{ID: "p2", Name: "Q", AnthropicBaseURL: "https://q/", APIKey: "k2"},
		},
	}
	env.app.mu.Unlock()

	if _, err := env.app.OpenWorkspaceACP(`D:\w`, "claude", "kshell:p2:m-x"); err != nil {
		t.Fatalf("OpenWorkspaceACP: %v", err)
	}
	var meta struct {
		ClaudeCode struct {
			Options struct {
				Settings map[string]any `json:"settings"`
			} `json:"options"`
		} `json:"claudeCode"`
	}
	if err := json.Unmarshal([]byte(backend.live.gotMeta), &meta); err != nil {
		t.Fatalf("载荷: %v", err)
	}
	envMap, _ := meta.ClaudeCode.Options.Settings["env"].(map[string]any)
	if envMap["ANTHROPIC_MODEL"] != "m-x" {
		t.Fatalf("前缀应剥成裸名: %v", envMap)
	}
}

// ListChatModels 必须合并 kshell 配置模型：agent 上报缺的追加，已存在的不重复。
func TestListChatModelsMergesConfigModels(t *testing.T) {
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, fakeChatBackend{})
	env.app.mu.Lock()
	env.app.opts.Config.SessionMode = config.SessionModeACP
	env.app.opts.Config.Model = config.ModelConfig{
		Enabled:     true,
		ActiveModel: "glm-5.3-flash",
		Platforms:   []config.ModelPlatform{{ID: "p1", Name: "P", Models: []string{"glm-5.3-flash", "deepseek-v4.1-flash", "kimi-k3"}}},
		ToolSync:    config.ToolSyncConfig{Enabled: map[string]bool{"claude": true}},
	}
	env.app.mu.Unlock()

	opened, err := env.app.OpenSessionACP("s1")
	if err != nil {
		t.Fatalf("OpenSessionACP: %v", err)
	}
	if opened.Kind != "chat" || opened.Chat == nil {
		t.Fatalf("应走聊天: %+v", opened)
	}
	v, err := env.app.ListChatModels(opened.Chat.ID)
	if err != nil {
		t.Fatalf("ListChatModels: %v", err)
	}
	ids := map[string]bool{}
	for _, mo := range v.Models {
		ids[mo.ID] = true
	}
	for _, want := range []string{"glm-5.3-flash", "deepseek-v4.1-flash", "kimi-k3"} {
		if !ids[want] {
			t.Fatalf("合并后缺配置模型 %s: %+v", want, v)
		}
	}
}

// agent 无模型视图但 kshell 有配置时，仍应返回 kshell 列表；
// Current 保持空（当前模型以 agent 上报为准，不再回落 ActiveModel）。
func TestListChatModelsAgentEmptyStillListsConfig(t *testing.T) {
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, fakeChatBackend{})
	env.app.mu.Lock()
	env.app.opts.Config.SessionMode = config.SessionModeACP
	env.app.opts.Config.Model = config.ModelConfig{
		Enabled:     true,
		ActiveModel: "glm-5.3-flash",
		Platforms:   []config.ModelPlatform{{ID: "p1", Name: "P", Models: []string{"glm-5.3-flash"}}},
		ToolSync:    config.ToolSyncConfig{Enabled: map[string]bool{"claude": true}},
	}
	env.app.mu.Unlock()

	opened, err := env.app.OpenSessionACP("s1")
	if err != nil {
		t.Fatalf("OpenSessionACP: %v", err)
	}
	v, err := env.app.ListChatModels(opened.Chat.ID)
	if err != nil {
		t.Fatalf("ListChatModels: %v", err)
	}
	if len(v.Models) != 1 || v.Models[0].ID != "glm-5.3-flash" {
		t.Fatalf("应回 kshell 配置模型: %+v", v)
	}
	if v.Current != "" {
		t.Fatalf("agent 未上报时 Current 应保持空: %+v", v)
	}
}

// modelsView 合并视图与非 nil 空切片行为（ListChatModels 与 chat:models 推送共用同一函数）。
func TestModelsViewMergesAndNeverNil(t *testing.T) {
	t.Run("合并配置模型", func(t *testing.T) {
		conn := &fakeChatConn{start: acp.SessionStart{SessionID: "s1",
			Models: &acp.SessionModelState{
				AvailableModels: []acp.ModelInfo{{ModelID: "m1", Name: "One"}}, CurrentModelID: "m1",
			}}, waitCh: make(chan struct{})}
		env := newChatTestApp(t, &providers.ACPDetection{
			Available: true, Source: "path", BinPath: "claude-agent-acp",
		}, &fakeChatCaptureBackend{conn: conn})
		env.app.mu.Lock()
		env.app.opts.Config.SessionMode = config.SessionModeACP
		env.app.opts.Config.Model = config.ModelConfig{
			Enabled:     true,
			ActiveModel: "cfg1",
			Platforms:   []config.ModelPlatform{{ID: "p1", Name: "P", Models: []string{"m1", "cfg1"}}},
			ToolSync:    config.ToolSyncConfig{Enabled: map[string]bool{"claude": true}},
		}
		env.app.mu.Unlock()

		opened, err := env.app.OpenSessionACP("s1")
		if err != nil {
			t.Fatalf("OpenSessionACP: %v", err)
		}
		v, err := env.app.modelsView(opened.Chat.ID)
		if err != nil {
			t.Fatalf("modelsView: %v", err)
		}
		if v.Current != "m1" {
			t.Fatalf("Current 应为 agent 上报的 m1: %+v", v)
		}
		ids := map[string]bool{}
		for _, mo := range v.Models {
			ids[mo.ID] = true
		}
		if !ids["m1"] || !ids["cfg1"] {
			t.Fatalf("合并后应含 m1 与 cfg1: %+v", v)
		}
	})
	t.Run("无模型时非nil空切片", func(t *testing.T) {
		env := newChatTestApp(t, &providers.ACPDetection{
			Available: true, Source: "path", BinPath: "claude-agent-acp",
		}, fakeChatBackend{})
		env.app.mu.Lock()
		env.app.opts.Config.SessionMode = config.SessionModeACP
		env.app.mu.Unlock()

		opened, err := env.app.OpenSessionACP("s1")
		if err != nil {
			t.Fatalf("OpenSessionACP: %v", err)
		}
		v, err := env.app.modelsView(opened.Chat.ID)
		if err != nil {
			t.Fatalf("modelsView: %v", err)
		}
		if v.Models == nil {
			t.Fatal("Models 为 nil，JSON 序列化会是 null")
		}
		if len(v.Models) != 0 {
			t.Fatalf("无模型时应为空切片: %+v", v.Models)
		}
	})
}

// Open 完成后必须向前端推一次 chat:models：握手期前端拉 ListChatModels
// 可能拿空（starting 状态竞态），manager 的 SetOnModels 回调在此补偿。
// 生产接线在 newChatManager（RealBackend 不可桩），这里按方案 (a) 分层：
// 用 newChatManagerWith 装配后手动接同一套 SetOnModels → modelsView → emit 逻辑。
func TestOpenEmitsChatModels(t *testing.T) {
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, &fakeChatCaptureBackend{conn: &fakeChatConn{start: acp.SessionStart{SessionID: "s1",
		Models: &acp.SessionModelState{
			AvailableModels: []acp.ModelInfo{{ModelID: "m1", Name: "One"}}, CurrentModelID: "m1",
		}}, waitCh: make(chan struct{})}})
	env.app.mu.Lock()
	env.app.opts.Config.SessionMode = config.SessionModeACP
	env.app.mu.Unlock()
	// 与 newChatManager 生产接线同构：modelsView 取视图，emitChatOut 语义（测试 emit 不翻译）
	env.app.chats().SetOnModels(func(id string) {
		v, err := env.app.modelsView(id)
		if err != nil {
			return
		}
		env.events.emit("chat:models", map[string]any{"id": id, "models": v})
	})

	opened, err := env.app.OpenSessionACP("s1")
	if err != nil {
		t.Fatalf("OpenSessionACP: %v", err)
	}
	ev := env.events.waitEvent(t, "chat:models")
	if ev.payload["id"] != opened.Chat.ID {
		t.Fatalf("payload id = %v, want %v", ev.payload["id"], opened.Chat.ID)
	}
	v, ok := ev.payload["models"].(chat.ModelsView)
	if !ok {
		t.Fatalf("payload models 类型不符: %T", ev.payload["models"])
	}
	if v.Models == nil {
		t.Fatal("推送的 Models 为 nil")
	}
	found := false
	for _, mo := range v.Models {
		if mo.ID == "m1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("推送视图应含 m1: %+v", v)
	}
}

// 模型注入未启用时不得把 kshell 配置模型列进下拉（选中也无法注入，假选项）。
func TestListChatModelsDisabledNoConfigModels(t *testing.T) {
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, fakeChatBackend{})
	env.app.mu.Lock()
	env.app.opts.Config.SessionMode = config.SessionModeACP
	env.app.opts.Config.Model = config.ModelConfig{
		Enabled:     false,
		ActiveModel: "glm-5.3-flash",
		Platforms:   []config.ModelPlatform{{ID: "p1", Name: "P", Models: []string{"glm-5.3-flash"}}},
	}
	env.app.mu.Unlock()

	opened, err := env.app.OpenSessionACP("s1")
	if err != nil {
		t.Fatalf("OpenSessionACP: %v", err)
	}
	v, err := env.app.ListChatModels(opened.Chat.ID)
	if err != nil {
		t.Fatalf("ListChatModels: %v", err)
	}
	for _, mo := range v.Models {
		if mo.ID == "glm-5.3-flash" {
			t.Fatalf("未启用时不应列出配置模型: %+v", v)
		}
	}
}

// 该工具未启用按工具定制同步（ToolSync.Enabled 无该键）时，ACP 下拉不得再并
// 配置平台模型：注入通道已被 agentsync 关掉（只清不建），列出来是选了也走不通
// 的假选项——用户实证 codebuddy 关同步后下拉仍在。
func TestListChatModelsSyncOffSkipsConfigModels(t *testing.T) {
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, fakeChatBackend{})
	env.app.mu.Lock()
	env.app.opts.Config.SessionMode = config.SessionModeACP
	env.app.opts.Config.Model = config.ModelConfig{
		Enabled:     true,
		ActiveModel: "glm-5.3-flash",
		Platforms:   []config.ModelPlatform{{ID: "p1", Name: "GLM", Models: []string{"glm-5.3-flash", "glm-4.7"}}},
		ToolSync: config.ToolSyncConfig{
			Enabled:  map[string]bool{"opencode": true},
			OpenCode: map[string][]string{"p1": {"glm-5.3-flash", "glm-4.7"}},
		},
	}
	env.app.mu.Unlock()

	opened, err := env.app.OpenSessionACP("s1")
	if err != nil {
		t.Fatalf("OpenSessionACP: %v", err)
	}
	v, err := env.app.ListChatModels(opened.Chat.ID)
	if err != nil {
		t.Fatalf("ListChatModels: %v", err)
	}
	for _, mo := range v.Models {
		if mo.ID == "glm-5.3-flash" || mo.ID == "glm-4.7" || strings.HasPrefix(mo.ID, "kshell:") {
			t.Fatalf("未启用同步的工具不应列出配置模型: %+v", v.Models)
		}
	}
	// 已启用的工具不受影响：同一配置下 mergeConfigModels 照常并勾选模型。
	v2 := env.app.mergeConfigModels(chat.ModelsView{Models: []chat.ModelView{}}, "opencode")
	found := false
	for _, mo := range v2.Models {
		if mo.ID == "glm-5.3-flash" {
			found = true
		}
	}
	if !found {
		t.Fatalf("已启用的工具应照常合并平台模型: %+v", v2.Models)
	}
}

// openChatEnv 构造一个已打开的聊天会话环境，返回环境与会话 ID。
func openChatEnv(t *testing.T, conn *fakeChatConn) (*chatTestEnv, string) {
	t.Helper()
	var backend chat.Backend = fakeChatBackend{}
	if conn != nil {
		backend = &fakeChatCaptureBackend{conn: conn}
	}
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, backend)
	env.app.mu.Lock()
	env.app.opts.Config.SessionMode = config.SessionModeACP
	env.app.mu.Unlock()
	opened, err := env.app.OpenSessionACP("s1")
	if err != nil {
		t.Fatalf("OpenSessionACP: %v", err)
	}
	return env, opened.Chat.ID
}

// Cursor ACP 不支持 kshell 平台模型注入：ListChatModels 不得合并配置模型，
// 只保留 agent 自报列表（如 Auto / default[]）。
func TestCursorSkipsMergeConfigModels(t *testing.T) {
	conn := &fakeChatConn{start: acp.SessionStart{SessionID: "cs1",
		Models: &acp.SessionModelState{
			AvailableModels: []acp.ModelInfo{{ModelID: "default[]", Name: "Auto"}},
			CurrentModelID:  "default[]",
		}}, waitCh: make(chan struct{})}
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "cursor-agent",
	}, &fakeChatCaptureBackend{conn: conn})
	env.app.mu.Lock()
	env.app.opts.Config.SessionMode = config.SessionModeACP
	env.app.opts.Providers = []providers.Provider{providers.Cursor{}}
	env.app.tools = []discovery.Tool{{
		ID: "cursor", Name: "Cursor", BinPath: "cursor-agent", Installed: true,
		ACP: &providers.ACPDetection{Available: true, Source: "path", BinPath: "cursor-agent"},
	}}
	env.app.opts.Config.Model = config.ModelConfig{
		Enabled:     true,
		ActiveModel: "cfg1",
		Platforms:   []config.ModelPlatform{{ID: "p1", Name: "P", Models: []string{"cfg1", "cfg2"}}},
	}
	env.app.mu.Unlock()

	opened, err := env.app.OpenWorkspaceACP(`D:\w`, "cursor", "")
	if err != nil {
		t.Fatalf("OpenWorkspaceACP: %v", err)
	}
	v, err := env.app.ListChatModels(opened.Chat.ID)
	if err != nil {
		t.Fatalf("ListChatModels: %v", err)
	}
	if len(v.Models) != 1 || v.Models[0].ID != "default[]" || v.Models[0].Name != "Auto" {
		t.Fatalf("cursor 应只保留 agent 自报模型: %+v", v.Models)
	}
	for _, mo := range v.Models {
		if mo.ID == "cfg1" || mo.ID == "cfg2" || strings.HasPrefix(mo.ID, "kshell:") {
			t.Fatalf("cursor 不得合并平台模型: %+v", v.Models)
		}
	}
}

// 单平台：ID 仍裸名（兼容旧 modelId）；Name 带平台展示前缀（仅展示，不进 ID）。
func TestMergeConfigModelsSinglePlatformBareNames(t *testing.T) {
	env, id := openChatEnv(t, nil)
	env.app.mu.Lock()
	env.app.opts.Config.Model = config.ModelConfig{
		Enabled:     true,
		ActiveModel: "m2",
		Platforms:   []config.ModelPlatform{{ID: "p1", Name: "P", Models: []string{"m1", " m2 "}}},
		ToolSync:    config.ToolSyncConfig{Enabled: map[string]bool{"claude": true}},
	}
	env.app.mu.Unlock()

	v, err := env.app.ListChatModels(id)
	if err != nil {
		t.Fatalf("ListChatModels: %v", err)
	}
	if v.Current != "" {
		t.Fatalf("agent 未上报时 Current 应保持空: %+v", v)
	}
	if len(v.Models) != 2 || v.Models[0].ID != "m1" || v.Models[0].Name != "P: m1" ||
		v.Models[1].ID != "m2" || v.Models[1].Name != "P: m2" {
		t.Fatalf("单平台 ID 裸名、Name 带平台前缀: %+v", v.Models)
	}
}

// claude 静态注入只指向 ClaudePlatform 端点：下拉只并该平台全部模型（裸名 ID、
// Name 带平台前缀）；Current 不回落（agent 上报为准），ActiveModel 不影响 Current。
func TestMergeConfigModelsMultiPlatformPrefixed(t *testing.T) {
	platforms := []config.ModelPlatform{
		{ID: "p1", Name: "Alpha", Models: []string{"a1", "a2"}},
		{ID: "p2", Name: "Beta", Models: []string{"b1", "b2"}},
	}
	cases := []struct {
		name       string
		claudePlat string
		active     string
		wantIDs    []string
		wantNames  map[string]string
	}{
		{
			name: "Claude平台排最前", claudePlat: "p2", active: "b1",
			wantIDs:   []string{"b1", "b2"},
			wantNames: map[string]string{"b1": "Beta: b1"},
		},
		{
			name: "ActiveModel不影响Current", claudePlat: "p2", active: "a1",
			wantIDs: []string{"b1", "b2"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, id := openChatEnv(t, nil)
			env.app.mu.Lock()
			env.app.opts.Config.Model = config.ModelConfig{
				Enabled:          true,
				ActivePlatformID: "p1",
				ActiveModel:      tc.active,
				Platforms:        platforms,
				ToolSync: config.ToolSyncConfig{
					Enabled:        map[string]bool{"claude": true},
					ClaudePlatform: tc.claudePlat,
				},
			}
			env.app.mu.Unlock()

			v, err := env.app.ListChatModels(id)
			if err != nil {
				t.Fatalf("ListChatModels: %v", err)
			}
			var gotIDs []string
			for _, mo := range v.Models {
				gotIDs = append(gotIDs, mo.ID)
			}
			if len(gotIDs) != len(tc.wantIDs) {
				t.Fatalf("条目数不符: got %v", gotIDs)
			}
			for i, want := range tc.wantIDs {
				if gotIDs[i] != want {
					t.Fatalf("顺序错误: got %v, want %v", gotIDs, tc.wantIDs)
				}
			}
			for _, mo := range v.Models {
				if want, ok := tc.wantNames[mo.ID]; ok && mo.Name != want {
					t.Fatalf("%s Name = %q, want %q", mo.ID, mo.Name, want)
				}
			}
			if v.Current != "" {
				t.Fatalf("agent 未上报时 Current 应保持空，got %q", v.Current)
			}
		})
	}
}

// opencode 多平台：按平台勾选集并入，ID 带 "kshell:<platID>:<model>" 前缀，
// 默认平台排最前、其余按列表顺序。
func TestMergeConfigModelsOpencodeMultiPlatform(t *testing.T) {
	env := newChatTestApp(t, nil, nil)
	env.app.mu.Lock()
	env.app.opts.Config.Model = config.ModelConfig{
		Enabled:          true,
		ActivePlatformID: "p2",
		Platforms: []config.ModelPlatform{
			{ID: "p1", Name: "Alpha", Models: []string{"a1", "a2"}},
			{ID: "p2", Name: "Beta", Models: []string{"b1", "b2"}},
		},
		ToolSync: config.ToolSyncConfig{
			Enabled:  map[string]bool{"opencode": true},
			OpenCode: map[string][]string{"p1": {"a1", "a2"}, "p2": {"b1", "b2"}},
		},
	}
	env.app.mu.Unlock()

	v := env.app.mergeConfigModels(chat.ModelsView{Models: []chat.ModelView{}}, "opencode")
	var gotIDs []string
	for _, mo := range v.Models {
		gotIDs = append(gotIDs, mo.ID)
	}
	want := []string{"kshell:p2:b1", "kshell:p2:b2", "kshell:p1:a1", "kshell:p1:a2"}
	if len(gotIDs) != len(want) {
		t.Fatalf("条目数不符: got %v", gotIDs)
	}
	for i := range want {
		if gotIDs[i] != want[i] {
			t.Fatalf("顺序错误: got %v, want %v", gotIDs, want)
		}
	}
}

// 多平台去重：agent 自报 ID 与前缀 ID 相同时只保留 agent 条目。
func TestMergeConfigModelsMultiPlatformDedupe(t *testing.T) {
	conn := &fakeChatConn{start: acp.SessionStart{SessionID: "s1",
		Models: &acp.SessionModelState{
			AvailableModels: []acp.ModelInfo{{ModelID: "kshell:p1:a1", Name: "Ghost"}},
			CurrentModelID:  "kshell:p1:a1",
		}}, waitCh: make(chan struct{})}
	env, _ := openChatEnv(t, conn)
	env.app.mu.Lock()
	env.app.opts.Config.Model = config.ModelConfig{
		Enabled:     true,
		ActiveModel: "b1",
		Platforms: []config.ModelPlatform{
			{ID: "p1", Name: "Alpha", Models: []string{"a1"}},
			{ID: "p2", Name: "Beta", Models: []string{"b1"}},
		},
		ToolSync: config.ToolSyncConfig{
			Enabled:  map[string]bool{"opencode": true},
			OpenCode: map[string][]string{"p1": {"a1"}, "p2": {"b1"}},
		},
	}
	env.app.mu.Unlock()

	v := env.app.mergeConfigModels(chat.ModelsView{Models: []chat.ModelView{
		{ID: "kshell:p1:a1", Name: "Ghost"},
	}}, "opencode")
	count := 0
	for _, mo := range v.Models {
		if mo.ID == "kshell:p1:a1" {
			count++
			if mo.Name != "Ghost" {
				t.Fatalf("应保留 agent 条目: %+v", mo)
			}
		}
	}
	if count != 1 {
		t.Fatalf("重复条目数 = %d: %+v", count, v)
	}
}

// setModelApp 构造带 Layout 的聊天 App，供 SetChatModel 断言配置落盘。
func setModelApp(t *testing.T, model config.ModelConfig) (*App, *fakeChatConn, config.Layout, string) {
	t.Helper()
	return setModelAppTool(t, model, "", acpSessionWithModels())
}

func setModelAppTool(t *testing.T, model config.ModelConfig, toolID string, start acp.SessionStart) (*App, *fakeChatConn, config.Layout, string) {
	t.Helper()
	conn := &fakeChatConn{start: start, waitCh: make(chan struct{})}
	dir := t.TempDir()
	layout := config.Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	m := newChatManagerWith(func(string, ...any) {}, &fakeChatCaptureBackend{conn: conn}, nil, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)
	info, err := m.Open("new:1", chat.Info{Kind: chat.KindNew, Workspace: "/w", ToolID: toolID}, chat.Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	cfg := config.Default()
	cfg.Model = model
	app := NewAppWith(Options{Chats: m, Config: cfg, Layout: layout})
	// 初态先落盘，模拟用户已保存过模型配置。
	if err := config.Save(layout, cfg); err != nil {
		t.Fatalf("Save 初态: %v", err)
	}
	return app, conn, layout, info.ID
}

// SetChatModel 成功后必须推 chat:models，否则 easynew 建后切模型前端仍显示 default。
func TestSetChatModelEmitsChatModels(t *testing.T) {
	conn := &fakeChatConn{
		start: acp.SessionStart{
			SessionID: "s1",
			Models: &acp.SessionModelState{
				AvailableModels: []acp.ModelInfo{
					{ModelID: "default", Name: "Default"},
					{ModelID: "deepseek-v4.1-flash", Name: "DS"},
				},
				CurrentModelID: "default",
			},
		},
		waitCh: make(chan struct{}),
	}
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, &fakeChatCaptureBackend{conn: conn})
	env.app.mu.Lock()
	env.app.opts.Config.SessionMode = config.SessionModeACP
	env.app.mu.Unlock()

	opened, err := env.app.OpenWorkspaceACP(`D:\w`, "claude", "")
	if err != nil {
		t.Fatalf("OpenWorkspaceACP: %v", err)
	}
	// newChatTestApp 不接线 SetOnModels（无握手 chat:models）；只断言 SetChatModel 推送
	if err := env.app.SetChatModel(opened.Chat.ID, "deepseek-v4.1-flash"); err != nil {
		t.Fatalf("SetChatModel: %v", err)
	}
	ev := env.events.waitEvent(t, "chat:models")
	if ev.payload["id"] != opened.Chat.ID {
		t.Fatalf("id = %v, want %v", ev.payload["id"], opened.Chat.ID)
	}
	v, ok := ev.payload["models"].(chat.ModelsView)
	if !ok {
		t.Fatalf("models 类型: %T", ev.payload["models"])
	}
	if v.Current != "deepseek-v4.1-flash" {
		t.Fatalf("推送 Current = %q, want deepseek-v4.1-flash", v.Current)
	}
}

func TestMergeConfigModelsUsesToolPlatform(t *testing.T) {
	app, _ := newModelApp(t)
	app.mu.Lock()
	app.opts.Config.Model = config.ModelConfig{
		Enabled: true,
		Platforms: []config.ModelPlatform{
			{ID: "p1", Name: "A", Models: []string{"m1"}},
			{ID: "p2", Name: "B", Models: []string{"m2"}},
		},
		ActivePlatformID: "p1",
		ToolSync: config.ToolSyncConfig{
			Enabled:           map[string]bool{"claude": true, "codebuddy": true},
			ClaudePlatform:    "p2",
			CodeBuddy:         []string{"m2"},
			CodeBuddyPlatform: "p2",
		},
	}
	app.mu.Unlock()

	claude := app.mergeConfigModels(chat.ModelsView{Models: []chat.ModelView{}}, "claude")
	if len(claude.Models) != 1 || claude.Models[0].ID != "m2" {
		t.Fatalf("claude models = %+v, want m2", claude.Models)
	}
	cb := app.mergeConfigModels(chat.ModelsView{Models: []chat.ModelView{}}, "codebuddy")
	if len(cb.Models) != 1 || cb.Models[0].ID != "m2" {
		t.Fatalf("codebuddy models = %+v, want m2", cb.Models)
	}
}

func TestSetChatModelPlatformPrefix(t *testing.T) {
	app, conn, layout, id := setModelApp(t, config.ModelConfig{
		Enabled:          true,
		ActivePlatformID: "p1",
		ActiveModel:      "a1",
		Platforms: []config.ModelPlatform{
			{ID: "p1", Name: "Alpha", Models: []string{"a1"}},
			{ID: "p2", Name: "Beta", Models: []string{"b1", "b2"}},
		},
	})

	// 1. 默认平台前缀：只切模型，config 不变。
	if err := app.SetChatModel(id, "kshell:p1:a1"); err != nil {
		t.Fatalf("SetChatModel: %v", err)
	}
	if len(conn.modelCalls) != 1 || conn.modelCalls[0] != "a1" {
		t.Fatalf("SetModel 应收到裸名: %v", conn.modelCalls)
	}
	loaded, _ := config.Load(layout)
	if loaded.Model.ActivePlatformID != "p1" || loaded.Model.ActiveModel != "a1" {
		t.Fatalf("默认平台切换不应改配置: %+v", loaded.Model)
	}

	// 2. 跨平台前缀：落盘 DefaultPlatform/ActiveModel，SetModel 收到裸名。
	if err := app.SetChatModel(id, "kshell:p2:b2"); err != nil {
		t.Fatalf("SetChatModel: %v", err)
	}
	if len(conn.modelCalls) != 2 || conn.modelCalls[1] != "b2" {
		t.Fatalf("跨平台 SetModel 应收到裸名: %v", conn.modelCalls)
	}
	loaded, _ = config.Load(layout)
	if loaded.Model.ActivePlatformID != "p2" || loaded.Model.ActiveModel != "b2" {
		t.Fatalf("跨平台切换应落盘: %+v", loaded.Model)
	}
	if got := app.snapshot().Config.Model.ActivePlatformID; got != "p2" {
		t.Fatalf("内存配置应同步更新: %q", got)
	}

	// 3. 前缀含未知 platID：透传不落盘。
	if err := app.SetChatModel(id, "kshell:px:m"); err != nil {
		t.Fatalf("SetChatModel: %v", err)
	}
	if conn.modelCalls[2] != "kshell:px:m" {
		t.Fatalf("未知平台应原样透传: %v", conn.modelCalls)
	}
	loaded, _ = config.Load(layout)
	if loaded.Model.ActivePlatformID != "p2" {
		t.Fatalf("未知平台不应误存: %+v", loaded.Model)
	}

	// 4. 非前缀 ID：透传。
	if err := app.SetChatModel(id, "plain-model"); err != nil {
		t.Fatalf("SetChatModel: %v", err)
	}
	if conn.modelCalls[3] != "plain-model" {
		t.Fatalf("非前缀应原样透传: %v", conn.modelCalls)
	}
	loaded, _ = config.Load(layout)
	if loaded.Model.ActivePlatformID != "p2" || loaded.Model.ActiveModel != "b2" {
		t.Fatalf("透传不应改配置: %+v", loaded.Model)
	}
}

// 跨平台前缀但 saveConfig 失败（无 Layout）：返回错误且不切模型。
func TestSetChatModelSaveFailsKeepsModel(t *testing.T) {
	conn := &fakeChatConn{start: acpSessionWithModels(), waitCh: make(chan struct{})}
	m := newChatManagerWith(func(string, ...any) {}, &fakeChatCaptureBackend{conn: conn}, nil, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)
	info, err := m.Open("new:1", chat.Info{Kind: chat.KindNew, Workspace: "/w"}, chat.Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	cfg := config.Default()
	cfg.Model = config.ModelConfig{
		Enabled: true,
		Platforms: []config.ModelPlatform{
			{ID: "p1", Name: "Alpha", Models: []string{"a1"}},
			{ID: "p2", Name: "Beta", Models: []string{"b1"}},
		},
	}
	app := NewAppWith(Options{Chats: m, Config: cfg}) // 无 Layout → saveConfig 失败

	if err := app.SetChatModel(info.ID, "kshell:p2:b1"); err == nil {
		t.Fatal("保存失败应返回错误")
	}
	if len(conn.modelCalls) != 0 {
		t.Fatalf("保存失败不应切模型: %v", conn.modelCalls)
	}
}

// 跨平台切 kshell 当前平台不应改变 Claude 的静态注入平台（各工具平台独立）。
func TestSetChatModelCrossPlatformKeepsClaudePlatform(t *testing.T) {
	home := t.TempDir()
	conn := &fakeChatConn{start: acpSessionWithModels(), waitCh: make(chan struct{})}
	dir := t.TempDir()
	layout := config.Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	m := newChatManagerWith(func(string, ...any) {}, &fakeChatCaptureBackend{conn: conn}, nil, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)
	info, err := m.Open("new:1", chat.Info{Kind: chat.KindNew, Workspace: "/w"}, chat.Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	cfg := config.Default()
	cfg.Model = config.ModelConfig{
		Enabled:          true,
		ActivePlatformID: "p1",
		ActiveModel:      "a1",
		Platforms: []config.ModelPlatform{
			{ID: "p1", Name: "Alpha", AnthropicBaseURL: "https://a.example/", APIKey: "ka", Models: []string{"a1"}},
			{ID: "p2", Name: "Beta", AnthropicBaseURL: "https://b.example/", APIKey: "kb", Models: []string{"b2"}},
		},
		ToolSync: config.ToolSyncConfig{
			Enabled:        map[string]bool{"claude": true},
			ClaudePlatform: "p1",
		},
	}
	if err := config.Save(layout, cfg); err != nil {
		t.Fatalf("Save 初态: %v", err)
	}
	app := NewAppWith(Options{Chats: m, Config: cfg, Layout: layout, Home: home})
	app.mu.Lock()
	app.tools = []discovery.Tool{{ID: "claude", Installed: true}}
	app.mu.Unlock()

	if err := app.SetChatModel(info.ID, "kshell:p2:b2"); err != nil {
		t.Fatalf("SetChatModel: %v", err)
	}

	// 同步经调度器防抖（400ms）：轮询等待落盘。
	path := filepath.Join(home, ".claude", "settings.json")
	deadline := time.Now().Add(3 * time.Second)
	var b []byte
	for {
		b, err = os.ReadFile(path)
		if err == nil && strings.Contains(string(b), "https://a.example/") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("应写入 settings.json（Claude 自有平台 p1）: err=%v body=%s", err, b)
		}
		time.Sleep(25 * time.Millisecond)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	env, _ := doc["env"].(map[string]any)
	if env["ANTHROPIC_BASE_URL"] != "https://a.example/" || env["ANTHROPIC_AUTH_TOKEN"] != "ka" {
		t.Fatalf("Claude env 应保持自有平台 p1，got %#v", env)
	}
}

// mergeConfigModels 对 agent 已上报的同名条目（kshell 注入的裸 mid，ACP 层
// 带 custom-local: 前缀，名 "openai: <m>"）不再追加配置侧副本，消除下拉重复。
func TestMergeConfigModelsDedupesByDisplayName(t *testing.T) {
	env := newChatTestApp(t, nil, nil)
	env.app.mu.Lock()
	env.app.opts.Config.Model = config.ModelConfig{
		Enabled:          true,
		ActivePlatformID: "p1",
		ActiveModel:      "glm-x",
		Platforms: []config.ModelPlatform{
			{ID: "p1", Name: "openai", Models: []string{"glm-x", "ds-x"}},
		},
		ToolSync: config.ToolSyncConfig{Enabled: map[string]bool{"claude": true}},
	}
	env.app.mu.Unlock()

	in := chat.ModelsView{Models: []chat.ModelView{
		{ID: "custom-local:glm-x", Name: "openai: glm-x"},
	}}
	got := env.app.mergeConfigModels(in, "claude")
	names := map[string]int{}
	for _, mo := range got.Models {
		names[mo.Name]++
	}
	if names["openai: glm-x"] != 1 {
		t.Fatalf("同名条目应去重为一份，got %#v", got.Models)
	}
	var hasDs bool
	for _, mo := range got.Models {
		if mo.Name == "openai: ds-x" {
			hasDs = true
		}
	}
	if !hasDs {
		t.Fatalf("不同名条目应照常追加，got %#v", got.Models)
	}
}

// openCode 配置模型切换：裸名须解析成 agent 原生 ID "kshell-<platID>/<model>"
// 再下发，否则 OpenCode 报 -32602 model not found。
func TestSetChatModelOpenCodeMapsBareToNative(t *testing.T) {
	app, conn, _, id := setModelAppTool(t, config.ModelConfig{
		Enabled:          true,
		ActivePlatformID: "p1",
		Platforms:        []config.ModelPlatform{{ID: "p1", Name: "P1", Models: []string{"glm-5.3-flash"}}},
	}, "opencode", acp.SessionStart{Models: &acp.SessionModelState{
		AvailableModels: []acp.ModelInfo{
			{ModelID: "kshell-p1/glm-5.3-flash", Name: "P1: glm-5.3-flash"},
			{ModelID: "zhip/glm-4.6", Name: "Zhip: glm-4.6"},
		},
		CurrentModelID: "zhip/glm-4.6",
	}})
	if err := app.SetChatModel(id, "glm-5.3-flash"); err != nil {
		t.Fatalf("SetChatModel: %v", err)
	}
	if got := conn.modelCalls[0]; got != "kshell-p1/glm-5.3-flash" {
		t.Fatalf("裸名应解析成原生 ID, got %q", got)
	}
}

// 点号/连字符差宽松尾段兜底：platID 构造未命中时按尾段唯一命中。
func TestSetChatModelOpenCodeLooseTail(t *testing.T) {
	app, conn, _, id := setModelAppTool(t, config.ModelConfig{
		Enabled:          true,
		ActivePlatformID: "p1",
		Platforms:        []config.ModelPlatform{{ID: "p1", Name: "P1", Models: []string{"glm-5.3-flash"}}},
	}, "opencode", acp.SessionStart{Models: &acp.SessionModelState{
		AvailableModels: []acp.ModelInfo{{ModelID: "kshell-p1/glm-5-3-flash", Name: "P1: glm"}},
	}})
	if err := app.SetChatModel(id, "glm-5.3-flash"); err != nil {
		t.Fatalf("SetChatModel: %v", err)
	}
	if got := conn.modelCalls[0]; got != "kshell-p1/glm-5-3-flash" {
		t.Fatalf("应按尾段宽松命中原生 ID, got %q", got)
	}
}

// opencode: 精确命中原生列表 → 透传;无任何匹配 → 裸名透传(保持报错可见)。
func TestSetChatModelOpenCodePassthrough(t *testing.T) {
	app, conn, _, id := setModelAppTool(t, config.ModelConfig{
		Enabled:          true,
		ActivePlatformID: "p1",
		Platforms:        []config.ModelPlatform{{ID: "p1", Name: "P1", Models: []string{"glm-5.3-flash"}}},
	}, "opencode", acp.SessionStart{Models: &acp.SessionModelState{
		AvailableModels: []acp.ModelInfo{
			{ModelID: "zhip/glm-4.6", Name: "Zhip: glm-4.6"},
		},
	}})
	if err := app.SetChatModel(id, "zhip/glm-4.6"); err != nil {
		t.Fatalf("SetChatModel: %v", err)
	}
	if got := conn.modelCalls[0]; got != "zhip/glm-4.6" {
		t.Fatalf("原生条目应原样下发, got %q", got)
	}
	if err := app.SetChatModel(id, "no-such-model"); err != nil {
		t.Fatalf("SetChatModel: %v", err)
	}
	if got := conn.modelCalls[1]; got != "no-such-model" {
		t.Fatalf("无匹配应透传裸名(报错可见), got %q", got)
	}
}

// 非 opencode 工具(claude 等)不受影响:裸名仍原样透传。
func TestSetChatModelNonOpenCodeUnaffected(t *testing.T) {
	app, conn, _, id := setModelAppTool(t, config.ModelConfig{
		Enabled:          true,
		ActivePlatformID: "p1",
		Platforms:        []config.ModelPlatform{{ID: "p1", Name: "P1", Models: []string{"a1"}}},
	}, "claude", acp.SessionStart{Models: &acp.SessionModelState{
		AvailableModels: []acp.ModelInfo{
			{ModelID: "kshell-p1/a1", Name: "P1: a1"},
		},
	}})
	if err := app.SetChatModel(id, "a1"); err != nil {
		t.Fatalf("SetChatModel: %v", err)
	}
	if got := conn.modelCalls[0]; got != "a1" {
		t.Fatalf("非 opencode 裸名应原样透传, got %q", got)
	}
}

// opencode 下拉去重:agent 自报已含 "kshell-<platID>/<model>" 原生条目时,
// 配置侧裸名副本不再追加(选原生条目即可);无原生条目时照常追加。
func TestMergeConfigModelsOpencodeNativeDedupe(t *testing.T) {
	env := newChatTestApp(t, nil, nil)
	env.app.mu.Lock()
	env.app.opts.Config.Model = config.ModelConfig{
		Enabled:          true,
		ActivePlatformID: "p1",
		Platforms:        []config.ModelPlatform{{ID: "p1", Name: "openai", Models: []string{"glm-x", "ds-x"}}},
		ToolSync: config.ToolSyncConfig{
			Enabled:  map[string]bool{"opencode": true, "claude": true},
			OpenCode: map[string][]string{"p1": {"glm-x", "ds-x"}},
		},
	}
	env.app.mu.Unlock()

	got := env.app.mergeConfigModels(chat.ModelsView{Models: []chat.ModelView{
		{ID: "kshell-p1/glm-x", Name: "openai: glm-x"},
	}}, "opencode")
	var hasGlmX, hasNative, hasDs int
	for _, mo := range got.Models {
		switch {
		case mo.Name == "openai: glm-x" && mo.ID != "kshell-p1/glm-x":
			hasGlmX++
		case mo.ID == "kshell-p1/glm-x":
			hasNative++
		case mo.Name == "openai: ds-x":
			hasDs++
		}
	}
	if hasNative != 1 || hasGlmX != 0 {
		t.Fatalf("原生条目应在且裸名副本应被去重, got %#v", got.Models)
	}
	if hasDs != 1 {
		t.Fatalf("无原生条目的配置条目应照常追加, got %#v", got.Models)
	}

	// 非 opencode(如 claude 裸 mid 上报):不启用原生尾段去重,行为不变。
	got2 := env.app.mergeConfigModels(chat.ModelsView{Models: []chat.ModelView{
		{ID: "custom-local:glm-x", Name: "other: glm-x"},
	}}, "claude")
	var bare int
	for _, mo := range got2.Models {
		if mo.ID == "glm-x" {
			bare++
		}
	}
	if bare != 1 {
		t.Fatalf("非 opencode 配置裸名副本应照常追加, got %#v", got2.Models)
	}
}

// codebuddy: 配置裸 mid 与其内置模型 id 撞名（glm-5.3-flash 等）时，配置侧
// 副本仍须追加——codebuddy 的自定义条目走 custom-local: 前缀与内置天然区分，
// 按 ID 判重只会把平台副本挤出下拉（撞名项选内置走的是自营通道，非用户平台）。
// 2026-11-01 实机：opencode1 组 7 条只活 3 条。
func TestMergeConfigModelsCodeBuddyKeepsBuiltinNameCollision(t *testing.T) {
	env := newChatTestApp(t, nil, nil)
	env.app.mu.Lock()
	env.app.opts.Config.Model = config.ModelConfig{
		Enabled:          true,
		ActivePlatformID: "p1",
		Platforms:        []config.ModelPlatform{{ID: "p1", Name: "opencode1", Models: []string{"glm-5.3-flash", "kimi-k3"}}},
		ToolSync: config.ToolSyncConfig{
			Enabled:   map[string]bool{"codebuddy": true},
			CodeBuddy: []string{"glm-5.3-flash", "kimi-k3"},
		},
	}
	env.app.mu.Unlock()

	in := chat.ModelsView{Models: []chat.ModelView{
		{ID: "glm-5.3-flash", Name: "GLM-5.3-Flash"},                        // 内置同名裸 mid
		{ID: "custom-local:qwen3.8-flash", Name: "opencode: qwen3.8-flash"}, // 旧名注入条目（models.json 冻结场景）
	}}
	got := env.app.mergeConfigModels(in, "codebuddy")
	var hit, bare int
	for _, mo := range got.Models {
		if mo.ID == "glm-5.3-flash" && mo.Name == "opencode1: glm-5.3-flash" {
			hit++
		}
		if mo.ID == "kimi-k3" {
			bare++
		}
	}
	if hit != 1 || bare != 1 {
		t.Fatalf("撞内置 id 的平台副本应照常追加, got %#v", got.Models)
	}

	// models.json 新鲜场景：agent 已自报同平台名条目，展示名判重仍生效，不重复。
	in2 := chat.ModelsView{Models: []chat.ModelView{
		{ID: "custom-local:glm-5.3-flash", Name: "opencode1: glm-5.3-flash"},
	}}
	got2 := env.app.mergeConfigModels(in2, "codebuddy")
	n := 0
	for _, mo := range got2.Models {
		if mo.Name == "opencode1: glm-5.3-flash" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("同名注入条目应去重为一份, got %#v", got2.Models)
	}
}
