# kshell 桌面端 UI 第二轮：扁平化 + 标题栏集成 + 内嵌终端 设计

**背景：** 2026-09-28 已完成第一轮桌面 UI 改造（Tailwind 4 + Radix + shadcn 风格 token、页签持久化、双面板常挂载等）。本轮针对真机使用暴露的问题继续改造。

**本轮的 6 项诉求（用户原话提炼）：**
1. 整体改扁平化风格
2. 首页与新开页签放进系统标题栏，设置放到最右侧
3. 工作区做成卡片，带最后活动时间，按时间倒序
4. 会话列表标题泄漏 XML 包装标签（`<local-command-caveat>…`）且行内样式挤压错乱
5. 会话页三栏支持宽度拖动；中心区改为嵌入式命令行，点「恢复」直接在中心区恢复对话，已恢复的多个对话可快捷切换
6. 新建会话支持选择使用的工具

**已确认的决策（用户选择）：**
- 窗口改无边框（Frameless），窗口控件由前端自绘。
- 终端用真终端：Go ConPTY + 前端 xterm.js。
- 中心区保留「预览」，终端作为中心区的页签（预览 + N 个终端 共存）。
- 会话切换用中心区顶部页签条，并保留「在外部终端打开」的次要入口。

**技术事实核对（本轮实测确认，均影响选型）：**
- Wails v2.16 `options.App.Frameless=true` 默认 `DisableFramelessWindowDecorations=false` → 走 `framelessWithDecorations` 分支，内部用 `win32.ExtendFrameIntoClientArea` 把客户区扩展进边框：**原生缩放边框、DWM 阴影、最小化/最大化动画、Aero Snap 基本保留**，只是标题栏由应用自绘。拖拽用 CSS `--wails-draggable: drag`。
- `creack/pty@v1.1.24` 的 `start_windows.go` 返回 `ErrUnsupported`：**Windows 不可用**。改用 `github.com/aymanbagabas/go-pty@v0.2.3`（Windows 走 ConPTY，Unix 走 creack/pty），API：`pty.New() → Pty.Command(bin, args...) → Cmd{Dir,Env}.Start()/Wait()`，`Pty.Read/Write/Resize/Close`。
- go-pty Windows 分支直接调 `CreateProcess`，**不会**像 `exec.Cmd` 那样自动包 `cmd.exe /c`。npm 全局 CLI 的 `.cmd` 兄弟文件必须显式经 `%COMSPEC% /c` 启动（`.ps1` 已被 `launcher.Resolve` 转成 `.cmd` 或 `powershell -Command`）。
- 会话缓存 `discovery.indexVersion` 当前为 3：标题解析逻辑变更时必须递增，否则旧缓存把旧标题继续喂给前端。
- Claude Code 的包装消息除标签外还带 `"isMeta": true`（`<local-command-caveat>` 记录实测均带），是比标签匹配更硬的信号。

---

## 1. 扁平设计语言

沿用现有 token 层（`frontend/src/style.css` 的 `@theme inline` + `prefers-color-scheme`），只调整取值与用法：

- 圆角降到 4px（`--radius-base: 4px`），页签/按钮/输入框统一 `rounded`（不再 `rounded-lg`/`rounded-full` 混用）。
- 去阴影、去 hover 抬升：`hover:shadow-sm`、`border-ring/50` 一类渐变过渡删掉，hover 一律用背景色切换。
- 层次靠「面」不靠「框」：容器用 `bg-card`/`bg-background` 区分，分隔用 1px `border-border`；同层面板之间不加框。
- 选中/激活态用左侧 2px 主色条 + 轻微底色（`bg-primary/8`），不用圆角胶囊填充。
- 深色模式 token 同步收敛。

## 2. 无边框窗口 + 标题栏

**Go（`main.go`）：** `Frameless: true`；`MinWidth/MinHeight` 保持。

**前端新增 `components/TitleBar.tsx`，替换 App 现有 `<header>`：**

```
┌──────────────────────────────────────────────────────────────────────────┐
│ ▮kshell  首页 │ recorder × │ strategy ×         [设置]  ─  □  ×          │
└──────────────────────────────────────────────────────────────────────────┘
```

- 整条 `h-9`，`style={{'--wails-draggable':'drag'}}`；所有可点元素 `--wails-draggable:no-drag`。
- 左侧 logo（`k` 主色 + `shell`）+ 页签区（首页固定、工作区页签可关、中键关闭保留）；激活态用底部 2px 主色下划线。
- 右侧：设置按钮（icon+文字，激活时高亮）→ 竖分隔 → 自绘窗口控件（最小化/最大化-还原/关闭），
  走 `wailsjs/runtime` 的 `WindowMinimise` / `WindowToggleMaximise` / `WindowClose`，
  并用 `WindowIsMaximised` + `EventsOn('common:WindowMaximise'/'common:WindowUnmaximise')` 同步图标。
- 双击标题栏空白区 → `WindowToggleMaximise`。
- 页签溢出横向滚动（保留现有 `overflow-x-auto`）。

## 3. 首页工作区卡片

- 数据已有：`Workspace.LastUsed`（`discovery.GroupSessions` 已按会话 `UpdatedAt` 取最大）。
- 排序改为 `LastUsed` 倒序（无时间的 git-only 工作区排最后，再按名称）；不再按会话数排序。
- 布局：响应式网格卡片（`grid-cols-[repeat(auto-fill,minmax(240px,1fr))]`），卡片内含
  名称（粗体，单行截断）、git 徽标、`N 个会话`、`最后活动 <相对时间>`、工具分布（最多 3 个工具徽标 + `+N`）。
- 首页顶部保留「重新扫描」，扫描中显示进度态；`Ctrl K/F` 提示行保留但扁平化。

## 4. 会话标题清洗 + 会话行重排

**Go（`internal/providers`）：**

- 新增 `cleanTitle(raw string) string`：
  1. 删除已知包装块：`local-command-caveat` / `command-name` / `command-message` / `command-args` /
     `local-command-stdout` / `system-reminder`（开闭标签成对删除，容错未闭合）。
  2. 删除剩余裸标签 `<...>`。
  3. `strings.Fields` 折叠空白；结果为空 → 返回空串（调用方跳过该候选）。
- `claude.go`：跳过 `isMeta == true` 的 user 记录；标题取第一条 `cleanTitle(messageText(...))` 非空的 user 记录。
- `generic.go`：标题候选同样过 `cleanTitle`（保留既有 `isWrapperText` 作为兜底）。
- `discovery/index.go`：`indexVersion` 3 → 4。

**前端（`SessionList.tsx`）渲染层兜底：** `lib/title.ts` 增 `displayTitle(raw)`：对历史/未重扫的库也做一次同样的标签剥离（与 Go 侧规则同源，纯字符串处理）。

**会话行重排（修「样式乱了」）：** 两行 flex，`min-w-0` 贯穿。

```
第一行： 标题（truncate）                    [✓ 运行中]
第二行： [工具徽标] 相对时间 · N 条          [恢复/切换] [⧉]
```

- 主按钮「恢复」→ 内嵌终端（已有运行中的终端时文案变「切换」，置 activate 语义）。
- 次要图标按钮（`aria-label="在外部终端打开"`）→ 现有 `ResumeSession`（弹出 Windows Terminal）。
- 工具筛选 chip 行保留，改为扁平下划线式。

## 5. 三栏可拖动 + 中心区页签（预览 + 终端）

**布局状态（`state/store.ts`，持久化）：** `layout: { left: number; right: number }`，默认 `left=280`、`right=300`，clamp `[200, 560]`。新增 `components/ResizeHandle.tsx`（pointer 事件 + `col-resize` 光标 + 拖动时禁选中）。

**中心区结构（`pages/WorkspaceTab.tsx` 重构）：**

```
[预览] [recorder 修复登录 (Claude) ×] [新会话 (Codex) ×]
┌──────────────────────────────────────────────────────┐
│  预览内容 或 xterm 终端（非激活的用 hidden 保活）      │
└──────────────────────────────────────────────────────┘
```

- 中心区页签 = 固定「预览」+ 每个内嵌终端一个页签；终端页签显示会话标题 + 工具徽标，可关闭（关闭即结束进程）。
- **常挂载策略**：内容区的「首页 / 设置 / 每个工作区页签」同时挂载，非激活项用 `hidden` 隐藏；
  工作区页签内的终端组件同理。因此 xterm 缓冲、焦点、文件树展开态、SSH 状态在整个应用生命周期内都不丢，
  换来的是各页签在启动时各自拉一次数据（都是进程内 IPC，代价可接受）。
  代价与边界：打开的工作区页签越多，常驻的列表/文件树订阅越多；关闭页签即结束该工作区的终端进程。
- 因为终端不会卸载，前端不做 scrollback 回放：**每次「打开终端」都是从新进程开始**。
  Go 侧仍保留 256 KiB 环形缓冲（`ScrollbackTerminal` 绑定），供后续需要回放的场景使用，
  也让「前端重载后 Go 侧终端仍在跑」这种情况可被 `ListTerminals` 还原出页签。

**终端数据流：**

| 方向 | 通道 |
|---|---|
| 开 | `OpenSessionTerminal(sessionID, cols, rows)` / `OpenWorkspaceTerminal(wsID, toolID, cols, rows)` → 返回 `TerminalInfo{ID,…}` |
| 输入 | xterm `onData` → `WriteTerminal(id, base64)` |
| 尺寸 | `FitAddon` + `ResizeObserver` → `ResizeTerminal(id, cols, rows)` |
| 输出 | Go 读协程 → 事件 `terminal:data{id, data(base64)}` → 前端全局单例订阅 → 分发到注册表里的 xterm |
| 退出 | 事件 `terminal:exit{id, exitCode}` → 页签状态改「已退出」，终端内打印收尾提示 |
| 关闭 | `CloseTerminal(id)`（幂等）；关闭工作区页签时连带关闭该工作区所有终端 |

- `lib/terminalRegistry.ts`：模块级 `Map<termId, XTermHandle>`，由 `TerminalView` 挂载/卸载时登记；`lib/api.ts` 侧的全局订阅（App 挂载时建立一次）负责把事件投递给已登记的实例，**未登记的终端不丢数据**（Go 侧缓冲兜住，重挂载时回放）。
- `components/TerminalView.tsx`：xterm.js + FitAddon（xterm 5 `@xterm/xterm`、`@xterm/addon-fit`）；主题跟随深浅色 token；挂载时先回放 scrollback 再订阅事件；退出后显示「会话已退出（code N）」且允许「重新打开」。

**Go 侧 `internal/terminal`：**

```go
type Spec struct{ Path string; Args []string; Dir string; Env []string }

type Info struct {
    ID, Kind, SessionID, Workspace, Title, ToolID string
    Status  string // running | exited
    ExitCode int
    Cols, Rows int
}

type Backend interface{ Start(Spec, cols, rows int) (Handle, error) }
type Handle interface {
    Read([]byte) (int, error)
    Write([]byte) (int, error)
    Resize(cols, rows int) error
    Wait() (int, error)
    Close() error
}

type Manager struct{ /* mu + sessions map + 环形缓冲 + 回调 */ }
func (m *Manager) Open(key string, spec Spec, cols, rows int) (Info, error) // key 相同则复用
func (m *Manager) Write(id string, data []byte) error
func (m *Manager) Resize(id string, cols, rows int) error
func (m *Manager) Close(id string) error
func (m *Manager) List() []Info
func (m *Manager) Scrollback(id string) ([]byte, error)
```

- 回调 `OnData(id, chunk)` / `OnExit(id, code)` 由 `desktop.App` 注入 → `Emit("terminal:data"/"terminal:exit", …)`。
- 环形缓冲 256 KiB/会话；关闭后 `Scrollback` 仍可读，重开（`Open`）才重置。
- Windows/Unix 后端分平台文件（Windows 用 `go-pty` 的 ConPTY，其它平台走同一实现即可——go-pty 已跨平台）；`.cmd/.bat` 经 `%COMSPEC% /c` 包装（见上文技术事实）。

## 6. 新建会话可选工具

- `components/ToolPicker.tsx`：紧凑按钮 + 下拉列表（已安装且 `BinPath != ""` 的工具，来自 `GetTools()`），
  默认选中该工作区会话数最多的工具（与 Go 侧 `launch.PreferredTool` 同规则，前端按 `ws.ToolCounts` 推导）。
- 点击「新建会话」→ `OpenWorkspaceTerminal(wsPath, toolID, cols, rows)` → 中心区新页签。
- 次要入口（下拉项 `在外部终端打开`）→ 新增绑定 `NewSessionWithTool(wsID, toolID)`（空 toolID = 原首选逻辑），
  `NewSession(wsID)` 保留并委托给它（不破坏既有测试与调用）。

## 非目标 / 已知限制

- 不做终端分屏、不做多窗口；终端不持久化（应用退出即结束所有内嵌会话）。
- 终端进程的孙进程（`cmd.exe /c` 包装的 node CLI）随 ConPTY 关闭的保证较弱，
  关闭终端页签后是否残留 node 进程列入冒烟清单人工确认。
- 「关闭」按钮与原生 X 同语义（收进托盘），不是退出应用。
- 不做自定义主题色/字号设置。
