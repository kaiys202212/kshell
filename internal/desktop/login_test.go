package desktop

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/applang"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/terminal"
)

// fakeLoginProvider 在 fakeProvider 上叠加可控的 LoginCmd 与显示名,
// 验证 OpenLoginTerminal 的组装口径。
type fakeLoginProvider struct {
	fakeProvider
	name string
	cmd  []string
}

func (f *fakeLoginProvider) DisplayName() string { return f.name }
func (f *fakeLoginProvider) LoginCmd() []string  { return f.cmd }

// newLoginApp 组装带终端桩的 App(不触发真实扫描),供登录终端测试使用。
// 默认探测表里 cursor 已装(BinPath 为裸名,与历史用例口径一致)。
func newLoginApp(t *testing.T, ps []providers.Provider) (*App, *stubTermBackend) {
	t.Helper()
	return newLoginAppWithTools(t, ps, []discovery.Tool{
		{ID: "cursor", Name: "Cursor", Installed: true, BinPath: "cursor-agent"},
	})
}

// newLoginAppWithTools 在 newLoginApp 基础上允许自定义工具探测表,
// 验证 OpenLoginTerminal 对 tool.BinPath/BinArgs 的复用与回退。
func newLoginAppWithTools(t *testing.T, ps []providers.Provider, tools []discovery.Tool) (*App, *stubTermBackend) {
	t.Helper()
	dir := t.TempDir()
	backend := &stubTermBackend{}
	app := NewAppWith(Options{
		Home:      dir,
		Providers: ps,
		Layout: config.Layout{
			Root:   dir,
			Config: filepath.Join(dir, "config.yaml"),
			Cache:  filepath.Join(dir, "cache"),
		},
		Terminals: newTerminalManagerWith(func(string, ...any) {}, backend),
		Emit:      func(string, ...any) {},
	})
	setTools(t, app, tools)
	return app, backend
}

// stubHomeDir 把 os.UserHomeDir 换成桩并注册恢复,返回桩目录。
func stubHomeDir(t *testing.T, dir string) {
	t.Helper()
	orig := userHomeDir
	userHomeDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { userHomeDir = orig })
}

func TestOpenLoginTerminalStartsLoginTerminal(t *testing.T) {
	app, backend := newLoginApp(t, []providers.Provider{
		&fakeLoginProvider{id: "cursor", name: "Cursor", cmd: []string{"cursor-agent", "login"}},
	})
	stubHomeDir(t, `D:\home-u`)
	// 标题直显文案依赖当前语言,钉住语言让断言稳定
	applang.Set("zh-CN")
	t.Cleanup(func() { applang.Set("en") })

	info, err := app.OpenLoginTerminal("cursor", 100, 30)
	if err != nil {
		t.Fatalf("OpenLoginTerminal error: %v", err)
	}
	if info.Key != "login:cursor" || info.Kind != terminal.KindLogin || info.ToolID != "cursor" {
		t.Fatalf("Info key/kind/tool = %+v", info)
	}
	if info.Workspace != "" {
		t.Fatalf("登录终端不依赖工作区, Workspace = %q", info.Workspace)
	}
	if info.Title != "Cursor 登录" {
		t.Fatalf("Title = %q, 期望「Cursor 登录」", info.Title)
	}
	if info.Status != terminal.StatusRunning || info.ID == "" {
		t.Fatalf("Info 状态 = %+v", info)
	}

	spec := backend.lastSpec()
	if spec.Path != "cursor-agent" || len(spec.Args) != 1 || spec.Args[0] != "login" {
		t.Fatalf("Spec 未按 LoginCmd 组装 = %+v", spec)
	}
	if spec.Dir != `D:\home-u` {
		t.Fatalf("Spec.Dir = %q, 期望用户主目录", spec.Dir)
	}
	if backend.starts() != 1 {
		t.Fatalf("应只起一个进程, starts = %d", backend.starts())
	}
}

// TestOpenLoginTerminalReusesRunning 同 key 且运行中重复调用直接复用既有终端:
// 登录弹窗可能重复触发打开,绝不重复起登录进程。
func TestOpenLoginTerminalReusesRunning(t *testing.T) {
	app, backend := newLoginApp(t, []providers.Provider{
		&fakeLoginProvider{id: "cursor", name: "Cursor", cmd: []string{"cursor-agent", "login"}},
	})

	first, err := app.OpenLoginTerminal("cursor", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	again, err := app.OpenLoginTerminal("cursor", 80, 24)
	if err != nil {
		t.Fatalf("重复 OpenLoginTerminal error: %v", err)
	}
	if again.ID != first.ID || backend.starts() != 1 {
		t.Fatalf("运行中应复用终端: again=%+v first=%+v starts=%d", again, first, backend.starts())
	}
}

// TestOpenLoginTerminalUnsupported 无登录命令能力或 toolID 未知的都判不支持:
// 哨兵错误固定为 err.login.unsupported,前端据此提示而非报异常栈。
func TestOpenLoginTerminalUnsupported(t *testing.T) {
	cases := []struct {
		name   string
		ps     []providers.Provider
		toolID string
	}{
		{"未实现 LoginCmdProvider", []providers.Provider{fakeProvider{id: "claude"}}, "claude"},
		{"toolID 不在 provider 表", []providers.Provider{
			&fakeLoginProvider{id: "cursor", name: "Cursor", cmd: []string{"cursor-agent", "login"}},
		}, "nope"},
		{"LoginCmd 为空", []providers.Provider{&fakeLoginProvider{id: "cursor", name: "Cursor"}}, "cursor"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			app, backend := newLoginApp(t, c.ps)
			_, err := app.OpenLoginTerminal(c.toolID, 80, 24)
			if !errors.Is(err, errLoginUnsupported) {
				t.Fatalf("OpenLoginTerminal err = %v, want %v", err, errLoginUnsupported)
			}
			if backend.starts() != 0 {
				t.Fatalf("不支持时不应起进程, starts = %d", backend.starts())
			}
		})
	}
}

// TestOpenLoginTerminalHomeFallback 拿不到用户主目录时回退空串:
// 终端后端继承当前目录即可,登录流程不受影响,不得报错。
func TestOpenLoginTerminalHomeFallback(t *testing.T) {
	app, backend := newLoginApp(t, []providers.Provider{
		&fakeLoginProvider{id: "cursor", name: "Cursor", cmd: []string{"cursor-agent", "login"}},
	})
	orig := userHomeDir
	userHomeDir = func() (string, error) { return "", errors.New("boom") }
	t.Cleanup(func() { userHomeDir = orig })

	info, err := app.OpenLoginTerminal("cursor", 80, 24)
	if err != nil {
		t.Fatalf("home 失败不应报错: %v", err)
	}
	if dir := backend.lastSpec().Dir; dir != "" {
		t.Fatalf("Spec.Dir = %q, 期望回退空串", dir)
	}
	if info.ID == "" {
		t.Fatal("应返回终端 ID")
	}
}

// TestOpenLoginTerminalUsesDetectedBinPath 探测到 BinPath 时优先复用探测结果:
// 用探测到的可执行入口(含 node 入口形态的 BinArgs 前缀)拼 login 子命令,
// 与工作区会话同等的启动能力——官方安装目录往往不在 GUI 进程 PATH,
// 裸命令会在 terminal 后端 LookPath 处失败。
func TestOpenLoginTerminalUsesDetectedBinPath(t *testing.T) {
	app, backend := newLoginAppWithTools(t, []providers.Provider{
		&fakeLoginProvider{id: "cursor", name: "Cursor", cmd: []string{"cursor-agent", "login"}},
	}, []discovery.Tool{
		{ID: "cursor", Name: "Cursor", Installed: true,
			BinPath: `D:\tools\node.exe`, BinArgs: []string{`D:\tools\cursor-agent\index.js`}},
	})
	stubHomeDir(t, `D:\home-u`)

	if _, err := app.OpenLoginTerminal("cursor", 80, 24); err != nil {
		t.Fatalf("OpenLoginTerminal error: %v", err)
	}
	spec := backend.lastSpec()
	if spec.Path != `D:\tools\node.exe` {
		t.Fatalf("Spec.Path = %q, 期望探测到的 BinPath", spec.Path)
	}
	if len(spec.Args) != 2 || spec.Args[0] != `D:\tools\cursor-agent\index.js` || spec.Args[1] != "login" {
		t.Fatalf("Spec.Args = %v, 期望 [BinArgs..., login]", spec.Args)
	}
}

// TestOpenLoginTerminalFallsBackToLoginCmd 探测缺失(BinPath 为空或工具不在
// 探测表)时回退 LoginCmd 裸命令:裸名对 launcher 的 shim 解析是 no-op,
// 真正的 PATH 解析在 terminal 后端 commandLine,PATH 命中的安装仍可用。
func TestOpenLoginTerminalFallsBackToLoginCmd(t *testing.T) {
	cases := []struct {
		name  string
		tools []discovery.Tool
	}{
		{"BinPath 为空", []discovery.Tool{{ID: "cursor", Name: "Cursor", Installed: true}}},
		{"工具不在探测表", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			app, backend := newLoginAppWithTools(t, []providers.Provider{
				&fakeLoginProvider{id: "cursor", name: "Cursor", cmd: []string{"cursor-agent", "login"}},
			}, c.tools)

			if _, err := app.OpenLoginTerminal("cursor", 80, 24); err != nil {
				t.Fatalf("OpenLoginTerminal error: %v", err)
			}
			spec := backend.lastSpec()
			if spec.Path != "cursor-agent" || len(spec.Args) != 1 || spec.Args[0] != "login" {
				t.Fatalf("Spec 未按 LoginCmd 裸命令组装 = %+v", spec)
			}
		})
	}
}

// TestOpenLoginTerminalTitleFallsBackToToolID provider 无显示名时标题回退 toolID,
// 不产出残缺标题。
func TestOpenLoginTerminalTitleFallsBackToToolID(t *testing.T) {
	app, _ := newLoginApp(t, []providers.Provider{
		&fakeLoginProvider{id: "cursor", cmd: []string{"cursor-agent", "login"}},
	})
	// 标题直显文案依赖当前语言,钉住语言让断言稳定
	applang.Set("zh-CN")
	t.Cleanup(func() { applang.Set("en") })

	info, err := app.OpenLoginTerminal("cursor", 80, 24)
	if err != nil {
		t.Fatalf("OpenLoginTerminal error: %v", err)
	}
	if info.Title != "cursor 登录" {
		t.Fatalf("Title = %q, 期望「cursor 登录」", info.Title)
	}
}

// TestOpenLoginTerminalNotReady 终端管理器未装配(未走 Startup)时报 not ready。
func TestOpenLoginTerminalNotReady(t *testing.T) {
	app := newAuthApp(t, []providers.Provider{
		&fakeLoginProvider{id: "cursor", name: "Cursor", cmd: []string{"cursor-agent", "login"}},
	})
	if _, err := app.OpenLoginTerminal("cursor", 80, 24); !errors.Is(err, errNotReady) {
		t.Fatalf("OpenLoginTerminal err = %v, want %v", err, errNotReady)
	}
}
