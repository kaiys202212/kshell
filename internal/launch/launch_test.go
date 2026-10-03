package launch

import (
	"errors"
	"testing"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

// fakeProvider 固定返回启动描述，便于断言参数透传。
type fakeProvider struct {
	id      string
	resume  providers.Launch
	newSess providers.Launch
}

func (f fakeProvider) ID() string                              { return f.id }
func (f fakeProvider) DisplayName() string                     { return f.id }
func (f fakeProvider) DetectSpec(string) providers.DetectSpec  { return providers.DetectSpec{} }
func (f fakeProvider) SessionRoots(string) []string            { return nil }
func (f fakeProvider) SessionFilePattern() string              { return "*.jsonl" }
func (f fakeProvider) ParseSession(string, []byte) (*providers.Session, error) {
	return nil, errors.New("not implemented")
}
func (f fakeProvider) NewSessionCmd(ws, bin string) providers.Launch {
	l := f.newSess
	l.Dir = ws
	return l
}
func (f fakeProvider) ResumeCmd(s providers.Session, bin string) providers.Launch {
	l := f.resume
	l.Dir = s.Workspace
	return l
}

var toolsRunnable = []discovery.Tool{{ID: "claude", Installed: true, BinPath: "claude"}}

func TestForSessionDelegatesToProvider(t *testing.T) {
	s := providers.Session{ID: "s1", ToolID: "claude", Workspace: `D:\ws`, Title: "t"}
	want := providers.Launch{Path: "claude", Args: []string{"--resume", "s1"}}
	p := fakeProvider{id: "claude", resume: want}

	got, err := ForSession([]providers.Provider{p}, toolsRunnable, s)
	if err != nil {
		t.Fatalf("ForSession error: %v", err)
	}
	if got.Path != want.Path || got.Dir != s.Workspace || len(got.Args) != 2 {
		t.Fatalf("ForSession = %+v, 期望透传 ResumeCmd 且 Dir=会话工作区", got)
	}
}

func TestForSessionFailsWithoutRunnableTool(t *testing.T) {
	s := providers.Session{ID: "s1", ToolID: "claude"}
	p := fakeProvider{id: "claude"}

	// 工具只有配置目录、没有可执行文件
	if _, err := ForSession([]providers.Provider{p}, []discovery.Tool{{ID: "claude", Installed: true, BinPath: ""}}, s); !errors.Is(err, ErrToolNotRunnable) {
		t.Fatalf("无可执行文件应返回 ErrToolNotRunnable, got %v", err)
	}
	// 工具不在 provider 列表
	if _, err := ForSession(nil, toolsRunnable, s); !errors.Is(err, ErrToolNotRunnable) {
		t.Fatalf("缺 provider 应返回 ErrToolNotRunnable, got %v", err)
	}
}

func TestForWorkspacePicksPreferredTool(t *testing.T) {
	ps := []providers.Provider{
		fakeProvider{id: "codex"},
		fakeProvider{id: "claude", newSess: providers.Launch{Path: "claude"}},
	}
	tools := append(toolsRunnable, discovery.Tool{ID: "codex", Installed: true, BinPath: "codex"})
	ws := discovery.Workspace{
		Path:       `D:\ws`,
		ToolCounts: map[string]int{"claude": 3, "codex": 1},
	}

	got, err := ForWorkspace(ps, tools, ws)
	if err != nil {
		t.Fatalf("ForWorkspace error: %v", err)
	}
	if got.Path != "claude" {
		t.Fatalf("应选会话数最多的 claude, got path=%q", got.Path)
	}
	if got.Dir != ws.Path {
		t.Fatalf("Dir = %q, 期望工作区路径", got.Dir)
	}
}
