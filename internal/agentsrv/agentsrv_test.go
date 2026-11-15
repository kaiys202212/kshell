package agentsrv

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/agentapi"
	"github.com/yangk/kshell/internal/hubtools"
)

// fakeBackend 是服务端测试用的最小 Backend 桩（镜像 agentapi 包内测试桩的形态）。
type fakeBackend struct {
	active     []agentapi.Session
	disk       []agentapi.Session
	archived   []string
	msgs       map[string][]agentapi.Message
	sent       []string
	failSendOn string
	tools      []agentapi.Option
	workspaces []agentapi.Option
	feed       *agentapi.Feed
}

func (f *fakeBackend) ActiveSessions() []agentapi.Session { return f.active }
func (f *fakeBackend) DiskSessions() []agentapi.Session   { return f.disk }
func (f *fakeBackend) ArchivedIDs() []string              { return f.archived }
func (f *fakeBackend) SessionMessages(chatID string) []agentapi.Message {
	return f.msgs[chatID]
}
func (f *fakeBackend) Send(chatID, text string) error {
	if f.failSendOn == chatID {
		return fmt.Errorf("err.chat.gone")
	}
	f.sent = append(f.sent, chatID+"|"+text)
	return nil
}
func (f *fakeBackend) Stop(chatID string) error { return nil }
func (f *fakeBackend) Recall(msgID int64) error { return nil }
func (f *fakeBackend) ListModels(chatID string) ([]agentapi.ModelOption, string, error) {
	return []agentapi.ModelOption{{ID: "m1", Name: "Model 1"}}, "m1", nil
}
func (f *fakeBackend) SetModel(chatID, model string) error                    { return nil }
func (f *fakeBackend) GrantPermission(chatID, reqID string, allow bool) error { return nil }
func (f *fakeBackend) Create(toolID, wsID, model string) (agentapi.Session, error) {
	return agentapi.Session{ID: "chat-new", Tool: toolID, Workspace: wsID, Status: agentapi.StatusRunning}, nil
}
func (f *fakeBackend) Open(sessionID string) (agentapi.Session, error) {
	return agentapi.Session{ID: "chat-opened", SessionID: sessionID, Status: agentapi.StatusRunning}, nil
}
func (f *fakeBackend) Tools() []agentapi.Option      { return f.tools }
func (f *fakeBackend) Workspaces() []agentapi.Option { return f.workspaces }
func (f *fakeBackend) Feed() *agentapi.Feed {
	if f.feed == nil {
		f.feed = agentapi.NewFeed()
	}
	return f.feed
}

// newFake 构造标准桩：1 个活跃会话 + 1 个磁盘会话，活跃会话带两条消息。
func newFake() *fakeBackend {
	return &fakeBackend{
		active: []agentapi.Session{
			{ID: "chat-a", SessionID: "sess-a", Tool: "claude", Title: "甲", Workspace: "d:/ws/a", Status: agentapi.StatusRunning, LastTs: 200, Summary: "最近回复"},
		},
		disk: []agentapi.Session{
			{SessionID: "sess-c", Tool: "gemini", Title: "丙", Workspace: "d:/ws/c", Status: agentapi.StatusDisk},
		},
		msgs: map[string][]agentapi.Message{
			"chat-a": {
				{ID: 1, SessionID: "chat-a", Kind: "user", Text: "开始", Ts: 100},
				{ID: 2, SessionID: "chat-a", Kind: "agent", Text: "回复", Ts: 200},
			},
		},
		tools:      []agentapi.Option{{ID: "claude", Label: "Claude Code"}},
		workspaces: []agentapi.Option{{ID: "d:/ws/a", Label: "a"}},
	}
}

// startTestSrv 起一个真实监听的测试服务，返回服务与其 API。
func startTestSrv(t *testing.T, b agentapi.Backend) (*Server, *agentapi.API) {
	t.Helper()
	api := agentapi.New(b)
	srv, err := Start(Options{API: api, EndpointPath: filepath.Join(t.TempDir(), "agentapi", "endpoint.json")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return srv, api
}

// get 用 Bearer 发 GET，返回状态码与响应体。
func get(t *testing.T, srv *Server, path string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL()+path, nil)
	req.Header.Set("Authorization", "Bearer "+srv.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := readAll(resp)
	return resp.StatusCode, body
}

func post(t *testing.T, srv *Server, path, body string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL()+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+srv.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, readAll(resp)
}

func readAll(resp *http.Response) []byte {
	b, _ := io.ReadAll(resp.Body)
	return b
}

func TestStartWritesEndpointFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agentapi", "endpoint.json")
	srv, err := Start(Options{API: agentapi.New(newFake()), EndpointPath: path, AppVersion: "v1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	if !strings.HasPrefix(srv.URL(), "http://127.0.0.1:") {
		t.Fatalf("应绑定 127.0.0.1: %s", srv.URL())
	}
	if srv.Token() == "" {
		t.Fatal("token 不应为空")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("endpoint 文件未写入: %v", err)
	}
	var ep map[string]any
	if err := json.Unmarshal(raw, &ep); err != nil {
		t.Fatalf("endpoint JSON 非法: %v", err)
	}
	if ep["url"] != srv.URL() || ep["token"] != srv.Token() {
		t.Fatalf("endpoint 内容不符: %v", ep)
	}
	if pid, _ := ep["pid"].(float64); int(pid) != os.Getpid() {
		t.Fatalf("pid 不符: %v", ep["pid"])
	}
	if ep["app"] != "v1.2.3" {
		t.Fatalf("app 版本应写入 endpoint: %v", ep["app"])
	}

	// Close 后 endpoint 文件删除。
	srv.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("Close 后 endpoint 文件应删除")
	}
}

func TestAuthRequired(t *testing.T) {
	srv, _ := startTestSrv(t, newFake())
	resp, err := http.Get(srv.URL() + "/api/v1/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("无 token 应 401: %d", resp.StatusCode)
	}
	// 错 token 同样 401。
	req, _ := http.NewRequest(http.MethodGet, srv.URL()+"/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("错 token 应 401: %d", resp2.StatusCode)
	}
}

func TestListSessions(t *testing.T) {
	srv, _ := startTestSrv(t, newFake())
	code, body := get(t, srv, "/api/v1/sessions?scope=all")
	if code != 200 {
		t.Fatalf("list 应 200: %d %s", code, body)
	}
	var sessions []agentapi.Session
	if err := json.Unmarshal(body, &sessions); err != nil {
		t.Fatalf("JSON 解析: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("all 应 2 个: %+v", sessions)
	}
	// scope=archived：无归档 → 空。
	code, body = get(t, srv, "/api/v1/sessions?scope=archived")
	if code != 200 || strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("archived 应 200 空数组: %d %s", code, body)
	}
	// 非法 scope → 500 内部错误。
	if code, _ = get(t, srv, "/api/v1/sessions?scope=bogus"); code != 500 {
		t.Fatalf("非法 scope 应 500: %d", code)
	}
}

func TestInfoAndNotFound(t *testing.T) {
	srv, _ := startTestSrv(t, newFake())
	code, body := get(t, srv, "/api/v1/sessions/chat-a")
	if code != 200 {
		t.Fatalf("info 应 200: %d", code)
	}
	var wrap struct {
		Session agentapi.Session `json:"session"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil || wrap.Session.Title != "甲" {
		t.Fatalf("info 不符: %s", body)
	}
	if code, _ := get(t, srv, "/api/v1/sessions/nope"); code != 404 {
		t.Fatalf("未知会话应 404: %d", code)
	}
}

func TestMessagesAndLastFull(t *testing.T) {
	srv, _ := startTestSrv(t, newFake())
	code, body := get(t, srv, "/api/v1/sessions/chat-a/messages?tail=1")
	if code != 200 {
		t.Fatalf("messages 应 200: %d", code)
	}
	var msgs []agentapi.Message
	if err := json.Unmarshal(body, &msgs); err != nil {
		t.Fatalf("messages JSON: %v", err)
	}
	if len(msgs) != 1 || msgs[0].ID != 2 {
		t.Fatalf("tail=1 应只有最后一条: %+v", msgs)
	}
	code, body = get(t, srv, "/api/v1/sessions/chat-a/output/last-full")
	if code != 200 || !strings.Contains(string(body), `"text":"回复"`) {
		t.Fatalf("last-full 不符: %d %s", code, body)
	}
	if code, _ = get(t, srv, "/api/v1/sessions/sess-c/output/last-full"); code != 404 {
		t.Fatalf("无 agent 回复应 404: %d", code)
	}
}

func TestSendStopModelRecall(t *testing.T) {
	f := newFake()
	srv, _ := startTestSrv(t, f)
	code, _ := post(t, srv, "/api/v1/sessions/chat-a/send", `{"text":"你好"}`)
	if code != 200 || len(f.sent) != 1 || f.sent[0] != "chat-a|你好" {
		t.Fatalf("send 不符: %d %v", code, f.sent)
	}
	if code, _ = post(t, srv, "/api/v1/sessions/chat-a/stop", `{}`); code != 200 {
		t.Fatalf("stop: %d", code)
	}
	if code, _ = post(t, srv, "/api/v1/sessions/chat-a/model", `{"model":"m1"}`); code != 200 {
		t.Fatalf("model: %d", code)
	}
	if code, _ = post(t, srv, "/api/v1/recall", `{"msgID":42}`); code != 200 {
		t.Fatalf("recall: %d", code)
	}
	// 未激活会话 send → 404（agentapi 未激活守卫）。
	if code, _ := post(t, srv, "/api/v1/sessions/sess-c/send", `{"text":"x"}`); code != 404 {
		t.Fatalf("磁盘会话 send 应 404: %d", code)
	}
}

func TestModelsAndPermissions(t *testing.T) {
	srv, _ := startTestSrv(t, newFake())
	code, body := get(t, srv, "/api/v1/sessions/chat-a/models")
	if code != 200 || !strings.Contains(string(body), `"current":"m1"`) {
		t.Fatalf("models 不符: %d %s", code, body)
	}
	code, _ = post(t, srv, "/api/v1/sessions/chat-a/permissions/req-1", `{"allow":true}`)
	if code != 200 {
		t.Fatalf("grant: %d", code)
	}
	if code, _ = get(t, srv, "/api/v1/sessions/chat-a/permissions"); code != 200 {
		t.Fatalf("permissions: %d", code)
	}
}

func TestCreateOpenCreateNL(t *testing.T) {
	f := newFake()
	srv, api := startTestSrv(t, f)

	code, body := post(t, srv, "/api/v1/sessions/create", `{"tool":"claude","ws":"d:/ws/a","model":""}`)
	if code != 200 || !strings.Contains(string(body), `"id":"chat-new"`) {
		t.Fatalf("create 不符: %d %s", code, body)
	}
	code, _ = post(t, srv, "/api/v1/sessions/open", `{"sessionID":"sess-c"}`)
	if code != 200 {
		t.Fatalf("open: %d", code)
	}

	// create-nl：LLM 未配置 → 503 llm_not_ready。
	code, body = post(t, srv, "/api/v1/sessions/create-nl", `{"text":"建会话"}`)
	if code != 503 || !strings.Contains(string(body), "llm_not_ready") {
		t.Fatalf("未配置 LLM 应 503: %d %s", code, body)
	}

	// 本地 httptest 充当 LLM 端点，注入后走确认协议。
	llm := fakeLLM(t, `{"tool":"claude","workspace":"d:/ws/a","task":"修 bug","ready":true}`)
	defer llm.Close()
	api.UpdateLLM(hubtools.Config{BaseURL: llm.URL, APIKey: "k", Model: "m"})
	code, body = post(t, srv, "/api/v1/sessions/create-nl", `{"text":"建会话修 bug"}`)
	if code != 200 || !strings.Contains(string(body), `"created"`) {
		t.Fatalf("create-nl 成功路径: %d %s", code, body)
	}
	if len(f.sent) != 1 || f.sent[0] != "chat-new|修 bug" {
		t.Fatalf("首条任务应投递: %v", f.sent)
	}
}

func TestCreateNLNeedsConfirm(t *testing.T) {
	f := newFake()
	srv, api := startTestSrv(t, f)
	llm := fakeLLM(t, `{"tool":"unknown","workspace":"","ready":false,"question":"用什么工具？"}`)
	defer llm.Close()
	api.UpdateLLM(hubtools.Config{BaseURL: llm.URL, APIKey: "k", Model: "m"})

	code, body := post(t, srv, "/api/v1/sessions/create-nl", `{"text":"建会话"}`)
	if code != 200 || !strings.Contains(string(body), `"needsConfirm"`) {
		t.Fatalf("NeedsConfirm 应 200 返回结果: %d %s", code, body)
	}
	// 确认重发：跳过 LLM（换一个会报耗尽的 LLM 端点验证确实没走网络）。
	llm2 := fakeLLM(t, "")
	defer llm2.Close()
	api.UpdateLLM(hubtools.Config{BaseURL: llm2.URL, APIKey: "k", Model: "m"})
	code, body = post(t, srv, "/api/v1/sessions/create-nl",
		`{"text":"","confirm":{"toolID":"claude","workspace":"d:/ws/a","task":"修 bug"}}`)
	if code != 200 || !strings.Contains(string(body), `"created"`) {
		t.Fatalf("confirm 路径: %d %s", code, body)
	}
}

func TestRouteDecideAndSend(t *testing.T) {
	f := newFake()
	srv, api := startTestSrv(t, f)
	llm := fakeLLM(t, `{"action":"single","targets":[1],"text":"通用","perTarget":{"1":"改写"}}`)
	defer llm.Close()
	api.UpdateLLM(hubtools.Config{BaseURL: llm.URL, APIKey: "k", Model: "m"})

	code, body := post(t, srv, "/api/v1/route/decide", `{"text":"通知"}`)
	if code != 200 || !strings.Contains(string(body), `"action":"single"`) {
		t.Fatalf("route decide: %d %s", code, body)
	}
	// 携带决策投递：decision + sessions 数组（decide 的返回原样回传）。
	dec, _ := json.Marshal(hubtools.RouteDecision{
		Action:        "single",
		Targets:       []int{1},
		Text:          "通用",
		PerTargetText: map[string]string{"1": "改写"},
	})
	plan, _ := json.Marshal(map[string]any{"decision": json.RawMessage(dec), "sessions": f.active})
	code, _ = post(t, srv, "/api/v1/route/send", string(plan))
	if code != 200 || len(f.sent) != 1 || f.sent[0] != "chat-a|改写" {
		t.Fatalf("route send: %d %v", code, f.sent)
	}
	// ask 决策：decide 仍 200（CLI 层翻译 exit 2）。
}

func TestEventsStream(t *testing.T) {
	f := newFake()
	srv, _ := startTestSrv(t, f)

	// 订阅后从 fake 的 Feed 推两帧，流里应各收到一行 NDJSON。
	req, _ := http.NewRequest(http.MethodGet, srv.URL()+"/api/v1/sessions/chat-a/events", nil)
	req.Header.Set("Authorization", "Bearer "+srv.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("events 应 200: %d", resp.StatusCode)
	}
	// 未知会话流应 404（订阅前校验）。
	if code, _ := get(t, srv, "/api/v1/sessions/nope/events"); code != 404 {
		t.Fatalf("未知会话 events 应 404: %d", code)
	}

	f.Feed().Publish(agentapi.Event{SessionID: "chat-a", Kind: agentapi.EventUpdate, Type: "assistant", Text: "帧1"})
	f.Feed().Publish(agentapi.Event{SessionID: "chat-a", Kind: agentapi.EventUpdate, Type: "assistant", Text: "帧2"})

	// 第一行是握手帧（让客户端拿到响应头），之后是两帧事件。
	scanner := bufio.NewScanner(resp.Body)
	var first agentapi.Event
	events := 0
	for scanner.Scan() {
		var e agentapi.Event
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("事件行非法: %s", scanner.Bytes())
		}
		if first.Kind == "" {
			first = e
			continue
		}
		events++
		if events == 2 {
			break
		}
	}
	if first.Kind != agentapi.EventOpen || first.SessionID != "chat-a" {
		t.Fatalf("握手帧不符: %+v", first)
	}
	if events != 2 {
		t.Fatalf("应收到 2 帧事件: %d", events)
	}
}

func TestLLMNotReadyOnRoute(t *testing.T) {
	srv, _ := startTestSrv(t, newFake())
	code, body := post(t, srv, "/api/v1/route/decide", `{"text":"x"}`)
	if code != 503 || !strings.Contains(string(body), "llm_not_ready") {
		t.Fatalf("route 未配置 LLM 应 503: %d %s", code, body)
	}
}

// fakeLLM 起一个本地 OpenAI 兼容假端点：按序回复 canned JSON。
func fakeLLM(t *testing.T, replies ...string) *httptest.Server {
	t.Helper()
	i := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if i >= len(replies) {
			http.Error(w, "回复耗尽", http.StatusInternalServerError)
			return
		}
		rpl := replies[i]
		i++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": rpl}}},
		})
	}))
}
