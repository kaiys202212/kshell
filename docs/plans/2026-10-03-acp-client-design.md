# Claude Code 交互模式改 ACP：通用 ACP 客户端设计

- 日期：2026-10-03
- 分支：`feat/acp-chat`
- 状态：设计已定稿，待写实现计划

## 1. 背景与目标

现状：桌面端中心区通过 ConPTY 内嵌 `claude` 的 TUI（xterm.js 渲染）；TUI 版通过 `tea.ExecProcess` 让出终端。一期设计明确把「内嵌对话（ACP）」列为不做，本期启动。

ACP（Agent Client Protocol）是编辑器与 coding agent 之间的标准协议：本地 agent 作为编辑器子进程，通过 **JSON-RPC 2.0 over stdio** 通信（换行分隔、消息不得含内嵌换行），协议版本 `protocolVersion: 1`。Claude 的 ACP 能力由适配器提供。

**本期目标**：把 kshell 桌面端做成**通用 ACP 客户端框架**，Claude 是第一个接入的 agent；后续 Codex/Gemini/OpenCode 等只需实现一个接口或改配置即可复用同一套聊天 UI。

**用户已确认的决策**：

1. 目标是「统一 ACP 客户端框架」，不只 Claude。
2. 先落桌面端（Wails + React）；TUI 版保持现状，不在本期范围。
3. 复用现有「发现 → 选择 → 恢复」流程：已扫描到的 Claude 历史会话用 ACP `session/load` 恢复（`sessionId` 用 jsonl 的 sessionId）。
4. 适配器获取：检测优先，`npx` 兜底。
5. 第一版做到「对话核心」：流式消息 + Markdown + 工具调用展示 + 权限确认 + 取消；不实现 fs/terminal 客户端回调。
6. UI 呈现：聊天作为**新的页签类型**，与现有 xterm 终端页签并存；适配器不可用时**自动回退**到现有 TUI。

## 2. 非目标（v1 明确不做）

- `fs/read_text_file`、`fs/write_text_file`、`terminal/*` 客户端回调（不声明该 capability，让 agent 用它自带的工具）。
- `elicitation`、session config options、session modes 切换 UI、slash commands UI。
- diff 渲染（tool content 里的 diff 类型先用纯文本降级展示）、plan 富交互（先简单列表）。
- ACP `session/list`、`session/delete`、`session/close`、`session/resume`。
- MCP server 配置 UI（`mcpServers` 恒传空数组）。
- TUI（Bubbletea）侧的 ACP 交互。
- 远程 / SSH 场景的 ACP。
- 多会话进程池化（v1：一个聊天页签 = 一个 agent 进程）。

## 3. 总体架构与分层

```
React ChatView ──store(chats/chatTimeline)── App 事件订阅
        │ Wails bindings (OpenSession/OpenWorkspace/SendChatPrompt/CancelChat/RespondChatPermission)
        ▼
internal/desktop/chat.go       绑定层：参数组装、事件转发、chat/terminal 选择与回退
        ▼
internal/chat/                 编排层：进程生命周期、会话状态、事件回调（Backend 可打桩）
        ▼
internal/acp/                  纯协议层：ndjson JSON-RPC、ACP 类型、反向请求分发
        ▼
claude-agent-acp 子进程（stdio）
```

- **`internal/acp`**（无 UI / 业务依赖）：换行分隔 JSON-RPC 2.0 编解码；请求 id 管理；通知与反向请求分发；ACP 类型（`initialize`、`session/new`、`session/load`、`session/prompt`、`session/cancel`、`session/update` 各变体、`session/request_permission`、`ContentBlock`、`ToolCall`、`StopReason`）。协议版本固定 `1`，对未知 `sessionUpdate` 变体容错忽略。
- **`internal/chat`**：编排一个 ACP agent 进程：`initialize` 握手 → 按需 `session/new` 或 `session/load`；维护前端要的时间线 / 待确认权限；对外回调 `onUpdate / onPermission / onExit`；方法与 `terminal.Manager` 同构（`Open / List / Close / CloseAll`）。
- **`internal/desktop/chat.go`**：仿 `terminal.go` 的绑定方法与 `chat:*` 事件；`App` 启动装配、退出 `CloseAll`。
- **前端**：新增 `ChatView`，页签新增 `chat` 类型，与现有 `terminal` 页签并存。

**进程模型（v1）**：一个打开的聊天页签 = 一个独立 ACP agent 进程（key 幂等，同 `terminal.Manager` 的 `session:<id>` / `new:<seq>`）。理由：与现有终端页签生命周期一致、崩溃隔离好、实现简单。代价是多页签各起一个 Node 进程（较重）。协议本身支持「一进程托管多会话」，`internal/chat` 按「一进程多会话」的方式设计接口，**进程池化留作后续优化**。

**能力声明（v1）**：`initialize` 只报 `clientCapabilities: {}`，不声明 `fs` / `terminal` / `elicitation`。

## 4. ACP 协议要点（实现依据）

- 传输：stdio，消息为单行 JSON（`\n` 分隔），agent 只从 stdout 发协议消息，日志走 stderr。
- `initialize`：客户端报 `protocolVersion:1`、`clientCapabilities:{}`、`clientInfo{name,title,version}`；agent 返回 `protocolVersion`、`agentCapabilities`（含 `loadSession`、`promptCapabilities`）、`agentInfo`、`authMethods`。
- `session/new {cwd, mcpServers:[]}` → `{sessionId}`。
- `session/load {sessionId, cwd, mcpServers:[]}`：仅当 `agentCapabilities.loadSession` 为真；agent 会**先重放**整段历史（`session/update`），再返回。
- `session/prompt {sessionId, prompt:[{type:"text",text}]}` → 整轮结束返回 `{stopReason}`（`end_turn|max_tokens|max_turn_requests|refusal|cancelled`）。
- `session/cancel {sessionId}`（通知）：客户端据此把本轮的未完成 tool call 标记 cancelled，并对所有 pending 权限回 `cancelled`。
- `session/request_permission {sessionId, toolCall, options}` → 客户端回 `{outcome:{outcome:"selected",optionId}}` 或 `{outcome:{outcome:"cancelled"}}`。
- `session/update` 变体：`user_message_chunk`、`agent_message_chunk`、`agent_thought_chunk`、`tool_call`、`tool_call_update`、`plan`、`available_commands_update`、`current_mode_update`、`config_option_update`、`session_info_update`、`usage_update`。

适配器：`@agentclientprotocol/claude-agent-acp`（bin `claude-agent-acp`，需 Node ≥ 22）。旧名 `@zed-industries/claude-code-acp`、`@zed-industries/claude-agent-acp` 均已弃用，仅作兼容探测。

## 5. Provider / discovery / launch 集成

### 5.1 providers 新增可选接口

不改现有 `Provider` 接口（避免破坏所有实现）。

```go
// ACPAdapter 声明某工具的 ACP 适配器来源。
type ACPAdapter struct {
    BinNames   []string // 优先在 PATH 探测的可执行名
    NPMPackage string   // 探测不到时 npx 兜底的包名
    ExtraArgs  []string // 追加到 ACP 进程的参数（可选）
}

// ACPProvider 可选接口：实现即表示该工具可用 ACP 交互。
type ACPProvider interface {
    ACPAdapter() ACPAdapter
}
```

Claude 实现：`BinNames:["claude-agent-acp", "claude-code-acp"]`，`NPMPackage:"@agentclientprotocol/claude-agent-acp"`。

### 5.2 providers 新增探测

```go
type ACPDetection struct {
    Available bool   // 可用
    Source    string // "path" | "npx"
    BinPath   string // path 命中时的可执行路径
    Package   string // npx 兜底时的包名
}
func DetectACP(a ACPAdapter) ACPDetection
```

逻辑：依次在 PATH 探测 `BinNames`；全未命中且 `NPMPackage` 非空时探测 `npx`（Windows 同时试 `npx.cmd`）→ `Source:"npx"`；都没有则 `Available:false`。

### 5.3 discovery 暴露可用性

`discovery.Tool` 增加字段 `ACP *ACPDetection`（provider 未实现 `ACPProvider` 时为 `nil`）。`DetectAll` 对实现了 `ACPProvider` 的 provider 调用 `DetectACP` 填充。前端据 `Tool.ACP.Available` 决定入口。

### 5.4 launch 产出 ACP 启动描述

新增 `launch.ForSessionACP(ps, tools, s)` 与 `launch.ForWorkspaceACP(ps, tools, ws, toolID) (providers.Launch, error)`：

- `Source:"path"` → `Path=BinPath`；
- `Source:"npx"` → `Path="npx"`，`Args=["-y", Package]`；
- `Dir` 设为工作区路径（真正权威的 cwd 以 ACP `session/new|load` 参数为准）。

随后仍走现有 `launcher.Build(l)` 得到 `Spec`，复用 Windows shim 解析。**验证项**：`npx` 在 Windows 上是 `npx.cmd`，需确认 `launcher.Resolve` 对 `.cmd` 的处理正确。

## 6. 编排层 internal/chat

### 6.1 对外模型

```go
type Spec struct{ Path string; Args []string; Dir string; Env []string }

const (
    KindSession = "session"; KindNew = "new"
    StatusStarting = "starting"; StatusReady = "ready"
    StatusRunning  = "running"; StatusExited = "exited"
)

type Info struct {
    ID, Kind, SessionID, Workspace, Title, ToolID, Status string
    ExitCode int; Error string
}

// 归一化时间线事件（带单调 Seq，前端据此去重/排序）
type Update struct {
    Seq        int64
    Type       string // user | assistant | thought | tool | plan | turn_done | error
    MessageID  string // 流式消息合并键（assistant/thought/user）
    Text       string // user/assistant/thought 的增量文本
    ToolCallID string
    Tool       *ToolCall // tool：完整快照（按 toolCallId upsert）
    Plan       []PlanEntry
    StopReason string // turn_done 时的停止原因
}

type PermissionRequest struct {
    RequestID string; SessionID string
    ToolCall  ToolCall; Options []PermissionOption
}
```

### 6.2 Manager 与可打桩 Backend

```go
type Backend interface{ Start(spec Spec, h acp.Handler) (Conn, error) }

// Conn 是 ACP 连接的高层封装；真实实现是 internal/acp.Agent，测试注入假实现。
type Conn interface {
    Initialize(ctx) (acp.AgentCapabilities, error)
    NewSession(ctx, cwd string) (sessionID string, err error)
    LoadSession(ctx, sessionID, cwd string) error
    Prompt(ctx, sessionID, text string) (stopReason string, err error) // 阻塞到整轮结束
    Cancel(sessionID string) error
    Close() error
}

func NewManager(b Backend,
    onUpdate func(id string, u Update),
    onPermission func(id string, r PermissionRequest),
    onExit func(id string, code int, errMsg string)) *Manager

// Open: 起进程 → initialize → session/new 或 session/load（sessionID!="" 则 load）。
func (m *Manager) Open(key string, info Info, spec Spec, sessionID string) (Info, error)
func (m *Manager) Prompt(id, text string) error
func (m *Manager) Cancel(id string) error
func (m *Manager) RespondPermission(id, requestID, optionID string) error
func (m *Manager) CancelPermission(id, requestID string) error
func (m *Manager) Close(id string) error
func (m *Manager) CloseAll()
func (m *Manager) List() []Info
func (m *Manager) History(id string) []Update // 含已退出会话，供重载页签恢复
```

要点：

- `Open` 同步到「会话就绪」；`session/load` 的历史重放由 `internal/acp` 经 handler 转成 `Update`：存入 History 并回调 `onUpdate`（前端用 Seq 去重）。
- `Prompt` 立即返回发送结果，整轮结果经 `Update{Type:"turn_done", StopReason}` 异步回传；`Status` 在 running/ready 间切换。
- 反向请求（`session/request_permission`）在**独立 goroutine** 处理，绝不阻塞读循环；等前端回应或取消/关闭（此时按协议回 `cancelled`）。

## 7. 桌面绑定与事件

`internal/desktop/chat.go`（仿 `terminal.go`）：

- 方法：`OpenSession(sessionID)`、`OpenWorkspace(wsID, toolID)`、`SendChatPrompt(id,text)`、`CancelChat(id)`、`RespondChatPermission(id,requestID,optionID)`、`CancelChatPermission(id,requestID)`、`CloseChat(id)`、`ListChats()`、`ChatHistory(id)`。
- 事件：`chat:update {id, update}`、`chat:permission {id, request}`、`chat:exit {id, exitCode, error}`。

**统一打开入口（自动回退的唯一决策点，放 Go 侧）**：

```go
type OpenedSession struct {
    Kind     string         // "chat" | "terminal"
    Chat     *chat.Info
    Terminal *terminal.Info
    Fallback string         // 非空表示因故从 chat 回退到 terminal，值为原因
}
func (a *App) OpenSession(sessionID string) (OpenedSession, error)      // 恢复历史会话
func (a *App) OpenWorkspace(wsID, toolID string) (OpenedSession, error) // 新建会话
```

Go 侧解析首选工具 + ACP 可用性：可用则起 ACP 聊天（`session/load` | `session/new`），否则回退现有终端。前端拿到结果按 `Kind` 入对应 store 切片。保留现有 `OpenSessionTerminal` / `OpenWorkspaceTerminal`（其他路径仍用）。

`App` 装配：`initRealDeps` 用 `newChatManager(a)`（回调经 `a.Emit` 转发），退出时 `Chats.CloseAll()`；与 `Terminals` 并列。

## 8. 前端 UI 与状态

**依赖**：新增 `react-markdown` + `remark-gfm`（唯一新增前端依赖）。

**`lib/api.ts` 扩展**：`ChatInfo` / `ChatUpdate` / `ToolCall` / `PermissionRequest` 类型；绑定封装与事件订阅 `onChatUpdate/onChatPermission/onChatExit`；`ToolInfo` 增加 `ACP?: { Available: boolean; Source: string }`。

**store 扩展**：

- `chats: ChatInfo[]` + `upsertChat/removeChat/markChatExited/setChats`。
- `chatItems: Record<string, TimelineItem[]>`、`chatSeq: Record<string, number>`、`applyChatUpdate(id, update)`：按 `Seq` 去重；`assistant/thought/user` 按 `MessageID` 累积文本，`tool` 按 `ToolCallID` upsert，`plan` upsert，`error/turn_done` 追加或改状态。
- `chatPermissions: Record<string, PermissionRequest | null>` + set/clear。

**`ChatView.tsx`**（props `{ chat, active }`，仿 `TerminalView` 常挂载）：

- 时间线：用户气泡、assistant（Markdown 流式）、thought（可折叠）、工具调用卡片（标题 / kind / 状态 / 内容）、plan、错误 / 退出横幅；
- 底部输入区：textarea + 发送（Enter 发送 / Shift+Enter 换行）；运行中显示「停止」→ `cancelChat`；非 `ready` 时禁用；
- 权限弹窗：`chatPermissions[id]` 存在时展示选项按钮 → `respondChatPermission`；取消 → `cancelChatPermission`；
- 新条目自动滚到底部（用户上滚时不打扰）。

**`WorkspaceTab.tsx` 集成**：

- `centerTab` 可指向 chat id；页签条在终端页签旁渲染聊天页签（工具色点 + 标题 + 运行中指示 + `×`）；内容区为每个 chat 渲染 `<ChatView>`（非激活 `hidden`）。
- `startSession` / `openTerminalForSession` 改走统一的 `openWorkspace` / `openSession`，按返回 `Kind` 入 chat 或 terminal 切片并 `setCenterTab(id)`。
- 关闭聊天页签：`removeChat` + `closeChat`。

**`App.tsx` 集成**：

- 挂载时 `listChats()` 重建镜像（同 `listTerminals`），再对每个仍存在的 chat 调 `chatHistory(id)` 取回 History 并重放进 store（Go 侧进程仍在跑时恢复页签与时间线）。
- 全局订阅 `chat:update` / `chat:permission` / `chat:exit` 一次，路由进 store；`chat:exit` 更新镜像并 toast。
- 关闭工作区页签时连带 `closeChat` 其所属聊天。

## 9. 关键流程

**恢复历史会话**（`SessionList` → 恢复）：`OpenSession(id)` → Go 找 session/provider → 若该工具 ACP 可用：`chat.Open(key="session:"+id, KindSession, spec=ForSessionACP, sessionID=s.ID)` → 起进程 → `initialize` → 校验 `loadSession` → `session/load{sessionId,cwd}`（历史经 handler 转 `Update`）；否则回退现有终端。

**新建会话**（`NewSessionMenu` 选工具）：`OpenWorkspace(wsID, toolID)` → 解析首选工具 → ACP 可用则 `chat.Open(key="new:"+seq, KindNew, sessionID="")` → `session/new{cwd=ws.Path}`（`Info.SessionID` 记录新 ID）；否则回退终端。沿用现有「延迟 3s 重扫」把新会话带进列表。

## 10. 错误处理与边界

- 适配器不可用 → 探测期 `Tool.ACP.Available=false` → 静默回退 TUI。
- ACP 启动失败（`initialize` / 协议版本）、`loadSession=false`、`session/load` 报错 → **自动回退终端**，返回结构带 `Fallback` 原因，前端 toast 说明。
- 进程中途崩溃 → 读循环结束 → `chat:exit`（`Status=exited` + stderr 尾部），前端提示可「重新打开」。
- 取消 / 关闭时有 pending 权限 → 先按协议回全部 `cancelled`，运行中再发 `session/cancel`。
- 未知 `sessionUpdate` 变体忽略；未知反向方法回 `method-not-found`。
- assistant 分片缺 `messageId` → 用运行内自增合成 key 归并同一轮连续分片。
- History 设上限（最近 N 条 + 单条文本上限，超出截断标记），避免长会话内存膨胀（参照 terminal scrollback）。
- Windows：`npx.cmd` / adapter 若为 `.cmd`，走 `launcher.Resolve`（验证项）。
- 只读 stdout 的 JSON，stderr 收集用于错误展示。

## 11. 测试策略

**Go 单测**：

- `internal/acp`：ndjson 分片读、超长行、非法 JSON 容错、id 匹配、通知分发、反向请求 goroutine 分发、outcome 回报。
- `internal/chat`：Backend/Conn 打桩：new/load、prompt→turn_done、cancel、权限请求→响应映射、关闭时 pending 回 cancelled、History/Seq、进程退出→onExit（参照 `terminal/manager_test.go`）。
- providers/discovery/launch：`DetectACP` 三种结果、`ForSessionACP`/`ForWorkspaceACP` 参数断言、`Tool.ACP` 填充。
- desktop：chat/terminal 选择与回退（打桩）。

**前端 vitest**：`applyChatUpdate` reducer（去重 / 累积 / upsert / 状态）；`ChatView`（渲染、发送 / 停止、权限弹窗、退出横幅、Markdown）；事件路由；chat/terminal 页签并存与关闭；绑定不可用兜底。

**冒烟（真机）**：Windows 上 Claude 新会话 + 恢复会话 + 权限确认 + 取消 + 退出，补 `docs/smoke-test*.md`。

## 12. 风险与验证点

1. **`session/load` 的 sessionId 是否等于 Claude jsonl 文件名（UUID）**——必须真机验证；不成立则「恢复历史」另择方案。
2. adapter 认证：复用本机 `claude` 登录态；需 `authenticate` 时 v1 仅提示错误。
3. Node ≥ 22 与 npx 冷启动延迟 → 靠「检测优先」规避。
4. 无官方 Go SDK → 以 v1 为准，容忍未知字段与变体。
5. Windows `.cmd` shim 解析。

## 13. 里程碑

- **M1** `internal/acp` 协议层 + 单测。
- **M2** `internal/chat` 编排层 + 假 Conn 单测。
- **M3** providers / discovery / launch 的 ACP 探测与启动 + 单测。
- **M4** desktop 绑定 / 事件 + `OpenSession` / `OpenWorkspace` 选择回退 + 单测。
- **M5** 前端 store / api / ChatView / 集成 + vitest。
- **M6** 冒烟：Windows 真机 Claude 新会话 + 恢复会话 + 权限确认 + 取消 + 退出；补 `docs/smoke-test*.md`。

## 14. 待实测确认

- `session/load` 的 sessionId 与 Claude jsonl 文件名是否一致（风险 1）。
- `claude-agent-acp` 的实际 `agentCapabilities`（`loadSession`、`promptCapabilities`）与是否需要 `authenticate`。
- Windows 上 `npx.cmd` 与 adapter 的 shim 解析是否被 `launcher.Resolve` 正确覆盖。
