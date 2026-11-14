package agentapi

import (
	"errors"
	"testing"
)

func TestListScopes(t *testing.T) {
	api := New(newFake())

	active, err := api.List(ScopeActive)
	if err != nil || len(active) != 2 {
		t.Fatalf("active 期望 2 个，得到 %d (err=%v)", len(active), err)
	}
	disk, err := api.List(ScopeDisk)
	if err != nil || len(disk) != 2 {
		t.Fatalf("disk 期望 2 个，得到 %d (err=%v)", len(disk), err)
	}
	// archived：sess-d 已归档（磁盘），活跃会话无归档。
	arch, err := api.List(ScopeArchived)
	if err != nil || len(arch) != 1 || arch[0].SessionID != "sess-d" {
		t.Fatalf("archived 期望 [sess-d]，得到 %+v (err=%v)", arch, err)
	}
	if arch[0].Status != StatusArchived {
		t.Fatalf("archived 会话状态应为 archived: %+v", arch[0])
	}
	all, err := api.List(ScopeAll)
	if err != nil || len(all) != 4 {
		t.Fatalf("all 期望 4 个，得到 %d (err=%v)", len(all), err)
	}
}

func TestListUnknownScope(t *testing.T) {
	api := New(newFake())
	if _, err := api.List("bogus"); err == nil {
		t.Fatal("未知 scope 应报错")
	}
}

func TestResolve(t *testing.T) {
	api := New(newFake())

	// 按 chatID 命中活跃。
	s, err := api.resolve("chat-a")
	if err != nil || s.ID != "chat-a" {
		t.Fatalf("chatID 解析失败: %+v err=%v", s, err)
	}
	// 按 SessionID 命中活跃（归一到同一个会话）。
	s, err = api.resolve("sess-a")
	if err != nil || s.ID != "chat-a" {
		t.Fatalf("SessionID 应归一到活跃 chat: %+v err=%v", s, err)
	}
	// 磁盘会话按 SessionID。
	s, err = api.resolve("sess-c")
	if err != nil || s.ID != "" || s.SessionID != "sess-c" {
		t.Fatalf("磁盘会话解析失败: %+v err=%v", s, err)
	}
	// 未知 id。
	if _, err = api.resolve("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("未知 id 期望 ErrNotFound，得到 %v", err)
	}
}

func TestInfo(t *testing.T) {
	api := New(newFake())
	s, ok := api.Info("chat-b")
	if !ok || s.Title != "写文档" || s.Status != StatusPerm {
		t.Fatalf("Info 失败: %+v ok=%v", s, ok)
	}
	if _, ok := api.Info("nope"); ok {
		t.Fatal("未知会话 ok 应为 false")
	}
}

func TestSearch(t *testing.T) {
	api := New(newFake())
	got := api.Search("登录")
	if len(got) != 1 || got[0].ID != "chat-a" {
		t.Fatalf("按标题搜索失败: %+v", got)
	}
	got = api.Search("d:/ws/b")
	if len(got) != 1 || got[0].ID != "chat-b" {
		t.Fatalf("按工作区搜索失败: %+v", got)
	}
	got = api.Search("GEMINI")
	if len(got) != 1 || got[0].SessionID != "sess-c" {
		t.Fatalf("大小写不敏感搜索失败: %+v", got)
	}
	if n := len(api.Search("")); n != 0 {
		t.Fatal("空关键字应返回空")
	}
}

func TestMessages(t *testing.T) {
	f := newFake()
	f.msgs["chat-a"] = []Message{
		{ID: 1, Kind: "user", Text: "开始", Ts: 100},
		{ID: 2, Kind: "agent", Text: "回复一", Ts: 200},
		{ID: 3, Kind: "user", Text: "继续", Ts: 300},
		{ID: 4, Kind: "agent", Text: "回复二", Ts: 400},
	}
	api := New(f)

	all, err := api.Messages("chat-a", MessageQuery{})
	if err != nil || len(all) != 4 {
		t.Fatalf("全量期望 4 条: %d err=%v", len(all), err)
	}
	// since 水位。
	got, _ := api.Messages("chat-a", MessageQuery{Since: 200})
	if len(got) != 2 || got[0].ID != 3 {
		t.Fatalf("since 过滤失败: %+v", got)
	}
	// tail 截尾。
	got, _ = api.Messages("chat-a", MessageQuery{Tail: 2})
	if len(got) != 2 || got[0].ID != 3 {
		t.Fatalf("tail 截尾失败: %+v", got)
	}
	// 磁盘未激活会话：无 hub 消息，返回空而非错误。
	got, err = api.Messages("sess-c", MessageQuery{})
	if err != nil || len(got) != 0 {
		t.Fatalf("磁盘会话期望空切片: %+v err=%v", got, err)
	}
	if _, err = api.Messages("nope", MessageQuery{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("未知会话期望 ErrNotFound: %v", err)
	}
}

func TestLastFullOutput(t *testing.T) {
	f := newFake()
	f.msgs["chat-a"] = []Message{
		{ID: 1, Kind: "user", Text: "开始", Ts: 100},
		{ID: 2, Kind: "agent", Text: "第一轮完整回复", Ts: 200},
		{ID: 3, Kind: "user", Text: "继续", Ts: 300},
		{ID: 4, Kind: "agent", Text: "第二轮完整回复", Ts: 400},
		{ID: 5, Kind: "user", Text: "再来", Ts: 500},
	}
	api := New(f)
	m, err := api.LastFullOutput("chat-a")
	if err != nil || m.ID != 4 || m.Text != "第二轮完整回复" {
		t.Fatalf("LastFullOutput 失败: %+v err=%v", m, err)
	}
	// 无 agent 消息。
	f.msgs["chat-b"] = []Message{{ID: 9, Kind: "user", Text: "hi", Ts: 1}}
	if _, err = api.LastFullOutput("chat-b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("无 agent 消息期望 ErrNotFound: %v", err)
	}
}
