package agentapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/hubtools"
)

func TestCreateOpenForward(t *testing.T) {
	f := newFake()
	api := New(f)
	s, err := api.Create("claude", "d:/ws/a", "sonnet")
	if err != nil || s.ID != "chat-new" || f.created[0] != [3]string{"claude", "d:/ws/a", "sonnet"} {
		t.Fatalf("Create: %+v err=%v", s, err)
	}
	s, err = api.Open("sess-c")
	if err != nil || s.SessionID != "sess-c" || f.opened[0] != "sess-c" {
		t.Fatalf("Open: %+v err=%v", s, err)
	}
}

// mockChat 返回一个注入的假 LLM：按输入序列依次回复。
func mockChat(api *API, replies ...string) {
	i := 0
	api.setChatFn(func(ctx context.Context, msgs []hubtools.Message) (hubtools.Response, error) {
		if i >= len(replies) {
			return hubtools.Response{}, fmt.Errorf("mock 回复耗尽")
		}
		r := replies[i]
		i++
		return hubtools.Response{Text: r}, nil
	})
}

func TestCreateNLNotReady(t *testing.T) {
	api := New(newFake())
	_, err := api.CreateNL("建个会话", NLCreate{})
	if !errors.Is(err, ErrLLMNotReady) {
		t.Fatalf("期望 ErrLLMNotReady: %v", err)
	}
}

func TestCreateNLLLMNotReady(t *testing.T) {
	api := New(newFake())
	api.UpdateLLM(hubtools.Config{Model: "m"}) // 缺端点/密钥：Ready 判不就绪
	_, err := api.CreateNL("建个会话", NLCreate{})
	if !errors.Is(err, ErrLLMNotReady) {
		t.Fatalf("期望 ErrLLMNotReady: %v", err)
	}
}

func TestCreateNLToolAmbiguous(t *testing.T) {
	api := New(newFake())
	api.UpdateLLM(hubtools.Config{BaseURL: "http://x", APIKey: "k", Model: "m"})
	// 模型给了不存在的工具。
	mockChat(api, `{"tool":"unknown","workspace":"d:/ws/a","task":"","ready":true}`)
	res, err := api.CreateNL("建个会话", NLCreate{})
	if err != nil {
		t.Fatalf("不应报错而应返回确认: %v", err)
	}
	if res.NeedsConfirm == nil || res.Created != nil {
		t.Fatalf("期望 NeedsConfirm: %+v", res)
	}
	if len(res.NeedsConfirm.ToolOptions) != 2 {
		t.Fatalf("应带工具候选: %+v", res.NeedsConfirm)
	}
}

func TestCreateNLWorkspaceAmbiguous(t *testing.T) {
	f := newFake()
	f.workspaces = append(f.workspaces, Option{ID: "d:/ws/a2", Label: "a2"}) // 模糊命中 "a" 时多候选
	api := New(f)
	api.UpdateLLM(hubtools.Config{BaseURL: "http://x", APIKey: "k", Model: "m"})
	mockChat(api, `{"tool":"claude","workspace":"","task":"跑测试","ready":false}`)
	res, err := api.CreateNL("在 a 建会话跑测试", NLCreate{})
	if err != nil {
		t.Fatalf("CreateNL: %v", err)
	}
	if res.NeedsConfirm == nil || len(res.NeedsConfirm.WorkspaceOptions) != 3 {
		t.Fatalf("工作区歧义应返回候选: %+v", res.NeedsConfirm)
	}
}

func TestCreateNLSuccess(t *testing.T) {
	f := newFake()
	api := New(f)
	api.UpdateLLM(hubtools.Config{BaseURL: "http://x", APIKey: "k", Model: "m"})
	mockChat(api, `{"tool":"claude","workspace":"d:/ws/a","task":"修复登录 bug","ready":true}`)
	res, err := api.CreateNL("在 a 工作区用 claude 建会话修复登录 bug", NLCreate{})
	if err != nil {
		t.Fatalf("CreateNL: %v", err)
	}
	if res.Created == nil || res.NeedsConfirm != nil {
		t.Fatalf("期望创建成功: %+v", res)
	}
	if res.Created.ID != "chat-new" {
		t.Fatalf("Created 不符: %+v", res.Created)
	}
	// task 非空应创建后即投递。
	if len(f.sent) != 1 || f.sent[0] != "chat-new|修复登录 bug" {
		t.Fatalf("首条任务应投递: %v", f.sent)
	}
}

func TestCreateNLConfirmPath(t *testing.T) {
	f := newFake()
	api := New(f)
	api.UpdateLLM(hubtools.Config{BaseURL: "http://x", APIKey: "k", Model: "m"})
	// Confirm 携带用户选择：不再走 LLM（mock 回复耗尽即失败，若走了会报错）。
	mockChat(api)
	res, err := api.CreateNL("随便", NLCreate{Confirm: &CreatePlan{
		ToolID: "codex", Workspace: "d:/ws/b", Task: "写文档",
	}})
	if err != nil {
		t.Fatalf("Confirm 路径不应调 LLM: %v", err)
	}
	if res.Created == nil || f.created[0] != [3]string{"codex", "d:/ws/b", ""} {
		t.Fatalf("Confirm 创建失败: %+v %+v", res.Created, f.created)
	}
	if len(f.sent) != 1 || f.sent[0] != "chat-new|写文档" {
		t.Fatalf("Confirm task 应投递: %v", f.sent)
	}
}

func TestCreateNLBadJSON(t *testing.T) {
	api := New(newFake())
	api.UpdateLLM(hubtools.Config{BaseURL: "http://x", APIKey: "k", Model: "m"})
	mockChat(api, `完全不是 JSON`)
	if _, err := api.CreateNL("建会话", NLCreate{}); err == nil {
		t.Fatal("坏 JSON 应报错")
	}
}

func TestRouteDecide(t *testing.T) {
	f := newFake()
	api := New(f)
	api.UpdateLLM(hubtools.Config{BaseURL: "http://x", APIKey: "k", Model: "m"})
	// 编号 1=chat-a, 2=chat-b（ActiveSessions 顺序）。
	mockChat(api, `{"action":"group","targets":[1,2],"text":"全体注意"}`)
	plan, err := api.RouteDecide("通知所有人")
	if err != nil {
		t.Fatalf("RouteDecide: %v", err)
	}
	if plan.Action != "group" || len(plan.Sessions) != 2 {
		t.Fatalf("plan 不符: %+v", plan)
	}
	if plan.Sessions[0].ID != "chat-a" || plan.Sessions[1].ID != "chat-b" {
		t.Fatalf("编号翻译失败: %+v", plan.Sessions)
	}
}

func TestRouteDecideNotReady(t *testing.T) {
	api := New(newFake())
	if _, err := api.RouteDecide("x"); !errors.Is(err, ErrLLMNotReady) {
		t.Fatalf("期望 ErrLLMNotReady: %v", err)
	}
}

func TestRouteSend(t *testing.T) {
	f := newFake()
	api := New(f)
	plan := RoutePlan{
		RouteDecision: hubtools.RouteDecision{
			Action:        "group",
			Targets:       []int{1, 2},
			Text:          "通用文本",
			PerTargetText: map[string]string{"2": "给 2 号的改写"},
		},
	}
	plan.Sessions = []Session{f.active[0], f.active[1]}
	if err := api.RouteSend(plan); err != nil {
		t.Fatalf("RouteSend: %v", err)
	}
	if len(f.sent) != 2 {
		t.Fatalf("应投递 2 条: %v", f.sent)
	}
	if f.sent[0] != "chat-a|通用文本" {
		t.Fatalf("1 号应投通用文本: %v", f.sent)
	}
	if f.sent[1] != "chat-b|给 2 号的改写" {
		t.Fatalf("2 号应投改写文本: %v", f.sent)
	}
	// ask/status/expand 不可投递。
	plan.Action = "ask"
	if err := api.RouteSend(plan); err == nil || !strings.Contains(err.Error(), "ask") {
		t.Fatalf("ask 不可 RouteSend: %v", err)
	}
}
