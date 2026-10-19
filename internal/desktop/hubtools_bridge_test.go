package desktop

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/hubtools"
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
	// 三轮：第一轮回 tool_calls（create_requirement），第二轮回工具循环收尾文本，
	// 第三轮（确认后喂回）回自然语言总结
	round := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		round++
		if round == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"create_requirement","arguments":"{\"project\":\"D:\\\\ws-a\",\"text\":\"新增需求\"}"}}]}}]}`))
			return
		}
		if round == 2 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"已提交确认。"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"需求已创建完成。"}}]}`))
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
	// 确认结果经 LLM 总结后落盘（第三轮 mock 文本），不是裸 JSON
	var final hub.Message
	for _, mm := range store.List() {
		if mm.Kind == "agent" && mm.Event == "" && mm.ID > confirm.ID {
			final = mm
		}
	}
	if final.Text != "需求已创建完成。" {
		t.Fatalf("确认结果应喂回 LLM 总结后落盘: %+v", store.List())
	}
	// 二次确认同一 pendingID 应报未知
	if err := a.ConfirmHubTool(confirm.ChatID, extractPendingID(confirm.Text), true); err == nil {
		t.Fatal("同一 pendingID 不应可重复消费")
	}
}

// TestHandlePromptBusyDegrades 验证串行化：已有在途工具化对话时，第二条输入
// 不触碰其 session/pending，直接返回 handled=false 落回原链路。
func TestHandlePromptBusyDegrades(t *testing.T) {
	release := make(chan struct{})
	gotReq := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(gotReq) })
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"第一条的回复"}}]}`))
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

	h := a.hubTools()
	if h == nil {
		t.Fatal("就绪配置下 hubTools 不应为 nil")
	}
	done := make(chan struct{})
	go func() {
		handled, _ := h.HandlePrompt("s1", "第一条")
		if !handled {
			t.Error("第一条应被工具化消费")
		}
		close(done)
	}()
	<-gotReq
	// 在途期间第二条：应拒绝（handled=false），且不等待第一条完成
	second, _ := h.HandlePrompt("s1", "第二条")
	if second {
		t.Fatal("在途期间第二条输入不应被工具化消费")
	}
	close(release)
	<-done
	// 第二条未落盘（原链路接管后才会落）
	for _, mm := range store.List() {
		if mm.Kind == "user" && mm.Text == "第二条" {
			t.Fatalf("第二条不应由 hubtools 落盘: %+v", mm)
		}
	}
}

// TestHandlePromptFailureClearsPending 验证 LLM 失败降级时清除在途会话与挂起：
// 降级后 ConfirmHubTool 不可再消费从未展示的确认卡。
func TestHandlePromptFailureClearsPending(t *testing.T) {
	round := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if round == 0 {
			round++
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"create_requirement","arguments":"{\"project\":\"p\",\"text\":\"t\"}"}}]}}]}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
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

	h := a.hubTools()
	handled, err := h.HandlePrompt("s1", "帮我新建需求")
	if handled || err != nil {
		t.Fatalf("失败应降级 handled=false, err=nil，得到 %v %v", handled, err)
	}
	if h.session != nil || h.currentPending() != nil {
		t.Fatalf("失败后 session/pending 应清除: %+v %+v", h.session, h.currentPending())
	}
	if cerr := a.ConfirmHubTool("s1", "whatever", true); cerr != hubtools.ErrUnknownPending {
		t.Fatalf("降级后确认应报未知挂起: %v", cerr)
	}
	// store 无痕（延迟落盘）
	if n := len(store.List()); n != 0 {
		t.Fatalf("失败降级不应落盘: %+v", store.List())
	}
}

// TestHubToolsReconfigFollowsConfig 验证路由器配置时效：配置变更重建、
// RouteMode=agent 即时关闭。
func TestHubToolsReconfigFollowsConfig(t *testing.T) {
	a, _, _ := newBridgeTestApp(t)
	a.opts.Config.Model.OpenAIBaseURL = "http://x"
	a.opts.Config.Model.APIKey = "k"
	a.opts.Config.Model.ActiveModel = "m1"
	h1 := a.hubTools()
	if h1 == nil || h1.cfg.Model != "m1" {
		t.Fatalf("初始路由器不符: %+v", h1)
	}
	a.opts.Config.Model.ActiveModel = "m2"
	h2 := a.hubTools()
	if h2 == nil || h2.cfg.Model != "m2" {
		t.Fatalf("配置变更后应重建路由器: %+v", h2)
	}
	a.opts.Config.AgentHub.RouteMode = config.RouteModeAgent
	if h := a.hubTools(); h != nil {
		t.Fatalf("RouteMode=agent 应即时关闭: %+v", h)
	}
	a.opts.Config.AgentHub.RouteMode = config.RouteModeAuto
	if h := a.hubTools(); h == nil {
		t.Fatal("回改 auto 后应恢复")
	}
}

// TestHandlePromptEmptyTextDegrades 验证模型完成但无文本且无挂起时降级原链路
//（user 消息尚未落盘，store 无痕）。
func TestHandlePromptEmptyTextDegrades(t *testing.T) {
	mock := &hubLLMMock{bodies: []string{`""`}}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(srv.Close)

	a, store, rec := newBridgeTestApp(t)
	a.opts.Config.Model.OpenAIBaseURL = srv.URL
	a.opts.Config.Model.APIKey = "k-test"
	a.opts.Config.Model.ActiveModel = "test-model"
	a.opts.Config.AgentHub.RouteMode = config.RouteModeForceAuto
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m

	h := a.hubTools()
	handled, err := h.HandlePrompt("s1", "hi")
	if handled || err != nil {
		t.Fatalf("空回复应降级 handled=false, err=nil，得到 %v %v", handled, err)
	}
	if n := len(store.List()); n != 0 {
		t.Fatalf("空回复降级不应落盘: %+v", store.List())
	}
}

// TestHandlePromptAutoRouteAgent 验证 auto 模式意图路由：模型输出 route=agent
// 时降级原链路（store 无痕）；force_auto 不做该判定直接消费。
func TestHandlePromptAutoRouteAgent(t *testing.T) {
	// 两次调用各取一条：第一次给 auto（route=agent），第二次给 force_auto（纯文本）
	mock := &hubLLMMock{bodies: []string{`"{\"route\":\"agent\"}"`, `"直接回答"`}}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(srv.Close)

	newApp := func(mode string) (*App, *hub.Store) {
		a, store, rec := newBridgeTestApp(t)
		a.opts.Config.Model.OpenAIBaseURL = srv.URL
		a.opts.Config.Model.APIKey = "k-test"
		a.opts.Config.Model.ActiveModel = "test-model"
		a.opts.Config.AgentHub.RouteMode = mode
		m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, nil, nil, nil)
		t.Cleanup(m.CloseAll)
		a.opts.Chats = m
		return a, store
	}

	// auto：route=agent → handled=false，store 无痕
	a, store := newApp(config.RouteModeAuto)
	h := a.hubTools()
	handled, err := h.HandlePrompt("s1", "帮我写个冒泡排序")
	if handled || err != nil {
		t.Fatalf("route=agent 应降级，得到 %v %v", handled, err)
	}
	if n := len(store.List()); n != 0 {
		t.Fatalf("route=agent 降级不应落盘: %+v", store.List())
	}

	// force_auto：同样回复不做路由判定 → 直接消费落盘
	a2, store2 := newApp(config.RouteModeForceAuto)
	h2 := a2.hubTools()
	handled2, err2 := h2.HandlePrompt("s1", "帮我写个冒泡排序")
	if !handled2 || err2 != nil {
		t.Fatalf("force_auto 应直接消费，得到 %v %v", handled2, err2)
	}
	var ans hub.Message
	for _, mm := range store2.List() {
		if mm.Kind == "agent" && mm.Event == "" {
			ans = mm
		}
	}
	if ans.Text == "" {
		t.Fatalf("force_auto 应落盘 assistant 消息: %+v", store2.List())
	}
}

// TestRecentMessagesFilters 验证聊天记录上下文映射：只取纯文本消息（排除
// 事件卡/排队消息）、字段映射、超长截断 200 字、n 条取尾部。
func TestRecentMessagesFilters(t *testing.T) {
	a, store, _ := newBridgeTestApp(t)
	long := strings.Repeat("长", 250)
	seed := []hub.Message{
		{Kind: "agent", Event: "tool_confirm", Text: "确认卡不应出现"},
		{Kind: "user", Status: hub.StatusQueued, Text: "排队消息不应出现"},
		{Kind: "agent", Event: "ack", Text: "ack 不应出现"},
		{Kind: "user", ChatID: "c1", Tool: "cursor", SessionTitle: "修 bug", Text: long},
		{Kind: "agent", ChatID: "c1", Tool: "cursor", SessionTitle: "修 bug", Text: "已修复"},
	}
	for i := range seed {
		if err := store.Append(seed[i]); err != nil {
			t.Fatal(err)
		}
	}
	b := &desktopBridge{app: a}
	got := b.RecentMessages(10)
	if len(got) != 2 {
		t.Fatalf("应只保留 2 条纯文本消息: %+v", got)
	}
	if got[0].Agent != "cursor「修 bug」" || got[0].Sender != "用户" {
		t.Fatalf("字段映射不符: %+v", got[0])
	}
	if n := len([]rune(got[0].Text)); n != 200 {
		t.Fatalf("超长文本应按字截断 200，得到 %d", n)
	}
	if got[0].ChatID != "c1" {
		t.Fatalf("ChatID 应随条目带出供 refMap: %+v", got[0])
	}
	if got[1].Sender != "助手" || got[1].Text != "已修复" {
		t.Fatalf("助手消息映射不符: %+v", got[1])
	}
	tail := b.RecentMessages(1)
	if len(tail) != 1 || tail[0].Text != "已修复" {
		t.Fatalf("n=1 应取尾部一条: %+v", tail)
	}
}

// TestHandlePromptDispatchTask 验证意图派发端到端：auto 模式模型第一轮发起
// dispatch_task，任务文本直投记录编号对应会话（落一条 queued user 消息），
// 第二轮收尾文本照常落盘。
func TestHandlePromptDispatchTask(t *testing.T) {
	round := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		round++
		if round == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"dispatch_task","arguments":"{\"ref\":\"1\",\"task\":\"继续修复登录\"}"}}]}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"已派发给 Cursor。"}}]}`))
	}))
	t.Cleanup(srv.Close)

	a, store, rec := newBridgeTestApp(t)
	a.opts.Config.Model.OpenAIBaseURL = srv.URL
	a.opts.Config.Model.APIKey = "k-test"
	a.opts.Config.Model.ActiveModel = "test-model"
	a.opts.Config.AgentHub.RouteMode = config.RouteModeAuto
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m
	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "修复登录", ToolID: "cursor",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatal(err)
	}
	// 预置一条归因到该会话的 agent 消息：记录编号 [1] → info.ID
	if err := store.Append(hub.Message{
		Kind: "agent", ChatID: info.ID, Tool: "cursor", SessionTitle: "修复登录", Text: "我在修",
	}); err != nil {
		t.Fatal(err)
	}

	if err := a.SendChatPrompt(info.ID, "继续修"); err != nil {
		t.Fatal(err)
	}

	// 派发任务直投目标会话：落一条 queued user 消息，正文为 task 文本
	var dispatched hub.Message
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && dispatched.ID == 0 {
		for _, mm := range store.List() {
			if mm.Kind == "user" && mm.Text == "继续修复登录" {
				dispatched = mm
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if dispatched.ID == 0 {
		t.Fatalf("派发任务未落盘: %+v", store.List())
	}
	if dispatched.ChatID != info.ID || dispatched.Status != hub.StatusQueued {
		t.Fatalf("派发消息应归因目标会话且排队: %+v", dispatched)
	}
	// 原始输入与最终回复照常落盘
	var orig, ans hub.Message
	for _, mm := range store.List() {
		if mm.Kind == "user" && mm.Text == "继续修" {
			orig = mm
		}
		if mm.Kind == "agent" && mm.Event == "" && mm.Text == "已派发给 Cursor。" {
			ans = mm
		}
	}
	if orig.ID == 0 || ans.ID == 0 {
		t.Fatalf("原始输入/最终回复未落盘: %+v", store.List())
	}
}

// TestConfirmHubToolNotReady 验证未配置模型时确认绑定返回未就绪错误。
func TestConfirmHubToolNotReady(t *testing.T) {
	a, _, _ := newBridgeTestApp(t)
	if err := a.ConfirmHubTool("s1", "p1", true); err != errNotReady {
		t.Fatalf("未配置模型应返回 errNotReady，得到 %v", err)
	}
}

// TestSaveHubRouteMode 验证路由模式写穿绑定：合法值落配置，非法值报 wire key。
func TestSaveHubRouteMode(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.AgentHub.RouteMode = config.RouteModeAuto
	a := NewAppWith(Options{
		Config: cfg,
		Layout: config.Layout{
			Root:   dir,
			Config: filepath.Join(dir, "config.yaml"),
			Cache:  filepath.Join(dir, "cache"),
		},
	})
	if err := a.SaveHubRouteMode("force_auto"); err != nil {
		t.Fatal(err)
	}
	if got := a.snapshot().Config.AgentHub.RouteMode; got != "force_auto" {
		t.Fatalf("路由模式应写穿配置: %s", got)
	}
	err := a.SaveHubRouteMode("bogus")
	if err == nil || err.Error() != "err.hub.invalid_route_mode" {
		t.Fatalf("非法值应报 err.hub.invalid_route_mode: %v", err)
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
