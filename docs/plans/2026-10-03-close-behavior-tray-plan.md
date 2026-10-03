# kshell 关闭行为（收进托盘 / 直接退出）与托盘左键恢复 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复隐藏后托盘图标左键无法恢复窗口的问题，并新增「关闭收进托盘 / 直接退出」可配置行为（默认收进托盘、立即生效、可持久化）。

**Architecture:** 复用 `internal/config` 承载 `close_behavior` 字段与 `config.Save` 写回；桌面 `App` 增加 `Options.Layout`、`trayActive` 与 `windowHide` 抽象，`BeforeClose` 依据行为与托盘状态分流；`tray.go` 补 `SetOnClick`；前端 Settings 新增分区，经 `GetCloseBehavior/SetCloseBehavior` 绑定读写。

**Tech Stack:** Go 1.2x + `gopkg.in/yaml.v3`、Wails v2、`energye/systray`、React 19 + Tailwind v4 + Zustand、vitest。

> 说明：按 AGENTS.md，提交只在用户明确要求时执行；下面各任务的 Commit 步骤在用户同意后再运行。

---

## 文件结构

**修改**
- `internal/config/config.go` — 新增 `CloseBehavior` 字段、常量、Default/normalized。
- `internal/config/close_behavior_test.go`（新增）— 默认值/归一化/往返。
- `internal/desktop/app.go` — `Options.Layout`、`App.trayActive`、`windowHide`、`BeforeClose`、`GetCloseBehavior`、`SetCloseBehavior`、`StartTray` 置位。
- `internal/desktop/close_behavior_test.go`（新增）— BeforeClose 四象限 + Get/Set 持久化。
- `internal/desktop/tray.go` — `SetOnClick`。
- `frontend/src/lib/api.ts` — 两个绑定声明与封装。
- `frontend/src/pages/Settings.tsx` — 「关闭行为」分区。
- `frontend/src/pages/Settings.test.tsx` — 新增用例与 mock。
- `docs/smoke-test-desktop.md` — 新增托盘/关闭行为冒烟条目。

---

## Task 1: config 增加 close_behavior

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/close_behavior_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/config/close_behavior_test.go`：

```go
package config

import "testing"

func TestCloseBehaviorDefaultIsTray(t *testing.T) {
	if got := Default().CloseBehavior; got != CloseBehaviorTray {
		t.Fatalf("default close_behavior = %q, want %q", got, CloseBehaviorTray)
	}
}

func TestCloseBehaviorNormalized(t *testing.T) {
	cases := map[string]string{
		"":      CloseBehaviorTray,
		"tray":  CloseBehaviorTray,
		"exit":  CloseBehaviorExit,
		"EXIT":  CloseBehaviorTray, // 只有精确小写合法，其余回落默认
		"bogus": CloseBehaviorTray,
	}
	for in, want := range cases {
		c := Config{MaxDepth: 1, CloseBehavior: in}
		if got := c.normalized().CloseBehavior; got != want {
			t.Fatalf("normalized(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCloseBehaviorSaveLoadRoundTrip(t *testing.T) {
	p := tempPaths(t)
	cfg := Default()
	cfg.CloseBehavior = CloseBehaviorExit
	if err := Save(p, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.CloseBehavior != CloseBehaviorExit {
		t.Fatalf("loaded = %q, want %q", loaded.CloseBehavior, CloseBehaviorExit)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/config/ -run TestCloseBehavior -count=1`
Expected: FAIL（`CloseBehaviorTray`/`CloseBehavior` 未定义）

- [ ] **Step 3: 实现配置字段**

在 `internal/config/config.go` 的 `SSHOptions` 定义之后、`Config` 之前加入常量：

```go
// 关闭窗口的行为取值。
const (
	CloseBehaviorTray = "tray" // 收进系统托盘（默认）
	CloseBehaviorExit = "exit" // 直接退出进程
)
```

在 `Config` 结构体末尾加字段：

```go
type Config struct {
	ScanRoots     []string        `yaml:"scan_roots"`
	MaxDepth      int             `yaml:"max_depth"`
	Exclude       []string        `yaml:"exclude"`
	SSHOptions    SSHOptions      `yaml:"ssh"`
	Scanners      map[string]bool `yaml:"scanners"`
	CloseBehavior string          `yaml:"close_behavior"` // tray | exit
}
```

在 `Default()` 返回的 `Config{...}` 里加：

```go
		CloseBehavior: CloseBehaviorTray,
```

在 `normalized()` 的 `return c` 之前加：

```go
	// 关闭行为：空值或非 exit 一律回落默认（tray）。
	if c.CloseBehavior != CloseBehaviorExit {
		c.CloseBehavior = CloseBehaviorTray
	}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/config/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/close_behavior_test.go
git commit -m "feat(config): 新增 close_behavior 关闭行为配置"
```

---

## Task 2: 桌面端 Layout、windowHide 与可配置 BeforeClose

**Files:**
- Modify: `internal/desktop/app.go`
- Test: `internal/desktop/close_behavior_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/desktop/close_behavior_test.go`：

```go
package desktop

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/config"
)

// stubWindowHide 替换 windowHide 并记录调用次数。
func stubWindowHide(t *testing.T) *int {
	t.Helper()
	calls := 0
	orig := windowHide
	windowHide = func(context.Context) { calls++ }
	t.Cleanup(func() { windowHide = orig })
	return &calls
}

// newCloseApp 组装一个只关心关闭行为的 App：带临时 Layout、指定行为与托盘状态。
func newCloseApp(t *testing.T, behavior string, trayActive bool) *App {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.CloseBehavior = behavior
	app := NewAppWith(Options{
		Config: cfg,
		Layout: config.Layout{
			Root:   dir,
			Config: filepath.Join(dir, "config.yaml"),
			Cache:  filepath.Join(dir, "cache"),
		},
	})
	app.trayActive = trayActive
	return app
}

func TestBeforeCloseHidesWhenTrayModeAndActive(t *testing.T) {
	calls := stubWindowHide(t)
	app := newCloseApp(t, config.CloseBehaviorTray, true)

	if got := app.BeforeClose(context.Background()); !got {
		t.Fatal("tray 模式应拦截关闭（返回 true）")
	}
	if *calls != 1 {
		t.Fatalf("windowHide 调用 %d 次, want 1", *calls)
	}
}

func TestBeforeCloseExitsWhenBehaviorExit(t *testing.T) {
	calls := stubWindowHide(t)
	app := newCloseApp(t, config.CloseBehaviorExit, true)

	if got := app.BeforeClose(context.Background()); got {
		t.Fatal("exit 模式应放行（返回 false）")
	}
	if *calls != 0 {
		t.Fatalf("exit 模式不应调用 windowHide, got %d", *calls)
	}
}

func TestBeforeCloseExitsWhenTrayInactive(t *testing.T) {
	calls := stubWindowHide(t)
	app := newCloseApp(t, config.CloseBehaviorTray, false)

	if got := app.BeforeClose(context.Background()); got {
		t.Fatal("托盘未激活时应直接退出（返回 false）")
	}
	if *calls != 0 {
		t.Fatalf("托盘未激活不应调用 windowHide, got %d", *calls)
	}
}

func TestBeforeCloseExitsWhenQuitting(t *testing.T) {
	calls := stubWindowHide(t)
	app := newCloseApp(t, config.CloseBehaviorTray, true)
	app.mu.Lock()
	app.quitting = true
	app.mu.Unlock()

	if got := app.BeforeClose(context.Background()); got {
		t.Fatal("quitting 应放行")
	}
	if *calls != 0 {
		t.Fatalf("quitting 不应调用 windowHide, got %d", *calls)
	}
}

func TestGetSetCloseBehaviorPersists(t *testing.T) {
	dir := t.TempDir()
	layout := config.Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	app := NewAppWith(Options{Config: config.Default(), Layout: layout})

	if got := app.GetCloseBehavior(); got != config.CloseBehaviorTray {
		t.Fatalf("默认 GetCloseBehavior = %q, want tray", got)
	}
	if err := app.SetCloseBehavior(config.CloseBehaviorExit); err != nil {
		t.Fatalf("SetCloseBehavior: %v", err)
	}
	if got := app.GetCloseBehavior(); got != config.CloseBehaviorExit {
		t.Fatalf("GetCloseBehavior = %q, want exit", got)
	}
	loaded, err := config.Load(layout)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.CloseBehavior != config.CloseBehaviorExit {
		t.Fatalf("persisted = %q, want exit", loaded.CloseBehavior)
	}

	if err := app.SetCloseBehavior("bogus"); err == nil {
		t.Fatal("非法值应返回错误")
	}
	if got := app.GetCloseBehavior(); got != config.CloseBehaviorExit {
		t.Fatalf("非法值不应改变当前行为, got %q", got)
	}
}

func TestSetCloseBehaviorNotReady(t *testing.T) {
	app := NewAppWith(Options{Config: config.Default()})
	if err := app.SetCloseBehavior(config.CloseBehaviorExit); err == nil {
		t.Fatal("未装配 Layout 应返回错误")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/desktop/ -run 'TestBeforeClose|TestGetSetCloseBehavior|TestSetCloseBehaviorNotReady' -count=1`
Expected: FAIL（`windowHide`/`Options.Layout`/`GetCloseBehavior`/`SetCloseBehavior`/`trayActive` 未定义）

- [ ] **Step 3: 扩展 Options / App / 包级抽象**

在 `internal/desktop/app.go` 的 `Options` 结构体末尾加：

```go
	// Layout 是 ~/.kshell 的路径布局，写回配置（关闭行为等）时使用。
	Layout config.Layout
```

在 `App` 结构体的 `quitting` 字段后加：

```go
	trayActive bool // 系统托盘消息循环已启动：BeforeClose 据此判断能否收进托盘
```

在 `quitRuntime` 变量附近加：

```go
// windowHide 是 runtime.WindowHide 的包级变量抽象：测试注入用，对齐 quitRuntime。
var windowHide = runtime.WindowHide
```

`app.go` 的 import 增加 `"fmt"`。

- [ ] **Step 4: initRealDeps 补齐 Layout**

在 `initRealDeps` 内、锁区内 `a.opts.Projects` 赋值段附近加：

```go
	if a.opts.Layout.Config == "" {
		a.opts.Layout = paths
	}
```

- [ ] **Step 5: 改造 StartTray 置位 trayActive**

把 `StartTray` 整体替换为：

```go
// StartTray 启动系统托盘（未注入托盘图标或 Wails 上下文未就绪时跳过）。
// systray.Run 独占调用方 goroutine 跑消息循环，必须在 goroutine 里启动。
func (a *App) StartTray() {
	a.mu.Lock()
	icon := a.opts.TrayIcon
	ctx := a.ctx
	if len(icon) == 0 || ctx == nil {
		a.mu.Unlock()
		return
	}
	a.trayActive = true
	a.mu.Unlock()

	go runTray(icon,
		func() { runtime.WindowShow(ctx) },
		func() { a.quitApp(ctx) },
	)
}
```

- [ ] **Step 6: 改造 BeforeClose 并新增两绑定**

把 `BeforeClose` 整体替换为：

```go
// BeforeClose 供 Wails OnBeforeClose 挂载：默认拦截窗口关闭改为隐藏到托盘（返回 true）。
// 放行（返回 false）的三种情形：托盘「退出」/重启/信号退出期间（quitting）、
// 关闭行为配置为 exit、或托盘未激活（避免窗口有去无回，直接退出）。
func (a *App) BeforeClose(ctx context.Context) bool {
	a.mu.Lock()
	quitting := a.quitting
	behavior := a.opts.Config.CloseBehavior
	trayActive := a.trayActive
	a.mu.Unlock()

	if quitting {
		return false
	}
	if behavior == config.CloseBehaviorExit || !trayActive {
		return false
	}
	windowHide(ctx)
	return true
}

// GetCloseBehavior 返回归一化后的关闭行为：exit 原样返回，其余一律 tray（默认）。
func (a *App) GetCloseBehavior() string {
	a.mu.Lock()
	behavior := a.opts.Config.CloseBehavior
	a.mu.Unlock()
	if behavior == config.CloseBehaviorExit {
		return config.CloseBehaviorExit
	}
	return config.CloseBehaviorTray
}

// SetCloseBehavior 校验并持久化关闭行为，立即生效（无需重启）。
// 先写盘成功再提交内存，避免写失败却留下不一致；非法值或未装配 Layout 直接报错且不改状态。
func (a *App) SetCloseBehavior(mode string) error {
	if mode != config.CloseBehaviorTray && mode != config.CloseBehaviorExit {
		return fmt.Errorf("非法的关闭行为 %q（可选 tray / exit）", mode)
	}

	a.mu.Lock()
	layout := a.opts.Layout
	cfg := a.opts.Config
	a.mu.Unlock()

	if layout.Config == "" {
		return errNotReady
	}
	cfg.CloseBehavior = mode
	if err := config.Save(layout, cfg); err != nil {
		return err
	}

	a.mu.Lock()
	a.opts.Config.CloseBehavior = mode
	a.mu.Unlock()
	return nil
}
```

- [ ] **Step 7: 运行测试确认通过**

Run: `go test ./internal/desktop/ -count=1`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/desktop/app.go internal/desktop/close_behavior_test.go
git commit -m "feat(desktop): 关闭行为可配置（托盘/退出）并补齐托盘状态与窗口隐藏抽象"
```

---

## Task 3: 托盘图标左键恢复主窗口

**Files:**
- Modify: `internal/desktop/tray.go`
- Modify: `docs/smoke-test-desktop.md`

- [ ] **Step 1: 注册左键回调**

在 `internal/desktop/tray.go` 的 `systray.SetTooltip("kshell")` 之后加：

```go
		// 左键单击图标恢复主窗口（WM_LBUTTONUP；缺此回调则左键无任何反应）
		systray.SetOnClick(func(systray.IMenu) {
			if onShow != nil {
				onShow()
			}
		})
```

- [ ] **Step 2: 编译确认**

Run: `go build ./...`
Expected: 无错误（`systray.IMenu`/`systray.SetOnClick` 存在于 v1.0.3）

- [ ] **Step 3: 更新冒烟清单**

在 `docs/smoke-test-desktop.md` 的「托盘」小节末尾追加：

```markdown
- [ ] **左键单击托盘图标恢复窗口**
  - 前置：应用正常启动，先点标题栏 × 收进托盘
  - 步骤：左键单击通知区托盘图标（不打开菜单）
  - 预期：主窗口立即还原到前台；再次收进托盘后左键仍可唤回；右键菜单「显示主窗口」行为不变
- [ ] **关闭行为可配置**
  - 步骤：设置页「关闭行为」选「直接退出」→ 点标题栏 ×；重启应用后改回「收进托盘」→ 再点 ×
  - 预期：选「直接退出」时点 × 进程结束（托盘图标消失、任务管理器无残留）；选「收进托盘」时点 × 仅隐藏窗口；设置重启后仍保持（写回 `~/.kshell/config.yaml` 的 `close_behavior`）
```

> 注：桌面自绘标题栏的关闭按钮改调 Wails `Quit()`（而非 `WindowHide`），以确保经由 `BeforeClose` 走可配置分流。

- [ ] **Step 4: Commit**

```bash
git add internal/desktop/tray.go docs/smoke-test-desktop.md
git commit -m "fix(desktop): 托盘图标左键单击恢复主窗口并补冒烟清单"
```

---

## Task 4: 前端 api 绑定封装

**Files:**
- Modify: `frontend/src/lib/api.ts`

- [ ] **Step 1: 声明绑定方法**

在 `frontend/src/lib/api.ts` 的 `AppBindings` 接口里，`GetDeletedProjects(): Promise<DeletedProject[]>;` 之后加：

```ts
  GetCloseBehavior(): Promise<string>;
  SetCloseBehavior(mode: string): Promise<void>;
```

- [ ] **Step 2: 新增封装**

在 `frontend/src/lib/api.ts` 文件末尾（`onTerminalExit` 之后）加：

```ts
// ---- 关闭行为 ----

// getCloseBehavior 返回当前关闭行为（tray | exit）；绑定不可用时兜底 tray。
export async function getCloseBehavior(): Promise<string> {
  const a = app();
  if (!a) return 'tray';
  return a.GetCloseBehavior();
}

// setCloseBehavior 设置关闭行为（Go 侧校验并写回 config.yaml），错误向上抛。
export async function setCloseBehavior(mode: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.SetCloseBehavior(mode);
}
```

- [ ] **Step 3: 类型检查**

Run: `npm run build`（在 `frontend/`）
Expected: 通过（tsc + vite build）

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/api.ts
git commit -m "feat(frontend): 关闭行为绑定封装"
```

---

## Task 5: Settings 页「关闭行为」分区

**Files:**
- Modify: `frontend/src/pages/Settings.test.tsx`
- Modify: `frontend/src/pages/Settings.tsx`

- [ ] **Step 1: 写失败测试**

在 `frontend/src/pages/Settings.test.tsx` 顶部 `mocks` 对象里补两个方法：

```ts
const mocks = vi.hoisted(() => ({
  getTools: vi.fn(),
  loadProvidersYAML: vi.fn(),
  saveProvidersYAML: vi.fn(),
  restartApp: vi.fn(),
  getCloseBehavior: vi.fn(),
  setCloseBehavior: vi.fn(),
}));
```

在 `beforeEach` 里加默认桩（放在 `mocks.saveProvidersYAML.mockResolvedValue(undefined);` 之后）：

```ts
  mocks.getCloseBehavior.mockResolvedValue('tray');
  mocks.setCloseBehavior.mockResolvedValue(undefined);
```

在 `describe('Settings', () => {` 内追加两个用例：

```tsx
  it('关闭行为默认收进托盘，切换为直接退出调用 SetCloseBehavior', async () => {
    useAppStore.setState({ toasts: [] });
    render(<Settings />);

    const trayBtn = await screen.findByRole('button', { name: '收进托盘' });
    expect(trayBtn).toHaveAttribute('aria-pressed', 'true');

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '直接退出' }));
    });
    expect(mocks.setCloseBehavior).toHaveBeenCalledWith('exit');
    expect(screen.getByRole('button', { name: '直接退出' })).toHaveAttribute('aria-pressed', 'true');
  });

  it('切换关闭行为失败时提示错误且回退选中态', async () => {
    useAppStore.setState({ toasts: [] });
    mocks.setCloseBehavior.mockRejectedValueOnce(new Error('写盘失败'));
    render(<Settings />);

    await screen.findByRole('button', { name: '收进托盘' });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '直接退出' }));
    });
    await waitFor(() => {
      expect(
        useAppStore.getState().toasts.some((t) => t.tone === 'error' && t.title === '写盘失败'),
      ).toBe(true);
    });
    expect(screen.getByRole('button', { name: '收进托盘' })).toHaveAttribute('aria-pressed', 'true');
  });
```

- [ ] **Step 2: 运行测试确认失败**

Run: `npm test -- --run pages/Settings.test.tsx`（在 `frontend/`）
Expected: FAIL（页面没有「收进托盘」按钮 / `setCloseBehavior` 未被调用）

- [ ] **Step 3: 实现设置分区**

在 `frontend/src/pages/Settings.tsx` 更新 api 导入：

```tsx
import { getCloseBehavior, getTools, loadProvidersYAML, restartApp, saveProvidersYAML, setCloseBehavior } from '../lib/api';
```

在组件顶部状态区加：

```tsx
  const [closeBehavior, setCloseBehaviorLocal] = useState<string>('tray');
```

在首个 `useEffect` 的 `loadProvidersYAML()` 调用之后（同一 effect 内）加：

```tsx
    getCloseBehavior()
      .then((mode) => {
        if (!cancelled) setCloseBehaviorLocal(mode);
      })
      .catch(() => {});
```

在 `handleRestart` 之后加切换处理：

```tsx
  // 关闭行为切换：立即生效（Go 侧写盘），失败提示并保持原选中态
  const handleCloseBehavior = async (mode: string) => {
    try {
      await setCloseBehavior(mode);
      setCloseBehaviorLocal(mode);
    } catch (e: unknown) {
      notify(e instanceof Error ? e.message : String(e), 'error');
    }
  };
```

在「工具检测」`<section>` 之前插入新分区：

```tsx
        <section className="mb-5 rounded border border-border bg-card p-3.5">
          <h2 className="mb-3 text-sm font-medium">关闭行为</h2>
          <div className="flex gap-2">
            {[
              { value: 'tray', label: '收进托盘' },
              { value: 'exit', label: '直接退出' },
            ].map((opt) => (
              <Button
                key={opt.value}
                variant={closeBehavior === opt.value ? 'default' : 'secondary'}
                aria-pressed={closeBehavior === opt.value}
                onClick={() => void handleCloseBehavior(opt.value)}
              >
                {opt.label}
              </Button>
            ))}
          </div>
        </section>
```

- [ ] **Step 4: 运行测试确认通过**

Run: `npm test -- --run pages/Settings.test.tsx`（在 `frontend/`）
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/Settings.tsx frontend/src/pages/Settings.test.tsx
git commit -m "feat(frontend): 设置页新增关闭行为切换"
```

---

## Task 6: 全量校验

**Files:** 无（仅运行校验）

- [ ] **Step 1: Go 校验**

Run:
```powershell
go vet ./...
go test ./... -count=1
```
Expected: 全部 PASS

- [ ] **Step 2: 前端校验**

Run（在 `frontend/`）：
```powershell
npm test
npm run build
```
Expected: 测试全绿、构建成功

- [ ] **Step 3: 手动冒烟**

按 `docs/smoke-test-desktop.md` 的「托盘」小节新条目执行：左键单击恢复窗口、关闭行为切换与持久化。
