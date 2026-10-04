# Agent 活动状态指示（页签 + 会话列表）设计

日期：2026-10-04  
状态：已与用户确认  
范围：kshell 桌面端前端（React）；不改 Go / ACP / 权限协议；TUI 不改。

## 背景与目标

用户需要在**中心区会话页签**和**左栏会话列表**一眼看出 agent 当前活动：

- 执行中
- 待用户确认（权限请求）
- 运行完成（保持到再次发送或切走）

现状：

- 页签仅在 `ChatInfo.Status === 'running'` 时显示绿色脉冲点
- 会话列表用 ✓ 表示「已打开且未退出」，与真实活动无关
- `chatPermissions` 已有待确认数据，但未反映到页签/列表
- 「一轮结束」后 Status 回到 `ready`，没有「已完成」保持态

目标：两处 UI 共用同一套活动状态与图标；覆盖 ACP 聊天与内嵌终端。

## 需求清单（已确认）

| # | 需求 | 选择 |
|---|---|---|
| 1 | 展示位置 | 中心页签 + 左栏会话列表（`SessionList`）；文件树不做 |
| 2 | 「运行完成」 | 保持标记，直到再次发送或切走该会话 |
| 3 | 覆盖范围 | ACP 聊天 + 内嵌终端（终端无「待确认」） |
| 4 | 实现路径 | 前端派生统一活动状态，不扩展后端 `Status` |

## 方案

**前端派生统一活动状态**（`running | awaiting | completed | idle`）：

- 从现有 `chats` / `terminals` / `chatPermissions` + 新增的 `activityCompleted` 计算
- 共享 `resolveAgentActivity` + `AgentActivityIcon`
- 页签与会话列表只消费派生结果，不各自拼条件

不采用：后端扩展 Status、独立 activity 事件总线（对当前需求过重）。

## 状态模型

### 枚举

```ts
type AgentActivity = 'awaiting' | 'running' | 'completed' | 'idle';
```

| 状态 | 含义 | 聊天来源 | 终端来源 |
|---|---|---|---|
| `awaiting` | 待用户确认 | `chatPermissions[id]` 非空 | 无 |
| `running` | 执行中 | `Status === 'running' \| 'starting'` | `Status === 'running' \| 'starting'` |
| `completed` | 本轮已完成（保持） | `activityCompleted[id]` | `activityCompleted[id]` |
| `idle` | 无提示 | 其余 | 其余 |

### 优先级

同一打开实例只显示一个图标：

`awaiting` > `running` > `completed` > `idle`

有 permission 时即使仍为 `running`，也显示 `awaiting`。

### `activityCompleted` 生命周期

Store 新增（**不持久化**，刷新即清空）：

```ts
activityCompleted: Record<string, true>; // key = chat.ID 或 terminal.ID
markActivityCompleted(id: string): void;
clearActivityCompleted(id: string): void;
```

| 事件 | 动作 |
|---|---|
| 聊天 `applyChat` 收到 `turn_done`（且非 exited） | `markActivityCompleted(chatId)` |
| 终端 `markTerminalExited` | `markActivityCompleted(termId)` |
| 聊天发送前（乐观置 `running` 之前） | `clearActivityCompleted(chatId)` |
| 终端再次变为 `running`（同 ID 重开/复活） | `clearActivityCompleted(termId)` |
| 中心区 `centerTab` 从某 chat/terminal ID 切走 | `clearActivityCompleted(prevId)` |
| `removeChat` / `removeTerminal` | 去掉对应 completed 键 |
| 聊天 `error` → `ready` | **不**标 completed（保持 idle） |

说明：`centerTab` 目前是 `WorkspaceTab` 本地 state；切走清除在该组件封装 `setCenterTab` 时调用 store。预览页签 `PREVIEW_TAB` 不参与 completed。

## UI

### `AgentActivityIcon`

路径：`frontend/src/components/AgentActivityIcon.tsx`  
输入：`activity: AgentActivity`；`idle` 不渲染。

| 状态 | 视觉 | 颜色 token | `title` / `aria-label` |
|---|---|---|---|
| `running` | 小圆环 `animate-spin`（约 10–12px） | `text-success` | 执行中 |
| `awaiting` | 实心圆 `animate-pulse` | `text-warning` | 待用户确认 |
| `completed` | 对勾（静态） | `text-muted-foreground` | 运行完成 |

约束：内联 SVG，不引入图标库；动效仅用现有 Tailwind。颜色不是唯一信息（形态区分：转圈 / 脉冲圆 / 对勾）。

### 挂载点

1. **中心页签**（`WorkspaceTab`）：聊天与终端标题左侧；替换现有仅 `running` 绿点。
2. **左栏会话列表**（`SessionList`）：标题行右侧；替换现有「已打开 = ✓」。

### 会话列表映射

左栏行对应扫描到的 `Session`（按 `Session.ID`）。活动状态取自**已打开**的 chat/terminal：

- 在 `chats` / `terminals` 中找 `SessionID === session.ID` 的实例；优先非 `exited`，否则仍可取已退出实例（以便终端 `exited` → `completed` 能显示）
- 用该实例的 `Status` + `chatPermissions[实例.ID]` + `activityCompleted[实例.ID]` 调用 `resolveAgentActivity`
- 未打开 → `idle`（无图标）


「恢复 / 切换」按钮文案逻辑可保留「是否已打开」判断，与活动图标解耦。

## 派生 helper

路径：`frontend/src/state/agentActivity.ts`

```ts
export function resolveAgentActivity(input: {
  status: string;           // starting | ready | running | exited
  hasPermission: boolean;
  completed: boolean;
}): AgentActivity
```

纯函数、无 store 依赖，便于单测。

可选薄封装（同文件或组件内）：

```ts
function activityForOpened(
  info: { ID: string; Status: string },
  kind: 'chat' | 'terminal',
  storeSlice: { chatPermissions; activityCompleted },
): AgentActivity
```

## 改动面

| 文件 | 改动 |
|---|---|
| `frontend/src/state/store.ts` | `activityCompleted` + mark/clear；`applyChat`/`markTerminalExited`/`remove*` 联动；发送路径清除 |
| `frontend/src/state/agentActivity.ts`（新） | `resolveAgentActivity` |
| `frontend/src/state/agentActivity.test.ts`（新） | 优先级与边界 |
| `frontend/src/components/AgentActivityIcon.tsx`（新） | 三态图标 |
| `frontend/src/components/AgentActivityIcon.test.tsx`（新） | 渲染 / aria |
| `frontend/src/pages/WorkspaceTab.tsx` | 页签图标；切走清除 completed |
| `frontend/src/components/ChatView.tsx` | 发送前 `clearActivityCompleted` |
| `frontend/src/components/SessionList.tsx` | 活动图标替换 ✓ |
| 既有集成测试 | 页签 / 列表断言更新 |

不改：`internal/chat`、`internal/desktop`、文件树、工作区顶栏项目页签、TUI。

## 测试计划

1. **优先级**：`hasPermission=true` + `status=running` → `awaiting`
2. **完成置位**：`turn_done` → `completed`；`error` → 非 completed
3. **清除**：发送前清除；`setCenterTab` 切走清除；`removeChat` 去掉键
4. **终端**：`exited` → completed；`running` → running；无 awaiting
5. **SessionList**：打开且 running → 转圈；仅打开 ready 无图标（修正旧 ✓）
6. **页签**：三态图标出现；与同会话列表一致

验证命令（实现阶段）：

```powershell
cd frontend; npm test; npm run build
```

Go 侧无改动时不必为该特性单独跑全量 Go 测试；若同 PR 无其它 Go 改动，前端全绿即可。合并收尾仍按仓库惯例。

## 明确不做

- 文件树 / 工作区首页卡片上的活动聚合
- 完成态跨应用重启持久化
- 错误专用图标（error 保持 idle；Error 文案仍在聊天内）
- 后端 Status 枚举扩展
- TUI 同步

## 成功标准

- 非当前页签的聊天在执行中 / 待确认 / 刚完成时，页签与左栏均可见对应图标
- 待确认优先于执行中
- 完成后图标保持，直到用户再次发送或切走该中心页签
- 未打开会话无活动图标；「已打开」不再误用 ✓

