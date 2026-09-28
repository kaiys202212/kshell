# kshell 桌面端 UI 第二轮 实现计划

> **设计文档：** `docs/plans/2026-09-28-desktop-ui-round2-design.md`

**Goal:** 扁平化视觉 + 无边框标题栏（页签入栏、设置贴右）+ 工作区卡片（时间倒序）+ 会话标题清洗 + 三栏可拖动与中心区内嵌终端（ConPTY + xterm.js，多会话页签切换）+ 新建会话工具选择。

**约束：**
- 用户可见文案全部简体中文。
- 既有绑定方法不删不改形状（只新增；`NewSession` 保留并委托给新方法）。
- 每个任务完成即跑 `go test ./...` 或 `npm test`，全绿再进入下一任务。
- 主题继续跟随系统（`prefers-color-scheme`），本轮不引入手动主题开关。
- 新依赖：Go `github.com/aymanbagabas/go-pty`；npm `@xterm/xterm`、`@xterm/addon-fit`。

---

## Task 1: Go — 会话标题清洗

**Files:**
- Modify: `internal/providers/provider.go`（新增 `cleanTitle`）
- Create: `internal/providers/title.go` + `internal/providers/title_test.go`
- Modify: `internal/providers/claude.go`（跳过 `isMeta`，标题过 `cleanTitle`）+ `claude_test.go`
- Modify: `internal/providers/generic.go`（候选文本过 `cleanTitle`）+ `generic_test.go`
- Modify: `internal/discovery/index.go`（`indexVersion` 3 → 4）

**要点：**
1. `cleanTitle`：删成对包装块（`local-command-caveat`/`command-name`/`command-message`/`command-args`/`local-command-stdout`/`system-reminder`，含未闭合容错）→ 删剩余裸标签 → 折叠空白；空返回空串。
2. `cleanTitle`（或 `oneLine`）必须保持 `oneLine` 的 80 rune 截断语义不变。
3. claude 测试补：`isMeta` 记录被跳过、`<local-command-caveat>` 记录不产生标题、真实用户消息仍正常出标题。
4. generic 测试补：包装标签记录不再当标题。

## Task 2: Go — `internal/terminal` 终端会话管理

**Files:**
- Create: `internal/terminal/manager.go`（Backend/Handle 接口 + Manager + 环形缓冲）
- Create: `internal/terminal/backend_pty.go`（go-pty 实现；`.cmd/.bat` 经 `%COMSPEC% /c`）
- Create: `internal/terminal/manager_test.go`、`internal/terminal/backend_test.go`
- Modify: `go.mod` / `go.sum`

**要点：**
1. `Backend.Start(Spec, cols, rows) (Handle, error)`；真实实现包 `go-pty`：`pty.New()` → `Command(path, args...)` → 设 `Dir`/`Env` → `Start`。
2. 读协程：`Read` → 追加环形缓冲（256 KiB）→ `onData(id, chunk)`；`Read` 返回 EOF/错误 → `Wait()` 取退出码 → `onExit(id, code)`、`Status="exited"`。
3. `Open(key, spec, cols, rows)` 按 key 幂等复用；`List` 返回快照（按创建顺序）；`Scrollback` 关闭后仍可读。
4. `Close` 幂等：终止进程 + 关 pty；不重复触发 `onExit`。
5. 测试用桩 Backend（内存管道），断言：幂等复用、缓冲上限截断、退出码透传、Close 幂等、并发 Open 同 key 只起一个进程。
6. `Write` 对已退出会话返回错误但不 panic。

## Task 3: Go — desktop 绑定与事件

**Files:**
- Create: `internal/desktop/terminal.go`（绑定方法 + 事件转发 + 终端管理器装配）
- Modify: `internal/desktop/app.go`（`Options.Terminals`、`initRealDeps` 装配、退出时关闭全部终端）
- Create: `internal/desktop/terminal_test.go`
- Modify: `internal/launch/launch.go`（新增 `ForWorkspaceTool`：指定工具 + 空 toolID 回退首选）

**要点：**
1. 绑定：`OpenSessionTerminal(sessionID, cols, rows) (Info, error)`（key=`session:`+ID，经 `launch.ForSession`）、
   `OpenWorkspaceTerminal(wsID, toolID string, cols, rows int) (Info, error)`（key=随机，经 `ForWorkspaceTool`）、
   `WriteTerminal(id, data string)`（base64 解码）、`ResizeTerminal`、`CloseTerminal`、`ListTerminals`、`ScrollbackTerminal(id) (string, error)`（base64）。
2. 事件：`terminal:data{id,data}`、`terminal:exit{id,exitCode}`（回调在锁外执行）。
3. `NewSessionWithTool(wsID, toolID)` 外部窗口路径；`NewSession(wsID)` 委托它。
4. 退出清理：`App.Shutdown`（Wails `OnShutdown`）关闭所有终端，避免残留子进程。
5. 测试：桩 Backend + 桩 Emit，断言事件名/payload、base64 往返、未知 id 报错、`NewSessionWithTool` 透传工具。

## Task 4: 前端 — 依赖、扁平 token、无边框标题栏

**Files:**
- Modify: `frontend/package.json`（`@xterm/xterm`、`@xterm/addon-fit`）
- Modify: `main.go`（`Frameless: true`、`OnShutdown`）
- Modify: `frontend/src/style.css`（扁平 token）
- Create: `frontend/src/components/TitleBar.tsx` + `TitleBar.test.tsx`
- Modify: `frontend/src/App.tsx`（用 TitleBar 替换 header）+ `App.test.tsx`
- Modify: `frontend/src/components/ui/*`（圆角/阴影收敛）

**要点：**
1. token：`--radius-base: 4px`；组件统一 `rounded`。
2. TitleBar：拖拽区 + 页签（首页/工作区/设置贴最右）+ 自绘窗口控件（runtime API）+ 双击最大化；
   可点元素加 `--wails-draggable:no-drag`。
3. 测试：页签渲染/切页签/关页签/设置按钮在右侧（DOM 顺序）/窗口控件调用 runtime（`vi.mock` wailsjs/runtime）。

## Task 5: 前端 — 首页工作区卡片

**Files:**
- Modify: `frontend/src/pages/Home.tsx` + `Home` 相关测试（现无独立测试文件，如被 `App.test.tsx` 覆盖则补充 `pages/Home.test.tsx`）

**要点：**
1. 排序：`LastUsed` 倒序（零值排最后，再按名称）。
2. 卡片网格：名称 / git 徽标 / N 个会话 / 最后活动相对时间 / 工具徽标（≤3 + `+N`）。
3. 空态、骨架屏、重新扫描按钮行为不变。

## Task 6: 前端 — 会话列表重排与标题清洗显示

**Files:**
- Modify: `frontend/src/lib/title.ts`（新增 `displayTitle`）+ `title.test.ts`
- Modify: `frontend/src/components/SessionList.tsx` + `SessionList.test.tsx`

**要点：**
1. `displayTitle` 与 Go 侧 `cleanTitle` 规则同源（剥包装标签、折叠空白；空则回退原文）。
2. 行结构改两行（标题行 + 元信息行），元信息行 `min-w-0` 防挤压；
   主按钮「恢复」= 内嵌终端（已有终端 → 「切换」），次要图标按钮 = 外部终端。
3. SessionList 接受 `onOpenTerminal(session)` 回调（由 WorkspaceTab 注入），保留既有 `resumeSession` 供外部入口。

## Task 7: 前端 — 三栏拖动 + 中心区页签（预览 + 终端）

**Files:**
- Create: `frontend/src/components/ResizeHandle.tsx` + 测试
- Create: `frontend/src/components/TerminalView.tsx` + 测试
- Create: `frontend/src/lib/terminalRegistry.ts` + 测试
- Modify: `frontend/src/lib/api.ts`（终端绑定 + 事件订阅 + `NewSessionWithTool`）
- Modify: `frontend/src/state/store.ts`（`terminals` 切片 + `layout` 持久化）+ `store.test.ts`
- Modify: `frontend/src/pages/WorkspaceTab.tsx` + `WorkspaceTab.test.tsx`
- Modify: `frontend/src/App.tsx`（挂载时建立终端事件订阅）

**要点：**
1. `layout:{left,right}` 持久化，`ResizeHandle` clamp `[200,560]`。
2. 中心区页签：预览固定 + 每终端一页签；全部常挂载、非激活 `hidden`。
3. `TerminalView`：xterm + FitAddon，`ResizeObserver` → `ResizeTerminal`，`onData` → `WriteTerminal`，
   挂载先回放 `ScrollbackTerminal`，退出后显示提示与「重新打开」。
4. 全局订阅在 App 挂载时建立一次（`terminal:data` → 注册表分发；`terminal:exit` → store 改状态 + 通知）。
5. 测试：mock `@xterm/xterm`（jsdom 下 canvas/尺寸不可用）；断言注册/注销、事件分发、退出状态、拖动改宽度并 clamp。

## Task 8: 前端 — 新建会话工具选择

**Files:**
- Create: `frontend/src/components/ToolPicker.tsx` + 测试
- Modify: `frontend/src/pages/WorkspaceTab.tsx`

**要点：**
1. 工具列表来自 `GetTools()` 过滤 `Installed && BinPath`；默认选中 `ToolCounts` 最大者（并列按列表顺序）。
2. 「新建会话」→ 内嵌终端新页签；下拉次要项「在外部终端打开」→ `NewSessionWithTool`。
3. 工具未就绪（无可用项）时按钮禁用并给提示。

## Task 9: 验证与交付

**Files:**
- Modify: `docs/smoke-test-desktop.md`（追加本轮冒烟项）
- Modify: `README.md`（如涉及使用说明）

**要点：**
1. `go test ./...`、`go vet ./...`、`npm test`、`npm run build` 全绿。
2. `.\build.ps1 -Desktop` 出新 exe。
3. 冒烟清单追加：标题栏拖拽/双击最大化/三按钮、页签入栏与设置贴右、首页卡片时间倒序、
   会话标题无 XML 标签、三栏拖动持久化、中心区页签切换、恢复会话进终端可交互（跑一次 `claude` 与一次其他 CLI）、
   新建会话选工具生效、关闭终端页签进程结束、应用退出无残留子进程。
