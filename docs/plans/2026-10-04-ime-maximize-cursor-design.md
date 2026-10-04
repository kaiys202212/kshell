# 设计：优化轮（IME 锚定 / 默认最大化 / Cursor provider）

日期：2026-10-04　分支：`feat/ime-maximize-cursor`

## 背景与目标

用户报告三项优化（已确认场景）：

1. **IME 浮窗挤压**：终端页跑 agent TUI（Claude Code 等）打中文时，输入法候选浮窗出现在屏幕最右缘，**把整个窗口都移动了**（用户确认是"窗口被移动"）。
2. **默认最大化**：桌面端启动时默认最大化。
3. **Cursor 接入**：把 cursor-agent CLI 纳入发现+恢复（仅会话列表与 resume，不做聊天页 ACP）。

## 1. IME 浮窗问题

### 根因

- xterm.js 把隐藏的 `.xterm-helper-textarea` 锚在 buffer 光标处（`_syncTextArea`），Windows IME 候选窗跟随该 textarea 的屏幕位置。
- agent TUI 等待输入时常把 buffer 光标 park 在行尾/输出区（最右侧），候选窗贴到屏幕右缘。
- 候选窗贴屏幕边缘时触发原生层的窗口移动行为（WebView2/OS，用户实测窗口被挪动）。
- xterm 上游已知情：issue #5734、修复 PR #5759（compositionstart 时重同步 textarea 位置，milestone 7.0 未发布）。本仓库用 @xterm/xterm 5.5，无此修复。

### 方案

在 `TerminalView.tsx` 应用层移植 #5759 思路并加强：

- 监听 textarea 的 `compositionstart`，把 textarea 位置重同步到 buffer 光标像素坐标，并**横向钳制**在终端视口内（不超过约 60% 宽度处），保证候选窗不贴屏幕右缘。
- 坐标计算抽成纯函数 `frontend/src/lib/imeAnchor.ts`（可单测）：输入 cols/rows/cursorX/cursorY/viewportY/视口宽高，输出 {left, top}。
- 不改 xterm 内部，不破坏选区/右键菜单。

### 风险

- 候选窗中途是否持续跟随 textarea 位置：Windows IME 每次组合更新都会按焦点元素位置重定位，成立。
- 若钳制后窗口仍被移动（原生层另有触发路径），再迭代排查 WebView2 层——先落地本修复让用户实测。

## 2. 默认最大化

`main.go` Wails options 增加 `WindowStartState: options.Maximised`。frameless 窗口已保留系统最大化动画（见 main.go 注释），直接生效。不做"记住上次窗口状态"（超出本次需求）。

## 3. Cursor provider（仅发现+恢复）

### 已实测的本机事实（2026-10-04）

- `cursor-agent` 安装于 `~/AppData/Local/cursor-agent/cursor-agent.cmd`；`--resume [chatId]` 支持。
- 会话 transcript：`~/.cursor/projects/<工作区slug>/agent-transcripts/<uuid>/<uuid>.jsonl`。
- JSONL 每行 `{"role":"user|assistant","message":{"content":[{type,text}|...]}}`，**无 sessionId/timestamp/cwd 字段**。首条 user 消息文本带 `<user_query>…</user_query>` 包装。
- slug 规则：路径分隔符与盘符冒号替换为 `-`（`d:\data\x` → `d-data-x`），与 Claude（冒号→`-`）不同：**冒号是删除还是替换需以样例为准**（样例 `d-data-...` 表明冒号删除、分隔符→`-`）。

### 设计

- `internal/providers/cursor.go`，空结构体 `Cursor{}`，加入 `builtins.go`。
- `DetectSpec`：BinName `cursor-agent`，ConfigDirs `~/.cursor`。
- `SessionRoots`：`~/.cursor/projects`；`SessionFilePattern: *.jsonl`。
- `PathMatcher`：rel 恰好 4 段且 `parts[1]=="agent-transcripts"`、`.jsonl` 结尾。
- `ParseSession`：
  - ID：取路径中 `<uuid>` 段（记录里没有 sessionId）。
  - 标题：首条 role=user 消息文本，剥 `<user_query>` 包装后 oneLine(80)；无则跳过（errCursorEmpty）。
  - 时间：记录无时间戳，用文件 mtime（os.Stat，path 已传入）同时作 Created/Updated。
  - Workspace：slug 逆解码（首段单字符视为盘符），**有损**（路径含 `-` 会误还原）；作为展示与 resume Dir 的尽力而为，规格明记此限制。
  - Messages：CountLines。
- `NewSessionCmd`：`cursor-agent` 于工作区启动；`ResumeCmd`：`--resume <id>` 于解码出的工作区启动。
- 不实现 Themer / ACPAdapter / SessionEnumerator。

### 已知限制

- slug 逆解码有损：含连字符的目录名会被拆错，工作区显示与 resume 启动目录可能偏差。resume 是否强依赖启动目录未实测，落地后由用户用真实会话验证。
- 无时间戳导致排序粒度只有文件 mtime（可接受，Claude 侧也有近似场景）。
