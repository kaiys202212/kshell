package chat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// normalizeUpdate 对三种消息分片返回块描述：image / resource_link / text 块 / 纯字符串。
func TestNormalizeUpdateBlockDesc(t *testing.T) {
	// image 块：ok、Text 为空、块描述标注 image
	raw := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"image","mimeType":"image/png","data":"aGVsbG8="}}`)
	u, bd, ok := normalizeUpdate(raw)
	if !ok || u.Type != "assistant" {
		t.Fatalf("image: ok=%v u=%+v", ok, u)
	}
	if u.Text != "" {
		t.Fatalf("image 块 Text 应为空, got %q", u.Text)
	}
	if bd == nil || bd.Type != "image" || bd.MimeType != "image/png" || bd.Data != "aGVsbG8=" {
		t.Fatalf("image 块描述 = %+v", bd)
	}

	// resource_link 块：URI/Name 进块描述
	raw = json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"resource_link","name":"spec.md","mimeType":"text/markdown","uri":"file:///docs/spec.md"}}`)
	_, bd, ok = normalizeUpdate(raw)
	if !ok {
		t.Fatal("resource_link: ok=false")
	}
	if bd == nil || bd.Type != "resource_link" || bd.URI != "file:///docs/spec.md" || bd.Name != "spec.md" {
		t.Fatalf("resource_link 块描述 = %+v", bd)
	}

	// thought 分片同样返回块描述
	raw = json.RawMessage(`{"sessionUpdate":"agent_thought_chunk","content":{"type":"image","mimeType":"image/png","data":"aGVsbG8="}}`)
	u, bd, ok = normalizeUpdate(raw)
	if !ok || u.Type != "thought" || bd == nil || bd.Type != "image" {
		t.Fatalf("thought: ok=%v u=%+v bd=%+v", ok, u, bd)
	}

	// 纯字符串 content：无块描述
	raw = json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":"纯文本"}`)
	u, bd, ok = normalizeUpdate(raw)
	if !ok || u.Text != "纯文本" || bd != nil {
		t.Fatalf("纯字符串: ok=%v u=%+v bd=%+v", ok, u, bd)
	}

	// text 块：块描述标注 text
	raw = json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"你好"}}`)
	u, bd, ok = normalizeUpdate(raw)
	if !ok || u.Text != "你好" || bd == nil || bd.Type != "text" {
		t.Fatalf("text 块: ok=%v u=%+v bd=%+v", ok, u, bd)
	}
}

// Manager 装配 root 后，agent 推送的 image 块落盘并挂到 assistant 时间线条目。
func TestManagerPersistsAgentImageChunk(t *testing.T) {
	m, b, _ := newTestManager(t)
	root := t.TempDir()
	m.SetAttachmentsRoot(root)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	b.last.SessionUpdate("sess-new", json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"image","mimeType":"image/png","data":"aGVsbG8="}}`))

	got := m.History(info.ID)
	var assistant *Update
	for i := range got {
		if got[i].Type == "assistant" {
			assistant = &got[i]
		}
	}
	if assistant == nil || len(assistant.Attachments) != 1 {
		t.Fatalf("assistant 消息缺附件 meta; history=%+v", got)
	}
	att := assistant.Attachments[0]
	if att.Kind != "image" || att.Path == "" {
		t.Fatalf("附件 meta = %+v", att)
	}
	data, err := os.ReadFile(att.Path)
	if err != nil {
		t.Fatalf("读落盘文件: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("落盘内容 = %q, want hello", data)
	}
	if filepath.Dir(att.Path) != filepath.Join(root, info.ID) {
		t.Fatalf("落盘目录 = %q", filepath.Dir(att.Path))
	}
}

// resource_link 块只构造 meta（URI 正确），不产生落盘文件。
func TestManagerResourceLinkChunkNoFile(t *testing.T) {
	m, b, _ := newTestManager(t)
	root := t.TempDir()
	m.SetAttachmentsRoot(root)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	b.last.SessionUpdate("sess-new", json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"resource_link","name":"spec.md","mimeType":"text/markdown","uri":"file:///docs/spec.md"}}`))

	got := m.History(info.ID)
	var assistant *Update
	for i := range got {
		if got[i].Type == "assistant" {
			assistant = &got[i]
		}
	}
	if assistant == nil || len(assistant.Attachments) != 1 {
		t.Fatalf("assistant 消息缺附件 meta; history=%+v", got)
	}
	att := assistant.Attachments[0]
	if att.Kind != "file" || att.URI != "file:///docs/spec.md" || att.Name != "spec.md" || att.Path != "" {
		t.Fatalf("resource_link meta = %+v", att)
	}
	if entries, err := os.ReadDir(filepath.Join(root, info.ID)); err == nil && len(entries) > 0 {
		t.Fatalf("resource_link 不应产生落盘文件: %d 个", len(entries))
	}
}

// 未设 root：image 块 meta 有 Kind 无 Path（与用户消息的兼容行为一致）。
func TestManagerAgentImageChunkNoRoot(t *testing.T) {
	m, b, _ := newTestManager(t)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	b.last.SessionUpdate("sess-new", json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"image","mimeType":"image/png","data":"aGVsbG8="}}`))

	got := m.History(info.ID)
	found := false
	for _, u := range got {
		if u.Type != "assistant" {
			continue
		}
		for _, att := range u.Attachments {
			found = true
			if att.Kind != "image" || att.Path != "" {
				t.Fatalf("未设 root 时 meta = %+v", att)
			}
		}
	}
	if !found {
		t.Fatalf("assistant 消息缺附件 meta; history=%+v", got)
	}
}
