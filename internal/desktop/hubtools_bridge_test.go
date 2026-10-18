package desktop

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/requirements"
)

// newBridgeTestApp 构造带需求 store、扫描结果与事件收集的 App，供桥接层测试。
func newBridgeTestApp(t *testing.T) (*App, *hub.Store, *notifyRecorder) {
	t.Helper()
	home := t.TempDir()
	store := hub.New(t.TempDir(), 0)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	reqStore := requirements.New(t.TempDir())
	if err := reqStore.Load(); err != nil {
		t.Fatal(err)
	}
	sessions := []providers.Session{
		{ID: "s1", ToolID: "claude", Workspace: `D:\ws-a`, Title: "修复上传白名单", UpdatedAt: time.Now()},
	}
	res := &discovery.Result{Sessions: sessions, Workspaces: discovery.GroupSessions(sessions)}
	rec := &notifyRecorder{}
	app := NewAppWith(Options{
		Home:              home,
		HubStore:          store,
		RequirementsStore: reqStore,
		Providers:         []providers.Provider{fakeProvider{id: "claude"}},
		Scan: func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
			return res, nil
		},
		Emit: rec.emit,
	})
	// 直接注入扫描结果：GetWorkspaces 从 rawWorkspaces 派生，不经真实扫描
	app.mu.Lock()
	app.rawWorkspaces = res.Workspaces
	app.result = res
	app.mu.Unlock()
	return app, store, rec
}

// hubLLMMock 是 OpenAI 兼容端点桩：按序回放响应体（choices[0].message.content）。
type hubLLMMock struct {
	mu     sync.Mutex
	bodies []string // 每轮请求回放的 choices JSON 片段
	calls  int
	tools  [][]string // 记录每轮请求携带的工具名（断言工具暴露）
}

func (m *hubLLMMock) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	i := m.calls
	m.calls++
	var req struct {
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	_ = readJSONBody(r, &req)
	names := make([]string, 0, len(req.Tools))
	for _, t := range req.Tools {
		names = append(names, t.Function.Name)
	}
	m.tools = append(m.tools, names)
	body := m.bodies[i]
	m.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + body + `}}]}`))
}

// TestBridgeAdaptsRequirements 验证 desktopBridge 把 App 既有数据映射为 hubtools 视图：
// 需求字段映射、工作区 ID 规则（local=Path、ssh=ConnID+RemotePath）、任务对齐 chat.Info。
func TestBridgeAdaptsRequirements(t *testing.T) {
	a, _, _ := newBridgeTestApp(t)
	b := &desktopBridge{app: a}

	// 需求：真 store 写入后经桥读取
	rs := a.getRequirementStore()
	it, err := rs.Add(`D:\ws-a`, "修复登录", "high", []string{"auth"})
	if err != nil {
		t.Fatal(err)
	}
	reqs, err := b.ListRequirements()
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 {
		t.Fatalf("应返回 1 条需求，得到 %d", len(reqs))
	}
	r := reqs[0]
	if r.ID != it.ID || r.Text != "修复登录" || r.Priority != "high" || r.Status != "pending" {
		t.Fatalf("需求映射不符: %+v", r)
	}
	if len(r.Tags) != 1 || r.Tags[0] != "auth" {
		t.Fatalf("tags 映射不符: %+v", r.Tags)
	}
	got, err := b.GetRequirement(it.ID)
	if err != nil || got == nil || got.ID != it.ID {
		t.Fatalf("GetRequirement: %+v err=%v", got, err)
	}
	if missing, err := b.GetRequirement("nope"); err != nil || missing != nil {
		t.Fatalf("不存在的需求应返回 nil,nil，得到 %+v err=%v", missing, err)
	}

	// 工作区：local 用 Path 作 ID
	wss, err := b.ListWorkspaces()
	if err != nil || len(wss) == 0 {
		t.Fatalf("ListWorkspaces: %v len=%d", err, len(wss))
	}
	var local *struct{ id, path string }
	for _, w := range wss {
		if w.Kind == "local" || w.Kind == "" {
			local = &struct{ id, path string }{w.ID, w.Path}
			break
		}
	}
	if local == nil || local.id != local.path {
		t.Fatalf("local 工作区 ID 应为 Path: %+v", wss)
	}
	if got, err := b.GetWorkspace(local.id); err != nil || got == nil || got.ID != local.id {
		t.Fatalf("GetWorkspace 未命中: %+v err=%v", got, err)
	}
	if got, err := b.GetWorkspace("ssh://x/y"); err != nil || got != nil {
		t.Fatalf("不存在的 ssh 工作区应返回 nil: %+v err=%v", got, err)
	}

	// 运行中任务：对齐 chat.Info（running 状态会话）
	m := newChatManagerWith(func(string, ...any) {}, fakeChatBackend{}, nil, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m
	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "跑任务", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	waitStatusDesktop(t, m, info.ID)
	tasks, err := b.ListRunningTasks()
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, tk := range tasks {
		if tk.ID == info.ID {
			found = true
			if tk.Title != "跑任务" || tk.ToolID != "claude" || tk.Status != chat.StatusRunning {
				t.Fatalf("任务映射不符: %+v", tk)
			}
		}
	}
	if !found {
		t.Fatalf("运行中会话应出现在任务列表: %+v", tasks)
	}
	if got, err := b.GetTaskDetail(info.ID); err != nil || got == nil || got.ID != info.ID {
		t.Fatalf("GetTaskDetail 未命中: %+v err=%v", got, err)
	}
}

// TestHandlePromptNotReady 验证模型未配置时 hubTools() 返回 nil，SendChatPrompt 走原链路。
func TestHandlePromptNotReady(t *testing.T) {
	a, store, rec := newBridgeTestApp(t)
	if h := a.hubTools(); h != nil {
		t.Fatalf("模型未配置时 hubTools 应返回 nil，得到 %+v", h)
	}
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m
	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "t", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SendChatPrompt(info.ID, "看看需求"); err != nil {
		t.Fatal(err)
	}
	// 原链路特征：manager Prompt 投递 + ack_delivered
	u := waitHubKind(t, store, "user")
	if u.Status == "" {
		// 空闲投递：等 ack
		waitHubEvent(t, store, "ui.hub.ack_delivered")
	}
}

// TestHandlePromptForceAuto 验证 force_auto 模式下输入走 hubtools 工具循环：
// assistant 最终文本落盘、不触达 chat manager、LLM 拿到工具 schema。
func TestHandlePromptForceAuto(t *testing.T) {
	mock := &hubLLMMock{bodies: []string{`"需求清单如下：共 1 条。"`}}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(srv.Close)

	a, store, rec := newBridgeTestApp(t)
	a.opts.Config.Model.OpenAIBaseURL = srv.URL
	a.opts.Config.Model.APIKey = "k-test"
	a.opts.Config.Model.ActiveModel = "test-model"
	a.opts.Config.AgentHub.RouteMode = config.RouteModeForceAuto
	rs := a.getRequirementStore()
	if _, err := rs.Add(`D:\ws-a`, "修复登录", "high", nil); err != nil {
		t.Fatal(err)
	}

	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m
	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "t", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SendChatPrompt(info.ID, "列出需求"); err != nil {
		t.Fatal(err)
	}

	// user 消息落盘（状态空 = 工具化路径不排队）
	u := waitHubKind(t, store, "user")
	if u.Status != "" {
		t.Fatalf("工具化路径 user 消息不应排队: %+v", u)
	}
	// assistant 最终回复落盘
	var ans hub.Message
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, mm := range store.List() {
			if mm.Kind == "agent" && mm.Event == "" && mm.ID > u.ID {
				ans = mm
			}
		}
		if ans.Text != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if ans.Text != "需求清单如下：共 1 条。" {
		t.Fatalf("assistant 最终回复不符: %+v", ans)
	}
	if ans.ChatID != info.ID {
		t.Fatalf("assistant 消息应归因到会话: %+v", ans)
	}
	if rec.count("hub:changed") == 0 {
		t.Fatal("应 emit hub:changed")
	}
	// LLM 应拿到 9 个工具 schema
	mock.mu.Lock()
	calls, tools := mock.calls, mock.tools
	mock.mu.Unlock()
	if calls < 1 || len(tools[0]) < 9 {
		t.Fatalf("LLM 请求应携带工具 schema: calls=%d tools=%v", calls, tools)
	}
	// fakeChatBackend Prompt 不会触发（未触达 chat manager）——若触发会在 turn 后落 agent 消息，
	// 这里只有一条 assistant（上面已断言文本），间接验证。
}

// TestHandlePromptConfirm 验证写工具触发确认：tool_confirm 消息落盘后 ConfirmHubTool 走通。
func TestHandlePromptConfirm(t *testing.T) {
	// 两轮：第一轮回 tool_calls（create_requirement），第二轮回纯文本
	round := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if round == 0 {
			round++
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"create_requirement","arguments":"{\"project\":\"D:\\\\ws-a\",\"text\":\"新增需求\"}"}}]}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"已提交确认。"}}]}`))
	}))
	t.Cleanup(srv.Close)

	a, store, rec := newBridgeTestApp(t)
	a.opts.Config.Model.OpenAIBaseURL = srv.URL
	a.opts.Config.Model.APIKey = "k-test"
	a.opts.Config.Model.ActiveModel = "test-model"
	a.opts.Config.AgentHub.RouteMode = config.RouteModeForceAuto

	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m
	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "t", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SendChatPrompt(info.ID, "帮我新建需求"); err != nil {
		t.Fatal(err)
	}

	// tool_confirm 卡片落盘
	var confirm hub.Message
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, mm := range store.List() {
			if mm.Kind == "agent" && mm.Event == "tool_confirm" {
				confirm = mm
			}
		}
		if confirm.ID != 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if confirm.ID == 0 {
		t.Fatal("未等到 tool_confirm 消息")
	}
	if !strings.Contains(confirm.Text, `"pending":"`) || !strings.Contains(confirm.Text, `"tool":"create_requirement"`) {
		t.Fatalf("tool_confirm 文本应带尾部 JSON: %q", confirm.Text)
	}
	if confirm.ChatID != info.ID {
		t.Fatalf("tool_confirm 应归因到会话: %+v", confirm)
	}

	// 需求尚未写入（挂起等确认）
	if got := a.getRequirementStore().List(); len(got) != 0 {
		t.Fatalf("确认前不应写入需求: %+v", got)
	}

	// 确认执行
	if err := a.ConfirmHubTool(confirm.ChatID, extractPendingID(confirm.Text), true); err != nil {
		t.Fatal(err)
	}
	got := a.getRequirementStore().List()
	if len(got) != 1 || got[0].Text != "新增需求" {
		t.Fatalf("确认后应写入需求: %+v", got)
	}
	// 二次确认同一 pendingID 应报未知
	if err := a.ConfirmHubTool(confirm.ChatID, extractPendingID(confirm.Text), true); err == nil {
		t.Fatal("同一 pendingID 不应可重复消费")
	}
}

// extractPendingID 从 tool_confirm 文本尾部 JSON 提取 pending ID。
func extractPendingID(text string) string {
	i := strings.LastIndex(text, "{")
	if i < 0 {
		return ""
	}
	var v struct {
		Pending string `json:"pending"`
	}
	if err := readJSONString(text[i:], &v); err != nil {
		return ""
	}
	return v.Pending
}

// readJSONBody / readJSONString 是测试用小工具（desktop 包无既有 body 解析辅助）。
func readJSONBody(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func readJSONString(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}
