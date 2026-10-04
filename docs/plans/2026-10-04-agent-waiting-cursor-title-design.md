# Agent 等待态指示 + Cursor 中文会话标题 设计

日期：2026-10-04  
状态：已与用户确认  
分支：`feat/agent-waiting-cursor-title`  
范围：kshell 桌面端（前端活动态 + Cursor 标题枚举）；附 Cursor 规则（尽力而为）；TUI 不改。

## 背景与目标

### 问题 1：tab 活动图标误报「执行中」

中心区会话页签与左栏会话列表上，agent 已停（等用户继续输入，或本轮任务结束）时，图标仍显示「执行中」。ACP 聊天与内嵌终端均有此现象。

根因（已确认）：

- **聊天**：`Status=running` 乐观置位后，若未正确落到「非执行」展示，或完成态用对勾不够贴合「等你」语义。
- **终端**：进程存活即 `Status=running`，活动图标把进程存活直接当成 agent 执行中；Cursor 等无 ACP 一轮结束信号的工具会一直转圈。

用户期望：

- 真在跑时：**明显转圈**动画
- 权限确认：保留黄点「待用户确认」
- 已停、等用户：单独的「等待用户」图标（与权限区分）
- 不再用「运行完成」对勾表达本轮已停

### 问题 2：Cursor 会话列表标题变英文

`~/.cursor/chats/.../meta.json` 的 `title` 由 Cursor 产品自动生成（多为英文 Title Case）。kshell 主源优先读 meta 后，列表显示英文；此前 transcript 首条中文 `user_query` 作标题时更符合习惯。

规则无法可靠改写 Cursor 产品侧命名；kshell 展示策略必须改，并辅以 Cursor 规则尽力约束。

## 需求清单（已确认）

| # | 需求 | 选择 |
|---|---|---|
| 1 | 误报位置 | 中心区 tab + 左栏 SessionList；ACP 与内嵌终端都修 |
| 2 | 停下后图标 | 单独「等待用户」（与权限确认区分） |
| 3 | 权限 vs 等待 | 权限保留黄点；等待用另一种静态图标 |
| 4 | 运行中图标 | 明显 `animate-spin` 转圈 |
| 5 | 实现路径（活动） | 方案 1：前端活动态细化 + 终端输出静默启发 |
| 6 | Cursor 标题 | 展示优先 transcript 中文用户消息；并加 Cursor 规则（尽力而为） |

## 方案

### 活动状态

扩展前端派生模型（不扩展 Go `Status` 枚举）：

```ts
type AgentActivity = 'awaiting' | 'running' | 'waiting' | 'idle';
// 优先级：awaiting > running > waiting > idle
```

去掉独立 `completed`；「本轮已停 / 等你继续」归入 `waiting`。

| 状态 | 含义 | 聊天来源 | 终端来源 |
|---|---|---|---|
| `awaiting` | 待用户确认（权限） | `chatPermissions[id]` 非空 | 无 |
| `running` | 执行中 | `Status === 'running' \| 'starting'` | `Status === 'starting'`，或 busy（见下） |
| `waiting` | 等待用户 | `Status === 'ready'`（无权限） | 进程仍 `running` 但非 busy |
| `idle` | 无图标 | 其余（含 exited、未打开） | `exited` / 未打开 |

聊天「执行中 → 等待用户」依赖既有 `turn_done`/`error` 把 `Status` 置回 `ready`。实现阶段若复现「agent 已停但 Status 仍 running」，须在本分支一并修清（例如事件未送达、乐观态未回退），不能只改图标派生。

### 终端 busy（输出静默启发）

Store 新增（**不持久化**）：

```ts
terminalBusy: Record<string, true>; // key = terminal.ID
```

| 事件 | 动作 |
|---|---|
| 收到该终端的 `terminal:data` | 标 busy，重置静默定时器 |
| 前端 `WriteTerminal` 成功写出 | 标 busy，重置静默定时器 |
| 静默超过 **2.5s** 且 `Status` 仍为 `running` | 清 busy → 派生为 `waiting` |
| `markTerminalExited` / `removeTerminal` | 清 busy 与定时器 |
| `Status === 'starting'` | 视为 `running`（可不依赖 busy） |

定时器实现注意：按 terminal id 去重；组件卸载或 remove 时清理；测试可注入时钟或直接测 busy 置位/清除 API。

### `activityCompleted` 收敛

现有 `activityCompleted`（对勾「运行完成」）不再驱动对勾 UI。

- 聊天：`Status === 'ready'` 直接派生 `waiting`，不必再靠 `activityCompleted`
- **删除** `activityCompleted` 及切页签清除逻辑，避免与 `waiting` 双轨

切走中心页签时：不再需要「清完成对勾」；`waiting` 在会话仍打开且 ready/静默时持续显示，直到再次进入 `running`。

### Cursor 标题

在 `enrichCursorSession`：

1. 若能解析 transcript 首条用户消息标题（现有 `ParseSession` / `cleanTitle`），**优先覆盖** meta `title`
2. 仅当 transcript 不可用或解析为空时保留 meta 标题
3. `discovery.indexVersion`：6 → **7**（旧缓存英文标题整体失效）

前端 `displayTitle` 继续清洗包装标签；不做英译中；不改写磁盘 `meta.json`。

Cursor 规则（尽力而为）：

- 新增 `.cursor/rules/` 短规则：会话命名优先简洁中文主题短语，避免英文 Title Case
- `AGENTS.md` 补一句交叉说明（不保证产品侧 `meta.title` 遵守）

## UI

### `AgentActivityIcon`

| 状态 | 视觉 | 颜色 token | `title` / `aria-label` |
|---|---|---|---|
| `running` | 圆环 + `animate-spin`，约 12–14px，描边清晰 | `text-success` | 执行中 |
| `awaiting` | 实心圆 `animate-pulse` | `text-warning` | 待用户确认 |
| `waiting` | 静态空心圆环（不转、不闪；与 awaiting 实心脉冲区分） | `text-muted-foreground` | 等待用户 |
| `idle` | 不渲染 | — | — |

约束：内联 SVG；颜色非唯一信息（形态区分）。

### 挂载点

1. `WorkspaceTab` 中心页签（聊天 + 终端）
2. `SessionList` 左栏会话行

两处共用 `resolveAgentActivity` + `AgentActivityIcon`。

## 改动面

| 文件 / 区域 | 改动 |
|---|---|
| `frontend/src/state/agentActivity.ts` | 四态；去掉 completed；终端 busy 入参 |
| `frontend/src/components/AgentActivityIcon.tsx` | waiting 样式；强化 running 转圈；删对勾 |
| `frontend/src/state/store.ts` | `terminalBusy` + mark/clear；删除或停用 `activityCompleted` |
| `frontend/src/App.tsx`（或统一 data 入口） | `terminal:data` 时 mark busy + 重置定时 |
| 写出路径（`writeTerminal` 封装 / TerminalView） | 写入后 mark busy |
| `WorkspaceTab.tsx` / `SessionList.tsx` | 传入新派生输入；去掉 completed 相关 |
| 相关 `*.test.ts(x)` | 更新断言 |
| `internal/providers/cursor_chats.go` | transcript 标题优先覆盖 meta |
| `internal/providers/cursor_*_test.go` | 覆盖优先级 |
| `internal/discovery/index.go` | `indexVersion = 7` |
| `.cursor/rules/` + `AGENTS.md` | 中文会话命名约束 |
| `docs/smoke/feat-agent-waiting-cursor-title.md` | 冒烟增量 |

不改：`internal/chat` Status 语义、ACP 协议、TUI、文件树活动聚合、改写 Cursor meta 文件。

## 测试计划

1. **优先级**：permission → awaiting；busy/running → running；ready/静默 → waiting；其余 → idle
2. **聊天**：发送后 running；`turn_done` → waiting；有权限时 awaiting 优先
3. **终端**：data/write → running；2.5s 静默 → waiting；exited → idle
4. **图标**：running 有 spin +「执行中」；waiting 为「等待用户」且无 animate-spin
5. **Cursor 标题**：meta 英文 + transcript 中文 → 中文；无 transcript → meta
6. **indexVersion**：升至 7

验证命令：

```powershell
go test ./internal/providers ./internal/discovery -count=1
cd frontend; npm test; npm run build
```

合并收尾按仓库惯例：worktree 内验证全绿 → 合并 master → `.\build.ps1 -Desktop`（构建不杀旧实例）。

## 明确不做

- 改写 Cursor `meta.json` 或机器翻译英文标题
- 扩展 Go `Status` / 后端 activity 事件总线
- TUI 同步
- 文件树 / 工作区首页活动聚合
- 终端静默阈值做成用户可配置（本轮固定 2.5s）

## 成功标准

- ACP 与内嵌终端：agent 停下后 tab/列表显示「等待用户」，不再假「执行中」
- 真正执行中有明显转圈
- 权限黄点保留且优先于 running / waiting
- Cursor 会话列表优先显示中文用户消息标题；仓库内 Cursor 规则已就位（产品侧自动命名不保证）

## 风险与缓解

| 风险 | 缓解 |
|---|---|
| 终端静默阈值误判（短停顿仍转圈 / 长输出间隙误 waiting） | 2.5s 折中；后续可调；busy 在任何新输出立即恢复 running |
| Cursor 规则无效 | 展示层 transcript 优先已覆盖主路径 |
| 删除 `activityCompleted` 牵动测试面 | 按调用点一次性替换为 waiting 派生，单测同步改 |
