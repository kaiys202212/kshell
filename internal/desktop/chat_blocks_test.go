package desktop

// 模型切换 / 技能命令推送 / 多块 Prompt 的绑定层行为测试。

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/acp"
	"github.com/yangk/kshell/internal/chat"
)

// TestChatBlockToACP 覆盖四种形态的转换，保证 wire 字段不丢。
func TestChatBlockToACP(t *testing.T) {
	text := chatBlockToACP(ChatBlock{Type: "text", Text: "hi"})
	if text.Type != "text" || text.Text != "hi" {
		t.Fatalf("text = %+v", text)
	}
	image := chatBlockToACP(ChatBlock{Type: "image", Data: "AAAA", MimeType: "image/png"})
	if image.Type != "image" || image.Data != "AAAA" || image.MimeType != "image/png" {
		t.Fatalf("image = %+v", image)
	}
	link := chatBlockToACP(ChatBlock{Type: "resource_link", URI: "file:///C:/a.txt", Name: "a.txt"})
	if link.Type != "resource_link" || link.URI != "file:///C:/a.txt" || link.Name != "a.txt" {
		t.Fatalf("resource_link = %+v", link)
	}
	res := chatBlockToACP(ChatBlock{Type: "resource", URI: "file:///a.txt", MimeType: "text/plain", Data: "QQ=="})
	if res.Type != "resource" || res.Resource == nil || res.Resource.URI != "file:///a.txt" || res.Resource.Blob != "QQ==" || res.Resource.MimeType != "text/plain" {
		t.Fatalf("resource = %+v", res)
	}
}

// TestSendChatPromptBlocksForwards 验证 SendChatPromptBlocks 走 manager 并落时间线。
func TestSendChatPromptBlocksForwards(t *testing.T) {
	m := newChatManagerWith(func(string, ...any) {}, fakeChatBackend{}, nil, nil, nil)
	t.Cleanup(m.CloseAll)
	info, err := m.Open("new:1", chat.Info{Kind: chat.KindNew, Workspace: "/w"}, chat.Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	blocks := []ChatBlock{
		{Type: "text", Text: "看这个"},
		{Type: "image", Data: "AA", MimeType: "image/png"},
	}
	if err := m.PromptBlocks(info.ID, chatBlocksToACP(blocks)); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		found := false
		for _, u := range m.History(info.ID) {
			if u.Type == "user" && u.Text == "看这个" && len(u.Attachments) == 1 {
				found = true
			}
		}
		if found {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("blocks not recorded in timeline")
}

// acpSessionWithModels 构造带 v1 models 的 SessionStart（fake 连接返回）。
func acpSessionWithModels() acp.SessionStart {
	return acp.SessionStart{SessionID: "s1", Models: &acp.SessionModelState{
		AvailableModels: []acp.ModelInfo{{ModelID: "m1", Name: "One"}, {ModelID: "m2", Name: "Two"}},
		CurrentModelID:  "m1",
	}}
}

// TestChatModelsAPI 验证 ListChatModels / SetChatModel 转发到 manager。
func TestChatModelsAPI(t *testing.T) {
	conn := &fakeChatConn{start: acpSessionWithModels(), waitCh: make(chan struct{})}
	b := &fakeChatCaptureBackend{conn: conn}
	rec := &notifyRecorder{}
	m := newChatManagerWith(rec.emit, b, nil, nil, nil)
	t.Cleanup(m.CloseAll)
	info, err := m.Open("new:1", chat.Info{Kind: chat.KindNew, Workspace: "/w"}, chat.Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	app := NewAppWith(Options{Chats: m})
	view, err := app.ListChatModels(info.ID)
	if err != nil {
		t.Fatalf("ListChatModels: %v", err)
	}
	if view.Current != "m1" || len(view.Models) != 2 || view.Models[1].ID != "m2" {
		t.Fatalf("view = %+v", view)
	}
	if err := app.SetChatModel(info.ID, "m2"); err != nil {
		t.Fatalf("SetChatModel: %v", err)
	}
	if len(conn.modelCalls) != 1 || conn.modelCalls[0] != "m2" {
		t.Fatalf("modelCalls = %v", conn.modelCalls)
	}
	if view, _ := app.ListChatModels(info.ID); view.Current != "m2" {
		t.Fatalf("current after set = %q", view.Current)
	}
}

// TestChatModelsAPINilSlice 验证 agent 未报告模型时 Models 是非 nil 空切片：
// nil slice 经 JSON 序列化成 null，前端 models.Models.find 会白屏（Cannot read properties of null）。
func TestChatModelsAPINilSlice(t *testing.T) {
	b := &fakeChatCaptureBackend{} // 无 models 的 SessionStart
	rec := &notifyRecorder{}
	m := newChatManagerWith(rec.emit, b, nil, nil, nil)
	t.Cleanup(m.CloseAll)
	info, err := m.Open("new:1", chat.Info{Kind: chat.KindNew, Workspace: "/w"}, chat.Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	app := NewAppWith(Options{Chats: m})
	view, err := app.ListChatModels(info.ID)
	if err != nil {
		t.Fatalf("ListChatModels: %v", err)
	}
	if view.Models == nil {
		t.Fatal("Models 为 nil，JSON 序列化会是 null")
	}
	if len(view.Models) != 0 {
		t.Fatalf("Models = %+v", view.Models)
	}
}

// TestChatCommandsEmitted 验证 available_commands_update 经 chat:commands 推给前端。
func TestChatCommandsEmitted(t *testing.T) {
	b := &fakeChatCaptureBackend{}
	rec := &notifyRecorder{}
	m := newChatManagerWith(rec.emit, b, nil, nil, nil)
	t.Cleanup(m.CloseAll)
	info, err := m.Open("new:1", chat.Info{Kind: chat.KindNew, Workspace: "/w"}, chat.Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"plan","description":"P"}]}`)
	b.lastHandler().SessionUpdate("s1", raw)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec.mu.Lock()
		found := false
		for _, n := range rec.names {
			if n == "chat:commands" {
				found = true
			}
		}
		rec.mu.Unlock()
		if found {
			if got := m.Commands(info.ID); len(got) != 1 || got[0].Name != "plan" {
				t.Fatalf("commands = %+v", got)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("chat:commands not emitted")
}
