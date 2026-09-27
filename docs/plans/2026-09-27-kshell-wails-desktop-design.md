# kshell 桌面版（Wails）设计

日期：2026-09-27
状态：设计定稿，待实施

## 背景与目标

kshell 现为 Bubble Tea TUI，存在三个问题：

1. 界面形态是终端，可发现性差（文件树/SSH 视图用户找不到），期望图形化界面
2. 恢复会话会独占终端（`tea.ExecProcess`），无法多开
3. 需要保留并强化 SSH、文件树等既有能力

决策（与用户逐条确认）：

- 技术栈：**Wails v2 + React/TS**（用户确认）。曾考虑 Tauri，但为复用全部 Go 核心需 sidecar + HTTP 桥，复杂度高；Wails 让 Go 编译进 exe、前端直接调用 Go 方法，仓库保持单语言。纯 Go GUI（Fyne/Walk）因控件生态弱、观感受限被否。
- 会话承载：**弹独立 Windows Terminal 窗口**，主窗口不阻塞，天然多开；内嵌 xterm.js+ConPTY 列为二期评估项。
- 会话列表默认按最近更新排序（用户确认）。

## 总体架构

```
┌─ kshell-desktop.exe (Wails v2) ─────────┐
│  React + TS + Vite 前端                  │
└──────────────┬──────────────────────────┘
               │ Wails 编译期绑定（类型化 JS 调 Go 方法）+ Events
┌──────────────┴──────────────────────────┐
│  internal/desktop（新增，薄绑定层）        │
│  ▼ 委托                                  │
│  discovery/providers/launcher/remote/    │
│  workspace/config（现有核心，零改动）      │
└─────────────────────────────────────────┘
```

- 现有 TUI（`cmd/kshell`）保留，两入口并存；TUI 与 GUI 共享 `~/.kshell` 的缓存/连接/providers.yaml
- 业务逻辑只在 Go 侧，前端纯展示

## 页面结构

- **首页**：工作区列表（会话数、git 标记）；点选工作区打开页签（可多开页签）
- **工作区页签**：左侧会话列表（标题/工具徽标/相对时间/消息数，按最近更新排序，全局过滤）；右侧「文件树 / SSH 连接」多页签；中间文件预览区
- 会话行操作：恢复 / 新建（可预置上下文篮文件）→ 弹 WT 窗口
- 文件树：懒加载、gitignore 过滤、预览、Space 加入上下文篮
- SSH：连接列表（五类扫描器 + 手动添加）、远程命令执行
- 设置：工具检测状态、providers.yaml 编辑、扫描目录配置
- 全局：深浅色主题跟随系统、托盘常驻

## 多开会话与窗口聚焦

- 弹窗：`wt.exe` 起独立窗口（非标签页），标题固定 `kshell · <会话标题>`，CLI 在会话工作区目录启动（复用 launcher 的 .cmd/.ps1 shim 解析）；未装 WT 回退 `powershell -NoExit` + `$host.UI.RawUI.WindowTitle`
- 主窗口永不阻塞，可连续多开
- 聚焦：维护「标题 → 进程信息」活跃窗口表；重复点击不弹新窗，用 Win32 `EnumWindows` 按标题匹配 HWND → `SetForegroundWindow` + `ShowWindow(SW_RESTORE)`。按标题而非 PID 匹配（wt.exe 启动器进程可能秒退，真窗口属于 WindowsTerminal 进程）
- 进程退出后轮询清理窗口表，前端收到 `window:closed` 事件还原行状态
- SSH 连接同机制（`ssh -o BatchMode=yes`，复用 remote 包连接参数）

## 数据流与 Wails 绑定

绑定方法（`internal/desktop`，薄封装只做校验与组装）：

| 方法 | 说明 |
|------|------|
| `ScanSessions()` / `GetWorkspaces()` | 扫描与聚合（复用 `discovery.Scan` + index.json 缓存） |
| `ResumeSession(id)` / `NewSession(wsID)` | 弹 WT 窗口 + 登记活跃窗口表 |
| `FocusWindow(id)` | 聚焦已开窗口 |
| `ListFiles(wsID, rel)` / `PreviewFile(path)` / `ToggleBasket(path)` | 文件树/预览/上下文篮 |
| `ListConnections()` / `OpenSSH(connID)` / `ExecRemote(connID, cmd)` | SSH（实操仍弹 WT） |
| `GetTools()` / `SaveProvidersYAML(content)` | 工具状态 / 设置编辑 |

事件推送（Wails Events）：`scan:progress` / `scan:done` / `window:closed`。

数据流约定：启动后台自动扫描，前端先渲染缓存再等 `scan:done` 刷新；所有绑定返回 `(result, error)`，前端统一 toast，窗口类错误（WT 缺失等）状态栏明示。

## 项目结构与构建

```
kshell/
├── cmd/kshell/          # TUI 入口（保留）
├── internal/desktop/    # 新增：Wails 绑定层 + 窗口管理器
├── frontend/            # 新增：React + TS + Vite（Wails 标准布局）
│   └── src/pages/       # Home(工作区) / WorkspaceTab(会话+文件+SSH)
│   └── src/components/  # SessionList / FileTree / Preview / SshPanel / Basket
├── main_wails.go        # Wails 入口（与 TUI 并存）
├── build.ps1            # 扩展 -Desktop 目标（调 wails build）
└── wails.json
```

- 依赖：Go、Node、WebView2（Win10/11 自带）；`wails dev` 热更新；`.\build.ps1 -Desktop` 产出 `dist/kshell-desktop.exe`

## 测试策略

- Go：窗口管理器以接口抽象 Win32 调用并打桩，标题匹配/存活清理/回退逻辑全单测；绑定层不重复测业务；现有约 90 个用例全量保留
- 前端：Vitest + Testing Library 测排序/过滤/篮子等交互逻辑
- 手工冒烟：弹窗聚焦（最小化恢复）、多开 3 会话、SSH 复用聚焦、WT 缺失回退、深色主题

## 实施顺序

桌面骨架（窗口 + 绑定层）→ 会话页 → 文件页 → SSH 页 → 设置页 → 窗口聚焦打磨。TUI 全程保留。

## 风险

- Windows Terminal 按标题聚焦依赖 `--title` 支持与 EnumWindows 匹配，需实机验证；不可行时回退按 PID + 进程名匹配
- ConPTY 内嵌终端（二期）Go 生态不成熟，仅评估不强求
- Wails v2 社区较小，遇到阻塞时降级方案：`go-webview2` 薄绑定自建窗口
