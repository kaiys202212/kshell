# kshell 关闭行为（收进托盘 / 直接退出）与托盘左键恢复 设计

日期：2026-10-03
状态：已确认（用户拍板：点击修复 + 可配置关闭行为）

## 1. 背景与目标

两项诉求合并为一轮改动：

1. **修复**：隐藏到托盘后，左键单击托盘图标无法恢复主窗口；只有右键菜单「显示主窗口」可用。
2. **新增**：关闭窗口的行为可配置——默认「收进托盘」，可切换为「直接退出进程」。

已确认决策：

| 决策点 | 结论 |
|---|---|
| 可选值 | `tray`（收进托盘，默认）/ `exit`（直接退出） |
| 配置入口 | 桌面 Settings 页「关闭行为」分区，切换即时生效并写回 `~/.kshell/config.yaml` |
| 配置字段 | `close_behavior`，非法/空值回落 `tray` |
| 生效时机 | 立即生效，无需重启 |
| 无托盘兜底 | 托盘未激活（无图标数据/上下文未就绪）时，关闭一律按「直接退出」，避免窗口有去无回 |
| 托盘左键 | 单击（复用现有 `onShow`）恢复主窗口；右键菜单行为不变 |

## 2. 现状

- 关闭收进托盘已实现：`main.go:60` 挂 `OnBeforeClose: desktopApp.BeforeClose`，`internal/desktop/app.go:295` 拦截为 `runtime.WindowHide`；真正退出只走托盘「退出」（`quitApp`，`quitting` 放行）。
- 该行为**写死**，无配置通路；`internal/config/config.go:21` `Config` 无关闭相关字段。
- 托盘菜单只有「显示主窗口 / 退出」两个菜单项（`internal/desktop/tray.go:20-22`），**未注册图标点击回调**。
- 库 `energye/systray@v1.0.3`：`systray_windows.go:362` 的 `WM_LBUTTONUP` 仅在 `t.onClick != nil` 时回调，否则什么都不做；`WM_RBUTTONUP`（`:356`）无回调时默认 `ShowMenu()`——这解释了「右键可用、左键失灵」。
- 写回配置的能力在桌面端尚不存在；同日已确认但未实现的 appearance 设计（`docs/plans/2026-10-03-appearance-theme-design.md`）计划引入 `Options.Layout config.Layout` 并复用 `config.Save(layout, cfg)`，本设计与之对齐。

## 3. 方案选型

关闭行为的持久化位置：

- **方案 A（采用）**：写入现有 `~/.kshell/config.yaml`，新增 `close_behavior` 字段。复用 `config` 包与（appearance 计划引入的）`Options.Layout`，语义最贴切。
- 备选 B：新建 `~/.kshell/desktop.yaml`。不碰 config.yaml，但要多加 Layout 路径与独立存储，收益不足。
- 备选 C：仅内存。不满足「可配置并持久」。

托盘点击的修复方式：库原生提供 `SetOnClick`，复用已验证可工作的 `onShow`（即 `runtime.WindowShow(ctx)`），不引入新机制。

## 4. 设计

### 4.1 配置（`internal/config`）

```go
const (
    CloseBehaviorTray = "tray" // 关闭收进托盘（默认）
    CloseBehaviorExit = "exit" // 关闭直接退出进程
)

type Config struct {
    // ...既有字段
    CloseBehavior string `yaml:"close_behavior"` // tray | exit
}
```

- `Default()` 置 `CloseBehavior: CloseBehaviorTray`。
- `normalized()`：空值或非 `exit`/`tray` 一律回落 `tray`。
- 持久化复用 `config.Save(p Layout, c Config)`；桌面 App 侧写回（config 包不新增 setter）。

### 4.2 桌面端（`internal/desktop`）

**Options / App 扩展**

- `Options` 增 `Layout config.Layout`（与 appearance 设计一致），`initRealDeps` 里 `if a.opts.Layout.Config == "" { a.opts.Layout = paths }`。
- `App` 增 `trayActive bool`：`StartTray` 只有在图标与 ctx 均就绪、即将启动消息循环时，在锁内置真（`BeforeClose` 在锁内读取，避免竞态）。

**BeforeClose 逻辑**

```go
func (a *App) BeforeClose(ctx context.Context) bool {
    a.mu.Lock()
    quitting := a.quitting
    behavior := a.opts.Config.CloseBehavior
    trayActive := a.trayActive
    a.mu.Unlock()

    if quitting {
        return false // 托盘「退出」/重启/信号退出：放行
    }
    if behavior == config.CloseBehaviorExit || !trayActive {
        return false // 直接退出；无托盘时也直接退出，避免窗口不可恢复
    }
    windowHide(ctx) // 收进托盘
    return true
}
```

- 为可测：把 `runtime.WindowHide` 抽为包级变量 `var windowHide = runtime.WindowHide`（对齐既有 `quitRuntime` 抽象），测试注入桩。

**绑定**

- `GetCloseBehavior() string`：返回归一化后的当前值（缺省 `tray`）。
- `SetCloseBehavior(mode string) error`：先校验（非 `tray`/`exit` 返回错误）；在内存 `Config` 的副本上设置新值，`config.Save(a.opts.Layout, cfg)` 写盘成功后，才把副本提交回 `a.opts.Config`（写盘失败则内存保持原值并返回错误）；无需重启即生效。`Layout.Config` 为空（未装配）时返回 `errNotReady`。

### 4.3 托盘左键（`internal/desktop/tray.go`）

```go
func runTray(icon []byte, onShow, onQuit func()) {
    systray.Run(func() {
        systray.SetIcon(icon)
        systray.SetTooltip("kshell")
        systray.SetOnClick(func(systray.IMenu) { // 新增：左键单击恢复主窗口
            if onShow != nil {
                onShow()
            }
        })
        // ...既有菜单项（显示主窗口 / 退出）不变
    }, nil)
}
```

- Windows：`WM_LBUTTONUP` 命中 `onClick` 恢复窗口；双击因先产生 `WM_LBUTTONUP`，同样会恢复一次。
- 右击仍走库默认 `ShowMenu()`，行为不变。

### 4.4 前端

- `frontend/src/lib/api.ts`：`AppBindings` 增 `GetCloseBehavior(): Promise<string>`、`SetCloseBehavior(mode: string): Promise<void>`；新增封装 `getCloseBehavior()`（绑定缺失时返回 `'tray'`）与 `setCloseBehavior(mode)`（错误向上抛）。
- `frontend/src/pages/Settings.tsx`：新增「关闭行为」分区，两个选项按钮（收进托盘=默认 / 直接退出），沿用 appearance 计划的 Button 分段样式。加载时读 `getCloseBehavior`，切换时调 `setCloseBehavior`；失败 `notify(..., 'error')` 并回退本地选中态。
- 桌面自绘标题栏的关闭按钮改调 Wails `Quit()`（而非 `WindowHide`），以确保经由 `BeforeClose` 走可配置分流。

## 5. 测试

- `internal/config`：`Default().CloseBehavior == tray`；非法/空值归一为 `tray`；`Save`/`Load` 往返保留 `exit`。
- `internal/desktop`：`BeforeClose` 四象限——`quitting` 放行、`exit` 放行、`tray` 且托盘激活时调用 `windowHide` 并返回 `true`、`tray` 但托盘未激活时放行；`SetCloseBehavior` 校验非法值报错、合法值写盘并可被 `config.Load` 读回、`GetCloseBehavior` 与内存一致。
- `frontend`（vitest）：Settings 渲染「关闭行为」、默认选中「收进托盘」、点击「直接退出」调用 `setCloseBehavior('exit')`；`setCloseBehavior` 失败显示错误且回退选中态。更新 `Settings.test.tsx` 的 `vi.mock` mocks（整体替换，缺方法会报错）。
- 托盘左键：`tray.go` 为纯 OS 胶水（`energye/systray` 全局单例，无注入缝），不做单测，补 `docs/smoke-test-desktop.md` 条目（隐藏后左键单击图标应恢复窗口）。
- 验证命令：`go test ./... -count=1`、`go vet ./...`、前端 `npm test` / `npm run build`。

## 6. 范围外（YAGNI）

- 不提供「最小化到任务栏」第三种模式。
- 不做托盘菜单内的行为切换入口（统一放 Settings）。
- 不改变托盘「退出」语义与退出信号文件链路。
- 不改 TUI（关闭行为仅桌面端有意义）。

## 7. 依赖与文件清单

- 新增依赖：无。
- 改动：
  - `internal/config/config.go`（字段、常量、Default/normalized）
  - `internal/config/config_test.go`（或新增 `close_behavior_test.go`）
  - `internal/desktop/app.go`（Options.Layout、trayActive、windowHide、BeforeClose、Get/SetCloseBehavior）
  - `internal/desktop/tray.go`（SetOnClick）
  - `internal/desktop/app_test.go`（或新增 `close_behavior_test.go`）
  - `frontend/src/lib/api.ts`、`frontend/src/pages/Settings.tsx`、`frontend/src/pages/Settings.test.tsx`
  - `docs/smoke-test-desktop.md`

> 与 appearance 设计的交叉：两者都改 `internal/config/config.go`、`internal/desktop/app.go`（`Options.Layout`）、`frontend/src/pages/Settings.tsx` 与 `Settings.test.tsx`。实现顺序上，先落地本设计时需自行引入 `Options.Layout`（appearance 尚未实现）；若 appearance 已实现则直接复用，互不冲突（各自 setter 都基于内存中的完整 `Config` 写回，不会互相清空字段）。
