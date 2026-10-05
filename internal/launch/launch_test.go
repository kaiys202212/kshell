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

	got, err := ForSession([]providers.Provider{p}, toolsRunnable, s, ThemeOptions{}, ModelOptions{}, PermissionOptions{})
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
	if _, err := ForSession([]providers.Provider{p}, []discovery.Tool{{ID: "claude", Installed: true, BinPath: ""}}, s, ThemeOptions{}, ModelOptions{}, PermissionOptions{}); !errors.Is(err, ErrToolNotRunnable) {
		t.Fatalf("无可执行文件应返回 ErrToolNotRunnable, got %v", err)
	}
	// 工具不在 provider 列表
	if _, err := ForSession(nil, toolsRunnable, s, ThemeOptions{}, ModelOptions{}, PermissionOptions{}); !errors.Is(err, ErrToolNotRunnable) {
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

	got, err := ForWorkspace(ps, tools, ws, ThemeOptions{}, ModelOptions{}, PermissionOptions{})
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

func acpTools(det providers.ACPDetection) []discovery.Tool {
	return []discovery.Tool{{ID: "claude", Name: "Claude Code", Installed: true, BinPath: "claude", ACP: &det}}
}

func TestForWorkspaceACP_NpxFallback(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := acpTools(providers.ACPDetection{Available: true, Source: "npx", Package: "@agentclientprotocol/claude-agent-acp"})
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "claude", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatalf("for workspace acp: %v", err)
	}
	if l.Path != "npx" || len(l.Args) != 2 || l.Args[0] != "-y" {
		t.Fatalf("launch = %+v", l)
	}
	if l.Dir != "/w" {
		t.Fatalf("dir = %q", l.Dir)
	}
}

func TestForWorkspaceACP_PathHit(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := acpTools(providers.ACPDetection{Available: true, Source: "path", BinPath: "/usr/bin/claude-agent-acp"})
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "claude", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Path != "/usr/bin/claude-agent-acp" {
		t.Fatalf("launch = %+v", l)
	}
}

func TestForWorkspaceACP_Unavailable(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := acpTools(providers.ACPDetection{Available: false})
	if _, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "claude", ModelOptions{}, PermissionOptions{}); err != ErrACPUnavailable {
		t.Fatalf("want ErrACPUnavailable, got %v", err)
	}
}

func TestForWorkspaceACP_ExtraArgs(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}

	pathTools := acpTools(providers.ACPDetection{Available: true, Source: "path", BinPath: "/usr/bin/claude-agent-acp", ExtraArgs: []string{"--foo"}})
	l, err := ForWorkspaceACP(ps, pathTools, discovery.Workspace{Path: "/w"}, "claude", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Args) != 1 || l.Args[0] != "--foo" {
		t.Fatalf("path args = %v", l.Args)
	}

	npxTools := acpTools(providers.ACPDetection{Available: true, Source: "npx", Package: "pkg", ExtraArgs: []string{"--foo"}})
	l, err = ForWorkspaceACP(ps, npxTools, discovery.Workspace{Path: "/w"}, "claude", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Args) != 3 || l.Args[0] != "-y" || l.Args[1] != "pkg" || l.Args[2] != "--foo" {
		t.Fatalf("npx args = %v", l.Args)
	}
}

func TestForSessionACP_UsesWorkspaceDir(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := acpTools(providers.ACPDetection{Available: true, Source: "path", BinPath: "claude-agent-acp"})
	s := providers.Session{ID: "s1", ToolID: "claude", Workspace: "/proj"}
	l, err := ForSessionACP(ps, tools, s, ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Dir != "/proj" {
		t.Fatalf("dir = %q", l.Dir)
	}
}

// cursor 直执行形态：kshell 探测到的 CLI 不在 PATH 上时，适配器自己找不到
// cursor-agent，必须经 CURSOR_AGENT_EXECUTABLE 把路径传过去。
func TestForWorkspaceACP_CursorInjectsAgentExecutable(t *testing.T) {
	ps := []providers.Provider{providers.Cursor{}}
	tools := []discovery.Tool{{
		ID: "cursor", Installed: true, BinPath: `%LOCALAPPDATA%\cursor-agent\cursor-agent.exe`,
		ACP: &providers.ACPDetection{Available: true, Source: "path", BinPath: "cursor-acp"},
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "cursor", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Env["CURSOR_AGENT_EXECUTABLE"] != `%LOCALAPPDATA%\cursor-agent\cursor-agent.exe` {
		t.Fatalf("env = %v", l.Env)
	}
}

// node 入口形态（官方更新器删光 shim 后的兜底）无法表达为单个可执行文件，
// 不注入，交由适配器走 PATH。
func TestForWorkspaceACP_CursorNodeEntryNoInject(t *testing.T) {
	ps := []providers.Provider{providers.Cursor{}}
	tools := []discovery.Tool{{
		ID: "cursor", Installed: true, BinPath: "node.exe", BinArgs: []string{"index.js"},
		ACP: &providers.ACPDetection{Available: true, Source: "path", BinPath: "cursor-acp"},
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "cursor", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := l.Env["CURSOR_AGENT_EXECUTABLE"]; ok {
		t.Fatalf("node 入口形态不应注入, env = %v", l.Env)
	}
}

// 非 cursor 工具不得注入任何环境变量，防止回归为全量注入。
func TestForWorkspaceACP_OtherToolNoEnv(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := acpTools(providers.ACPDetection{Available: true, Source: "path", BinPath: "claude-agent-acp"})
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "claude", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Env) != 0 {
		t.Fatalf("非 cursor 工具 env 应为空, got %v", l.Env)
	}
}

// npx 兜底形态下注入依然生效（本体路径与适配器来源无关）。
func TestForWorkspaceACP_CursorNpxStillInjects(t *testing.T) {
	ps := []providers.Provider{providers.Cursor{}}
	tools := []discovery.Tool{{
		ID: "cursor", Installed: true, BinPath: `%LOCALAPPDATA%\cursor-agent\cursor-agent.exe`,
		ACP: &providers.ACPDetection{Available: true, Source: "npx", Package: "cursor-acp"},
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "cursor", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Args[0] != "-y" || l.Args[1] != "cursor-acp" {
		t.Fatalf("args = %v", l.Args)
	}
	if l.Env["CURSOR_AGENT_EXECUTABLE"] != `%LOCALAPPDATA%\cursor-agent\cursor-agent.exe` {
		t.Fatalf("env = %v", l.Env)
	}
}

func TestApplyModelTerminalCodex(t *testing.T) {
	ps := []providers.Provider{providers.Codex{}}
	tools := []discovery.Tool{{ID: "codex", Name: "Codex CLI", Installed: true, BinPath: "codex"}}
	s := providers.Session{ID: "s1", ToolID: "codex", Workspace: "/proj"}
	mo := ModelOptions{Resolver: func(toolID string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{OpenAIBaseURL: "https://h/", AnthropicBaseURL: "https://h/", APIKey: "k", Model: "gpt-x"}, true
	}}
	l, err := ForSession(ps, tools, s, ThemeOptions{}, mo, PermissionOptions{})
	if err != nil {
		t.Fatalf("ForSession: %v", err)
	}
	found := false
	for i := 0; i+1 < len(l.Args); i++ {
		if l.Args[i] == "-m" && l.Args[i+1] == "gpt-x" {
			found = true
		}
	}
	if !found {
		t.Fatalf("args 缺少 -m gpt-x：%v", l.Args)
	}
	if l.Env["OPENAI_BASE_URL"] != "https://h/" || l.Env["OPENAI_API_KEY"] != "k" {
		t.Fatalf("env = %+v", l.Env)
	}
}

func TestApplyModelACPClaude(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := acpTools(providers.ACPDetection{Available: true, Source: "path", BinPath: "claude-agent-acp"})
	mo := ModelOptions{Resolver: func(string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{OpenAIBaseURL: "https://h/", AnthropicBaseURL: "https://h/", APIKey: "k", Model: "mimo-v2.5"}, true
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "claude", mo, PermissionOptions{})
	if err != nil {
		t.Fatalf("ForWorkspaceACP: %v", err)
	}
	if l.Env["ANTHROPIC_MODEL"] != "mimo-v2.5" || l.Env["ANTHROPIC_BASE_URL"] != "https://h/" {
		t.Fatalf("ACP env = %+v", l.Env)
	}
}

func TestApplyModelPrecedesSubcommand(t *testing.T) {
	ps := []providers.Provider{providers.Codex{}}
	tools := []discovery.Tool{{ID: "codex", Installed: true, BinPath: "codex"}}
	s := providers.Session{ID: "s1", ToolID: "codex", Workspace: "/p"}
	mo := ModelOptions{Resolver: func(string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{Model: "gpt-x"}, true
	}}
	l, err := ForSession(ps, tools, s, ThemeOptions{}, mo, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Args) < 3 || l.Args[0] != "-m" || l.Args[1] != "gpt-x" || l.Args[2] != "resume" {
		t.Fatalf("模型参数应位于子命令之前，got %v", l.Args)
	}
}

func TestApplyModelNilResolverNoInjection(t *testing.T) {
	// 直接验证 applyModel：避免 applyTheme 追加的通用主题变量干扰断言。
	l := applyModel(providers.Launch{}, providers.Codex{}, ModelOptions{})
	if len(l.Env) != 0 || len(l.Args) != 0 {
		t.Fatalf("无 resolver 不应注入：args=%v env=%+v", l.Args, l.Env)
	}
}

func TestApplyPermissionBypassClaude(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := []discovery.Tool{{ID: "claude", Installed: true, BinPath: "claude"}}
	s := providers.Session{ID: "s1", ToolID: "claude", Workspace: "/p"}
	l, err := ForSession(ps, tools, s, ThemeOptions{}, ModelOptions{}, PermissionOptions{Bypass: true})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range l.Args {
		if a == "--dangerously-skip-permissions" {
			found = true
		}
	}
	if !found {
		t.Fatalf("应注入 bypass 参数，got %v", l.Args)
	}
}

func TestForSessionPrependsBinArgs(t *testing.T) {
	s := providers.Session{ID: "s1", ToolID: "cursor"}
	p := fakeProvider{id: "cursor", resume: providers.Launch{Path: "node.exe", Args: []string{"--resume", "s1"}}}
	tools := []discovery.Tool{{ID: "cursor", Installed: true, BinPath: "node.exe", BinArgs: []string{"index.js"}}}
	got, err := ForSession([]providers.Provider{p}, tools, s, ThemeOptions{}, ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Args) != 3 || got.Args[0] != "index.js" || got.Args[1] != "--resume" {
		t.Fatalf("BinArgs 应前置, got %v", got.Args)
	}
	if got.Path != "node.exe" {
		t.Fatalf("Path = %q", got.Path)
	}
}

func TestForWorkspaceToolPrependsBinArgs(t *testing.T) {
	p := fakeProvider{id: "cursor", newSess: providers.Launch{Path: "node.exe"}}
	tools := []discovery.Tool{{ID: "cursor", Installed: true, BinPath: "node.exe", BinArgs: []string{"index.js"}}}
	ws := discovery.Workspace{Path: `D:\ws`}
	got, err := ForWorkspaceTool([]providers.Provider{p}, tools, ws, "cursor", ThemeOptions{}, ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Args) != 1 || got.Args[0] != "index.js" {
		t.Fatalf("新建会话也应前置 BinArgs, got %v", got.Args)
	}
}
