package agentcli_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/agentapi"
	"github.com/yangk/kshell/internal/agentapi/apitest"
	"github.com/yangk/kshell/internal/agentcli"
	"github.com/yangk/kshell/internal/agentsrv"
	"github.com/yangk/kshell/internal/hubtools"
)

// newTestClient 起真实 agentsrv（fake Backend）并返回客户端与桩。
func newTestClient(t *testing.T) (*agentcli.Client, *apitest.FakeBackend) {
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
	return agentcli.NewClient(srv.URL(), srv.Token()), fake
}

func TestClientListSearchInfo(t *testing.T) {
	c, _ := newTestClient(t)
	all, err := c.List("all")
	if err != nil || len(all) != 2 {
		t.Fatalf("List all: %d err=%v", len(all), err)
	}
	active, _ := c.List("active")
	if len(active) != 1 || active[0].ID != "chat-a" {
		t.Fatalf("List active: %+v", active)
	}
	got, err := c.Search("丙")
	if err != nil || len(got) != 1 || got[0].SessionID != "sess-c" {
		t.Fatalf("Search: %+v err=%v", got, err)
	}
	s, ok, err := c.Info("chat-a")
	if err != nil || !ok || s.Title != "甲" {
		t.Fatalf("Info: %+v ok=%v err=%v", s, ok, err)
	}
	if _, ok, _ := c.Info("nope"); ok {
		t.Fatal("未知会话 ok 应为 false")
	}
}

func TestClientMessagesLastFull(t *testing.T) {
	c, _ := newTestClient(t)
	msgs, err := c.Messages("chat-a", 1, 0)
	if err != nil || len(msgs) != 1 || msgs[0].ID != 2 {
		t.Fatalf("Messages tail: %+v err=%v", msgs, err)
	}
	msgs, err = c.Messages("chat-a", 0, 150)
	if err != nil || len(msgs) != 1 || msgs[0].Ts != 200 {
		t.Fatalf("Messages since: %+v", msgs)
	}
	m, err := c.LastFullOutput("chat-a")
	if err != nil || m.Text != "回复" {
		t.Fatalf("LastFullOutput: %+v err=%v", m, err)
	}
	// 磁盘会话无 agent 回复 → ErrNotFound 哨兵穿透到客户端，且显示文本干净
	//（无双层前缀，服务端 message 剥掉 agentapi: 前缀）。
	if _, err = c.LastFullOutput("sess-c"); !errors.Is(err, agentapi.ErrNotFound) {
		t.Fatalf("期望 ErrNotFound: %v", err)
	}
	var apiErr *agentcli.APIError
	if !errors.As(err, &apiErr) || !strings.Contains(apiErr.Message, "会话无 agent 回复") {
		t.Fatalf("期望 APIError 携带干净消息: %+v", apiErr)
	}
	if strings.Contains(err.Error(), "agentapi:") {
		t.Fatalf("显示文本不应含哨兵前缀: %q", err.Error())
	}
}

func TestClientControl(t *testing.T) {
	c, fake := newTestClient(t)
	if err := c.Send("chat-a", "你好"); err != nil || fake.Sent[0] != "chat-a|你好" {
		t.Fatalf("Send: %v %v", err, fake.Sent)
	}
	if err := c.Stop("chat-a"); err != nil || len(fake.Stopped) != 1 {
		t.Fatalf("Stop: %v", err)
	}
	if err := c.Recall(42); err != nil || len(fake.Recalled) != 1 {
		t.Fatalf("Recall: %v", err)
	}
	models, cur, err := c.Models("chat-a")
	if err != nil || cur != "m1" || len(models) != 1 {
		t.Fatalf("Models: %v", err)
	}
	if err := c.SetModel("chat-a", "m2"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	if err := c.GrantPermission("chat-a", "req-1", true); err != nil {
		t.Fatalf("GrantPermission: %v", err)
	}
	perms, err := c.PendingPermissions("chat-a")
	if err != nil || len(perms) != 0 {
		t.Fatalf("PendingPermissions: %v", err)
	}
	// 未激活会话 → ErrNotFound 穿透。
	if err := c.Send("sess-c", "x"); !errors.Is(err, agentapi.ErrNotFound) {
		t.Fatalf("未激活 Send 期望 ErrNotFound: %v", err)
	}
}

func TestClientCreateOpen(t *testing.T) {
	c, fake := newTestClient(t)
	s, err := c.Create("claude", "d:/ws/a", "")
	if err != nil || s.ID != "chat-new" || fake.Created[0][0] != "claude" {
		t.Fatalf("Create: %+v err=%v", s, err)
	}
	s, err = c.Open("sess-c")
	if err != nil || s.SessionID != "sess-c" {
		t.Fatalf("Open: %+v err=%v", s, err)
	}
}

func TestClientCreateNLAndRoute(t *testing.T) {
	c, _ := newTestClient(t)

	// LLM 未配置 → ErrLLMNotReady 穿透。
	if _, err := c.CreateNL(context.Background(), "建会话", nil); !errors.Is(err, agentapi.ErrLLMNotReady) {
		t.Fatalf("期望 ErrLLMNotReady: %v", err)
	}

	// 起一个注入了 LLM 配置的服务（端点为本地假 LLM）。
	llm := startFakeLLM(t, `{"tool":"claude","workspace":"d:/ws/a","task":"修 bug","ready":true}`)
	defer llm.Close()
	fake2 := apitest.NewFake()
	api2 := agentapi.New(fake2)
	srv2, err := agentsrv.Start(agentsrv.Options{API: api2, EndpointPath: filepath.Join(t.TempDir(), "ep2.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer srv2.Close()
	api2.UpdateLLM(hubtools.Config{BaseURL: llm.URL, APIKey: "k", Model: "m"})

	c2 := agentcli.NewClient(srv2.URL(), srv2.Token())
	res, err := c2.CreateNL(context.Background(), "建会话修 bug", nil)
	if err != nil || res.Created == nil || res.Created.ID != "chat-new" {
		t.Fatalf("CreateNL: %+v err=%v", res, err)
	}
	if len(fake2.Sent) != 1 || fake2.Sent[0] != "chat-new|修 bug" {
		t.Fatalf("首条任务应投递: %v", fake2.Sent)
	}

	// 路由：decide + send 往返。
	llm2 := startFakeLLM(t, `{"action":"single","targets":[1],"text":"hi"}`)
	defer llm2.Close()
	api2.UpdateLLM(hubtools.Config{BaseURL: llm2.URL, APIKey: "k", Model: "m"})
	plan, err := c2.RouteDecide(context.Background(), "通知")
	if err != nil || plan.Action != "single" || len(plan.Sessions) != 1 {
		t.Fatalf("RouteDecide: %+v err=%v", plan, err)
	}
	if err := c2.RouteSend(plan); err != nil || len(fake2.Sent) != 2 {
		t.Fatalf("RouteSend: %v %v", err, fake2.Sent)
	}
}

func TestClientWatch(t *testing.T) {
	fake := apitest.NewFake()
	api := agentapi.New(fake)
	srv, err := agentsrv.Start(agentsrv.Options{API: api, EndpointPath: filepath.Join(t.TempDir(), "ep.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	c := agentcli.NewClient(srv.URL(), srv.Token())

	ch, cancel, err := c.Watch("chat-a")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	// 首帧 open 握手。
	select {
	case e := <-ch:
		if e.Kind != agentapi.EventOpen {
			t.Fatalf("首帧应为 open: %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("未收到握手帧")
	}
	fake.Feed().Publish(agentapi.Event{SessionID: "chat-a", Kind: agentapi.EventUpdate, Type: "assistant", Text: "增量"})
	select {
	case e := <-ch:
		if e.Text != "增量" {
			t.Fatalf("事件不符: %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("未收到事件")
	}
	// 未知会话 → ErrNotFound。
	if _, _, err = c.Watch("nope"); !errors.Is(err, agentapi.ErrNotFound) {
		t.Fatalf("Watch 未知会话期望 ErrNotFound: %v", err)
	}
}

func TestLoadEndpointAndNotReachable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "endpoint.json")
	// 文件缺失 → ErrNotReachable。
	if _, err := agentcli.LoadEndpoint(path); !agentcli.IsNotReachable(err) {
		t.Fatalf("缺失文件期望 ErrNotReachable: %v", err)
	}
	// 写入合法文件。
	ep := agentcli.Endpoint{URL: "http://127.0.0.1:1", Token: "t", PID: 1, Version: 1}
	raw, _ := json.Marshal(ep)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := agentcli.LoadEndpoint(path)
	if err != nil || got.URL != ep.URL {
		t.Fatalf("LoadEndpoint: %+v err=%v", got, err)
	}
	// 版本不符 → ErrNotReachable。
	ep.Version = 99
	raw, _ = json.Marshal(ep)
	os.WriteFile(path, raw, 0o600)
	if _, err := agentcli.LoadEndpoint(path); !agentcli.IsNotReachable(err) {
		t.Fatalf("版本不符期望 ErrNotReachable: %v", err)
	}
	// 连不上的地址 → ErrNotReachable。
	c := agentcli.NewClient("http://127.0.0.1:1", "t")
	if _, err := c.List("all"); !agentcli.IsNotReachable(err) {
		t.Fatalf("连接失败期望 ErrNotReachable: %v", err)
	}
}

// startFakeLLM 起一个 OpenAI 兼容假端点（单次回复）。
func startFakeLLM(t *testing.T, reply string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": reply}}},
		})
	}))
}
