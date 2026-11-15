package agentcli_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/agentapi"
	"github.com/yangk/kshell/internal/agentapi/apitest"
	"github.com/yangk/kshell/internal/agentcli"
	"github.com/yangk/kshell/internal/agentsrv"
	"github.com/yangk/kshell/internal/hubtools"
)

// cliHarness 起真实 agentsrv 并提供注入连接的 run 辅助。
type cliHarness struct {
	t         *testing.T
	fake      *apitest.FakeBackend
	api       *agentapi.API
	client    *agentcli.Client // connectFn 返回的同一实例，供用例注入 Client 侧状态
	connectFn func() (*agentcli.Client, error)
	out       bytes.Buffer
	errb      bytes.Buffer
}

func newCliHarness(t *testing.T) *cliHarness {
	t.Helper()
	fake := apitest.NewFake()
	api := agentapi.New(fake)
	srv, err := agentsrv.Start(agentsrv.Options{
		API:          api,
		EndpointPath: filepath.Join(t.TempDir(), "agentapi", "endpoint.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	client := agentcli.NewClient(srv.URL(), srv.Token())
	return &cliHarness{
		t:    t,
		fake: fake,
		api:  api,
		client: client,
		connectFn: func() (*agentcli.Client, error) {
			return client, nil
		},
	}
}

// run 执行一条命令，返回退出码。
func (h *cliHarness) run(args ...string) int {
	h.out.Reset()
	h.errb.Reset()
	return agentcli.Run(args, &h.out, &h.errb, h.connectFn)
}

func (h *cliHarness) stdout() string { return h.out.String() }
func (h *cliHarness) stderr() string { return h.errb.String() }

// setHarnessLLM 把 harness 服务的 LLM 配置指向给定端点。
func (h *cliHarness) setHarnessLLM(baseURL string) {
	h.api.UpdateLLM(hubtools.Config{BaseURL: baseURL, APIKey: "k", Model: "m"})
}

func TestCLIListJSON(t *testing.T) {
	h := newCliHarness(t)
	code := h.run("list", "--json")
	if code != 0 {
		t.Fatalf("list 退出码 %d stderr=%s", code, h.stderr())
	}
	var sessions []agentapi.Session
	if err := json.Unmarshal([]byte(h.stdout()), &sessions); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("all 应 2 个: %d", len(sessions))
	}
}

func TestCLIListTable(t *testing.T) {
	h := newCliHarness(t)
	code := h.run("list", "--scope", "active")
	if code != 0 {
		t.Fatalf("退出码 %d stderr=%s", code, h.stderr())
	}
	if !strings.Contains(h.stdout(), "甲") || !strings.Contains(h.stdout(), "running") {
		t.Fatalf("表格应含标题与状态: %s", h.stdout())
	}
}

func TestCLIOutputLast(t *testing.T) {
	h := newCliHarness(t)
	code := h.run("output", "--last", "chat-a")
	if code != 0 || strings.TrimSpace(h.stdout()) != "回复" {
		t.Fatalf("--last 输出: %q code=%d", h.stdout(), code)
	}
	code = h.run("output", "--tail", "1", "chat-a")
	if code != 0 {
		t.Fatalf("output: %d", code)
	}
	lines := strings.Split(strings.TrimSpace(h.stdout()), "\n")
	var m agentapi.Message
	if err := json.Unmarshal([]byte(lines[0]), &m); err != nil || m.ID != 2 {
		t.Fatalf("NDJSON: %s", h.stdout())
	}
}

func TestCLISendStop(t *testing.T) {
	h := newCliHarness(t)
	if code := h.run("send", "chat-a", "跑测试"); code != 0 {
		t.Fatalf("send: %d %s", code, h.stderr())
	}
	if len(h.fake.Sent) != 1 || h.fake.Sent[0] != "chat-a|跑测试" {
		t.Fatalf("send 未达: %v", h.fake.Sent)
	}
	if code := h.run("stop", "chat-a"); code != 0 || len(h.fake.Stopped) != 1 {
		t.Fatalf("stop: %d", code)
	}
	// 未激活会话 → exit 1。
	if code := h.run("send", "sess-c", "x"); code != 1 {
		t.Fatalf("未激活 send 应 exit 1: %d", code)
	}
}

func TestCLINewNeedsConfirmAndConfirm(t *testing.T) {
	h := newCliHarness(t)
	// LLM 未配置 → exit 3。
	if code := h.run("new", "建会话"); code != 3 {
		t.Fatalf("LLM 未配置应 exit 3: %d %s", code, h.stderr())
	}
	// 歧义 → exit 2 + needsConfirm JSON。
	llm := startFakeLLM(t, `{"tool":"","workspace":"","question":"用什么工具？","ready":false}`)
	defer llm.Close()
	h.setHarnessLLM(llm.URL)
	code := h.run("new", "建会话")
	if code != 2 || !strings.Contains(h.stdout(), "needsConfirm") {
		t.Fatalf("needsConfirm 应 exit 2: %d %s", code, h.stdout())
	}
	// 确认重发（跳过 LLM）→ exit 0 + created。
	plan := `{"toolID":"claude","workspace":"d:/ws/a","task":"修 bug"}`
	code = h.run("new", "--confirm", plan)
	if code != 0 || !strings.Contains(h.stdout(), `"created"`) {
		t.Fatalf("confirm 应 exit 0: %d %s", code, h.stdout())
	}
	if len(h.fake.Sent) != 1 || h.fake.Sent[0] != "chat-new|修 bug" {
		t.Fatalf("首条任务应投递: %v", h.fake.Sent)
	}
}

func TestCLIRouteAskAndSend(t *testing.T) {
	h := newCliHarness(t)
	// LLM 未配置 → exit 3。
	if code := h.run("route", "通知"); code != 3 {
		t.Fatalf("route 未就绪应 exit 3: %d", code)
	}
	// ask → exit 2。
	llmAsk := startFakeLLM(t, `{"action":"ask","question":"哪个会话？"}`)
	defer llmAsk.Close()
	h.setHarnessLLM(llmAsk.URL)
	if code := h.run("route", "发这个"); code != 2 {
		t.Fatalf("ask 应 exit 2: %d", code)
	}

	// --send：single 直接投递。
	llm := startFakeLLM(t, `{"action":"single","targets":[1],"text":"去修"}`)
	defer llm.Close()
	h.setHarnessLLM(llm.URL)
	if code := h.run("route", "--send", "去修"); code != 0 {
		t.Fatalf("route --send: %d %s", code, h.stderr())
	}
	if len(h.fake.Sent) != 1 || h.fake.Sent[0] != "chat-a|去修" {
		t.Fatalf("投递未达: %v", h.fake.Sent)
	}
}

func TestCLIInfoNotFound(t *testing.T) {
	h := newCliHarness(t)
	if code := h.run("info", "chat-a"); code != 0 {
		t.Fatalf("info: %d", code)
	}
	if code := h.run("info", "nope"); code != 1 {
		t.Fatalf("未知会话应 exit 1: %d", code)
	}
}

func TestCLINotReachable(t *testing.T) {
	h := newCliHarness(t)
	h.connectFn = func() (*agentcli.Client, error) {
		return nil, agentcli.ErrNotReachable
	}
	if code := h.run("list"); code != 3 {
		t.Fatalf("不可达应 exit 3: %d", code)
	}
	if !strings.Contains(h.stderr(), "请先启动 kshell 桌面端") {
		t.Fatalf("应提示启动桌面端: %s", h.stderr())
	}
}
