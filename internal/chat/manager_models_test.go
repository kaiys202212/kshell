package chat

// 模型切换 / 技能命令 / 多块 Prompt（附件）的会话层行为测试。

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/acp"
)

func waitTurnDone(t *testing.T, c *collector) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		found := false
		for _, u := range c.updates {
			if u.Type == "turn_done" {
				found = true
			}
		}
		c.mu.Unlock()
		if found {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no turn_done; updates=%+v", func() []Update {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.updates
	}())
}

// available_commands_update 必须归一化为 commands 事件（名称/描述/输入提示）。
func TestNormalizeUpdateCommands(t *testing.T) {
	raw := json.RawMessage(`{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"create_plan","description":"写计划","input":{"hint":"主题"}}]}`)
	u, _, ok := normalizeUpdate(raw)
	if !ok || u.Type != "commands" {
		t.Fatalf("normalize = ok=%v u=%+v", ok, u)
	}
	if len(u.Commands) != 1 {
		t.Fatalf("commands = %+v", u.Commands)
	}
	if u.Commands[0].Name != "create_plan" || u.Commands[0].Description != "写计划" || u.Commands[0].InputHint != "主题" {
		t.Fatalf("command[0] = %+v", u.Commands[0])
	}
}

// 命令列表存入会话并经 onCommands 推送；不得泄漏进时间线历史。
func TestManagerCommandsStoredAndPushed(t *testing.T) {
	m, b, _ := newTestManager(t)
	cmdCh := make(chan []Command, 1)
	m.SetOnCommands(func(id string, cmds []Command) { cmdCh <- cmds })
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"plan","description":"P"}]}`)
	b.last.SessionUpdate("sess-new", raw)
	select {
	case cmds := <-cmdCh:
		if len(cmds) != 1 || cmds[0].Name != "plan" || cmds[0].Description != "P" {
			t.Fatalf("pushed = %+v", cmds)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("onCommands not called")
	}
	if got := m.Commands(info.ID); len(got) != 1 || got[0].Name != "plan" {
		t.Fatalf("Commands = %+v", got)
	}
	if h := m.History(info.ID); len(h) != 0 {
		t.Fatalf("commands leaked into history: %+v", h)
	}
}

// v1 models 形态：Models 返回可用列表与当前值；SetModel 成功后乐观更新当前值。
func TestManagerModelsFromState(t *testing.T) {
	m, b, _ := newTestManager(t)
	b.conn.newStart = acp.SessionStart{SessionID: "sess-new", Models: &acp.SessionModelState{
		AvailableModels: []acp.ModelInfo{{ModelID: "m1", Name: "One"}, {ModelID: "m2", Name: "Two", Description: "D"}},
		CurrentModelID:  "m1",
	}}
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	v, ok := m.Models(info.ID)
	if !ok || v.Current != "m1" || len(v.Models) != 2 {
		t.Fatalf("view = ok=%v %+v", ok, v)
	}
	if v.Models[1].ID != "m2" || v.Models[1].Name != "Two" || v.Models[1].Description != "D" {
		t.Fatalf("model[1] = %+v", v.Models[1])
	}
	if err := m.SetModel(info.ID, "m2"); err != nil {
		t.Fatalf("set model: %v", err)
	}
	if v, _ := m.Models(info.ID); v.Current != "m2" {
		t.Fatalf("current after set = %q", v.Current)
	}
}

// configOptions 形态：当前值来自 currentValue（JSON 字符串），列表来自 options。
func TestManagerModelsFromConfigOption(t *testing.T) {
	m, b, _ := newTestManager(t)
	b.conn.newStart = acp.SessionStart{SessionID: "sess-new", ModelConfig: &acp.SessionConfigOption{
		ID: "model", CurrentValue: json.RawMessage(`"m1"`),
		Options: []acp.ConfigOptionValue{{Value: "m1", Name: "One"}, {Value: "m2", Name: "Two"}},
	}}
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	v, ok := m.Models(info.ID)
	if !ok || v.Current != "m1" || len(v.Models) != 2 || v.Models[1].Name != "Two" {
		t.Fatalf("view = ok=%v %+v", ok, v)
	}
	if err := m.SetModel(info.ID, "m2"); err != nil {
		t.Fatalf("set model: %v", err)
	}
	if v, _ := m.Models(info.ID); v.Current != "m2" {
		t.Fatalf("current after set = %q", v.Current)
	}
}

// SetModel 失败必须上抛且当前值不变。
func TestManagerSetModelFailureKeepsCurrent(t *testing.T) {
	m, b, _ := newTestManager(t)
	b.conn.newStart = acp.SessionStart{SessionID: "sess-new", Models: &acp.SessionModelState{
		AvailableModels: []acp.ModelInfo{{ModelID: "m1", Name: "One"}}, CurrentModelID: "m1",
	}}
	b.conn.modelErr = errors.New("err.chat.set_model_failed")
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetModel(info.ID, "m2"); err == nil {
		t.Fatal("expected set model error")
	}
	if v, _ := m.Models(info.ID); v.Current != "m1" {
		t.Fatalf("current changed on failure: %q", v.Current)
	}
}

// SetModel agent 不应答时必须超时返回，而不是永挂：创建窗体曾因此永远关不上
// （runAgentCreate 在等 SetModel，createPromise 永不释放）。超时错误须映射成
// 前端可翻译的 wire key（与握手超时同口径）。
func TestManagerSetModelTimeoutWhenAgentSilent(t *testing.T) {
	old := modelSetTimeout
	modelSetTimeout = 50 * time.Millisecond
	t.Cleanup(func() { modelSetTimeout = old })

	m, b, _ := newTestManager(t)
	b.conn.newStart = acp.SessionStart{SessionID: "sess-new", Models: &acp.SessionModelState{
		AvailableModels: []acp.ModelInfo{{ModelID: "m1", Name: "One"}}, CurrentModelID: "m1",
	}}
	b.conn.setModelGate = make(chan struct{})
	defer close(b.conn.setModelGate) // 测试结束后放行 goroutine
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- m.SetModel(info.ID, "m2") }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected timeout error")
		}
		if !strings.Contains(err.Error(), "err.chat.model_timeout") {
			t.Fatalf("err = %v, want err.chat.model_timeout", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SetModel hung past timeout")
	}
	// 超时不得改写当前值
	if v, _ := m.Models(info.ID); v.Current != "m1" {
		t.Fatalf("current changed on timeout: %q", v.Current)
	}
}

// Open 完成（new 或 load）后必须推送一次模型视图回调：握手期前端拉空
// ListChatModels 的竞态靠此补偿（codebuddy 偶发丢内置模型的根因）。
func TestManagerPushesModelsAfterOpen(t *testing.T) {
	for _, tc := range []struct {
		name string
		load bool
	}{
		{name: "new"},
		{name: "load"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, b, _ := newTestManager(t)
			b.conn.newStart = acp.SessionStart{SessionID: "sess-new", Models: &acp.SessionModelState{
				AvailableModels: []acp.ModelInfo{{ModelID: "m1", Name: "One"}}, CurrentModelID: "m1",
			}}
			pushed := make(chan string, 1)
			m.SetOnModels(func(id string) { pushed <- id })
			var info Info
			var err error
			if tc.load {
				info, err = m.Open("session:s1", Info{Kind: KindSession, Workspace: "/w", SessionID: "s1"}, Spec{Path: "x"}, "s1")
			} else {
				info, err = m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
			}
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			select {
			case id := <-pushed:
				if id != info.ID {
					t.Fatalf("pushed id = %q, want %q", id, info.ID)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("onModels not called")
			}
			if v, ok := m.Models(info.ID); !ok || v.Current != "m1" || len(v.Models) != 1 {
				t.Fatalf("Models = %+v ok=%v", v, ok)
			}
		})
	}
}

// agent 未报告模型信息（nil modelState 且 nil modelConfig）时不推送。
func TestManagerNoModelsPushWhenUnreported(t *testing.T) {
	m, _, _ := newTestManager(t)
	pushed := make(chan string, 1)
	m.SetOnModels(func(id string) { pushed <- id })
	if _, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, ""); err != nil {
		t.Fatalf("open: %v", err)
	}
	select {
	case id := <-pushed:
		t.Fatalf("unexpected push for %q", id)
	case <-time.After(100 * time.Millisecond):
	}
}

// 多块 Prompt：时间线 user 事件带展开文本与附件 meta，连接收到原始 blocks。
func TestManagerPromptBlocksTimelineAndConn(t *testing.T) {
	m, b, c := newTestManager(t)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	blocks := []acp.ContentBlock{
		{Type: "text", Text: "看这个"},
		{Type: "image", Data: "AA", MimeType: "image/png"},
	}
	if err := m.PromptBlocks(info.ID, blocks); err != nil {
		t.Fatalf("prompt blocks: %v", err)
	}
	waitTurnDone(t, c)
	var user *Update
	for _, u := range m.History(info.ID) {
		if u.Type == "user" {
			user = &u
		}
	}
	if user == nil || user.Text != "看这个" {
		t.Fatalf("user update = %+v", user)
	}
	if len(user.Attachments) != 1 || user.Attachments[0].MimeType != "image/png" {
		t.Fatalf("attachments = %+v", user.Attachments)
	}
	b.conn.mu.Lock()
	defer b.conn.mu.Unlock()
	if len(b.conn.promptBlocks) != 1 || len(b.conn.promptBlocks[0]) != 2 || b.conn.promptBlocks[0][1].Type != "image" {
		t.Fatalf("conn blocks = %+v", b.conn.promptBlocks)
	}
}

// 忙时入队的多块消息，turn 结束后必须按原 blocks 投递，时间线只补一次。
func TestManagerPromptQueueCarriesBlocks(t *testing.T) {
	m, b, c := newTestManager(t)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	b.conn.promptGate = make(chan struct{})
	if err := m.Prompt(info.ID, "first"); err != nil {
		t.Fatal(err)
	}
	second := []acp.ContentBlock{
		{Type: "text", Text: "second"},
		{Type: "resource_link", URI: "file:///C:/a.txt", Name: "a.txt"},
	}
	if err := m.PromptBlocks(info.ID, second); err != nil {
		t.Fatal(err)
	}
	close(b.conn.promptGate)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.conn.promptCount() >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if b.conn.promptCount() < 2 {
		t.Fatal("queued blocks not delivered")
	}
	b.conn.mu.Lock()
	got := b.conn.promptBlocks[1]
	b.conn.mu.Unlock()
	if len(got) != 2 || got[1].Type != "resource_link" || got[1].Name != "a.txt" {
		t.Fatalf("queued blocks = %+v", got)
	}
	var user *Update
	for _, u := range m.History(info.ID) {
		if u.Type == "user" && u.Text == "second" {
			user = &u
		}
	}
	if user == nil || len(user.Attachments) != 1 || user.Attachments[0].Name != "a.txt" {
		t.Fatalf("queued user update = %+v", user)
	}
	_ = c
}

// 旧 Prompt(id, text) 必须等价于单 text block，历史行为不回归。
func TestManagerPromptTextStillWorks(t *testing.T) {
	m, b, c := newTestManager(t)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(info.ID, "hello world"); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, c)
	b.conn.mu.Lock()
	defer b.conn.mu.Unlock()
	if len(b.conn.promptBlocks) != 1 || len(b.conn.promptBlocks[0]) != 1 || b.conn.promptBlocks[0][0].Text != "hello world" {
		t.Fatalf("conn blocks = %+v", b.conn.promptBlocks)
	}
}

func TestConfigOptionUpdateReplacesState(t *testing.T) {
	conn := &fakeConn{newStart: acp.SessionStart{SessionID: "sess-new", ConfigOptions: []acp.SessionConfigOption{
		{ID: "model", Type: "select", CurrentValue: json.RawMessage(`"m1"`), Options: []acp.ConfigOptionValue{{Value: "m1", Name: "One"}}},
		{ID: "mode", Type: "select", CurrentValue: json.RawMessage(`"build"`), Options: []acp.ConfigOptionValue{{Value: "build", Name: "build"}}},
	}}}
	b := &fakeBackend{conn: conn}
	m := NewManager(b, func(string, Update) {}, func(string, PermissionRequest) {}, func(string, int, string) {})
	var pushed bool
	m.SetOnConfig(func(string) { pushed = true })
	info, err := m.Open("new:1", Info{Workspace: "/w", ToolID: "opencode"}, Spec{Path: "x", Dir: "/w"}, "")
	if err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"sessionUpdate":"config_option_update","configOptions":[{"id":"model","name":"Model","category":"model","type":"select","currentValue":"m2","options":[{"value":"m2","name":"Two"}]},{"id":"mode","name":"Session Mode","category":"mode","type":"select","currentValue":"plan","options":[{"value":"plan","name":"plan"}]}]}`)
	b.last.SessionUpdate(info.SessionID, raw)
	if !pushed {
		t.Fatal("onConfig not called")
	}
	v := m.ConfigOptions(info.ID)
	if len(v.Options) != 2 || v.Options[1].ID != "mode" || v.Options[1].Current != "plan" {
		t.Fatalf("config view = %+v", v)
	}
	models, ok := m.Models(info.ID)
	if !ok || models.Current != "m2" {
		t.Fatalf("models = %+v ok=%v", models, ok)
	}
}

func TestSetConfigOptionUpdatesState(t *testing.T) {
	conn := &fakeConn{newStart: acp.SessionStart{SessionID: "sess-new", ConfigOptions: []acp.SessionConfigOption{
		{ID: "model", Type: "select", CurrentValue: json.RawMessage(`"m1"`), Options: []acp.ConfigOptionValue{{Value: "m1", Name: "One"}}},
		{ID: "mode", Type: "select", CurrentValue: json.RawMessage(`"build"`), Options: []acp.ConfigOptionValue{{Value: "build", Name: "build"}, {Value: "plan", Name: "plan"}}},
	}}}
	b := &fakeBackend{conn: conn}
	m := NewManager(b, func(string, Update) {}, func(string, PermissionRequest) {}, func(string, int, string) {})
	info, err := m.Open("new:1", Info{Workspace: "/w", ToolID: "opencode"}, Spec{Path: "x", Dir: "/w"}, "")
	if err != nil {
		t.Fatal(err)
	}
	conn.configSet = []acp.SessionConfigOption{
		{ID: "model", Type: "select", CurrentValue: json.RawMessage(`"m1"`)},
		{ID: "mode", Type: "select", CurrentValue: json.RawMessage(`"plan"`)},
	}
	if err := m.SetConfigOption(info.ID, "mode", "plan"); err != nil {
		t.Fatal(err)
	}
	if got := m.ConfigOptions(info.ID).Options[1].Current; got != "plan" {
		t.Fatalf("mode = %q", got)
	}
}

func TestSetConfigOptionResponseWins(t *testing.T) {
	conn := &fakeConn{newStart: acp.SessionStart{SessionID: "sess-new", ConfigOptions: []acp.SessionConfigOption{
		{ID: "model", Type: "select", CurrentValue: json.RawMessage(`"m1"`)},
		{ID: "mode", Type: "select", CurrentValue: json.RawMessage(`"plan"`), Options: []acp.ConfigOptionValue{{Value: "plan", Name: "plan"}, {Value: "build", Name: "build"}}},
	}}}
	b := &fakeBackend{conn: conn}
	m := NewManager(b, func(string, Update) {}, func(string, PermissionRequest) {}, func(string, int, string) {})
	var configPushed bool
	m.SetOnConfig(func(string) { configPushed = true })
	info, err := m.Open("new:1", Info{Workspace: "/w", ToolID: "opencode"}, Spec{Path: "x", Dir: "/w"}, "")
	if err != nil {
		t.Fatal(err)
	}
	// agent 把请求值规范化成了 build：本地必须采用回包，而不是盲目回填 plan。
	conn.configSet = []acp.SessionConfigOption{
		{ID: "model", Type: "select", CurrentValue: json.RawMessage(`"m1"`)},
		{ID: "mode", Type: "select", CurrentValue: json.RawMessage(`"build"`), Options: []acp.ConfigOptionValue{{Value: "plan", Name: "plan"}, {Value: "build", Name: "build"}}},
	}
	if err := m.SetConfigOption(info.ID, "mode", "plan"); err != nil {
		t.Fatal(err)
	}
	if got := m.ConfigOptions(info.ID).Options[1].Current; got != "build" {
		t.Fatalf("mode = %q, want agent response build", got)
	}
	if !configPushed {
		t.Fatal("onConfig not called")
	}
}

func TestSetConfigOptionOptimisticWhenResponseEmpty(t *testing.T) {
	conn := &fakeConn{newStart: acp.SessionStart{SessionID: "sess-new", ConfigOptions: []acp.SessionConfigOption{
		{ID: "model", Type: "select", CurrentValue: json.RawMessage(`"m1"`)},
		{ID: "mode", Type: "select", CurrentValue: json.RawMessage(`"plan"`), Options: []acp.ConfigOptionValue{{Value: "plan", Name: "plan"}, {Value: "build", Name: "build"}}},
	}}}
	b := &fakeBackend{conn: conn}
	m := NewManager(b, func(string, Update) {}, func(string, PermissionRequest) {}, func(string, int, string) {})
	var configPushed bool
	m.SetOnConfig(func(string) { configPushed = true })
	info, err := m.Open("new:1", Info{Workspace: "/w", ToolID: "opencode"}, Spec{Path: "x", Dir: "/w"}, "")
	if err != nil {
		t.Fatal(err)
	}
	conn.configSet = nil
	if err := m.SetConfigOption(info.ID, "mode", "build"); err != nil {
		t.Fatal(err)
	}
	if got := m.ConfigOptions(info.ID).Options[1].Current; got != "build" {
		t.Fatalf("mode = %q, want optimistic build", got)
	}
	if !configPushed {
		t.Fatal("onConfig not called")
	}
}

func TestSetConfigOptionModelFiresBothCallbacks(t *testing.T) {
	conn := &fakeConn{newStart: acp.SessionStart{SessionID: "sess-new", ConfigOptions: []acp.SessionConfigOption{
		{ID: "model", Type: "select", CurrentValue: json.RawMessage(`"m1"`), Options: []acp.ConfigOptionValue{{Value: "m1", Name: "One"}, {Value: "m2", Name: "Two"}}},
		{ID: "mode", Type: "select", CurrentValue: json.RawMessage(`"build"`)},
	}}}
	b := &fakeBackend{conn: conn}
	m := NewManager(b, func(string, Update) {}, func(string, PermissionRequest) {}, func(string, int, string) {})
	var configPushed, modelsPushed bool
	m.SetOnConfig(func(string) { configPushed = true })
	m.SetOnModels(func(string) { modelsPushed = true })
	info, err := m.Open("new:1", Info{Workspace: "/w", ToolID: "opencode"}, Spec{Path: "x", Dir: "/w"}, "")
	if err != nil {
		t.Fatal(err)
	}
	conn.configSet = []acp.SessionConfigOption{
		{ID: "model", Type: "select", CurrentValue: json.RawMessage(`"m2"`), Options: []acp.ConfigOptionValue{{Value: "m1", Name: "One"}, {Value: "m2", Name: "Two"}}},
		{ID: "mode", Type: "select", CurrentValue: json.RawMessage(`"build"`)},
	}
	if err := m.SetConfigOption(info.ID, "model", "m2"); err != nil {
		t.Fatal(err)
	}
	if !configPushed {
		t.Fatal("onConfig not called")
	}
	if !modelsPushed {
		t.Fatal("onModels not called for model change")
	}
	if got := m.ConfigOptions(info.ID).Options[0].Current; got != "m2" {
		t.Fatalf("model = %q", got)
	}
	if v, ok := m.Models(info.ID); !ok || v.Current != "m2" {
		t.Fatalf("models = %+v ok=%v", v, ok)
	}
}
