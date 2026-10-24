package desktop

// 聊天附件缓存读取与本地打开的桌面绑定测试：
// ReadChatAttachmentData 只允许 ChatAttachmentsDir/<chatID>/ 下的路径
//（防任意读文件原语），OpenLocalPath 不限路径但拒绝目录。

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/acp"
	"github.com/yangk/kshell/internal/chat"
)

// newAttachmentApp 装配只测附件绑定的最小 App（不跑 initRealDeps，不碰真实用户目录）。
func newAttachmentApp(t *testing.T, root string) *App {
	t.Helper()
	return NewAppWith(Options{
		Home:               t.TempDir(),
		ChatAttachmentsDir: root,
		Windows:            NewWindowManager(&stubLauncher{}, nil),
	})
}

func TestReadChatAttachmentDataOK(t *testing.T) {
	root := t.TempDir()
	app := newAttachmentApp(t, root)
	content := append([]byte("\x89PNG\r\n\x1a\n"), []byte("fake-png-body")...)
	abs := filepath.Join(root, "chat-1", "shot.png")
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, content, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := app.ReadChatAttachmentData(abs)
	if err != nil {
		t.Fatalf("ReadChatAttachmentData: %v", err)
	}
	if got.Mime != "image/png" {
		t.Fatalf("Mime = %q, want image/png", got.Mime)
	}
	if got.Size != int64(len(content)) {
		t.Fatalf("Size = %d, want %d", got.Size, len(content))
	}
	if got.AbsPath != abs {
		t.Fatalf("AbsPath = %q, want %q", got.AbsPath, abs)
	}
	decoded, err := base64.StdEncoding.DecodeString(got.Base64)
	if err != nil {
		t.Fatalf("Base64 decode: %v", err)
	}
	if !bytes.Equal(decoded, content) {
		t.Fatalf("decoded 内容不一致: %d vs %d 字节", len(decoded), len(content))
	}
}

// ../ 换目录（即使清洗后仍在缓存根内）必须拒绝：清洗后首段不再等于原始首段。
func TestReadChatAttachmentDataRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	app := newAttachmentApp(t, root)
	if err := os.MkdirAll(filepath.Join(root, "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "other", "x.png")
	if err := os.WriteFile(outside, []byte("leak"), 0o600); err != nil {
		t.Fatal(err)
	}
	// filepath.Join 会预先 Clean，这里手工拼出未清洗的原始路径
	abs := filepath.ToSlash(root) + "/chat-1/../other/x.png"
	if _, err := app.ReadChatAttachmentData(abs); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("穿越路径应报 errPathOutsideWorkspace, got %v", err)
	}
}

func TestReadChatAttachmentDataRejectsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	app := newAttachmentApp(t, root)
	outside := filepath.Join(t.TempDir(), "notepad.exe")
	if err := os.WriteFile(outside, []byte("M"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ReadChatAttachmentData(outside); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("缓存外路径应报 errPathOutsideWorkspace, got %v", err)
	}
	// 根目录直接下的文件（缺 chatID 层级）同样拒绝
	loose := filepath.Join(root, "loose.png")
	if err := os.WriteFile(loose, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ReadChatAttachmentData(loose); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("缺 chatID 层级应报 errPathOutsideWorkspace, got %v", err)
	}
}

func TestReadChatAttachmentDataEmptyRoot(t *testing.T) {
	app := newAttachmentApp(t, "")
	if _, err := app.ReadChatAttachmentData(filepath.Join(t.TempDir(), "a.png")); !errors.Is(err, errNotReady) {
		t.Fatalf("未装配应报 errNotReady, got %v", err)
	}
}

func TestOpenLocalPath(t *testing.T) {
	dir := t.TempDir()
	app := newAttachmentApp(t, dir)
	file := filepath.Join(dir, "outside.html")
	if err := os.WriteFile(file, []byte("<p>hi</p>"), 0o600); err != nil {
		t.Fatal(err)
	}

	var called string
	orig := openLocalFile
	openLocalFile = func(p string) error { called = p; return nil }
	t.Cleanup(func() { openLocalFile = orig })

	if err := app.OpenLocalPath(file); err != nil {
		t.Fatalf("OpenLocalPath: %v", err)
	}
	if called != file {
		t.Fatalf("openLocalFile 收到 %q, want %q", called, file)
	}
	if err := app.OpenLocalPath(dir); err == nil || err.Error() != "err.files.dir_not_openable" {
		t.Fatalf("目录应报 dir_not_openable, got %v", err)
	}
}

// newChatManager 应把 App 的 ChatAttachmentsDir 装配进 manager：
// 发 image 块后附件落到 <ChatAttachmentsDir>/<chatID>/ 下。
func TestNewChatManagerWiresAttachmentsRoot(t *testing.T) {
	root := t.TempDir()
	app := NewAppWith(Options{
		Home:               t.TempDir(),
		ChatAttachmentsDir: root,
		Windows:            NewWindowManager(&stubLauncher{}, nil),
	})
	app.mu.Lock()
	app.opts.Chats = newChatManager(app, fakeChatBackend{})
	m := app.opts.Chats
	app.mu.Unlock()
	t.Cleanup(m.CloseAll)

	info, err := m.Open("new:1", chat.Info{Kind: chat.KindNew, Workspace: "/w"}, chat.Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := m.PromptBlocks(info.ID, []acp.ContentBlock{
		{Type: "text", Text: "带图消息"},
		{Type: "image", MimeType: "image/png", Data: "aGVsbG8="},
	}); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(filepath.Join(root, info.ID))
		if err == nil && len(entries) > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("附件未落到 ChatAttachmentsDir/<chatID>/")
}
