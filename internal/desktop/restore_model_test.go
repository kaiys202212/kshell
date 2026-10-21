package desktop

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/acp"
	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/providers"
)

// restoreModelBackend 每次 Start 产出带「Current=default、可用含 m2」的连接，
// 供激活与 rebuild 两条路径断言 SetModel。
type restoreModelBackend struct {
	mu    sync.Mutex
	conns []*fakeChatConn
}

func (b *restoreModelBackend) Start(spec chat.Spec, h acp.Handler) (chat.Conn, error) {
	c := &fakeChatConn{
		start: acp.SessionStart{
			SessionID: "s1",
			Models: &acp.SessionModelState{
				AvailableModels: []acp.ModelInfo{
					{ModelID: "default", Name: "Default"},
					{ModelID: "m2", Name: "Two"},
				},
				CurrentModelID: "default",
			},
		},
		waitCh: make(chan struct{}),
	}
	b.mu.Lock()
	b.conns = append(b.conns, c)
	b.mu.Unlock()
	return c, nil
}

func (b *restoreModelBackend) connAt(i int) *fakeChatConn {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.conns[i]
}

// OpenSessionACP 激活 Claude 会话后：JSONL 尾部上次助手模型在可用列表内且
// 与 Current 不同时，应自动 SetModel 恢复。
func TestOpenSessionACPRestoresLastModelFromJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.jsonl")
	body := `{"type":"assistant","message":{"model":"m2","content":"hi"}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := &fakeChatConn{
		start: acp.SessionStart{
			SessionID: "s1",
			Models: &acp.SessionModelState{
				AvailableModels: []acp.ModelInfo{
					{ModelID: "default", Name: "Default"},
					{ModelID: "m2", Name: "Two"},
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
	env.app.result.Sessions[0].Path = path
	env.app.mu.Unlock()

	if _, err := env.app.OpenSessionACP("s1"); err != nil {
		t.Fatalf("OpenSessionACP: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(conn.modelCalls) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(conn.modelCalls) != 1 || conn.modelCalls[0] != "m2" {
		t.Fatalf("应 SetModel(m2): %v", conn.modelCalls)
	}
}

// 上次模型不在可用列表：静默跳过，不 SetModel、不挡激活。
func TestOpenSessionACPSkipsRestoreWhenModelNotInList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.jsonl")
	body := `{"type":"assistant","message":{"model":"gone-m","content":"hi"}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := &fakeChatConn{
		start: acp.SessionStart{
			SessionID: "s1",
			Models: &acp.SessionModelState{
				AvailableModels: []acp.ModelInfo{{ModelID: "default", Name: "Default"}},
				CurrentModelID:  "default",
			},
		},
		waitCh: make(chan struct{}),
	}
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, &fakeChatCaptureBackend{conn: conn})
	env.app.mu.Lock()
	env.app.opts.Config.SessionMode = config.SessionModeACP
	env.app.result.Sessions[0].Path = path
	env.app.mu.Unlock()

	if _, err := env.app.OpenSessionACP("s1"); err != nil {
		t.Fatalf("OpenSessionACP: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if len(conn.modelCalls) != 0 {
		t.Fatalf("不在列表应不 SetModel: %v", conn.modelCalls)
	}
}

// rebuildChat（ensureChatAlive）恢复退出会话时同样应按 JSONL 恢复模型。
func TestRebuildChatRestoresLastModelFromJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.jsonl")
	body := `{"type":"assistant","message":{"model":"m2","content":"hi"}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	b := &restoreModelBackend{}
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, b)
	env.app.mu.Lock()
	env.app.opts.Config.SessionMode = config.SessionModeACP
	env.app.result.Sessions[0].Path = path
	env.app.mu.Unlock()

	opened, err := env.app.OpenSessionACP("s1")
	if err != nil {
		t.Fatalf("OpenSessionACP: %v", err)
	}
	oldID := opened.Chat.ID
	// 首开也会 restore 一次；清掉计数，只断言重建那次。
	b.connAt(0).modelCalls = nil
	exitChatConn(t, env.app.chats(), oldID, b.connAt(0))

	if _, err := env.app.ensureChatAlive(oldID); err != nil {
		t.Fatalf("ensureChatAlive: %v", err)
	}
	c1 := b.connAt(1)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(c1.modelCalls) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(c1.modelCalls) != 1 || c1.modelCalls[0] != "m2" {
		t.Fatalf("重建应 SetModel(m2): %v", c1.modelCalls)
	}
}

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
	}
	for _, tc := range cases {
		if got := modelIDLooseEqual(tc.a, tc.b); got != tc.want {
			t.Errorf("modelIDLooseEqual(%q,%q)=%v want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestResolveRestoreModelID(t *testing.T) {
	models := []chat.ModelView{
		{ID: "default", Name: "Default"},
		{ID: "glm-5.3-flash", Name: "Flash"},
		{ID: "kshell:p2:deepseek-v4.1-flash", Name: "DS"},
	}
	cases := []struct {
		name, last, current, want string
	}{
		{"精确命中", "glm-5.3-flash", "default", "glm-5.3-flash"},
		{"点号连字符宽松→规范裸名", "glm-5-3-flash", "default", "glm-5.3-flash"},
		{"多平台前缀宽松", "deepseek-v4-1-flash", "default", "deepseek-v4.1-flash"},
		{"已是 Current 宽松相等跳过", "glm-5-3-flash", "glm-5.3-flash", ""},
		{"无匹配", "gone-m", "default", ""},
		{"空 last", "", "default", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveRestoreModelID(tc.last, models, tc.current)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

// JSONL 落盘连字符、配置列表点号：激活后应 SetModel 规范点号名。
func TestOpenSessionACPRestoresModelLooseDotHyphen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.jsonl")
	body := `{"type":"assistant","message":{"model":"glm-5-3-flash","content":"hi"}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := &fakeChatConn{
		start: acp.SessionStart{
			SessionID: "s1",
			Models: &acp.SessionModelState{
				AvailableModels: []acp.ModelInfo{
					{ModelID: "default", Name: "Default"},
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
	env.app.opts.Config.Model = config.ModelConfig{
		Enabled: true,
		Platforms: []config.ModelPlatform{{
			ID: "default", Name: "openai", Models: []string{"glm-5.3-flash"},
		}},
	}
	env.app.result.Sessions[0].Path = path
	env.app.mu.Unlock()

	if _, err := env.app.OpenSessionACP("s1"); err != nil {
		t.Fatalf("OpenSessionACP: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(conn.modelCalls) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(conn.modelCalls) != 1 || conn.modelCalls[0] != "glm-5.3-flash" {
		t.Fatalf("应 SetModel(glm-5.3-flash): %v", conn.modelCalls)
	}
}
