package desktop

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

// newShellThemeTestApp 构造带指定颜色模式的终端测试 App：
// 复用终端桩件（stubTermBackend / eventLog），只把 Config 与 Layout 换成
// 可指定 Appearance.Mode 的真实装配，供 shell 终端主题 env 断言使用。
func newShellThemeTestApp(t *testing.T, mode string) *termTestEnv {
	t.Helper()

	sessions := []providers.Session{
		{ID: "s1", ToolID: "claude", Workspace: `D:\ws-a`, Title: "占位", UpdatedAt: time.Now()},
	}
	res := &discovery.Result{Sessions: sessions, Workspaces: discovery.GroupSessions(sessions)}

	dir := t.TempDir()
	cfg := config.Default()
	cfg.Appearance.Mode = mode

	backend := &stubTermBackend{}
	events := newEventLog()

	app := NewAppWith(Options{
		Home:      "D:/home",
		CachePath: filepath.Join(dir, "cache/index.json"),
		Config:    cfg,
		Layout: config.Layout{
			Root:   dir,
			Config: filepath.Join(dir, "config.yaml"),
			Cache:  filepath.Join(dir, "cache"),
		},
		Providers: []providers.Provider{},
		Scan: func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
			return res, nil
		},
		Windows:   NewWindowManager(&stubLauncher{}, nil),
		Terminals: newTerminalManagerWith(events.emit, backend),
		Emit:      events.emit,
	})

	app.runScan()
	return &termTestEnv{app: app, backend: backend, events: events}
}

// TestOpenShellTerminalInjectsThemeEnv 纯 shell 终端也要带主题变量：
// 在 shell tab 里手动敲 cursor-agent 等 CLI 时，靠 COLORFGBG 判定明暗
//（ConPTY 不应答 OSC 11，env 是唯一通道）。
func TestOpenShellTerminalInjectsThemeEnv(t *testing.T) {
	cases := []struct{ mode, want string }{
		{"light", "COLORFGBG=0;15"},
		{"dark", "COLORFGBG=15;0"},
	}
	for _, tc := range cases {
		env := newShellThemeTestApp(t, tc.mode)
		info, err := env.app.OpenShellTerminal(`D:\ws-a`, 80, 24)
		if err != nil {
			t.Fatalf("mode=%s OpenShellTerminal error: %v", tc.mode, err)
		}
		if info.ID == "" {
			t.Fatalf("mode=%s 应返回终端 ID", tc.mode)
		}
		joined := strings.Join(env.backend.lastSpec().Env, "\n")
		if !strings.Contains(joined, tc.want) {
			t.Fatalf("mode=%s Spec.Env 缺 %s: %v", tc.mode, tc.want, env.backend.lastSpec().Env)
		}
		if !strings.Contains(joined, "COLORTERM=truecolor") {
			t.Fatalf("mode=%s Spec.Env 缺 COLORTERM=truecolor: %v", tc.mode, env.backend.lastSpec().Env)
		}
	}
}
