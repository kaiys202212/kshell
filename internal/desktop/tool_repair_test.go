package desktop

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

func repairTestApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	return NewAppWith(Options{
		Layout: config.Layout{Root: root, State: filepath.Join(root, "state")},
	})
}

// 卸载成功写标记；安装成功清标记。
func TestUninstallMarkerLifecycle(t *testing.T) {
	a := repairTestApp(t)
	if a.uninstalledByUser("cursor") {
		t.Fatal("初始不应有卸载标记")
	}
	a.setUninstalledMarker("cursor")
	if !a.uninstalledByUser("cursor") {
		t.Fatal("写标记后应读到")
	}
	a.clearUninstalledMarker("cursor")
	if a.uninstalledByUser("cursor") {
		t.Fatal("清标记后不应读到")
	}
}

// 重试计数：递增、读取、清零。
func TestRepairAttemptsLifecycle(t *testing.T) {
	a := repairTestApp(t)
	if n := a.readRepairAttempts("cursor"); n != 0 {
		t.Fatalf("初始 attempts=%d", n)
	}
	a.bumpRepairAttempts("cursor")
	a.bumpRepairAttempts("cursor")
	if n := a.readRepairAttempts("cursor"); n != 2 {
		t.Fatalf("attempts=%d, want 2", n)
	}
	a.clearRepairState("cursor")
	if n := a.readRepairAttempts("cursor"); n != 0 {
		t.Fatalf("清零后 attempts=%d", n)
	}
}

// 损坏的状态文件视为 0，不阻塞修复流程。
func TestRepairAttemptsCorruptFile(t *testing.T) {
	a := repairTestApp(t)
	if err := os.MkdirAll(a.opts.Layout.State, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(a.opts.Layout.State, "cursor-repair.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n := a.readRepairAttempts("cursor"); n != 0 {
		t.Fatalf("损坏文件应视为 0，got %d", n)
	}
}

// Layout.State 未装配（部分测试的零值 Layout）：标记与计数全部静默 no-op。
func TestRepairStateWithoutLayout(t *testing.T) {
	a := NewAppWith(Options{})
	a.setUninstalledMarker("cursor")
	if a.uninstalledByUser("cursor") {
		t.Fatal("无 State 目录不应读到标记")
	}
	a.bumpRepairAttempts("cursor")
	if n := a.readRepairAttempts("cursor"); n != 0 {
		t.Fatalf("无 State 目录 attempts 应恒为 0，got %d", n)
	}
}

// repairFakeProvider：DetectSpec 落在「无入口、有配置+残留」的残缺形态。
type repairFakeProvider struct{}

func (repairFakeProvider) ID() string          { return "cursor" }
func (repairFakeProvider) DisplayName() string { return "Cursor" }
func (repairFakeProvider) DetectSpec(home string) providers.DetectSpec {
	return providers.DetectSpec{
		BinName:     "no-such-bin-xyz",
		InstallDirs: []string{filepath.Join(home, "nowhere")},
		ConfigDirs:  []string{"~/.cursor"},
		ResidueDirs: []string{"~/.cursor/projects"},
	}
}
func (repairFakeProvider) SessionRoots(string) []string { return nil }
func (repairFakeProvider) SessionFilePattern() string   { return "*.jsonl" }
func (repairFakeProvider) ParseSession(string, []byte) (*providers.Session, error) {
	return nil, nil
}
func (repairFakeProvider) NewSessionCmd(ws, bin string) providers.Launch {
	return providers.Launch{Path: bin, Dir: ws}
}
func (repairFakeProvider) ResumeCmd(s providers.Session, bin string) providers.Launch {
	return providers.Launch{Path: bin, Dir: s.Workspace}
}
func (repairFakeProvider) InstallRecipe() providers.InstallRecipe {
	return providers.InstallRecipe{InstallCmd: "fake-install", Shell: "cmd"}
}

// repairNoResidueProvider：模拟 gemini 类工具——有配置目录但未声明会话残留。
type repairNoResidueProvider struct{ repairFakeProvider }

func (repairNoResidueProvider) DetectSpec(home string) providers.DetectSpec {
	spec := repairFakeProvider{}.DetectSpec(home)
	spec.ResidueDirs = nil
	return spec
}

// brokenRepairApp 装配「残缺 + 有残留」场景的 App，并记录安装命令调用。
// 安装目录真实存在（对应 updater 只删入口、目录还在的残缺形态），进程占用检查才会走到。
func brokenRepairApp(t *testing.T, opts ...func(*Options)) (*App, *[]string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(filepath.Join(home, ".cursor", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "nowhere"), 0o755); err != nil {
		t.Fatal(err)
	}
	var calls []string
	o := Options{
		Home:      home,
		Layout:    config.Layout{Root: filepath.Join(root, ".kshell"), State: filepath.Join(root, ".kshell", "state")},
		Providers: []providers.Provider{repairFakeProvider{}},
		InstallRunner: func(ctx context.Context, shell, cmdline string, onLog func(string)) error {
			calls = append(calls, cmdline)
			return nil
		},
		Emit: func(string, ...any) {},
	}
	for _, fn := range opts {
		fn(&o)
	}
	return NewAppWith(o), &calls
}

func brokenTools() []discovery.Tool {
	return []discovery.Tool{{ID: "cursor", Name: "Cursor", Installed: true, Broken: true, Source: "config-dir"}}
}

// 全条件满足：启动修复任务，Trigger=auto，attempts 记为 1。
// runner 阻塞到断言完成：保证断言时 job 仍 Running、日志事件可观测。
func TestMaybeAutoRepairStartsJob(t *testing.T) {
	block := make(chan struct{})
	var calls *[]string
	a, calls := brokenRepairApp(t, func(o *Options) {
		o.ProcScan = func(string) bool { return false } // 无进程占用
		o.InstallRunner = func(ctx context.Context, shell, cmdline string, onLog func(string)) error {
			*calls = append(*calls, cmdline)
			<-block
			return nil
		}
	})
	a.maybeAutoRepair(brokenTools())
	if job := a.GetToolInstallJob(); job.Trigger != "auto" {
		t.Fatalf("Trigger=%q, want auto", job.Trigger)
	}
	if n := a.readRepairAttempts("cursor"); n != 1 {
		t.Fatalf("attempts=%d, want 1", n)
	}
	close(block)
	waitJobIdle(t, a)
	if len(*calls) != 1 || (*calls)[0] != "fake-install" {
		t.Fatalf("calls=%v, want [fake-install]", *calls)
	}
	// 安装退出 0 不清计数：假成功（装完仍探不到）时计数必须累计到上限才停，
	// 计数只由下轮扫描发现不再 Broken 时清零。
	if n := a.readRepairAttempts("cursor"); n != 1 {
		t.Fatalf("安装成功不应清计数, attempts=%d, want 1", n)
	}
}

// 扫描发现工具不再 Broken（或未安装）→ 计数清零，为下次故障恢复额度。
func TestMaybeAutoRepairClearsAttemptsWhenHealthy(t *testing.T) {
	a, _ := brokenRepairApp(t)
	a.bumpRepairAttempts("cursor")
	a.bumpRepairAttempts("cursor")
	a.maybeAutoRepair([]discovery.Tool{{ID: "cursor", Installed: true, Broken: false}})
	if n := a.readRepairAttempts("cursor"); n != 0 {
		t.Fatalf("恢复健康后 attempts=%d, want 0", n)
	}
}

// opt-in：未声明 ResidueDirs 的工具（其 fallback 残留“配置非空”几乎恒真）
// 不允许自动修复——防止对 npx 一次性使用的工具后台静默全局安装。
func TestMaybeAutoRepairRequiresResidueDirsOptIn(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls []string
	a := NewAppWith(Options{
		Home:     home,
		Layout:   config.Layout{Root: filepath.Join(root, ".kshell"), State: filepath.Join(root, ".kshell", "state")},
		Providers: []providers.Provider{repairNoResidueProvider{}},
		InstallRunner: func(ctx context.Context, shell, cmdline string, onLog func(string)) error {
			calls = append(calls, cmdline)
			return nil
		},
		ProcScan: func(string) bool { return false },
		Emit:     func(string, ...any) {},
	})
	a.maybeAutoRepair([]discovery.Tool{{ID: "cursor", Installed: true, Broken: true}})
	if len(calls) != 0 {
		t.Fatalf("未声明 ResidueDirs 不应自动修复, calls=%v", calls)
	}
}

// 有卸载标记 → 不修。
func TestMaybeAutoRepairSkipsUninstalledMarker(t *testing.T) {
	a, calls := brokenRepairApp(t, func(o *Options) { o.ProcScan = func(string) bool { return false } })
	a.setUninstalledMarker("cursor")
	a.maybeAutoRepair(brokenTools())
	if len(*calls) != 0 {
		t.Fatalf("有卸载标记不应启动，calls=%v", *calls)
	}
}

// 无残留 → 不修。
func TestMaybeAutoRepairSkipsNoResidue(t *testing.T) {
	a, calls := brokenRepairApp(t, func(o *Options) { o.ProcScan = func(string) bool { return false } })
	if err := os.RemoveAll(filepath.Join(a.opts.Home, ".cursor", "projects")); err != nil {
		t.Fatal(err)
	}
	a.maybeAutoRepair(brokenTools())
	if len(*calls) != 0 {
		t.Fatalf("无残留不应启动，calls=%v", *calls)
	}
}

// 进程占用 → 跳过本轮且不计重试。
func TestMaybeAutoRepairSkipsWhenProcessRunning(t *testing.T) {
	a, calls := brokenRepairApp(t, func(o *Options) { o.ProcScan = func(string) bool { return true } })
	a.maybeAutoRepair(brokenTools())
	if len(*calls) != 0 {
		t.Fatalf("进程占用不应启动，calls=%v", *calls)
	}
	if n := a.readRepairAttempts("cursor"); n != 0 {
		t.Fatalf("跳过不应计数，attempts=%d", n)
	}
}

// 重试到上限 → 不再启动。
func TestMaybeAutoRepairStopsAtMaxAttempts(t *testing.T) {
	a, calls := brokenRepairApp(t, func(o *Options) { o.ProcScan = func(string) bool { return false } })
	for i := 0; i < maxAutoRepairAttempts; i++ {
		a.bumpRepairAttempts("cursor")
	}
	a.maybeAutoRepair(brokenTools())
	if len(*calls) != 0 {
		t.Fatalf("达上限不应启动，calls=%v", *calls)
	}
}

// 已有安装任务在跑 → 不启动（防并发）。
func TestMaybeAutoRepairSkipsWhenJobBusy(t *testing.T) {
	a, calls := brokenRepairApp(t, func(o *Options) { o.ProcScan = func(string) bool { return false } })
	a.mu.Lock()
	a.installJob = installJob{toolID: "other", action: "install", running: true}
	a.mu.Unlock()
	a.maybeAutoRepair(brokenTools())
	if len(*calls) != 0 {
		t.Fatalf("任务忙不应启动，calls=%v", *calls)
	}
}

// 非残缺工具（健康）→ 不修。
func TestMaybeAutoRepairIgnoresHealthyTool(t *testing.T) {
	a, calls := brokenRepairApp(t, func(o *Options) { o.ProcScan = func(string) bool { return false } })
	a.maybeAutoRepair([]discovery.Tool{{ID: "cursor", Installed: true, BinPath: "C:\\x.exe"}})
	if len(*calls) != 0 {
		t.Fatalf("健康工具不应启动，calls=%v", *calls)
	}
}

// 扫描链路挂载：publishDetectedTools 发布残缺工具表后自动触发修复。
func TestPublishDetectedToolsTriggersAutoRepair(t *testing.T) {
	block := make(chan struct{})
	var calls *[]string
	a, calls := brokenRepairApp(t, func(o *Options) {
		o.ProcScan = func(string) bool { return false }
		o.InstallRunner = func(ctx context.Context, shell, cmdline string, onLog func(string)) error {
			*calls = append(*calls, cmdline)
			<-block
			return nil
		}
	})
	a.publishDetectedTools(brokenTools())
	if job := a.GetToolInstallJob(); job.Trigger != "auto" || !job.Running {
		t.Fatalf("job=%+v, want running auto repair", job)
	}
	close(block)
	waitJobIdle(t, a)
	if len(*calls) != 1 {
		t.Fatalf("calls=%v, want 1 auto repair", calls)
	}
}

// %VAR% 展开：Windows 的 InstallDirs 写作 %LOCALAPPDATA%\cursor-agent，
// 展不开会让进程检查永远落空（os.ExpandEnv 只认 $VAR）。
func TestExpandHomeForRepairPercentEnv(t *testing.T) {
	t.Setenv("KSHELL_TEST_LOCAL", `C:\Users\x\AppData\Local`)
	got := expandHomeForRepair(`%KSHELL_TEST_LOCAL%\cursor-agent`, `C:\h`)
	want := `C:\Users\x\AppData\Local\cursor-agent`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	// 未知变量保持字面量，不吞
	if got := expandHomeForRepair(`%NO_SUCH_VAR_XYZ%`, `C:\h`); got != `%NO_SUCH_VAR_XYZ%` {
		t.Fatalf("未知变量被改写: %q", got)
	}
	// ~ 前缀仍优先
	if got := expandHomeForRepair(`~/.cursor`, `C:\h`); got != filepath.Join(`C:\h`, ".cursor") {
		t.Fatalf("~ 展开错误: %q", got)
	}
}

// 多安装目录：逐个 OR，不取首个存在的目录短路（首目录不忙、次目录忙也算忙）。
func TestInstallDirBusyChecksAllDirs(t *testing.T) {
	root := t.TempDir()
	dirA := filepath.Join(root, "a")
	dirB := filepath.Join(root, "b")
	for _, d := range []string{dirA, dirB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	a := NewAppWith(Options{
		ProcScan: func(dir string) bool { return filepath.Base(dir) == "b" },
	})
	spec := providers.DetectSpec{InstallDirs: []string{dirA, dirB}}
	if !a.installDirBusy(spec, root) {
		t.Fatal("次目录有进程也应视为忙（不短路）")
	}
}
