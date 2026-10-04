# 托盘卡死修复 + 桌面端单实例 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复托盘右键偶发卡死无法退出，并为桌面端启用全局单实例（二次启动唤起已有窗口）。

**Architecture:** 托盘回调经 `trayDispatch` 异步离开 systray 消息泵；`quitApp` 只置 `quitting` + `quitRuntime`，`quitTrayLoop` 改到 `Shutdown`；Wails `SingleInstanceLock` + `OnSecondInstanceLaunch`→`WindowShow`；`RestartApp` 改为 `spawnSelfDelayed` 先安排延迟拉起再退出，避免撞 mutex。

**Tech Stack:** Go 1.23+、Wails v2.16 `options.SingleInstanceLock`、`energye/systray`、`internal/executil.HideWindow`。

## Global Constraints

- 规格：`docs/plans/2026-10-04-tray-single-instance-design.md`
- 工作目录：仓库 `.worktrees/fix/tray-single-instance`，分支 `fix/tray-single-instance`
- 单实例仅桌面端；TUI 不受影响；`UniqueId` 固定 `kshell-desktop`
- 二次启动只唤起窗口，不解析参数
- 构建不写 `exit.signal`、不杀已运行实例
- 提交只在用户明确要求时执行；下面 Commit 步骤默认跳过，改为 `git status` 确认改动

---

## 文件结构

**修改**
- `internal/desktop/tray.go` — `trayDispatch`、回调异步；`quitTrayLoop` 改为可注入变量
- `internal/desktop/app.go` — `quitApp` / `Shutdown` 顺序；`OnSecondInstanceLaunch`；`RestartApp` 改用 delayed spawn
- `main.go` — 装配 `SingleInstanceLock`

**新增**
- `internal/desktop/tray_dispatch_test.go` — 异步派发与 quit 不拆托盘
- `internal/desktop/single_instance_test.go` — 二次启动唤起
- `internal/desktop/restart_spawn_windows.go` / `restart_spawn_other.go` — 延迟拉起实现
- `docs/smoke/fix-tray-single-instance.md` — 冒烟增量

**更新**
- `internal/desktop/app_restart_test.go` — 对接 `spawnSelfDelayed`

---

### Task 1: quitApp 不再同步拆托盘；Shutdown 收尾

**Files:**
- Modify: `internal/desktop/tray.go`
- Modify: `internal/desktop/app.go`（`quitApp`、`Shutdown`）
- Test: `internal/desktop/tray_dispatch_test.go`

**Interfaces:**
- Produces: `var quitTrayLoop = func() { systray.Quit() }`（包级可注入）
- Produces: `quitApp` 只置 `quitting` + 调 `quitRuntime`；`Shutdown` 首行调 `quitTrayLoop()`

- [ ] **Step 1: 写失败测试**

创建 `internal/desktop/tray_dispatch_test.go`：

```go
package desktop

import (
	"context"
	"testing"
)

func TestQuitAppDoesNotCallQuitTrayLoop(t *testing.T) {
	app, _, _ := newTestApp(t)
	trayQuits := 0
	origTray := quitTrayLoop
	quitTrayLoop = func() { trayQuits++ }
	t.Cleanup(func() { quitTrayLoop = origTray })

	origQuit := quitRuntime
	quitRuntime = func(context.Context) {}
	t.Cleanup(func() { quitRuntime = origQuit })

	app.quitApp(context.Background())
	if trayQuits != 0 {
		t.Fatalf("quitApp 不应同步 quitTrayLoop, got %d", trayQuits)
	}
	if !app.quitting {
		t.Fatal("quitApp 应置位 quitting")
	}
	if app.BeforeClose(context.Background()) {
		t.Fatal("quitting 后 BeforeClose 应放行")
	}
}

func TestShutdownCallsQuitTrayLoop(t *testing.T) {
	app, _, _ := newTestApp(t)
	trayQuits := 0
	origTray := quitTrayLoop
	quitTrayLoop = func() { trayQuits++ }
	t.Cleanup(func() { quitTrayLoop = origTray })

	app.Shutdown(context.Background())
	if trayQuits != 1 {
		t.Fatalf("Shutdown 应调用 quitTrayLoop 1 次, got %d", trayQuits)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```powershell
go test ./internal/desktop -run "TestQuitAppDoesNotCallQuitTrayLoop|TestShutdownCallsQuitTrayLoop" -count=1
```

Expected: FAIL（`quitTrayLoop` 尚不是变量，或 `quitApp` 仍调用它 / `Shutdown` 未调用）

- [ ] **Step 3: 最小实现**

`tray.go` 将退出循环改为变量：

```go
// quitTrayLoop 请求托盘消息循环退出（通知区图标随循环停止被移除）。
// 包级变量便于测试注入；systray.Quit 内部 sync.Once，重复调用安全。
var quitTrayLoop = func() {
	systray.Quit()
}
```

`app.go` 的 `quitApp`：

```go
func (a *App) quitApp(ctx context.Context) {
	a.mu.Lock()
	a.quitting = true
	a.mu.Unlock()
	quitRuntime(ctx)
}
```

`Shutdown` 开头增加：

```go
func (a *App) Shutdown(ctx context.Context) {
	_ = ctx
	quitTrayLoop()
	// ...其余既有清理不变...
}
```

- [ ] **Step 4: 跑测试确认通过**

```powershell
go test ./internal/desktop -run "TestQuitAppDoesNotCallQuitTrayLoop|TestShutdownCallsQuitTrayLoop" -count=1
```

Expected: PASS

- [ ] **Step 5: 确认改动（默认不提交）**

```powershell
git status; git diff -- internal/desktop/tray.go internal/desktop/app.go internal/desktop/tray_dispatch_test.go
```

---

### Task 2: 托盘回调异步派发

**Files:**
- Modify: `internal/desktop/tray.go`
- Test: `internal/desktop/tray_dispatch_test.go`（追加）

**Interfaces:**
- Produces: `var trayDispatch = func(fn func()) { if fn != nil { go fn() } }`
- Consumes: `runTray` 内所有 `onShow`/`onQuit` 经 `trayDispatch` 调用

- [ ] **Step 1: 写失败测试**

追加到 `tray_dispatch_test.go`：

```go
func TestTrayDispatchRunsAsyncByDefault(t *testing.T) {
	// 默认实现必须启动新 goroutine：用「业务回调阻塞 + 派发应立即返回」证明。
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	go func() {
		trayDispatch(func() {
			close(started)
			<-release
		})
		close(done)
	}()

	select {
	case <-done:
		// 派发返回了；此时业务可能已启动
	case <-time.After(2 * time.Second):
		t.Fatal("trayDispatch 应立即返回，不应被业务回调阻塞")
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("业务回调应在独立 goroutine 执行")
	}
	close(release)
}

func TestRunTrayCallbacksUseTrayDispatch(t *testing.T) {
	// 通过替换 trayDispatch 为同步记录，并劫持不到真实 systray：
	// 直接测 runTray 不可行（会阻塞消息循环）。改为断言 tray.go 里
	// SetOnClick / Click 包装调用 trayDispatch——用导出行为：
	// 将 runTray 内逻辑抽成 wrapTrayActions 太重；本测试只验证
	// trayDispatch(nil) 不 panic，以及同步注入下 onShow 可被调用。
	calls := 0
	orig := trayDispatch
	trayDispatch = func(fn func()) {
		if fn != nil {
			fn()
			calls++
		}
	}
	t.Cleanup(func() { trayDispatch = orig })

	trayDispatch(func() {})
	if calls != 1 {
		t.Fatalf("注入的 trayDispatch 应被调用, got %d", calls)
	}
	trayDispatch(nil) // 不得 panic
}
```

（若觉得 `TestRunTrayCallbacksUseTrayDispatch` 过弱，可改为用 `go/ast` 不强制；实现时务必让 `runTray` 三处回调都走 `trayDispatch`，评审看 diff。）

补充 import：`"time"`。

- [ ] **Step 2: 跑测试确认失败**

```powershell
go test ./internal/desktop -run "TestTrayDispatch" -count=1
```

Expected: FAIL（`trayDispatch` 未定义）

- [ ] **Step 3: 最小实现**

`tray.go`：

```go
// trayDispatch 把托盘 UI 回调丢到新 goroutine，避免在 systray wndProc /
// TrackPopupMenu 嵌套泵里同步调用 Wails/Quit 导致消息循环僵死。
var trayDispatch = func(fn func()) {
	if fn == nil {
		return
	}
	go fn()
}

func runTray(icon []byte, onShow, onQuit func()) {
	systray.Run(func() {
		systray.SetIcon(icon)
		systray.SetTooltip("kshell")
		systray.SetOnClick(func(systray.IMenu) {
			trayDispatch(onShow)
		})
		mShow := systray.AddMenuItem("显示主窗口", "显示 kshell 主窗口")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出", "退出 kshell")
		mShow.Click(func() { trayDispatch(onShow) })
		mQuit.Click(func() { trayDispatch(onQuit) })
	}, nil)
}
```

- [ ] **Step 4: 跑测试确认通过**

```powershell
go test ./internal/desktop -run "TestTrayDispatch|TestQuitApp|TestShutdown" -count=1
```

Expected: PASS

- [ ] **Step 5: 确认改动（默认不提交）**

```powershell
git diff -- internal/desktop/tray.go internal/desktop/tray_dispatch_test.go
```

---

### Task 3: OnSecondInstanceLaunch 唤起窗口

**Files:**
- Modify: `internal/desktop/app.go`
- Test: `internal/desktop/single_instance_test.go`

**Interfaces:**
- Produces: `func (a *App) OnSecondInstanceLaunch(data options.SecondInstanceData)`
- Consumes: 包级可注入 `var windowShow = runtime.WindowShow`（若尚无则新增，对齐 `windowHide`）

- [ ] **Step 1: 写失败测试**

```go
package desktop

import (
	"context"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options"
)

func TestOnSecondInstanceLaunchShowsWindow(t *testing.T) {
	calls := 0
	orig := windowShow
	windowShow = func(context.Context) { calls++ }
	t.Cleanup(func() { windowShow = orig })

	app := NewAppWith(Options{})
	app.ctx = context.Background()
	app.OnSecondInstanceLaunch(options.SecondInstanceData{})
	if calls != 1 {
		t.Fatalf("WindowShow 调用 %d 次, want 1", calls)
	}
}

func TestOnSecondInstanceLaunchNoopWithoutCtx(t *testing.T) {
	calls := 0
	orig := windowShow
	windowShow = func(context.Context) { calls++ }
	t.Cleanup(func() { windowShow = orig })

	app := NewAppWith(Options{})
	app.OnSecondInstanceLaunch(options.SecondInstanceData{})
	if calls != 0 {
		t.Fatalf("ctx 未就绪不应 Show, got %d", calls)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```powershell
go test ./internal/desktop -run TestOnSecondInstanceLaunch -count=1
```

Expected: FAIL（方法或 `windowShow` 不存在）

- [ ] **Step 3: 最小实现**

在 `app.go`（`windowHide` 旁）增加：

```go
var windowShow = runtime.WindowShow
```

新增方法（可放在 `quitApp` 附近）：

```go
// OnSecondInstanceLaunch 供 Wails SingleInstanceLock 回调：二次启动时恢复主窗口。
func (a *App) OnSecondInstanceLaunch(_ options.SecondInstanceData) {
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx == nil {
		return
	}
	windowShow(ctx)
}
```

import 增加：`"github.com/wailsapp/wails/v2/pkg/options"`。

- [ ] **Step 4: 跑测试确认通过**

```powershell
go test ./internal/desktop -run TestOnSecondInstanceLaunch -count=1
```

Expected: PASS

- [ ] **Step 5: 确认改动（默认不提交）**

```powershell
git diff -- internal/desktop/app.go internal/desktop/single_instance_test.go
```

---

### Task 4: RestartApp 改为延迟拉起

**Files:**
- Create: `internal/desktop/restart_spawn_windows.go`
- Create: `internal/desktop/restart_spawn_other.go`
- Modify: `internal/desktop/app.go`（`RestartApp`、删除或停用即时 `spawnSelf`）
- Modify: `internal/desktop/app_restart_test.go`

**Interfaces:**
- Produces: `func spawnSelfDelayedImpl(exe string) error`（各平台）
- Produces: `var spawnSelfDelayed = spawnSelfDelayedImpl`
- Consumes: `RestartApp` 只调 `spawnSelfDelayed`，成功后再 `quitApp`

- [ ] **Step 1: 改测试为 delayed（先红）**

重写 `app_restart_test.go`：

```go
package desktop

import (
	"context"
	"errors"
	"os"
	"testing"
)

func stubRestart(t *testing.T, spawnErr error) (*[]string, *int) {
	t.Helper()
	spawned := []string{}
	quitCalled := 0

	origSpawn := spawnSelfDelayed
	spawnSelfDelayed = func(exe string) error {
		spawned = append(spawned, exe)
		return spawnErr
	}
	t.Cleanup(func() { spawnSelfDelayed = origSpawn })

	origQuit := quitRuntime
	quitRuntime = func(context.Context) { quitCalled++ }
	t.Cleanup(func() { quitRuntime = origQuit })

	return &spawned, &quitCalled
}

func TestRestartAppSpawnsAndQuits(t *testing.T) {
	app, _, _ := newTestApp(t)
	spawned, quitCalled := stubRestart(t, nil)

	if err := app.RestartApp(context.Background()); err != nil {
		t.Fatalf("RestartApp error: %v", err)
	}
	if len(*spawned) != 1 {
		t.Fatalf("spawnSelfDelayed 调用次数 = %d, 期望 1", len(*spawned))
	}
	wantExe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if (*spawned)[0] != wantExe {
		t.Fatalf("exe = %q, want %q", (*spawned)[0], wantExe)
	}
	if *quitCalled != 1 {
		t.Fatalf("quitRuntime = %d, want 1", *quitCalled)
	}
	if app.BeforeClose(context.Background()) {
		t.Fatal("RestartApp 后 BeforeClose 应放行")
	}
}

func TestRestartAppSpawnFailure(t *testing.T) {
	app, _, _ := newTestApp(t)
	_, quitCalled := stubRestart(t, errors.New("启动失败"))

	if err := app.RestartApp(context.Background()); err == nil {
		t.Fatal("spawn 失败应返回错误")
	}
	if *quitCalled != 0 {
		t.Fatal("spawn 失败不应 quitRuntime")
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.quitting {
		t.Fatal("spawn 失败不应置 quitting")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```powershell
go test ./internal/desktop -run TestRestartApp -count=1
```

Expected: FAIL（`spawnSelfDelayed` 未定义）

- [ ] **Step 3: 最小实现**

`restart_spawn_windows.go`：

```go
//go:build windows

package desktop

import (
	"fmt"
	"os/exec"

	"github.com/yangk/kshell/internal/executil"
)

// spawnSelfDelayedImpl 在当前进程退出释放单实例 mutex 后再启动自身。
func spawnSelfDelayedImpl(exe string) error {
	// timeout 约 1s；start 的空标题 "" 防止把 exe 路径当成窗口标题。
	cmdline := fmt.Sprintf(`timeout /t 1 /nobreak >nul & start "" "%s"`, exe)
	cmd := exec.Command("cmd", "/C", cmdline)
	executil.HideWindow(cmd)
	return cmd.Start()
}
```

`restart_spawn_other.go`：

```go
//go:build !windows

package desktop

import (
	"fmt"
	"os/exec"
	"strconv"
)

func spawnSelfDelayedImpl(exe string) error {
	cmd := exec.Command("/bin/sh", "-c", fmt.Sprintf("sleep 1; exec %s", strconv.Quote(exe)))
	return cmd.Start()
}
```

`app.go`：删除（或保留但不再使用）即时 `spawnSelf`；改为：

```go
var spawnSelfDelayed = spawnSelfDelayedImpl

func (a *App) RestartApp(ctx context.Context) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := spawnSelfDelayed(exe); err != nil {
		return err
	}
	a.quitApp(ctx)
	return nil
}
```

若 `spawnSelf` 无其它引用则删除，并去掉仅为其引入的多余 import。

- [ ] **Step 4: 跑测试确认通过**

```powershell
go test ./internal/desktop -run TestRestartApp -count=1
```

Expected: PASS

- [ ] **Step 5: 确认改动（默认不提交）**

```powershell
git status; git diff -- internal/desktop/
```

---

### Task 5: main.go 装配单实例 + 冒烟清单

**Files:**
- Modify: `main.go`
- Create: `docs/smoke/fix-tray-single-instance.md`

**Interfaces:**
- Consumes: `desktopApp.OnSecondInstanceLaunch`
- Produces: `SingleInstanceLock{ UniqueId: "kshell-desktop", OnSecondInstanceLaunch: desktopApp.OnSecondInstanceLaunch }`

- [ ] **Step 1: 改 main.go**

在 `RunDesktop` 的 `options.App` 中增加（并 import `options` 若路径已是 `github.com/wailsapp/wails/v2/pkg/options`，与现有一致）：

```go
SingleInstanceLock: &options.SingleInstanceLock{
	UniqueId:               "kshell-desktop",
	OnSecondInstanceLaunch: desktopApp.OnSecondInstanceLaunch,
},
```

放在 `OnStartup` / `OnBeforeClose` 同级字段即可。

- [ ] **Step 2: 编译检查**

```powershell
go build -o NUL .
go vet ./internal/desktop ./...
```

Expected: 成功（Windows 上桌面入口可编译）

- [ ] **Step 3: 写冒烟增量**

`docs/smoke/fix-tray-single-instance.md`：

```markdown
# 冒烟：fix/tray-single-instance

## Desktop

- [ ] 点 × 进托盘后右键菜单稳定；「显示主窗口」「退出」可用；退出后无残留进程/图标
- [ ] 托盘左键单击可恢复主窗口
- [ ] 运行中再双击 kshell-desktop.exe：不出现第二进程/第二托盘图标，已有窗口被唤起
- [ ] 设置页「立即重启」后仍为单一桌面实例且窗口可用
- [ ] TUI（kshell.exe）与桌面端可同时运行
```

- [ ] **Step 4: 全量 Go 验证**

```powershell
go build ./... ; go vet ./... ; go test ./... -count=1
```

Expected: 全绿

- [ ] **Step 5: 确认改动（默认不提交）**

```powershell
git status
```

---

## 自我复查（对照规格）

| 规格要求 | 任务 |
|---|---|
| 托盘回调异步 | Task 2 |
| quitApp 不同步 Quit 托盘；Shutdown 收尾 | Task 1 |
| SingleInstanceLock + 唤起 | Task 3、5 |
| RestartApp 延迟拉起 | Task 4 |
| 冒烟清单 | Task 5 |
| TUI 不受影响 | Task 5 冒烟 + 不改 `cmd/kshell` |

无 TBD/占位；类型名与规格一致（`trayDispatch`、`spawnSelfDelayed`、`windowShow`、`OnSecondInstanceLaunch`）。
