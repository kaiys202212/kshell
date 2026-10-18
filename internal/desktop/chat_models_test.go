package desktop

import (
	"encoding/json"
	"testing"

	"github.com/yangk/kshell/internal/acp"
	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/config"
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
		Enabled:          true,
		AnthropicBaseURL: "https://h/",
		APIKey:           "k",
		ActiveModel:      "glm-5.3-flash",
	}
	env.app.mu.Unlock()

	got, err := env.app.OpenWorkspaceACP(`D:\w`, "claude")
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
				Settings string `json:"settings"`
			} `json:"options"`
		} `json:"claudeCode"`
	}
	if err := json.Unmarshal([]byte(backend.live.gotMeta), &meta); err != nil {
		t.Fatalf("载荷非法 JSON: %v (%q)", err, backend.live.gotMeta)
	}
	var envWrap map[string]any
	if err := json.Unmarshal([]byte(meta.ClaudeCode.Options.Settings), &envWrap); err != nil {
		t.Fatalf("settings 值应为内联 JSON 字符串: %v (%q)", err, meta.ClaudeCode.Options.Settings)
	}
	if envWrap["env"] == nil {
		t.Fatalf("settings 缺 env 块: %q", meta.ClaudeCode.Options.Settings)
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
		Models:      []string{"glm-5.3-flash", "deepseek-v4.1-flash", "kimi-k3"},
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

// agent 无模型视图但 kshell 有配置时，仍应返回 kshell 列表（Current=ActiveModel）。
func TestListChatModelsAgentEmptyStillListsConfig(t *testing.T) {
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, fakeChatBackend{})
	env.app.mu.Lock()
	env.app.opts.Config.SessionMode = config.SessionModeACP
	env.app.opts.Config.Model = config.ModelConfig{
		Enabled:     true,
		ActiveModel: "glm-5.3-flash",
		Models:      []string{"glm-5.3-flash"},
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
	if v.Current != "glm-5.3-flash" {
		t.Fatalf("Current 应为 ActiveModel: %+v", v)
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
			Models:      []string{"m1", "cfg1"},
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
		Models:      []string{"glm-5.3-flash"},
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
