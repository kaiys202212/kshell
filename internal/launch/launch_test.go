package launch

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/appearance"
	"github.com/yangk/kshell/internal/cursoragent"
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
	if got := ErrNotLoggedIn.Error(); got != "err.launch.not_logged_in" {
		t.Fatalf("ErrNotLoggedIn = %q, want err.launch.not_logged_in", got)
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

// ForWorkspaceACP 登录门禁：Auth=out 的新建 ACP 直接拒绝（ErrNotLoggedIn）；
// in/unknown 保守放行——unknown 表示探测不稳定，宁可多跑一次失败的 ACP，
// 不能错杀已登录用户。显式 toolID 与 PreferredTool（toolID 为空）两条选路
// 都要被同一道门禁覆盖；恢复路径（ForSessionACP）按用户决策不拦，一并锁死。
func TestForWorkspaceACP_AuthGate(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	det := providers.ACPDetection{Available: true, Source: "path", BinPath: "claude-agent-acp"}
	cases := []struct {
		name    string
		auth    string
		wantErr error
	}{
		{"未登录拒绝", "out", ErrNotLoggedIn},
		{"已登录放行", "in", nil},
		{"未知保守放行", "unknown", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tools := []discovery.Tool{{ID: "claude", Installed: true, BinPath: "claude", Auth: c.auth, ACP: &det}}
			_, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "claude", ModelOptions{}, PermissionOptions{})
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("Auth=%q err = %v, want %v", c.auth, err, c.wantErr)
			}
		})
	}

	// PreferredTool 路径（toolID 为空）同样被门禁覆盖。
	tools := []discovery.Tool{{ID: "claude", Installed: true, BinPath: "claude", Auth: "out", ACP: &det}}
	ws := discovery.Workspace{Path: "/w", ToolCounts: map[string]int{"claude": 3}}
	if _, err := ForWorkspaceACP(ps, tools, ws, "", ModelOptions{}, PermissionOptions{}); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("PreferredTool 路径 Auth=out 应 ErrNotLoggedIn, got %v", err)
	}

	// 恢复路径不拦截（用户决策：恢复不门禁），防止后续误把门禁扩到 ForSessionACP。
	s := providers.Session{ID: "s1", ToolID: "claude", Workspace: "/p"}
	if _, err := ForSessionACP(ps, tools, s, ModelOptions{}, PermissionOptions{}); err != nil {
		t.Fatalf("Auth=out 恢复 ACP 不应被拒, got %v", err)
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

// Broken 形态（如 Cursor 更新器清空入口窗口内 Detected=config-dir）：
// 恢复会话必须拒绝——除非官方通道能从已知位置解析出入口。
func TestForSessionACP_NoRunnableBinRefused(t *testing.T) {
	oldShim := cursorRootShimFn
	oldEntry := cursorNodeEntryFn
	t.Cleanup(func() { cursorRootShimFn = oldShim; cursorNodeEntryFn = oldEntry })
	cursorRootShimFn = func() (string, bool) { return "", false }
	cursorNodeEntryFn = func() (string, bool) { return "", false }

	ps := []providers.Provider{providers.Cursor{}}
	s := providers.Session{ID: "s1", ToolID: "cursor", Workspace: "/proj"}
	det := providers.ACPDetection{Available: true, Source: "npx", Package: "cursor-acp"}

	broken := []discovery.Tool{{ID: "cursor", Installed: true, Broken: true, ACP: &det}}
	if _, err := ForSessionACP(ps, broken, s, ModelOptions{}, PermissionOptions{}); !errors.Is(err, ErrToolNotRunnable) {
		t.Fatalf("Broken 工具恢复会话应 ErrToolNotRunnable, got %v", err)
	}

	notInstalled := []discovery.Tool{{ID: "cursor", ACP: &det}}
	if _, err := ForSessionACP(ps, notInstalled, s, ModelOptions{}, PermissionOptions{}); !errors.Is(err, ErrToolNotRunnable) {
		t.Fatalf("未安装工具恢复会话应 ErrToolNotRunnable, got %v", err)
	}
}

// BinPath 为空的 Broken 窗口（更新器清空 versions\<ver>\node.exe，discovery 给
// BinPath=""）但根目录 shim 仍在：官方通道从已知位置兜底解析，新建与恢复会话都可用。
func TestForSessionACP_CursorBrokenBinRescuedByRootShim(t *testing.T) {
	shim := `C:\ca\cursor-agent.cmd`
	oldShim := cursorRootShimFn
	oldNode := nodeLookPathFn
	oldScript := cursorScriptResolveFn
	t.Cleanup(func() { cursorRootShimFn = oldShim; nodeLookPathFn = oldNode; cursorScriptResolveFn = oldScript })
	cursorRootShimFn = func() (string, bool) { return shim, true }
	nodeLookPathFn = func() (string, bool) { return `D:\nodejs\node.exe`, true }
	cursorScriptResolveFn = func(string) (string, bool) { return `C:\ca\versions\v1\index.js`, true }

	ps := []providers.Provider{providers.Cursor{}}
	s := providers.Session{ID: "s1", ToolID: "cursor", Workspace: "/proj"}
	det := providers.ACPDetection{Available: true, Source: "npx", Package: "cursor-acp"}
	broken := []discovery.Tool{{ID: "cursor", Installed: true, Broken: true, ACP: &det}}

	l, err := ForSessionACP(ps, broken, s, ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Path != `D:\nodejs\node.exe` || len(l.Args) != 2 || l.Args[0] != `C:\ca\versions\v1\index.js` || l.Args[1] != "acp" {
		t.Fatalf("launch = %+v", l)
	}
}

// 极端窗口：根目录 shim 也被删光、只剩 versions\<ver>\index.js——
// 用系统 node 直接跑最新版本的 index.js 官方 acp。
func TestForWorkspaceACP_CursorBrokenBinRescuedByVersionScript(t *testing.T) {
	root := t.TempDir()
	ver := filepath.Join(root, "versions", "2026.10.16-abc")
	if err := os.MkdirAll(ver, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ver, "index.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundled := filepath.Join(ver, "node.exe")
	if err := os.WriteFile(bundled, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldShim := cursorRootShimFn
	oldNode := nodeLookPathFn
	oldEntry := cursorNodeEntryFn
	t.Cleanup(func() { cursorRootShimFn = oldShim; nodeLookPathFn = oldNode; cursorNodeEntryFn = oldEntry })
	cursorRootShimFn = func() (string, bool) { return "", false }
	nodeLookPathFn = func() (string, bool) { return `D:\nodejs\node.exe`, true }
	cursorNodeEntryFn = func() (string, bool) { return filepath.Join(ver, "index.js"), true }

	ps := []providers.Provider{providers.Cursor{}}
	tools := []discovery.Tool{{ID: "cursor", Installed: true}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "cursor", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Path != bundled || len(l.Args) != 2 || l.Args[0] != filepath.Join(ver, "index.js") || l.Args[1] != "acp" {
		t.Fatalf("launch = %+v, want bundled %s", l, bundled)
	}
}

// cursor 原生二进制：官方通道直接 `本体 acp`，不再经第三方适配器、不注入任何 env。
func TestForWorkspaceACP_CursorExeOfficialACP(t *testing.T) {
	ps := []providers.Provider{providers.Cursor{}}
	tools := []discovery.Tool{{
		ID: "cursor", Installed: true, BinPath: `%LOCALAPPDATA%\cursor-agent\cursor-agent.exe`,
		ACP: &providers.ACPDetection{Available: true, Source: "path", BinPath: "cursor-acp"},
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "cursor", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Path != `%LOCALAPPDATA%\cursor-agent\cursor-agent.exe` || len(l.Args) != 1 || l.Args[0] != "acp" {
		t.Fatalf("launch = %+v", l)
	}
	if len(l.Env) != 0 {
		t.Fatalf("官方通道不应注入 env, got %v", l.Env)
	}
}

// node 入口形态：官方通道由 kshell 直接 spawn `node index.js acp`，
// 不再需要可 spawn 的 PE 代理（kshell 自身就是 ACP 客户端）。
func TestForWorkspaceACP_CursorNodeEntryOfficialACP(t *testing.T) {
	ps := []providers.Provider{providers.Cursor{}}
	tools := []discovery.Tool{{
		ID: "cursor", Installed: true, BinPath: `C:\agent\node.exe`, BinArgs: []string{`C:\agent\index.js`},
		ACP: &providers.ACPDetection{Available: true, Source: "npx", Package: "cursor-acp"},
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "cursor", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Path != `C:\agent\node.exe` || len(l.Args) != 2 || l.Args[0] != `C:\agent\index.js` || l.Args[1] != "acp" {
		t.Fatalf("launch = %+v", l)
	}
	if len(l.Env) != 0 {
		t.Fatalf("官方通道不应注入 env, got %v", l.Env)
	}
}

// 非 cursor 工具不得注入任何环境变量，防止回归为全量注入。
// claude 是 git-bash 注入对象，这里 mock 探测未命中使 env 确定为空
// （真实探测依赖本机 PATH/注册表，不能作为断言依据）。
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

// shim 形态（.cmd/.bat）：官方通道解析 node + versions\<ver>\index.js 后
// 直接 spawn，不依赖第三方 cursor-acp，也不注入代理三元组。
func TestForWorkspaceACP_CursorCmdShimOfficialACP(t *testing.T) {
	root := t.TempDir()
	ver := filepath.Join(root, "versions", "2026.10.16-e3cb207")
	if err := os.MkdirAll(ver, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile := func(p string) {
		t.Helper()
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(filepath.Join(root, "cursor-agent.cmd"))
	writeFile(filepath.Join(ver, "index.js"))
	bundled := filepath.Join(ver, "node.exe")
	writeFile(bundled)
	shim := filepath.Join(root, "cursor-agent.cmd")

	oldNode := nodeLookPathFn
	oldScript := cursorScriptResolveFn
	t.Cleanup(func() { nodeLookPathFn = oldNode; cursorScriptResolveFn = oldScript })
	// PATH 上另有系统 Node：官方通道必须优先 versions 内自带 node.exe
	nodeLookPathFn = func() (string, bool) { return `D:\nodejs\node.exe`, true }
	cursorScriptResolveFn = func(shimPath string) (string, bool) {
		return filepath.Join(ver, "index.js"), true
	}

	ps := []providers.Provider{providers.Cursor{}}
	tools := []discovery.Tool{{
		ID: "cursor", Installed: true, BinPath: shim,
		ACP: &providers.ACPDetection{Available: true, Source: "path", BinPath: "cursor-acp"},
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "cursor", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Path != bundled || len(l.Args) != 2 || l.Args[0] != filepath.Join(ver, "index.js") || l.Args[1] != "acp" {
		t.Fatalf("launch = %+v, want bundled node %s", l, bundled)
	}
	if len(l.Env) != 0 {
		t.Fatalf("官方通道不应注入 env, got %v", l.Env)
	}
}

// 无自带 node.exe 时仍回落 PATH node（老布局 / 残缺安装兜底）。
func TestForWorkspaceACP_CursorCmdShimFallsBackToPathNode(t *testing.T) {
	root := t.TempDir()
	ver := filepath.Join(root, "versions", "2026.10.16-e3cb207")
	if err := os.MkdirAll(ver, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ver, "index.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(root, "cursor-agent.cmd")
	if err := os.WriteFile(shim, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldNode := nodeLookPathFn
	oldScript := cursorScriptResolveFn
	t.Cleanup(func() { nodeLookPathFn = oldNode; cursorScriptResolveFn = oldScript })
	nodeLookPathFn = func() (string, bool) { return `D:\nodejs\node.exe`, true }
	cursorScriptResolveFn = func(string) (string, bool) { return filepath.Join(ver, "index.js"), true }

	ps := []providers.Provider{providers.Cursor{}}
	tools := []discovery.Tool{{ID: "cursor", Installed: true, BinPath: shim}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "cursor", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Path != `D:\nodejs\node.exe` {
		t.Fatalf("无自带 node 时应回落 PATH, got %+v", l)
	}
}

// shim 形态但 node 解析不到：官方通道不可用，回退第三方适配器路径
// （cursor-acp 在 PATH 上时直执行 + 直注 shim env，保持既有兜底行为）。
func TestForWorkspaceACP_CursorCmdShimWithoutNodeFallsBack(t *testing.T) {
	shim := `C:\ca\cursor-agent.cmd`
	oldNode := nodeLookPathFn
	t.Cleanup(func() { nodeLookPathFn = oldNode })
	nodeLookPathFn = func() (string, bool) { return "", false }

	ps := []providers.Provider{providers.Cursor{}}
	tools := []discovery.Tool{{
		ID: "cursor", Installed: true, BinPath: shim,
		ACP: &providers.ACPDetection{Available: true, Source: "path", BinPath: "cursor-acp"},
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "cursor", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Path != "cursor-acp" || len(l.Args) != 0 {
		t.Fatalf("launch = %+v, 期望回退 cursor-acp 适配器", l)
	}
	if l.Env["CURSOR_AGENT_EXECUTABLE"] != shim {
		t.Fatalf("env = %v, 期望退回直注 shim", l.Env)
	}
	if _, ok := l.Env[cursoragent.EnvAsProxy]; ok {
		t.Fatalf("无 node 时不得注入代理变量, got %v", l.Env)
	}
}

// 非结束（.exe / 无后缀）形态走官方直连；stub 被调用即失败，
// 锁死 .exe 分支绝不进入 shim 解析。
func TestForWorkspaceACP_CursorExeOfficialNoShimResolve(t *testing.T) {
	oldNode := nodeLookPathFn
	oldScript := cursorScriptResolveFn
	t.Cleanup(func() { nodeLookPathFn = oldNode; cursorScriptResolveFn = oldScript })
	nodeLookPathFn = func() (string, bool) { t.Error(".exe 分支不得解析 node"); return "", false }
	cursorScriptResolveFn = func(string) (string, bool) { t.Error(".exe 分支不得解析脚本"); return "", false }

	ps := []providers.Provider{providers.Cursor{}}
	tools := []discovery.Tool{{
		ID: "cursor", Installed: true, BinPath: `C:\ca\cursor-agent.exe`,
		ACP: &providers.ACPDetection{Available: true, Source: "path", BinPath: "cursor-acp"},
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "cursor", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Path != `C:\ca\cursor-agent.exe` || len(l.Args) != 1 || l.Args[0] != "acp" {
		t.Fatalf("launch = %+v", l)
	}
}

// .bat 同样是 shim 形态，与 .cmd 同走官方直连。
func TestForWorkspaceACP_CursorBatShimOfficialACP(t *testing.T) {
	oldNode := nodeLookPathFn
	oldScript := cursorScriptResolveFn
	t.Cleanup(func() { nodeLookPathFn = oldNode; cursorScriptResolveFn = oldScript })
	nodeLookPathFn = func() (string, bool) { return `D:\nodejs\node.exe`, true }
	cursorScriptResolveFn = func(string) (string, bool) { return `C:\ca\index.js`, true }

	ps := []providers.Provider{providers.Cursor{}}
	tools := []discovery.Tool{{
		ID: "cursor", Installed: true, BinPath: `C:\ca\agent.bat`,
		ACP: &providers.ACPDetection{Available: true, Source: "path", BinPath: "cursor-acp"},
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "cursor", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Path != `D:\nodejs\node.exe` || len(l.Args) != 2 || l.Args[0] != `C:\ca\index.js` || l.Args[1] != "acp" {
		t.Fatalf("launch = %+v", l)
	}
}

// shim 形态但脚本解析不到（如 index.js 一并被更新器删光）：官方通道不可用，
// 回退第三方适配器路径（直注 shim env，无代理变量）。
func TestForWorkspaceACP_CursorCmdShimWithoutScriptFallsBack(t *testing.T) {
	shim := `C:\ca\cursor-agent.cmd`
	oldNode := nodeLookPathFn
	oldScript := cursorScriptResolveFn
	t.Cleanup(func() { nodeLookPathFn = oldNode; cursorScriptResolveFn = oldScript })
	nodeLookPathFn = func() (string, bool) { return `D:\nodejs\node.exe`, true }
	cursorScriptResolveFn = func(string) (string, bool) { return "", false }

	ps := []providers.Provider{providers.Cursor{}}
	tools := []discovery.Tool{{
		ID: "cursor", Installed: true, BinPath: shim,
		ACP: &providers.ACPDetection{Available: true, Source: "path", BinPath: "cursor-acp"},
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "cursor", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Path != "cursor-acp" || len(l.Args) != 0 {
		t.Fatalf("launch = %+v, 期望回退 cursor-acp 适配器", l)
	}
	if l.Env["CURSOR_AGENT_EXECUTABLE"] != shim {
		t.Fatalf("env = %v, 期望退回直注 shim", l.Env)
	}
	if _, ok := l.Env[cursoragent.EnvAsProxy]; ok {
		t.Fatalf("无脚本时不得注入代理变量, got %v", l.Env)
	}
}

// parseVersionDir 表驱动：合法形态解析与非法形态拒绝。
func TestParseVersionDir(t *testing.T) {
	cases := []struct {
		name  string
		want  [6]int
		valid bool
	}{
		{"2026.10.16-e3cb207", [6]int{2026, 10, 16, 0, 0, 0}, true},
		{"2026.9.1-abc", [6]int{2026, 9, 1, 0, 0, 0}, true},
		{"2026.10.16-12-30-05-e3cb207", [6]int{2026, 10, 16, 12, 30, 5}, true},
		{"2026.10", [6]int{}, false},
		{"2026.10.16", [6]int{}, false},
		{"2026.10.16-12-abc", [6]int{}, false}, // 3 段非法
		{"abc.def.ghi-x", [6]int{}, false},
		{"", [6]int{}, false},
	}
	for _, c := range cases {
		got, ok := parseVersionDir(c.name)
		if ok != c.valid {
			t.Errorf("parseVersionDir(%q) valid = %v, want %v", c.name, ok, c.valid)
			continue
		}
		if ok && got != c.want {
			t.Errorf("parseVersionDir(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// cursorVersionDirs 表驱动：多版本按 6 段数值降序（月份不补零不跨月错序），
// 畸形目录排最后；versions 目录不存在返回空。
func TestCursorVersionDirs(t *testing.T) {
	root := t.TempDir()
	vers := filepath.Join(root, "versions")
	mk := func(names ...string) {
		t.Helper()
		for _, n := range names {
			if err := os.MkdirAll(filepath.Join(vers, n), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("2026.10.16-aaa", "2026.9.20-bbb", "2026.10.16-12-00-00-ccc", "garbage-dir", "bad.10.16-x")
	got := cursorVersionDirs(root)
	// garbage-dir 无 . 和 - 直接跳过；bad.10.16-x 含两者但解析失败，排最后兜底。
	want := []string{
		filepath.Join(vers, "2026.10.16-12-00-00-ccc"),
		filepath.Join(vers, "2026.10.16-aaa"),
		filepath.Join(vers, "2026.9.20-bbb"),
		filepath.Join(vers, "bad.10.16-x"),
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if dirs := cursorVersionDirs(filepath.Join(root, "no-such")); dirs != nil {
		t.Fatalf("versions 不存在应返回 nil, got %v", dirs)
	}
}

// 官方通道不可用（shim 且无 node）时回退 npx 适配器，env 仍直注 shim。
func TestForWorkspaceACP_CursorNpxFallbackWithoutNode(t *testing.T) {
	shim := `C:\ca\cursor-agent.cmd`
	oldNode := nodeLookPathFn
	t.Cleanup(func() { nodeLookPathFn = oldNode })
	nodeLookPathFn = func() (string, bool) { return "", false }

	ps := []providers.Provider{providers.Cursor{}}
	tools := []discovery.Tool{{
		ID: "cursor", Installed: true, BinPath: shim,
		ACP: &providers.ACPDetection{Available: true, Source: "npx", Package: "cursor-acp"},
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "cursor", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Path != "npx" || len(l.Args) != 2 || l.Args[0] != "-y" || l.Args[1] != "cursor-acp" {
		t.Fatalf("args = %v", l.Args)
	}
	if l.Env["CURSOR_AGENT_EXECUTABLE"] != shim {
		t.Fatalf("env = %v", l.Env)
	}
}

// 官方 acp 子命令不接受外部模型名（`--model <自定义名>` 进程即退出
// "Cannot use this model"），cursor 的模型切换只能走会话内 session/set_model：
// 模型注入必须整体跳过。
func TestForWorkspaceACP_CursorOfficialSkipsModelInjection(t *testing.T) {
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
	if len(l.Args) != 1 || l.Args[0] != "acp" {
		t.Fatalf("args = %v, want [acp]", l.Args)
	}
}

// 恢复会话路径同样走官方直连且不注入模型。
// opencode 的 acp 是子命令，不接受 --auto 这类权限全局参数：注入后
// `opencode --auto acp` 会把 acp 当成项目目录并退出（Failed to change
// directory to ...\acp）。ACP 权限由 kshell 自身自动放行，必须整体跳过注入。
func TestForACP_OpencodeSkipsPermissionFlag(t *testing.T) {
	ps := []providers.Provider{providers.Opencode{}}
	tools := []discovery.Tool{{
		ID: "opencode", Installed: true, BinPath: "opencode.cmd",
		ACP: &providers.ACPDetection{Available: true, Source: "path", BinPath: "opencode.cmd", ExtraArgs: []string{"acp"}},
	}}
	po := PermissionOptions{Bypass: true}

	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "opencode", ModelOptions{}, po)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Args) != 1 || l.Args[0] != "acp" {
		t.Fatalf("新建 ACP args = %v, want [acp]", l.Args)
	}

	s := providers.Session{ID: "s1", ToolID: "opencode", Workspace: "/w"}
	l, err = ForSessionACP(ps, tools, s, ModelOptions{}, po)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Args) != 1 || l.Args[0] != "acp" {
		t.Fatalf("恢复 ACP args = %v, want [acp]", l.Args)
	}
}

func TestForSessionACP_CursorOfficialSkipsModelInjection(t *testing.T) {
	ps := []providers.Provider{providers.Cursor{}}
	tools := []discovery.Tool{{
		ID: "cursor", Installed: true, BinPath: "cursor-agent",
		ACP: &providers.ACPDetection{Available: true, Source: "path", BinPath: "cursor-acp"},
	}}
	s := providers.Session{ID: "s1", ToolID: "cursor", Workspace: "/p"}
	mo := ModelOptions{Resolver: func(string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{Model: "m1"}, true
	}}
	l, err := ForSessionACP(ps, tools, s, mo, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Dir != "/p" {
		t.Fatalf("dir = %q", l.Dir)
	}
	if len(l.Args) != 1 || l.Args[0] != "acp" {
		t.Fatalf("args = %v, want [acp]", l.Args)
	}
}

// 官方通道不依赖 cursor-acp 适配器探测：ACP 探测不可用时仍可直连。
func TestForWorkspaceACP_CursorOfficialWithoutAdapterDetection(t *testing.T) {
	ps := []providers.Provider{providers.Cursor{}}
	tools := []discovery.Tool{{
		ID: "cursor", Installed: true, BinPath: "cursor-agent",
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "cursor", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Path != "cursor-agent" || len(l.Args) != 1 || l.Args[0] != "acp" {
		t.Fatalf("launch = %+v", l)
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
	// ACP 端点/密钥走 ACPSettingsJSON（_meta），不再经 argv --settings。
	if l.ACPSettingsJSON == "" {
		t.Fatalf("ACP 应产出 ACPSettingsJSON: %+v", l)
	}
	if findSettingsJSON(l.Args) != "" {
		t.Fatalf("ACP argv 不应再含模型 --settings: %v", l.Args)
	}
	if l.Env["ANTHROPIC_MODEL"] != "" || l.Env["ANTHROPIC_BASE_URL"] != "" {
		t.Fatalf("ACP 不应走进程 env 注入: %v", l.Env)
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

// claude 终端路径：端点/密钥改静态写入 ~/.claude/settings.json，启动时
// 不再经 --settings 合入 ANTHROPIC_*；也不走进程 env。主题 --settings 可保留。
func TestForWorkspaceToolClaudeModelNoSettingsInject(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := []discovery.Tool{{ID: "claude", Installed: true, BinPath: "claude"}}
	mo := ModelOptions{Resolver: func(string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{AnthropicBaseURL: "https://h/", APIKey: "k", Model: "glm-5"}, true
	}}
	l, err := ForWorkspaceTool(ps, tools, discovery.Workspace{Path: "/p"}, "claude", ThemeOptions{}, mo, PermissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if raw := findSettingsJSON(l.Args); raw != "" {
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatalf("--settings 非法 JSON: %v", err)
		}
		if envObj, _ := m["env"].(map[string]any); envObj != nil {
			for _, k := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_MODEL"} {
				if _, ok := envObj[k]; ok {
					t.Fatalf("终端 --settings 不应合入 %s: %+v", k, m)
				}
			}
		}
	}
	if l.Env["ANTHROPIC_MODEL"] != "" || l.Env["ANTHROPIC_BASE_URL"] != "" || l.Env["ANTHROPIC_AUTH_TOKEN"] != "" {
		t.Fatalf("claude 不应走进程 env 注入, env = %v", l.Env)
	}
}

// 主题仍走 --settings；模型 env 不再合入（静态 settings.json 已覆盖端点）。
func TestForWorkspaceToolClaudeThemeSettingsOnly(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := []discovery.Tool{{ID: "claude", Installed: true, BinPath: "claude"}}
	mo := ModelOptions{Resolver: func(string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{Model: "glm-5", AnthropicBaseURL: "https://h/", APIKey: "k"}, true
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
		t.Fatalf("主题应保留单个 --settings, got %d: %v", count, l.Args)
	}
	raw := findSettingsJSON(l.Args)
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("非法 JSON: %v", err)
	}
	if m["theme"] != "dark" {
		t.Fatalf("主题键应保留: %+v", m)
	}
	if envObj, _ := m["env"].(map[string]any); envObj != nil {
		if _, ok := envObj["ANTHROPIC_BASE_URL"]; ok {
			t.Fatalf("主题 --settings 不应再合入模型 env: %+v", m)
		}
		if _, ok := envObj["ANTHROPIC_MODEL"]; ok {
			t.Fatalf("主题 --settings 不应再合入模型 env: %+v", m)
		}
	}
}

// ACP 路径经 ACPSettingsJSON（_meta）注入，argv 不再带模型 --settings。
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
	if l.ACPSettingsJSON == "" {
		t.Fatalf("ACP 应产出 ACPSettingsJSON")
	}
	if findSettingsJSON(l.Args) != "" {
		t.Fatalf("ACP argv 不应含模型 --settings: %v", l.Args)
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

// ACP npx + bypass：权限参数必须落在 -y <pkg> 之后；模型走 ACPSettingsJSON。
func TestForSessionACPClaudeNpxSettingsWithBypass(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := acpTools(providers.ACPDetection{Available: true, Source: "npx", Package: "pkg"})
	mo := ModelOptions{Resolver: func(string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{AnthropicBaseURL: "https://h/", Model: "glm-5"}, true
	}}
	s := providers.Session{ID: "ses_x", ToolID: "claude", Workspace: "/w"}
	l, err := ForSessionACP(ps, tools, s, mo, PermissionOptions{Bypass: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Args) < 3 || l.Args[0] != "-y" || l.Args[1] != "pkg" {
		t.Fatalf("args 应以 -y pkg 开头: %v", l.Args)
	}
	if l.ACPSettingsJSON == "" {
		t.Fatalf("应产出 ACPSettingsJSON")
	}
	if findSettingsJSON(l.Args) != "" {
		t.Fatalf("argv 不应含模型 --settings: %v", l.Args)
	}
	skip := false
	for _, a := range l.Args[2:] {
		if a == "--dangerously-skip-permissions" {
			skip = true
		}
	}
	if !skip {
		t.Fatalf("args 应含 bypass 参数: %v", l.Args)
	}
}

// claude ACP 必须产出 ACPSettingsJSON 载荷——完整的 _meta 形态
// {"claudeCode":{"options":{"settings":"{\"env\":{...}}"}}}，settings 值为
// JSON 字符串（SDK 原样透传进 spawn argv，对象形态会变 "[object Object]"）。
func TestFinalizeACPClaudeSettingsPayload(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := acpTools(providers.ACPDetection{Available: true, Source: "path", BinPath: "claude-agent-acp"})
	mo := ModelOptions{Resolver: func(string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{AnthropicBaseURL: "https://h/", APIKey: "k", Model: "glm-5.3-flash"}, true
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "claude", mo, PermissionOptions{})
	if err != nil {
		t.Fatalf("ForWorkspaceACP: %v", err)
	}
	if l.ACPSettingsJSON == "" {
		t.Fatalf("claude ACP 应产出 ACPSettingsJSON, launch=%+v", l)
	}
	var meta struct {
		ClaudeCode struct {
			Options struct {
				Settings string `json:"settings"`
			} `json:"options"`
		} `json:"claudeCode"`
	}
	if err := json.Unmarshal([]byte(l.ACPSettingsJSON), &meta); err != nil {
		t.Fatalf("载荷非法 JSON: %v (%q)", err, l.ACPSettingsJSON)
	}
	var envObj map[string]any
	if err := json.Unmarshal([]byte(meta.ClaudeCode.Options.Settings), &envObj); err != nil {
		t.Fatalf("settings 值应为内联 JSON 字符串: %v (%q)", err, meta.ClaudeCode.Options.Settings)
	}
	env, _ := envObj["env"].(map[string]any)
	if env["ANTHROPIC_MODEL"] != "glm-5.3-flash" || env["ANTHROPIC_BASE_URL"] != "https://h/" || env["ANTHROPIC_AUTH_TOKEN"] != "k" {
		t.Fatalf("env 块不符: %+v", env)
	}
}

// OpenCode Go 网关要求请求带 x-opencode-session：旧版 SDK 内置 CLI 不发原生
// 会话头（实测 2.1.83 无、独立 2.1.301 有），须经 ACP options.env 的
// ANTHROPIC_CUSTOM_HEADERS 补上，且每次启动生成独立 UUID。
func TestFinalizeACPClaudeOpencodeSessionHeader(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := acpTools(providers.ACPDetection{Available: true, Source: "path", BinPath: "claude-agent-acp"})
	mo := ModelOptions{Resolver: func(string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{
			AnthropicBaseURL: "https://opencode.ai/inference/go/anthropic",
			APIKey:           "k",
			Model:            "glm-5.3-flash",
		}, true
	}}
	optEnv := func(l providers.Launch) map[string]any {
		t.Helper()
		var meta struct {
			ClaudeCode struct {
				Options struct {
					Settings string         `json:"settings"`
					Env      map[string]any `json:"env"`
				} `json:"options"`
			} `json:"claudeCode"`
		}
		if err := json.Unmarshal([]byte(l.ACPSettingsJSON), &meta); err != nil {
			t.Fatalf("载荷非法 JSON: %v (%q)", err, l.ACPSettingsJSON)
		}
		var settings map[string]any
		if err := json.Unmarshal([]byte(meta.ClaudeCode.Options.Settings), &settings); err != nil {
			t.Fatalf("settings 值应为内联 JSON 字符串: %v", err)
		}
		env, _ := settings["env"].(map[string]any)
		if env["ANTHROPIC_BASE_URL"] != "https://opencode.ai/inference/go/anthropic" {
			t.Fatalf("settings.env 应保留网关端点: %+v", env)
		}
		return meta.ClaudeCode.Options.Env
	}

	var headers []string
	for i := 0; i < 2; i++ {
		l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "claude", mo, PermissionOptions{})
		if err != nil {
			t.Fatalf("ForWorkspaceACP: %v", err)
		}
		h, _ := optEnv(l)["ANTHROPIC_CUSTOM_HEADERS"].(string)
		if !strings.HasPrefix(h, "x-opencode-session: ") {
			t.Fatalf("应补 x-opencode-session 头, got %q", h)
		}
		headers = append(headers, h)
	}
	if headers[0] == headers[1] {
		t.Fatalf("不同会话应使用不同 session id: %q", headers[0])
	}
}

// 非 OpenCode 网关不注入会话头（官方 Anthropic / 第三方端点无需该头）。
func TestFinalizeACPClaudeOtherGatewayNoSessionHeader(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := acpTools(providers.ACPDetection{Available: true, Source: "path", BinPath: "claude-agent-acp"})
	mo := ModelOptions{Resolver: func(string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{AnthropicBaseURL: "https://h/", APIKey: "k", Model: "glm-5"}, true
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "claude", mo, PermissionOptions{})
	if err != nil {
		t.Fatalf("ForWorkspaceACP: %v", err)
	}
	var meta struct {
		ClaudeCode struct {
			Options struct {
				Env map[string]any `json:"env"`
			} `json:"options"`
		} `json:"claudeCode"`
	}
	if err := json.Unmarshal([]byte(l.ACPSettingsJSON), &meta); err != nil {
		t.Fatalf("载荷非法 JSON: %v", err)
	}
	if v, ok := meta.ClaudeCode.Options.Env["ANTHROPIC_CUSTOM_HEADERS"]; ok {
		t.Fatalf("非 OpenCode 网关不应注入会话头: %v", v)
	}
}

// isOpencodeGateway 的域名/路径语义：大小写、端口、子域、伪后缀、无尾斜杠。
func TestIsOpencodeGateway(t *testing.T) {
	cases := []struct {
		base string
		want bool
	}{
		{"https://opencode.ai/inference/go/anthropic", true},
		{"https://OPENCODE.AI/inference", true},          // 大写 host + 无尾斜杠
		{"https://opencode.ai:8443/inference/go", true},  // 带端口
		{"https://api.opencode.ai/inference/anthropic", true}, // 子域
		{"https://notopencode.ai/inference/go", false},   // 伪后缀
		{"https://opencode.ai.evil.com/inference/go", false}, // 域名不在末尾
		{"https://opencode.ai/console", false},           // 非 inference 路径
		{"https://api.anthropic.com", false},             // 官方端点
		{"", false},
		{"::bad-url::", false},
	}
	for _, c := range cases {
		if got := isOpencodeGateway(c.base); got != c.want {
			t.Errorf("isOpencodeGateway(%q) = %v, want %v", c.base, got, c.want)
		}
	}
}

// 恢复路径（ForSessionACP）同样注入会话头：桌面端恢复历史会话走同一载荷。
func TestForSessionACPClaudeOpencodeSessionHeader(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := acpTools(providers.ACPDetection{Available: true, Source: "path", BinPath: "claude-agent-acp"})
	mo := ModelOptions{Resolver: func(string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{AnthropicBaseURL: "https://opencode.ai/inference/go/anthropic", APIKey: "k", Model: "glm-5"}, true
	}}
	s := providers.Session{ID: "ses_x", ToolID: "claude", Workspace: "/w"}
	l, err := ForSessionACP(ps, tools, s, mo, PermissionOptions{})
	if err != nil {
		t.Fatalf("ForSessionACP: %v", err)
	}
	var meta struct {
		ClaudeCode struct {
			Options struct {
				Env map[string]any `json:"env"`
			} `json:"options"`
		} `json:"claudeCode"`
	}
	if err := json.Unmarshal([]byte(l.ACPSettingsJSON), &meta); err != nil {
		t.Fatalf("载荷非法 JSON: %v", err)
	}
	if h, _ := meta.ClaudeCode.Options.Env["ANTHROPIC_CUSTOM_HEADERS"].(string); !strings.HasPrefix(h, "x-opencode-session: ") {
		t.Fatalf("恢复路径应补 x-opencode-session 头, got %q", h)
	}
}

// 非 claude 工具不得产出 ACPSettingsJSON。
func TestFinalizeACPNonClaudeNoSettings(t *testing.T) {
	ps := []providers.Provider{providers.Codex{}}
	tools := []discovery.Tool{{ID: "codex", Name: "Codex", Installed: true, BinPath: "codex", ACP: &providers.ACPDetection{Available: true, Source: "path", BinPath: "codex-acp"}}}
	mo := ModelOptions{Resolver: func(string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{OpenAIBaseURL: "https://h/", APIKey: "k", Model: "gpt-x"}, true
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "codex", mo, PermissionOptions{})
	if err != nil {
		t.Fatalf("ForWorkspaceACP: %v", err)
	}
	if l.ACPSettingsJSON != "" {
		t.Fatalf("codex ACP 不应产出 settings 载荷: %q", l.ACPSettingsJSON)
	}
}

// 模型未启用（Resolver nil）或 cfg 全空时不得产出。
func TestFinalizeACPEmptyConfigNoSettings(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := acpTools(providers.ACPDetection{Available: true, Source: "path", BinPath: "claude-agent-acp"})
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "claude", ModelOptions{}, PermissionOptions{})
	if err != nil {
		t.Fatalf("ForWorkspaceACP: %v", err)
	}
	if l.ACPSettingsJSON != "" {
		t.Fatalf("Resolver nil 不应产出: %q", l.ACPSettingsJSON)
	}
	mo := ModelOptions{Resolver: func(string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{}, true
	}}
	l, err = ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "claude", mo, PermissionOptions{})
	if err != nil {
		t.Fatalf("ForWorkspaceACP: %v", err)
	}
	if l.ACPSettingsJSON != "" {
		t.Fatalf("空 cfg 不应产出: %q", l.ACPSettingsJSON)
	}
}

// 终端路径不得产出 ACPSettingsJSON（那是 ACP 专属通道）。
func TestTerminalNoACPSettingsPayload(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := []discovery.Tool{{ID: "claude", Installed: true, BinPath: "claude"}}
	mo := ModelOptions{Resolver: func(string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{AnthropicBaseURL: "https://h/", APIKey: "k", Model: "glm-5"}, true
	}}
	l, err := ForWorkspaceTool(ps, tools, discovery.Workspace{Path: "/w"}, "claude", ThemeOptions{}, mo, PermissionOptions{})
	if err != nil {
		t.Fatalf("ForWorkspaceTool: %v", err)
	}
	if l.ACPSettingsJSON != "" {
		t.Fatalf("终端路径不应产出 ACPSettingsJSON: %q", l.ACPSettingsJSON)
	}
}

// ACPCapable 轻量能力判定：适配器可用或 cursor 官方直连可解析即 true。
func TestACPCapable(t *testing.T) {
	orig := cursorRootShimFn
	origNode := nodeLookPathFn
	origScript := cursorScriptResolveFn
	t.Cleanup(func() { cursorRootShimFn = orig; nodeLookPathFn = origNode; cursorScriptResolveFn = origScript })
	cursorRootShimFn = func() (string, bool) { return "C:/fake/cursor-agent.cmd", true }
	nodeLookPathFn = func() (string, bool) { return "C:/fake/node.exe", true }
	cursorScriptResolveFn = func(string) (string, bool) { return "C:/fake/versions/0.1/index.js", true }

	cases := []struct {
		name string
		tool discovery.Tool
		want bool
	}{
		{"适配器可用", discovery.Tool{ID: "claude", ACP: &providers.ACPDetection{Available: true}}, true},
		{"适配器探测不可用", discovery.Tool{ID: "gemini", ACP: &providers.ACPDetection{Available: false}}, false},
		{"无 ACP 探测", discovery.Tool{ID: "codex"}, false},
		{"cursor 官方直连兜底", discovery.Tool{ID: "cursor"}, true},
	}
	for _, c := range cases {
		if got := ACPCapable(c.tool); got != c.want {
			t.Fatalf("%s: ACPCapable = %v, 想要 %v", c.name, got, c.want)
		}
	}
}
