package chat

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/acp"
)

func TestMimeExt(t *testing.T) {
	cases := []struct{ mime, want string }{
		{"image/png", ".png"},
		{"image/jpeg", ".jpg"},
		{"image/webp", ".webp"},
		{"image/gif", ".gif"},
		{"application/pdf", ".bin"},
		{"", ".bin"},
	}
	for _, tc := range cases {
		if got := mimeExt(tc.mime); got != tc.want {
			t.Fatalf("mimeExt(%q) = %q, want %q", tc.mime, got, tc.want)
		}
	}
}

// persistBlocks 基础行为：image/resource 落盘、resource_link 只留 URI、audio 保持现状。
func TestPersistBlocks(t *testing.T) {
	root := t.TempDir()
	blocks := []acp.ContentBlock{
		{Type: "text", Text: "看这张图"},
		{Type: "image", MimeType: "image/png", Data: "aGVsbG8="},
		{Type: "resource", Name: "report.pdf", MimeType: "application/pdf", Data: "aGVsbG8="},
		{Type: "resource_link", Name: "spec.md", MimeType: "text/markdown", URI: "file:///docs/spec.md"},
		{Type: "audio", MimeType: "audio/wav", Data: "aGVsbG8="},
	}
	metas := persistBlocks(root, "chat1", blocks)

	if len(metas) != 4 {
		t.Fatalf("metas 数量 = %d, want 4（text 跳过）: %+v", len(metas), metas)
	}
	img := metas[0]
	if img.Kind != "image" || img.MimeType != "image/png" || img.Path == "" {
		t.Fatalf("image meta = %+v", img)
	}
	data, err := os.ReadFile(img.Path)
	if err != nil {
		t.Fatalf("读落盘文件: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("落盘内容 = %q, want hello", data)
	}
	if filepath.Dir(img.Path) != filepath.Join(root, "chat1") {
		t.Fatalf("落盘目录 = %q", filepath.Dir(img.Path))
	}

	res := metas[1]
	if res.Kind != "file" || res.Name != "report.pdf" || res.Path == "" {
		t.Fatalf("resource meta = %+v", res)
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Fatalf("resource 文件未落盘: %v", err)
	}

	link := metas[2]
	if link.Kind != "file" || link.URI != "file:///docs/spec.md" || link.Path != "" {
		t.Fatalf("resource_link meta = %+v", link)
	}

	aud := metas[3]
	if aud.Kind != "" || aud.MimeType != "audio/wav" || aud.Path != "" {
		t.Fatalf("audio meta = %+v", aud)
	}

	// 两个落盘块文件名不得冲突
	if img.Path == res.Path {
		t.Fatalf("文件名冲突: %q", img.Path)
	}
	entries, err := os.ReadDir(filepath.Join(root, "chat1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("落盘文件数 = %d, want 2", len(entries))
	}
}

// root 为空串：完全不落盘，meta 不带 Path，不 panic。
func TestPersistBlocksEmptyRoot(t *testing.T) {
	blocks := []acp.ContentBlock{
		{Type: "image", MimeType: "image/png", Data: "aGVsbG8="},
		{Type: "resource", Name: "a.bin", Data: "aGVsbG8="},
	}
	metas := persistBlocks("", "chat1", blocks)
	if len(metas) != 2 {
		t.Fatalf("metas 数量 = %d", len(metas))
	}
	for _, m := range metas {
		if m.Path != "" {
			t.Fatalf("空 root 不应带 Path: %+v", m)
		}
	}
}

// 坏 base64：不 panic，该块 meta 不带 Path。
func TestPersistBlocksBadBase64(t *testing.T) {
	root := t.TempDir()
	metas := persistBlocks(root, "chat1", []acp.ContentBlock{
		{Type: "image", MimeType: "image/png", Data: "!!!"},
	})
	if len(metas) != 1 {
		t.Fatalf("metas 数量 = %d", len(metas))
	}
	if metas[0].Path != "" || metas[0].Kind != "image" || metas[0].MimeType != "image/png" {
		t.Fatalf("坏 base64 meta = %+v", metas[0])
	}
	if entries, err := os.ReadDir(filepath.Join(root, "chat1")); err == nil && len(entries) > 0 {
		t.Fatalf("坏 base64 不应产生落盘文件: %d 个", len(entries))
	}
}

// Manager 装配 attachmentsRoot 后，用户消息的 image 块落盘且 meta 指向存在的文件。
func TestManagerPersistsImageAttachment(t *testing.T) {
	m, _, c := newTestManager(t)
	m.SetAttachmentsRoot(t.TempDir())
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	blocks := []acp.ContentBlock{
		{Type: "text", Text: "带图消息"},
		{Type: "image", MimeType: "image/png", Data: "aGVsbG8="},
	}
	if err := m.PromptBlocks(info.ID, blocks); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	// 等 turn 结束，确保时间线已写入
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		found := false
		for _, u := range c.updates {
			if u.Type == "turn_done" {
				found = true
			}
		}
		c.mu.Unlock()
		if found {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	got := m.History(info.ID)
	var user *Update
	for i := range got {
		if got[i].Type == "user" && got[i].Text == "带图消息" {
			user = &got[i]
		}
	}
	if user == nil || len(user.Attachments) == 0 {
		t.Fatalf("user 消息缺附件 meta; history=%+v", got)
	}
	att := user.Attachments[0]
	if att.Kind != "image" || att.Path == "" {
		t.Fatalf("附件 meta = %+v", att)
	}
	if _, err := os.Stat(att.Path); err != nil {
		t.Fatalf("meta.Path 指向的文件不存在: %v", err)
	}
}

// 不设 root：Path 为空（保持旧兼容行为，仅元数据）。
func TestManagerNoRootNoPath(t *testing.T) {
	m, _, _ := newTestManager(t)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := m.PromptBlocks(info.ID, []acp.ContentBlock{
		{Type: "text", Text: "hi"},
		{Type: "image", MimeType: "image/png", Data: "aGVsbG8="},
	}); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	got := m.History(info.ID)
	for _, u := range got {
		if u.Type != "user" {
			continue
		}
		for _, att := range u.Attachments {
			if att.Path != "" {
				t.Fatalf("未设 root 时不应有 Path: %+v", att)
			}
		}
	}
}
