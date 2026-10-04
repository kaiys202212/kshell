package discovery

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/providers"
)

// fakeEnumerator 模拟「会话不在文件里」的工具（如 opencode 读 SQLite 库）。
type fakeEnumerator struct {
	root    string
	session providers.Session
	err     error
	calls   int
}

func (f *fakeEnumerator) ID() string          { return "fake" }
func (f *fakeEnumerator) DisplayName() string { return "Fake" }

func (f *fakeEnumerator) DetectSpec(home string) providers.DetectSpec {
	return providers.DetectSpec{BinName: "kshell-fake-cli", InstallDirs: []string{f.root}}
}

func (f *fakeEnumerator) SessionRoots(home string) []string { return nil }
func (f *fakeEnumerator) SessionFilePattern() string        { return "" }

func (f *fakeEnumerator) ParseSession(path string, head []byte) (*providers.Session, error) {
	return nil, errors.New("fake: 会话不在文件里")
}

func (f *fakeEnumerator) NewSessionCmd(ws string, bin string) providers.Launch {
	return providers.Launch{Path: bin, Dir: ws}
}

func (f *fakeEnumerator) ResumeCmd(s providers.Session, bin string) providers.Launch {
	return providers.Launch{Path: bin, Dir: s.Workspace}
}

func (f *fakeEnumerator) EnumerateSessions(home, bin string) ([]providers.Session, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return []providers.Session{f.session}, nil
}

// fakeCLIDir 造一个「安装目录」，让 providers.Detect 能从里面解析出 CLI 路径
// （Windows 找 .exe/.cmd，其他平台找同名文件，两个都建以兼容双平台）。
func fakeCLIDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"kshell-fake-cli", "kshell-fake-cli.exe", "kshell-fake-cli.cmd"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestScanIncludesEnumeratorSessions(t *testing.T) {
	home := t.TempDir()
	ws := filepath.Join(home, "proj")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}

	fake := &fakeEnumerator{
		root: fakeCLIDir(t),
		session: providers.Session{
			ID:        "ses_1",
			ToolID:    "fake",
			Workspace: ws,
			Title:     "来自数据库的会话",
			UpdatedAt: time.Now(),
		},
	}

	res, err := Scan(home, []providers.Provider{fake}, filepath.Join(home, "cache", "index.json"), ScanOptions{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("enumerate calls = %d, want 1", fake.calls)
	}
	if len(res.Sessions) != 1 || res.Sessions[0].ID != "ses_1" {
		t.Fatalf("sessions = %+v", res.Sessions)
	}
	// 枚举出来的会话同样要参与工作区聚合。
	if len(res.Workspaces) != 1 || res.Workspaces[0].SessionCount != 1 {
		t.Fatalf("workspaces = %+v", res.Workspaces)
	}
}

func TestScanRecordsEnumeratorFailure(t *testing.T) {
	home := t.TempDir()
	fake := &fakeEnumerator{root: fakeCLIDir(t), err: errors.New("db 被占用")}

	res, err := Scan(home, []providers.Provider{fake}, filepath.Join(home, "cache", "index.json"), ScanOptions{})
	if err != nil {
		t.Fatalf("scan 不该因为枚举失败而整体失败: %v", err)
	}
	if len(res.Failed) != 1 {
		t.Fatalf("failed = %v, want 1 条", res.Failed)
	}
	if !strings.Contains(res.Failed[0], "Fake") || !strings.Contains(res.Failed[0], "db 被占用") {
		t.Fatalf("failed = %q, want 含工具名与原因", res.Failed[0])
	}
}

func TestScanEnumeratesWithoutBin(t *testing.T) {
	home := t.TempDir()
	ws := filepath.Join(home, "proj")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	// root 指向空目录：Detect 解析不出 CLI，仍应调用 EnumerateSessions（如 Cursor 只读 chats）。
	fake := &fakeEnumerator{
		root:    t.TempDir(),
		session: providers.Session{ID: "ses_x", ToolID: "fake", Workspace: ws, Title: "无 bin"},
	}

	res, err := Scan(home, []providers.Provider{fake}, filepath.Join(home, "cache", "index.json"), ScanOptions{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("calls = %d, want 1", fake.calls)
	}
	if len(res.Sessions) != 1 || res.Sessions[0].ID != "ses_x" {
		t.Fatalf("sessions = %+v", res.Sessions)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("failed = %v, want empty", res.Failed)
	}
}


