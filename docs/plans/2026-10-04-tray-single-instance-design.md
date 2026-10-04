# 设计：托盘右键卡死修复 + 桌面端单实例

日期：2026-10-04  
状态：已确认（方案 1）  
分支：`fix/tray-single-instance`

## 1. 背景与目标

两项诉求合并为一轮改动：

1. **修复**：部分情况下主窗口关闭到托盘后，托盘右键无反应，只能任务管理器杀进程；卡死发生在**非多开**状态。
2. **新增**：桌面端全局单实例；再双击 exe 时唤起已有窗口（若在托盘则恢复），新进程退出。TUI 不受约束。

已确认决策：

| 决策点 | 结论 |
|---|---|
| 方案 | 修托盘回调时序 + Wails `SingleInstanceLock` |
| 二次启动 | 激活已有窗口 / 从托盘恢复（选项 A） |
| 单实例范围 | 仅桌面端（选项 A）；TUI 可并行 |
| 二次启动参数 | 暂不解析 `SecondInstanceData`，只做唤起 |
| RestartApp | 改为延迟拉起，避免撞单实例锁 |

## 2. 根因与现状

### 2.1 托盘卡死

- 托盘库：`github.com/energye/systray@v1.0.3`，Windows 在托盘 `wndProc` 内同步处理 `WM_LBUTTONUP` / `WM_COMMAND`。
- 当前装配（`StartTray`）：
  - 左键 /「显示主窗口」→ 同步 `runtime.WindowShow(ctx)`
  - 「退出」→ 同步 `quitApp`：先 `quitTrayLoop()`（`systray.Quit` → 向托盘窗 `WM_CLOSE`），再 `quitRuntime`
- 「退出」常发生在 `TrackPopupMenu` 的嵌套消息泵内；此时同步拆托盘窗口 / 请求 Wails 退出，易使托盘消息循环僵死，而主进程仍存活 → 右键无菜单、无法退出。
- 左键路径虽经 Wails `Invoke` 异步到主窗线程，但仍在 `wndProc` 内同步进入回调；统一异步派发更稳妥。
- 多开会叠加幽灵图标，但用户确认卡死可在单实例复现；单实例仍是本轮必须项。

### 2.2 单实例

- 现状：无任何互斥；双击 exe 可再开，托盘多一个图标。
- Wails v2.16 已提供 `options.SingleInstanceLock`（Windows mutex + `WM_COPYDATA`；其它平台有对应实现）。
- 现有 `RestartApp` 为「先 `spawnSelf` 再 `quitApp`」：启用单实例后，新进程会被当成二次启动并退出，旧进程随后也退出 → 无人存活。必须改重启顺序。

## 3. 方案选型（已拍板：方案 1）

| 方案 | 要点 | 取舍 |
|---|---|---|
| **1（采用）** | 托盘回调异步 + 调整 `quitApp` 收尾；Wails `SingleInstanceLock`；RestartApp 延迟拉起 | 改动小，复用框架 |
| 2 | 换 Wails 原生托盘 | v2 能力弱，迁移成本高 |
| 3 | 自研 mutex/管道 | 重复造轮子，跨平台负担大 |

## 4. 设计

### 4.1 托盘回调异步（`internal/desktop/tray.go`）

```go
func runTray(icon []byte, onShow, onQuit func()) {
	systray.Run(func() {
		systray.SetIcon(icon)
		systray.SetTooltip("kshell")
		systray.SetOnClick(func(systray.IMenu) {
			go safeCall(onShow)
		})
		mShow := systray.AddMenuItem("显示主窗口", "显示 kshell 主窗口")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出", "退出 kshell")
		mShow.Click(func() { go safeCall(onShow) })
		mQuit.Click(func() { go safeCall(onQuit) })
	}, nil)
}

func safeCall(fn func()) {
	if fn != nil {
		fn()
	}
}
```

- 回调必须立刻返回托盘消息泵；业务在新 goroutine 执行。
- 不在本轮 fork `energye/systray` 补 `WM_NULL`；若异步后仍偶发右键失灵，再开跟进项。

### 4.2 退出路径（`internal/desktop/app.go`）

```go
func (a *App) quitApp(ctx context.Context) {
	a.mu.Lock()
	a.quitting = true
	a.mu.Unlock()
	quitRuntime(ctx) // 不再在此处同步 quitTrayLoop
}

func (a *App) Shutdown(ctx context.Context) {
	quitTrayLoop() // 进程退出收尾：移除托盘图标并停消息循环
	// ...既有终端/聊天/外观清理...
}
```

- `quitting` 仍保证 `BeforeClose` 放行。
- `Shutdown` 幂等：`systray.Quit` 本身 `sync.Once`，重复调用安全。
- 信号文件退出、托盘退出、RestartApp 均走 `quitApp`，收尾统一进 `Shutdown`。

### 4.3 单实例（`main.go` + `App`）

```go
SingleInstanceLock: &options.SingleInstanceLock{
	UniqueId:               "kshell-desktop",
	OnSecondInstanceLaunch: desktopApp.OnSecondInstanceLaunch,
}
```

```go
func (a *App) OnSecondInstanceLaunch(_ options.SecondInstanceData) {
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx == nil {
		return
	}
	runtime.WindowShow(ctx)
}
```

- `UniqueId` 固定为 `kshell-desktop`（本机全局一把锁，不跟安装路径走）。
- 仅桌面入口生效；`cmd/kshell` 不装配。
- 二次启动参数本轮忽略。

### 4.4 RestartApp 延迟拉起

```go
// spawnSelfDelayed：先退出当前进程释放 mutex，再启动新实例。
// Windows 示例语义：timeout 后 start 自身；抽象为包级变量便于测试注入。
var spawnSelfDelayed = func(exe string) error { /* 平台实现 */ }

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

- 测试：注入 `spawnSelfDelayed`，断言收到 exe 且进入 `quitApp`；禁止真起进程。
- 旧 `spawnSelf` 若无其它调用方可删除或改为 delayed 的内部实现细节。

### 4.5 可测性

| 场景 | 断言 |
|---|---|
| 托盘回调 | 注册的 click/`SetOnClick` 包装在返回前不调用业务（或经 channel 证明异步） |
| quitApp | 置 `quitting`；`BeforeClose` 返回 false；**不**在 quitApp 内调用 `quitTrayLoop`（可替换 `quitTrayLoop` 桩计数） |
| Shutdown | 调用托盘退出一次 |
| OnSecondInstanceLaunch | 调用 `WindowShow` 桩；ctx 未就绪时 no-op |
| RestartApp | `spawnSelfDelayed` 收到 exe；随后 quitting；spawn 失败不 quitting |

## 5. 非目标

- 不解析二次启动 CLI 参数打开指定工作区。
- 不限制 TUI 与桌面并存。
- 不迁移到 Wails 原生托盘 / 不升级 Wails v3。
- 不修改 `energye/systray` 上游（本轮）。

## 6. 验收（手工冒烟）

增量清单：`docs/smoke/fix-tray-single-instance.md`

1. 点 × 进托盘 → 右键菜单稳定出现，可选「显示 / 退出」；退出后进程与图标均消失。
2. 托盘左键可恢复主窗口。
3. 应用运行中再双击桌面端 exe → 不出现第二进程/第二托盘图标，已有窗口被唤起。
4. 设置页「立即重启」后仍回到单一桌面实例。
5. TUI 与桌面端可同时运行。

## 7. 实现顺序（概要）

1. worktree `fix/tray-single-instance`（已建）
2. 写实现计划 `docs/plans/2026-10-04-tray-single-instance-plan.md`
3. TDD：托盘异步与 quit 顺序 → 单实例回调 → RestartApp 延迟拉起 → 装配 `main.go`
4. `go test` / `go vet` 全绿；冒烟条目落盘
5. 合并回 master 后 `.\build.ps1 -Desktop` 产出可执行文件（构建不杀已运行实例）
