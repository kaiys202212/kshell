# kshell 桌面版（Wails）实施计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this task-by-task.

**Goal:** 在 kshell 仓库内新增 Wails v2 桌面应用（React 前端 + 现有 Go 核心直连），实现工作区页签、会话列表、文件树/预览/上下文篮、SSH 连接四大页面，会话与 SSH 弹 Windows Terminal 窗口且支持按标题聚焦复用，TUI 保留。

**Architecture:** Wails v2 把 `internal/desktop` 绑定层暴露给前端；绑定层薄封装现有 `discovery/providers/launcher/remote/workspace/config` 包。窗口管理器用接口抽象 Win32 调用便于打桩测试。设计文档：`docs/plans/2026-09-27-kshell-wails-desktop-design.md`（原型已验证：`wt --title <t>` 窗口标题即设置值，EnumWindows 按标题匹配 + SetForegroundWindow 聚焦可行）。

**Tech Stack:** Go 1.x（现有）、Wails v2、React + TypeScript + Vite、Win32 syscall（user32）、Windows Terminal。

**前置确认（Task 0 前必须做）**：工作区有未提交的会话标题修复代码（codex/codebuddy 解析），需先提交再开工，否则 worktree 拿不到这些核心修复。

---

### Task 0: 提交遗留修复 + 环境准备

**Step 1: 提交现有未提交改动**（会话标题修复 + build.ps1，本次会话早前产物）

```powershell
git add internal/providers/ internal/discovery/ build.ps1
git commit -m "fix: 会话标题解析与子代理过滤；chore: build.ps1"
```

**Step 2: 安装 Wails CLI 并自检**

```powershell
$env:Path='D:\software\go\bin;'+$env:Path
go install github.com/wailsapp/wails/v2/cmd/wails@latest
wails doctor
```

Expected: Node ✓、WebView2 ✓、Go ✓。失败则停止并报告缺失项。

---

### Task 1: Wails 项目骨架

**Files:**
- Create: `wails.json`、`main.go`（根包，Wails 入口）、`go.mod`（追加依赖）
- Create: `frontend/`（wails 生成 React-TS 模板）

**实施修订（Task 1 实际执行时确认）**：计划原定的「`cmd/kshell -desktop` 分支 + desktop 构建标签」不可行——wails 绑定生成阶段强制剔除 desktop 等标签（bindings.go: `lo.Without(tags, "desktop", ...)`），根包必须无条件可编译；且 embed 无法跨包引用 frontend/dist。实际方案：根包 `main.go` 无标签，`frontend/dist/.gitkeep` 占位保证 embed 始终可解析，桌面版唯一构建入口为 `wails build`。

**Step 1:** 在仓库根执行 `wails init -n kshell -t react-ts`（生成 frontend/ 与模板 main.go），删除模板示例代码，保留最小骨架（根包 main.go，无构建标签）：

```go
package main

import (
	"embed"
	"io/fs"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func RunDesktop(src fs.FS) error {
	return wails.Run(&options.App{
		Title:       "kshell",
		Width:       1280,
		Height:      800,
		AssetServer: &assetserver.Options{Assets: src},
		OnStartup:   desktopApp.Startup,
		Bind:        []interface{}{desktopApp},
	})
}
```

**Step 2:** TUI 入口 `cmd/kshell/main.go` 保持零改动；桌面版通过 `wails build` 构建（根包 main.go）。

**Step 3:** 验证两目标都能构建：

```powershell
go build -o dist/kshell-tui.exe ./cmd/kshell    # TUI 照旧
wails build                                     # 桌面版
```

Expected: 均成功，`build/bin/kshell.exe` 可弹出空窗口。

**Step 4: Commit** `feat: Wails 桌面骨架`

---

### Task 2: 窗口管理器（弹窗/聚焦/存活跟踪，TDD）

**Files:**
- Create: `internal/desktop/window.go`
- Create: `internal/desktop/window_win.go`（Win32 实现）
- Create: `internal/desktop/window_test.go`

**Step 1: 写失败测试**（接口与纯逻辑，不打真实窗口）：

```go
package desktop

// WindowLauncher 抽象弹窗与查找，Win32 实现之外可打桩
type WindowLauncher interface {
	// 在 dir 中启动新终端窗口，窗口标题恒为 "kshell · " + title，返回是否成功
	Launch(dir, title string, args []string) error
	// 按标题找已开窗口并聚焦（最小化也拉回）；不存在返回 false
	Focus(title string) bool
}

// WindowManager 维护 标题→状态 的活跃窗口表
func TestManagerLaunchRegisters(t *testing.T)          // Launch 后 Alive(title)==true
func TestManagerFocusDelegates(t *testing.T)           // Focus 转发到 launcher
func TestManagerReapClosesDeadWindows(t *testing.T)    // 进程死亡后 Alive==false，触发 windowClosed 回调
```

**Step 2:** `go test ./internal/desktop/ -v` 确认 FAIL（包不存在）。

**Step 3: 实现 window.go（平台无关逻辑）**：Manager 持有 `launcher WindowLauncher`、`alive map[string]bool`、`onClosed func(title string)`；`Reap()` 轮询时对 dead 项调用 onClosed。窗口标题统一 `TerminalTitle(kind, id string) string`（如 `kshell · <会话标题>`）。

**Step 4: 实现 window_win.go**（原型代码平移）：`wt.exe` 定位顺序 `PATH → %LOCALAPPDATA%\Microsoft\WindowsApps\wt.exe`；`exec.Command("wt", "-w", "new", "nt", "--title", title, launcher, args...)`，`cmd.Dir = dir`；找不到 wt 回退 `powershell -NoExit -Command "$host.UI.RawUI.WindowTitle='<title>'; ..." `；Focus 用 EnumWindows 匹配标题 → SW_RESTORE + SetForegroundWindow（含 ALT 兜底链）。

**Step 5:** `go test ./internal/desktop/ -v` 全绿；`go vet ./...` 干净。

**Step 6:** 手工冒烟：真实弹一个 `wt --title "kshell · 冒烟" powershell`，再调 Focus 聚焦；杀掉窗口后 Reap 清理。

**Step 7: Commit** `feat: 桌面版窗口管理器（弹窗/聚焦/存活跟踪）`

---

### Task 3: 绑定层——扫描与会话

**Files:**
- Create: `internal/desktop/app.go`
- Create: `internal/desktop/app_test.go`
- Modify: `main_wails.go`（注册 App）

**Step 1: 失败测试**：`TestScanSessionsDelegatesToDiscovery`（打桩 opts，断言委托与结果透传）、`TestResumeSessionLaunchesWindow`（打桩 launcher + provider，断言 wt 参数、标题、Dir=会话工作区、窗口表登记）。

**Step 2: 实现 App**：

```go
type App struct {
	home       string
	paths      config.Paths
	scan       func() (*discovery.Result, error) // 注入便于测试
	windows    *WindowManager
	providers  []providers.Provider
	basket     map[string]bool
}
func (a *App) ScanSessions() (*discovery.Result, error)   // 首次返回缓存，后台扫完 EventsEmit("scan:done")
func (a *App) GetWorkspaces() []discovery.Workspace
func (a *App) ResumeSession(id string) error              // 复用 ui.Model.resumeLaunch 的委托逻辑（抽到 providers 层复用，UI 与 desktop 共用）
func (a *App) NewSession(wsID string) error               // 含上下文篮注入
func (a *App) FocusSession(id string) bool
```

注意：`resumeLaunch` 目前是 `ui.Model` 私有方法——把「选中会话 → Launch」逻辑下沉为 `providers.BuildLaunch(p, s, bin)` 之类的包级函数，UI/desktop 两端调用（勿复制粘贴）。

**Step 3:** 测试全绿 → **Step 4: Commit** `feat: 绑定层扫描/恢复/新建会话`

---

### Task 4: 绑定层——文件树、SSH、设置

**Files:**
- Modify: `internal/desktop/app.go`、`app_test.go`

**Step 1: 失败测试**：`TestListFilesDelegatesToWorkspace`（懒加载参数透传）、`TestPreviewFile`、`TestToggleBasket`、`TestOpenSSHLaunchesWindow`（标题 `kshell · <连接名>`、BatchMode 参数）、`TestExecRemote`（launcher.Run 复用）。

**Step 2: 实现**：

```go
func (a *App) ListFiles(wsID, relPath string) ([]workspace.TreeNode, error)
func (a *App) PreviewFile(wsID, path string) (string, error)  // 复用 workspace.Preview（异步缓存）
func (a *App) ToggleBasket(path string) bool
func (a *App) GetBasket() []string
func (a *App) ListConnections() []remote.Connection
func (a *App) OpenSSH(connID string) error                    // WT 弹窗，ssh -o BatchMode=yes
func (a *App) ExecRemote(connID, cmd string) (string, error)  // launcher.Run
func (a *App) GetTools() []discovery.Tool
func (a *App) SaveProvidersYAML(content string) error         // 写前校验 yaml.Unmarshal 合法
```

**Step 3:** 全绿 → **Step 4: Commit** `feat: 绑定层文件/SSH/设置`

---

### Task 5: 前端——首页与页签框架

**Files:**
- Modify: `frontend/src/App.tsx`、`App.css`
- Create: `frontend/src/pages/Home.tsx`、`frontend/src/pages/WorkspaceTab.tsx`
- Create: `frontend/src/state/store.ts`（Zustand：workspaces / openTabs / basket / windowStatus）

**Step 1:** Home 渲染工作区列表（名称、会话数、git 标记、排序按会话数），点击开页签（可多开、可关闭，页签栏在上）。WorkspaceTab 三栏：左会话列表（本 Task 先占位空态）、右侧 `文件 | SSH` 页签（占位）、中间预览区（占位）。

**Step 2:** 样式基线：CSS 变量定义深浅色 token（`prefers-color-scheme` 跟随系统），布局用 flex，无 UI 库（保持依赖最小）。

**Step 3:** `wails dev` 手工验证：列表渲染、多页签开合、主题切换。

**Step 4: Commit** `feat(frontend): 首页工作区列表与页签框架`

---

### Task 6: 前端——会话列表

**Files:**
- Create: `frontend/src/components/SessionList.tsx` + `SessionList.test.tsx`
- Create: `frontend/src/components/WorkspaceSearch.tsx`

**Step 1: 失败测试**（Vitest）：按 `updatedAt` 降序排序；过滤词匹配标题/工具名；点击「恢复」调用 `ResumeSession` 并把行状态置 `open`；收到 `window:closed` 事件还原；重复点击已 open 的会话调用 `FocusSession` 而非 Resume。

**Step 2: 实现**：会话行 = 标题 + 工具徽标（CodeBuddy/Codex/Claude/Gemini 各配色）+ 相对时间 + 消息数 + 恢复按钮；顶部过滤框；`EventsOn("scan:done")` 刷新、`EventsOn("window:closed", ...)` 还原。

**Step 3:** `npm test` 全绿 → `wails dev` 实测恢复弹窗、重复点击聚焦。

**Step 4: Commit** `feat(frontend): 会话列表与多开/聚焦交互`

---

### Task 7: 前端——文件树、预览、上下文篮

**Files:**
- Create: `frontend/src/components/FileTree.tsx`、`Preview.tsx`、`BasketBar.tsx` 及对应 test

**Step 1: 失败测试**：目录懒加载（展开才调 ListFiles）；点文件加载预览；Space/点击加入篮子；篮子栏增删同步。

**Step 2: 实现**：树用递归组件 + 懒加载；预览只读 `<pre>`（等宽、行号），篮子条目在「新建会话」时作为上下文传入（调 `NewSession` 前读 store）。

**Step 3:** 测试绿 → 实测大文件预览不卡（复用 Go 侧异步缓存）→ **Commit** `feat(frontend): 文件树/预览/上下文篮`

---

### Task 8: 前端——SSH 页签与设置页

**Files:**
- Create: `frontend/src/components/SshPanel.tsx`、`pages/Settings.tsx` 及 test

**Step 1: 失败测试**：连接列表渲染（含扫描器来源标注）；「连接」→ OpenSSH 且 open 状态复用聚焦；命令输入 → ExecRemote 显示输出尾部；设置页读 GetTools 渲染工具状态（未验证 generic 工具显示徽标）、providers.yaml 文本编辑保存。

**Step 2: 实现 + 测试绿 → Commit** `feat(frontend): SSH 面板与设置页`

---

### Task 9: 事件推送、托盘、收尾

**Files:**
- Modify: `internal/desktop/app.go`（后台扫描 goroutine + EventsEmit、窗口 Reap 定时器）
- Modify: `main_wails.go`（托盘最小化、关闭行为=隐藏到托盘）

**Step 1:** 测试：扫描进度事件序列（progress→done）；窗口退出回调触发 `window:closed`。

**Step 2:** 实现 + `wails build` 出 `dist/kshell-desktop.exe`。

**Step 3: Commit** `feat: 桌面版事件推送与托盘`

---

### Task 10: 构建脚本与全量回归

**Step 1:** `build.ps1` 增加 `-Desktop` 开关（调 `wails build`，产物拷到 `dist/`）。

**Step 2:** 全量回归：`go test ./... -count=1` 全绿（现有约 90 用例 + desktop 新增）、`go vet ./...` 干净、`npm test` 全绿。

**Step 3:** 手工冒烟清单（docs/smoke-test-desktop.md）：
- 多开 3 个不同工具会话窗口互不影响
- 最小化的会话窗口点击后拉回前台
- 同一会话重复点击不弹新窗
- SSH 连接弹窗与聚焦复用
- WT 未安装回退 PowerShell（可临时改 PATH 模拟）
- 深浅色主题、TUI `go build ./cmd/kshell` 照常可用

**Step 4: Commit** `feat: 桌面版构建脚本与冒烟清单`
