package launch

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/yangk/kshell/internal/appearance"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

// fakeProvider 固定返回启动描述，便于断言参数透传。
type fakeProvider struct {
	id      string
	resume  providers.Launch
	newSess providers.Launch
}

func (f fakeProvider) ID() string                             { return f.id }
func (f fakeProvider) DisplayName() string                    { return f.id }
func (f fakeProvider) DetectSpec(string) providers.DetectSpec { return providers.DetectSpec{} }
func (f fakeProvider) SessionRoots(string) []string           { return nil }
func (f fakeProvider) SessionFilePattern() string             { return "*.jsonl" }
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

// 对外错误是 wire key，供前端 translateBackend 翻译。
func TestLaunchErrorKeys(t *testing.T) {
	if got := ErrToolNotRunnable.Error(); got != "err.launch.no_binary" {
		t.Fatalf("ErrToolNotRunnable = %q, want err.launch.no_binary", got)
	}
	if got := ErrACPUnavailable.Error(); got != "err.launch.no_acp_adapter" {
		t.Fatalf("ErrACPUnavailable = %q, want err.launch.no_acp_adapter", got)
	}
	if got := ErrToolUpdating.Error(); got != "err.launch.updating" {
		t.Fatalf("ErrToolUpdating = %q, want err.launch.updating", got)
	}
}

// 自更新进行中的工具：新建/恢复会话一律返回 ErrToolUpdating（前端译成「稍后重试」），
// 不得落进 ErrToolNotRunnable 或 ErrACPUnavailable。
func TestUpdatingToolReturnsErrToolUpdating(t *testing.T) {
	updating := []discovery.Tool{{ID: "cursor", Installed: true, Source: "updating", Updating: true}}
	p := fakeProvider{id: "cursor", newSess: providers.Launch{Path: "cursor-agent"}}

	if _, err := ForWorkspaceTool([]providers.Provider{p}, updating, discovery.Workspace{Path: `D:\ws`}, "cursor", ThemeOptions{}, ModelOptions{}, PermissionOptions{}); !errors.Is(err, ErrToolUpdating) {
		t.Fatalf("ForWorkspaceTool 更新中应返回 ErrToolUpdating, got %v", err)
	}
	if _, err := ForWorkspaceACP([]providers.Provider{p}, updating, discovery.Workspace{Path: `D:\ws`}, "cursor", ModelOptions{}, PermissionOptions{}); !errors.Is(err, ErrToolUpdating) {
		t.Fatalf("ForWorkspaceACP 更新中应返回 ErrToolUpdating, got %v", err)
	}
	s := providers.Session{ID: "s1", ToolID: "cursor"}
	if _, err := ForSession([]providers.Provider{p}, updating, s, ThemeOptions{}, ModelOptions{}, PermissionOptions{}); !errors.Is(err, ErrToolUpdating) {
		t.Fatalf("ForSession 更新中应返回 ErrToolUpdating, got %v", err)
	}
	// 首选工具逻辑：更新中的工具不可被选为新建会话工具
	if _, _, ok := PreferredTool([]providers.Provider{p}, updating, discovery.Workspace{Path: `D:\ws`}); ok {
		t.Fatal("更新中的工具不得被 PreferredTool 选中")
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

// node 入口形态无法直接当 spawn 目标：注入当前可执行文件作代理，并带上 node/脚本路径。
func TestForWorkspaceACP_CursorNodeEntryInjectsProxy(t *testing.T) {
	ps := []providers.Provider{providers.Cursor{}}
	tools := []discovery.Tool{{
		ID: "cursor", Installed: true, BinPath: `C:\agent\node.exe`, BinArgs: []string{`C:\agent\index.js`},
		ACP: &providers.ACPDetection{Available: true, Source: "npx", Package: "cursor-acp"},
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "cursor", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if l.Env["CURSOR_AGENT_EXECUTABLE"] != exe {
		t.Fatalf("CURSOR_AGENT_EXECUTABLE = %q, 期望当前可执行文件 %q", l.Env["CURSOR_AGENT_EXECUTABLE"], exe)
	}
	if l.Env["KSHELL_AS_CURSOR_AGENT"] != "1" {
		t.Fatalf("KSHELL_AS_CURSOR_AGENT = %q", l.Env["KSHELL_AS_CURSOR_AGENT"])
	}
	if l.Env["KSHELL_CURSOR_AGENT_NODE"] != `C:\agent\node.exe` {
		t.Fatalf("NODE = %q", l.Env["KSHELL_CURSOR_AGENT_NODE"])
	}
	if l.Env["KSHELL_CURSOR_AGENT_SCRIPT"] != `C:\agent\index.js` {
		t.Fatalf("SCRIPT = %q", l.Env["KSHELL_CURSOR_AGENT_SCRIPT"])
	}
}

// 非 cursor 工具不得注入任何环境变量，防止回归为全量注入。
// claude 是 git-bash 注入对象，这里 mock 探测未命中使 env 确定为空
//（真实探测依赖本机 PATH/注册表，不能作为断言依据）。
func TestForWorkspaceACP_OtherToolNoEnv(t *testing.T) {
	oldGitBash := gitBashEnvFn
	t.Cleanup(func() { gitBashEnvFn = oldGitBash })
	gitBashEnvFn = func() (string, bool) { return "", false }

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

// npx 形态下注入参数必须落在 -y <package> 之后：
// `npx --model X -y pkg` 时 npm 把模型名当待安装的包（E404），只有
// `npx -y pkg --model X` 才把参数传给适配器进程。
// 用 cursor 验证：其 InjectModel 产出 --model 参数（claude 走 env 注入无 args）。
func TestForWorkspaceACP_NpxModelArgsAfterPackage(t *testing.T) {
	ps := []providers.Provider{providers.Cursor{}}
	tools := []discovery.Tool{{
		ID: "cursor", Installed: true, BinPath: "cursor-agent",
		ACP: &providers.ACPDetection{Available: true, Source: "npx", Package: "cursor-acp"},
	}}
	mo := ModelOptions{Resolver: func(string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{Model: "glm-5.3-flash"}, true
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "cursor", mo, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-y", "cursor-acp", "--model", "glm-5.3-flash"}
	if len(l.Args) != len(want) {
		t.Fatalf("args = %v, want %v", l.Args, want)
	}
	for i := range want {
		if l.Args[i] != want[i] {
			t.Fatalf("args = %v, want %v", l.Args, want)
		}
	}
}

// npx 形态带 ExtraArgs 时注入参数在 -y <pkg> 之后、ExtraArgs 之前。
func TestForWorkspaceACP_NpxModelArgsWithExtraArgs(t *testing.T) {
	ps := []providers.Provider{providers.Cursor{}}
	tools := []discovery.Tool{{
		ID: "cursor", Installed: true, BinPath: "cursor-agent",
		ACP: &providers.ACPDetection{Available: true, Source: "npx", Package: "pkg", ExtraArgs: []string{"--foo"}},
	}}
	mo := ModelOptions{Resolver: func(string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{Model: "m1"}, true
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "cursor", mo, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-y", "pkg", "--model", "m1", "--foo"}
	if len(l.Args) != len(want) {
		t.Fatalf("args = %v, want %v", l.Args, want)
	}
	for i := range want {
		if l.Args[i] != want[i] {
			t.Fatalf("args = %v, want %v", l.Args, want)
		}
	}
}

// path 形态下模型参数仍应前置（无 -y 前缀问题）。
func TestForWorkspaceACP_PathModelArgsPrepended(t *testing.T) {
	ps := []providers.Provider{providers.Cursor{}}
	tools := []discovery.Tool{{
		ID: "cursor", Installed: true, BinPath: "cursor-agent",
		ACP: &providers.ACPDetection{Available: true, Source: "path", BinPath: "cursor-acp"},
	}}
	mo := ModelOptions{Resolver: func(string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{Model: "m1"}, true
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "cursor", mo, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Args) != 2 || l.Args[0] != "--model" || l.Args[1] != "m1" {
		t.Fatalf("path args = %v", l.Args)
	}
}

// npx 形态 + bypass 权限参数同样要落在包名之后。
func TestForSessionACP_NpxBypassArgsAfterPackage(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := acpTools(providers.ACPDetection{Available: true, Source: "npx", Package: "pkg"})
	s := providers.Session{ID: "s1", ToolID: "claude", Workspace: "/p"}
	l, err := ForSessionACP(ps, tools, s, ModelOptions{}, PermissionOptions{Bypass: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-y", "pkg", "--dangerously-skip-permissions"}
	if len(l.Args) != len(want) {
		t.Fatalf("args = %v, want %v", l.Args, want)
	}
	for i := range want {
		if l.Args[i] != want[i] {
			t.Fatalf("args = %v, want %v", l.Args, want)
		}
	}
}

// claude ACP 启动注入 git-bash 路径（非标准安装位置时 CLI 启动即退出的修复）。
func TestForWorkspaceACP_ClaudeInjectsGitBash(t *testing.T) {
	oldGitBash := gitBashEnvFn
	t.Cleanup(func() { gitBashEnvFn = oldGitBash })
	gitBashEnvFn = func() (string, bool) { return `D:\software\Git\bin\bash.exe`, true }

	ps := []providers.Provider{providers.Claude{}}
	tools := acpTools(providers.ACPDetection{Available: true, Source: "path", BinPath: "claude-agent-acp"})
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "claude", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Env["CLAUDE_CODE_GIT_BASH_PATH"] != `D:\software\Git\bin\bash.exe` {
		t.Fatalf("env = %v", l.Env)
	}
}

// 非 claude 工具不注入 git-bash 变量。
func TestForWorkspaceACP_NonClaudeNoGitBash(t *testing.T) {
	oldGitBash := gitBashEnvFn
	t.Cleanup(func() { gitBashEnvFn = oldGitBash })
	gitBashEnvFn = func() (string, bool) { return `D:\software\Git\bin\bash.exe`, true }

	ps := []providers.Provider{providers.Cursor{}}
	tools := []discovery.Tool{{
		ID: "cursor", Installed: true, BinPath: "cursor-agent",
		ACP: &providers.ACPDetection{Available: true, Source: "path", BinPath: "cursor-acp"},
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "cursor", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := l.Env["CLAUDE_CODE_GIT_BASH_PATH"]; ok {
		t.Fatalf("非 claude 工具不应注入 git-bash, env = %v", l.Env)
	}
}

// GitBashEnv 报不需要（如探测不到 git-bash）时不注入该变量。
func TestForWorkspaceACP_GitBashUnavailableSkips(t *testing.T) {
	oldGitBash := gitBashEnvFn
	t.Cleanup(func() { gitBashEnvFn = oldGitBash })
	gitBashEnvFn = func() (string, bool) { return "", false }

	ps := []providers.Provider{providers.Claude{}}
	tools := acpTools(providers.ACPDetection{Available: true, Source: "path", BinPath: "claude-agent-acp"})
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "claude", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := l.Env["CLAUDE_CODE_GIT_BASH_PATH"]; ok {
		t.Fatalf("不应注入 git-bash, env = %v", l.Env)
	}
}

func TestApplyModelTerminalCodex(t *testing.T) {
	ps := []providers.Provider{providers.Codex{}}
	tools := []discovery.Tool{{ID: "codex", Name: "Codex CLI", Installed: true, BinPath: "codex"}}
	mo := ModelOptions{Resolver: func(toolID string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{OpenAIBaseURL: "https://h/", AnthropicBaseURL: "https://h/", APIKey: "k", Model: "gpt-x"}, true
	}}
	l, err := ForWorkspaceTool(ps, tools, discovery.Workspace{Path: "/proj"}, "codex", ThemeOptions{}, mo, PermissionOptions{})
	if err != nil {
		t.Fatalf("ForWorkspaceTool: %v", err)
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

// TestForSessionSkipsModel 恢复会话不得注入模型（沿用会话原模型；避免 OpenCode 等因 --model 破坏续聊）。
func TestForSessionSkipsModel(t *testing.T) {
	ps := []providers.Provider{providers.Opencode{}}
	tools := []discovery.Tool{{ID: "opencode", Installed: true, BinPath: "opencode"}}
	s := providers.Session{ID: "ses_x", ToolID: "opencode", Workspace: "/p"}
	mo := ModelOptions{Resolver: func(string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{Model: "kshell/glm", OpenAIBaseURL: "https://h/", APIKey: "k"}, true
	}}
	l, err := ForSession(ps, tools, s, ThemeOptions{}, mo, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for i, a := range l.Args {
		if a == "--model" || a == "-m" {
			t.Fatalf("恢复不应含模型参数，got %v (at %d)", l.Args, i)
		}
	}
	if l.Env["OPENCODE_CONFIG_CONTENT"] != "" {
		t.Fatalf("恢复不应注入 OPENCODE_CONFIG_CONTENT，got %q", l.Env["OPENCODE_CONFIG_CONTENT"])
	}
	if len(l.Args) < 2 || l.Args[0] != "--session" || l.Args[1] != "ses_x" {
		t.Fatalf("应保留 --session，got %v", l.Args)
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
	// claude 模型注入改走 --settings env 块（env 注入会被用户 settings.json 覆盖）
	raw := findSettingsJSON(l.Args)
	if raw == "" {
		t.Fatalf("ACP args 应含 --settings: %v", l.Args)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("非法 JSON: %v", err)
	}
	envObj, _ := m["env"].(map[string]any)
	if envObj["ANTHROPIC_MODEL"] != "mimo-v2.5" || envObj["ANTHROPIC_BASE_URL"] != "https://h/" {
		t.Fatalf("ACP settings env = %+v", envObj)
	}
}

func TestApplyModelPrecedesSubcommand(t *testing.T) {
	// 直接组合：模型参数必须前置到 provider 子命令之前（如 codex resume）。
	l := applyModel(providers.Launch{Args: []string{"resume", "s1"}}, providers.Codex{}, ModelOptions{
		Resolver: func(string) (providers.ModelConfig, bool) {
			return providers.ModelConfig{Model: "gpt-x"}, true
		},
	})
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

// TestApplyPermissionBypassOpencode 断言端到端命令形状：--auto 必须在 --session 之前。
func TestApplyPermissionBypassOpencode(t *testing.T) {
	ps := []providers.Provider{providers.Opencode{}}
	tools := []discovery.Tool{{ID: "opencode", Installed: true, BinPath: "opencode"}}
	s := providers.Session{ID: "s1", ToolID: "opencode", Workspace: "/p"}
	l, err := ForSession(ps, tools, s, ThemeOptions{}, ModelOptions{}, PermissionOptions{Bypass: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Args) != 3 || l.Args[0] != "--auto" || l.Args[1] != "--session" || l.Args[2] != "s1" {
		t.Fatalf("bypass 命令形状应为 [--auto --session s1]，got %v", l.Args)
	}
}

// TestApplyPermissionBypassNodeEntry 断言 node 入口形态下权限参数必须落在脚本名之后：
// `node --force index.js` 会被 node 直接拒绝（bad option），`node index.js --force` 才正确。
func TestApplyPermissionBypassNodeEntry(t *testing.T) {
	ps := []providers.Provider{providers.Cursor{}}
	tools := []discovery.Tool{{ID: "cursor", Installed: true, BinPath: "node.exe",
		BinArgs: []string{`C:\agent\index.js`}}}
	s := providers.Session{ID: "s1", ToolID: "cursor", Workspace: "/p"}
	l, err := ForSession(ps, tools, s, ThemeOptions{}, ModelOptions{}, PermissionOptions{Bypass: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`C:\agent\index.js`, "--force", "--resume", "s1"}
	if len(l.Args) != len(want) {
		t.Fatalf("命令形状 = %v, want %v", l.Args, want)
	}
	for i := range want {
		if l.Args[i] != want[i] {
			t.Fatalf("命令形状 = %v, want %v", l.Args, want)
		}
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

// findSettingsJSON 取 args 里 --settings 的值（JSON 字符串），未找到返回空。
func findSettingsJSON(args []string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--settings" {
			return args[i+1]
		}
	}
	return ""
}

// claude 新建会话：模型注入必须走 --settings env 块（用户级 settings.json 的
// env 会替换进程环境变量，走 env 注入会被覆盖），且不再产出 ANTHROPIC_* env。
func TestForWorkspaceToolClaudeModelViaSettings(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := []discovery.Tool{{ID: "claude", Installed: true, BinPath: "claude"}}
	mo := ModelOptions{Resolver: func(string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{AnthropicBaseURL: "https://h/", APIKey: "k", Model: "glm-5"}, true
	}}
	l, err := ForWorkspaceTool(ps, tools, discovery.Workspace{Path: "/p"}, "claude", ThemeOptions{}, mo, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	raw := findSettingsJSON(l.Args)
	if raw == "" {
		t.Fatalf("args 应含 --settings: %v", l.Args)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("--settings 非法 JSON: %v (%q)", err, raw)
	}
	envObj, _ := m["env"].(map[string]any)
	if envObj["ANTHROPIC_MODEL"] != "glm-5" || envObj["ANTHROPIC_BASE_URL"] != "https://h/" || envObj["ANTHROPIC_AUTH_TOKEN"] != "k" {
		t.Fatalf("settings env = %+v", envObj)
	}
	if l.Env["ANTHROPIC_MODEL"] != "" || l.Env["ANTHROPIC_BASE_URL"] != "" {
		t.Fatalf("claude 不应再走 env 注入, env = %v", l.Env)
	}
}

// 与主题注入合并为单个 --settings 参数，env 与主题键共存。
func TestForWorkspaceToolClaudeModelMergedWithTheme(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := []discovery.Tool{{ID: "claude", Installed: true, BinPath: "claude"}}
	mo := ModelOptions{Resolver: func(string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{Model: "glm-5"}, true
	}}
	to := ThemeOptions{Mode: appearance.Dark}
	l, err := ForWorkspaceTool(ps, tools, discovery.Workspace{Path: "/p"}, "claude", to, mo, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, a := range l.Args {
		if a == "--settings" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("应合并为单个 --settings, got %d: %v", count, l.Args)
	}
	raw := findSettingsJSON(l.Args)
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("非法 JSON: %v", err)
	}
	if m["theme"] != "dark" {
		t.Fatalf("主题键应保留: %+v", m)
	}
	envObj, _ := m["env"].(map[string]any)
	if envObj["ANTHROPIC_MODEL"] != "glm-5" {
		t.Fatalf("env 应合入: %+v", m)
	}
}

// ACP 路径同样经 --settings 注入（参数传给适配器，最终转发给 claude CLI）。
func TestForWorkspaceACPClaudeModelViaSettings(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := acpTools(providers.ACPDetection{Available: true, Source: "path", BinPath: "claude-agent-acp"})
	mo := ModelOptions{Resolver: func(string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{AnthropicBaseURL: "https://h/", Model: "glm-5"}, true
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "claude", mo, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	raw := findSettingsJSON(l.Args)
	if raw == "" {
		t.Fatalf("ACP args 应含 --settings: %v", l.Args)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("非法 JSON: %v", err)
	}
	envObj, _ := m["env"].(map[string]any)
	if envObj["ANTHROPIC_MODEL"] != "glm-5" {
		t.Fatalf("ACP settings env = %+v", envObj)
	}
	if l.Env["ANTHROPIC_MODEL"] != "" {
		t.Fatalf("ACP 不应再走 env 注入, env = %v", l.Env)
	}
}

// codex 未实现 SettingsEnvInjector：行为不变（-m 前置 + OPENAI_* env）。
func TestForWorkspaceToolCodexUnchanged(t *testing.T) {
	ps := []providers.Provider{providers.Codex{}}
	tools := []discovery.Tool{{ID: "codex", Installed: true, BinPath: "codex"}}
	mo := ModelOptions{Resolver: func(string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{OpenAIBaseURL: "https://h/", APIKey: "k", Model: "gpt-x"}, true
	}}
	l, err := ForWorkspaceTool(ps, tools, discovery.Workspace{Path: "/p"}, "codex", ThemeOptions{}, mo, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if findSettingsJSON(l.Args) != "" {
		t.Fatalf("codex 不应注入 --settings: %v", l.Args)
	}
	if l.Env["OPENAI_BASE_URL"] != "https://h/" || l.Env["OPENAI_API_KEY"] != "k" {
		t.Fatalf("codex env = %+v", l.Env)
	}
}
