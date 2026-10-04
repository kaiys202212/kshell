# Agent 活动状态指示 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在中心区页签与左栏会话列表用统一图标标出 agent 执行中 / 待确认 / 运行完成。

**Architecture:** 前端纯派生：`resolveAgentActivity` 从 `Status` + `chatPermissions` + `activityCompleted` 算出四态；共享 `AgentActivityIcon` 挂到 `WorkspaceTab` 页签与 `SessionList`。完成态仅前端保持，不改 Go。设计文档：`docs/plans/2026-10-04-agent-activity-indicators-design.md`。

**Tech Stack:** React 19 + TypeScript strict、Zustand、vitest + jsdom、Tailwind v4（内联 SVG，无图标库）。

## Global Constraints

- 范围：仅桌面端前端；不改 Go / ACP / TUI / 文件树 / 工作区顶栏。
- 工作区隔离：在 `.worktrees/feat-agent-activity`（分支 `feat/agent-activity`）内执行；禁止在 `master` 上直接改代码。
- 提交：中文 `type: 简述`；**仅当用户明确要求时才 `git commit`**（计划中的 Commit 步骤在未授权时跳过，改记「待提交」）。
- TDD：每个任务先红后绿；验证命令必须真实跑通。
- 状态优先级：`awaiting` > `running` > `completed` > `idle`。
- `activityCompleted` **不**进入 persist `partialize`。
- 错误（`error`→`ready`）不标 `completed`。

## 文件结构（将创建/修改）

| 文件 | 职责 |
|---|---|
| `frontend/src/state/agentActivity.ts` | `AgentActivity` 类型 + `resolveAgentActivity` |
| `frontend/src/state/agentActivity.test.ts` | 派生优先级单测 |
| `frontend/src/state/store.ts` | `activityCompleted` + mark/clear；与 applyChat/remove/exited 联动 |
| `frontend/src/state/store.test.ts` | completed 置位/清除 |
| `frontend/src/components/AgentActivityIcon.tsx` | 三态图标 |
| `frontend/src/components/AgentActivityIcon.test.tsx` | 渲染 / aria |
| `frontend/src/pages/WorkspaceTab.tsx` | 页签图标；切走清除 |
| `frontend/src/components/ChatView.tsx` | 发送前 clear |
| `frontend/src/components/SessionList.tsx` | 活动图标替换 ✓ |
| 既有 `*.test.tsx` | 断言同步更新 |
| `docs/smoke/feat-agent-activity.md` | 冒烟增量 |

---

### Task 0: 建立 worktree

**Files:** 无代码

- [ ] **Step 1: 建隔离工作区**

```powershell
git worktree add .worktrees/feat-agent-activity -b feat/agent-activity
```

- [ ] **Step 2: 确认基线**

```powershell
cd .worktrees/feat-agent-activity\frontend
npm test
```

Expected: 全绿。后续路径相对该 worktree 根。若设计/计划仅在 master 工作区，先复制两份 md 到 worktree 的 `docs/plans/`。

- [ ] **Step 3: 待提交（若用户要求）** — 跳过或 `docs: agent 活动状态指示设计与计划`

---

### Task 1: `resolveAgentActivity` 纯函数

**Files:**
- Create: `frontend/src/state/agentActivity.ts`
- Create: `frontend/src/state/agentActivity.test.ts`

**Interfaces:**
- Produces:
  - `export type AgentActivity = 'awaiting' | 'running' | 'completed' | 'idle'`
  - `export function resolveAgentActivity(input: { status: string; hasPermission: boolean; completed: boolean }): AgentActivity`

- [ ] **Step 1: 写失败测试**

```ts
// frontend/src/state/agentActivity.test.ts
import { describe, expect, it } from 'vitest';
import { resolveAgentActivity } from './agentActivity';

describe('resolveAgentActivity', () => {
  it('权限优先于 running', () => {
    expect(
      resolveAgentActivity({ status: 'running', hasPermission: true, completed: true }),
    ).toBe('awaiting');
  });

  it('running / starting → running', () => {
    expect(resolveAgentActivity({ status: 'running', hasPermission: false, completed: false })).toBe('running');
    expect(resolveAgentActivity({ status: 'starting', hasPermission: false, completed: false })).toBe('running');
  });

  it('completed 在 ready 时生效；running 时被压制', () => {
    expect(resolveAgentActivity({ status: 'ready', hasPermission: false, completed: true })).toBe('completed');
    expect(resolveAgentActivity({ status: 'running', hasPermission: false, completed: true })).toBe('running');
  });

  it('exited + completed → completed；exited 无标记 → idle', () => {
    expect(resolveAgentActivity({ status: 'exited', hasPermission: false, completed: true })).toBe('completed');
    expect(resolveAgentActivity({ status: 'exited', hasPermission: false, completed: false })).toBe('idle');
  });

  it('ready 无标记 → idle', () => {
    expect(resolveAgentActivity({ status: 'ready', hasPermission: false, completed: false })).toBe('idle');
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

```powershell
cd frontend
npx vitest run src/state/agentActivity.test.ts
```

Expected: FAIL（模块不存在）。

- [ ] **Step 3: 最小实现**

```ts
// frontend/src/state/agentActivity.ts
export type AgentActivity = 'awaiting' | 'running' | 'completed' | 'idle';

export function resolveAgentActivity(input: {
  status: string;
  hasPermission: boolean;
  completed: boolean;
}): AgentActivity {
  if (input.hasPermission) return 'awaiting';
  if (input.status === 'running' || input.status === 'starting') return 'running';
  if (input.completed) return 'completed';
  return 'idle';
}
```

- [ ] **Step 4: 跑测试确认通过**

```powershell
npx vitest run src/state/agentActivity.test.ts
```

Expected: PASS。

- [ ] **Step 5: 待提交** — `feat: 新增 resolveAgentActivity 派生`

---

### Task 2: store — `activityCompleted` 置位与清除

**Files:**
- Modify: `frontend/src/state/store.ts`
- Modify: `frontend/src/state/store.test.ts`

**Interfaces:**
- Consumes: Task 1 无直接依赖（本任务只管标记）
- Produces:
  - `activityCompleted: Record<string, true>`
  - `markActivityCompleted(id: string): void`
  - `clearActivityCompleted(id: string): void`
  - `applyChat` 在 `turn_done` 且非 exited 时 mark
  - `markTerminalExited` 时 mark
  - `upsertChat` / `upsertTerminal`：当新 `Status` 为 `running`|`starting` 时 clear
  - `removeChat` / `removeTerminal`：去掉对应键
  - `partialize` 不含 `activityCompleted`

- [ ] **Step 1: 写失败测试**（追加到 `store.test.ts`）

```ts
import type { ChatUpdate } from '../lib/api';

// 在 beforeEach 的 setState 中增加：activityCompleted: {}

it('turn_done 标记 activityCompleted；error 不标记', () => {
  const chat: ChatInfo = {
    ID: 'c1', Kind: 'session', SessionID: 's1', Workspace: 'D:\\p',
    Title: 't', ToolID: 'claude', Status: 'running', ExitCode: 0, Error: '',
  };
  useAppStore.setState({ chats: [chat], activityCompleted: {} });
  useAppStore.getState().applyChat('c1', {
    Seq: 1, Type: 'turn_done',
  } as ChatUpdate);
  expect(useAppStore.getState().activityCompleted.c1).toBe(true);
  expect(useAppStore.getState().chats[0].Status).toBe('ready');

  useAppStore.setState({
    chats: [{ ...chat, Status: 'running' }],
    activityCompleted: {},
  });
  useAppStore.getState().applyChat('c1', {
    Seq: 2, Type: 'error', Text: 'boom',
  } as ChatUpdate);
  expect(useAppStore.getState().activityCompleted.c1).toBeUndefined();
});

it('markTerminalExited 标记 completed；upsert running 清除', () => {
  const term: TerminalInfo = {
    ID: 't1', SessionID: 's1', Workspace: 'D:\\p', Title: 'x',
    ToolID: 'claude', Status: 'running', ExitCode: 0,
  };
  useAppStore.setState({ terminals: [term], activityCompleted: {} });
  useAppStore.getState().markTerminalExited('t1', 0);
  expect(useAppStore.getState().activityCompleted.t1).toBe(true);

  useAppStore.getState().upsertTerminal({ ...term, Status: 'running' });
  expect(useAppStore.getState().activityCompleted.t1).toBeUndefined();
});

it('removeChat 去掉 completed 键；clearActivityCompleted 可手动清', () => {
  useAppStore.setState({
    chats: [{
      ID: 'c1', Kind: 'new', SessionID: '', Workspace: 'D:\\p',
      Title: 't', ToolID: 'claude', Status: 'ready', ExitCode: 0, Error: '',
    }],
    activityCompleted: { c1: true },
  });
  useAppStore.getState().clearActivityCompleted('c1');
  expect(useAppStore.getState().activityCompleted.c1).toBeUndefined();

  useAppStore.setState({
    chats: [{
      ID: 'c1', Kind: 'new', SessionID: '', Workspace: 'D:\\p',
      Title: 't', ToolID: 'claude', Status: 'ready', ExitCode: 0, Error: '',
    }],
    activityCompleted: { c1: true },
  });
  useAppStore.getState().removeChat('c1');
  expect(useAppStore.getState().activityCompleted.c1).toBeUndefined();
});
```

（`TerminalInfo` 字段以 `frontend/src/lib/api.ts` 为准，缺字段按现有测试夹具补齐。）

- [ ] **Step 2: 跑测试确认失败**

```powershell
npx vitest run src/state/store.test.ts
```

Expected: FAIL（`activityCompleted` / 方法不存在）。

- [ ] **Step 3: 实现 store 变更**

在 `AppState` 接口增加：

```ts
activityCompleted: Record<string, true>;
markActivityCompleted(id: string): void;
clearActivityCompleted(id: string): void;
```

初始值与实现要点：

```ts
activityCompleted: {},
markActivityCompleted: (id) =>
  set((s) => ({ activityCompleted: { ...s.activityCompleted, [id]: true } })),
clearActivityCompleted: (id) =>
  set((s) => {
    if (!(id in s.activityCompleted)) return {};
    return { activityCompleted: omitKey(s.activityCompleted, id) };
  }),
```

`applyChat`：在现有 `turn_done`/`error` 分支中，对 `turn_done` 且当前 chat 非 `exited` 时同时写入 `activityCompleted[id]=true`（与 chats 更新同一 `set` 返回值）。`error` 不写 completed。

`markTerminalExited`：更新 Status 的同时 `activityCompleted[id]=true`。

`upsertChat` / `upsertTerminal`：合并 info 后，若 `info.Status === 'running' || info.Status === 'starting'`，从 `activityCompleted` 去掉该 id。

`removeChat` / `removeTerminal`：现有 omit 逻辑中一并 `omitKey(s.activityCompleted, id)`。

确认 `partialize` 仍只含 `openTabs/activeTabId/layout/newSessionTool`。

- [ ] **Step 4: 跑测试确认通过**

```powershell
npx vitest run src/state/store.test.ts
```

Expected: PASS。

- [ ] **Step 5: 待提交** — `feat: store 增加 activityCompleted 生命周期`

---

### Task 3: `AgentActivityIcon` 组件

**Files:**
- Create: `frontend/src/components/AgentActivityIcon.tsx`
- Create: `frontend/src/components/AgentActivityIcon.test.tsx`

**Interfaces:**
- Consumes: `AgentActivity` from `../state/agentActivity`
- Produces: `<AgentActivityIcon activity={...} className? />`；`idle` 返回 `null`

- [ ] **Step 1: 写失败测试**

```tsx
import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import AgentActivityIcon from './AgentActivityIcon';

describe('AgentActivityIcon', () => {
  it('idle 不渲染', () => {
    const { container } = render(<AgentActivityIcon activity="idle" />);
    expect(container.firstChild).toBeNull();
  });

  it('running / awaiting / completed 带对应 aria-label', () => {
    const { rerender } = render(<AgentActivityIcon activity="running" />);
    expect(screen.getByLabelText('执行中')).toBeInTheDocument();
    rerender(<AgentActivityIcon activity="awaiting" />);
    expect(screen.getByLabelText('待用户确认')).toBeInTheDocument();
    rerender(<AgentActivityIcon activity="completed" />);
    expect(screen.getByLabelText('运行完成')).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

```powershell
npx vitest run src/components/AgentActivityIcon.test.tsx
```

Expected: FAIL。

- [ ] **Step 3: 实现组件**

```tsx
// frontend/src/components/AgentActivityIcon.tsx
import { cn } from '../lib/cn';
import type { AgentActivity } from '../state/agentActivity';

interface Props {
  activity: AgentActivity;
  className?: string;
}

export default function AgentActivityIcon({ activity, className }: Props) {
  if (activity === 'idle') return null;
  if (activity === 'running') {
    return (
      <span
        className={cn('inline-flex h-3 w-3 shrink-0 text-success', className)}
        role="img"
        aria-label="执行中"
        title="执行中"
      >
        <svg viewBox="0 0 12 12" className="h-3 w-3 animate-spin" aria-hidden="true">
          <circle cx="6" cy="6" r="4.5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeDasharray="18 8" />
        </svg>
      </span>
    );
  }
  if (activity === 'awaiting') {
    return (
      <span
        className={cn('inline-flex h-3 w-3 shrink-0 items-center justify-center text-warning', className)}
        role="img"
        aria-label="待用户确认"
        title="待用户确认"
      >
        <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-warning" aria-hidden="true" />
      </span>
    );
  }
  // completed
  return (
    <span
      className={cn('inline-flex h-3 w-3 shrink-0 text-muted-foreground', className)}
      role="img"
      aria-label="运行完成"
      title="运行完成"
    >
      <svg viewBox="0 0 12 12" className="h-3 w-3" aria-hidden="true">
        <path
          d="M2.5 6.5 L5 9 L9.5 3.5"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    </span>
  );
}
```

- [ ] **Step 4: 跑测试确认通过**

```powershell
npx vitest run src/components/AgentActivityIcon.test.tsx
```

Expected: PASS。

- [ ] **Step 5: 待提交** — `feat: 新增 AgentActivityIcon`

---

### Task 4: ChatView 发送前清除 completed

**Files:**
- Modify: `frontend/src/components/ChatView.tsx`（`send` 内、乐观 `upsertChat` 之前）
- Modify: `frontend/src/components/ChatView.test.tsx`

**Interfaces:**
- Consumes: `clearActivityCompleted`（upsert running 也会清；发送前显式 clear 保证失败回退 ready 时不残留旧 completed——若 upsert 已 clear，显式调用仍幂等）

- [ ] **Step 1: 写失败测试**

在 `ChatView.test.tsx` 追加：先 `setState({ activityCompleted: { c1: true }, chats: [CHAT] })`，mock `sendChatPrompt` resolve，触发发送，断言 `activityCompleted.c1` 为 `undefined`。

- [ ] **Step 2: 跑测试确认失败**

```powershell
npx vitest run src/components/ChatView.test.tsx
```

- [ ] **Step 3: 在 `send` 中、`upsertChat` 前调用**

```ts
useAppStore.getState().clearActivityCompleted(id);
```

（若 Task 2 的 `upsertChat` 已在 running 时 clear，此步仍保留显式调用以表达意图，双清无害。）

- [ ] **Step 4: 跑测试确认通过**

```powershell
npx vitest run src/components/ChatView.test.tsx
```

- [ ] **Step 5: 待提交** — `feat: 发送前清除 activityCompleted`

---

### Task 5: WorkspaceTab 页签图标 + 切走清除

**Files:**
- Modify: `frontend/src/pages/WorkspaceTab.tsx`
- Modify: `frontend/src/pages/WorkspaceTab.test.tsx` 与/或 `frontend/src/components/WorkspaceTab.test.tsx`（以现有测页签的文件为准）

**Interfaces:**
- Consumes: `resolveAgentActivity`、`AgentActivityIcon`、`activityCompleted`、`chatPermissions`、`clearActivityCompleted`

- [ ] **Step 1: 写失败测试**

覆盖：

1. chat `Status=running` 时页签出现 `aria-label="执行中"`
2. `chatPermissions[c.ID]` 存在时出现「待用户确认」
3. `activityCompleted[c.ID]` 且 ready 时出现「运行完成」
4. 点击切到预览页签后，`activityCompleted` 中该 id 被清除

（按现有测试里 mock/store 夹具风格编写。）

- [ ] **Step 2: 跑测试确认失败**

```powershell
npx vitest run src/pages/WorkspaceTab.test.tsx src/components/WorkspaceTab.test.tsx
```

- [ ] **Step 3: 实现**

1. 订阅：

```ts
const chatPermissions = useAppStore((s) => s.chatPermissions);
const activityCompleted = useAppStore((s) => s.activityCompleted);
```

2. 封装切页签：

```ts
const selectCenterTab = (next: string) => {
  setCenterTab((prev) => {
    if (prev !== next && prev !== PREVIEW_TAB) {
      useAppStore.getState().clearActivityCompleted(prev);
    }
    return next;
  });
};
```

将所有 `setCenterTab(...)` 业务切换改为 `selectCenterTab`（关闭时若当前就是该 id，先 clear 再切到预览亦可；`removeChat` 已清键则重复 clear 幂等）。注意：`setCenterTab` 若用函数式更新，React 的 `useState` setter 支持；若当前是直接赋值，改为：

```ts
const selectCenterTab = (next: string) => {
  setCenterTab((prev) => {
    if (prev !== next && prev !== PREVIEW_TAB) {
      useAppStore.getState().clearActivityCompleted(prev);
    }
    return next;
  });
};
```

3. 终端/聊天页签标题左侧：

```tsx
<AgentActivityIcon
  activity={resolveAgentActivity({
    status: c.Status, // 或 t.Status
    hasPermission: kind === 'chat' && !!chatPermissions[c.ID],
    completed: !!activityCompleted[c.ID],
  })}
/>
```

删除旧的 `c.Status === 'running' && <span className="... animate-pulse ..."/>`。

终端：`hasPermission: false`。

- [ ] **Step 4: 跑测试确认通过**

```powershell
npx vitest run src/pages/WorkspaceTab.test.tsx src/components/WorkspaceTab.test.tsx
```

- [ ] **Step 5: 待提交** — `feat: 页签显示 agent 活动图标`

---

### Task 6: SessionList 活动图标

**Files:**
- Modify: `frontend/src/components/SessionList.tsx`
- Modify: `frontend/src/components/SessionList.test.tsx`

**Interfaces:**
- Consumes: `resolveAgentActivity`、`AgentActivityIcon`、store 的 chats/terminals/permissions/completed

辅助逻辑（可放组件内或 `agentActivity.ts`）：

```ts
function findOpenedForSession(
  sessionID: string,
  chats: ChatInfo[],
  terminals: TerminalInfo[],
): { ID: string; Status: string; kind: 'chat' | 'terminal' } | null {
  const chatHit = chats.filter((c) => c.SessionID === sessionID);
  const termHit = terminals.filter((t) => t.SessionID === sessionID);
  const prefer = (list: { ID: string; Status: string }[]) =>
    list.find((x) => x.Status !== 'exited') ?? list[0];
  const c = prefer(chatHit);
  if (c) return { ID: c.ID, Status: c.Status, kind: 'chat' };
  const t = prefer(termHit);
  if (t) return { ID: t.ID, Status: t.Status, kind: 'terminal' };
  return null;
}
```

「是否已打开」（按钮 恢复/切换）可继续用非 exited 判断，与图标解耦。

- [ ] **Step 1: 写失败测试**

1. 打开 chat `running` → 列表行有「执行中」
2. 有 permission →「待用户确认」
3. ready + activityCompleted →「运行完成」
4. 仅打开 ready、无 completed → **没有** ✓，也没有活动图标
5. 终端 exited + completed →「运行完成」

- [ ] **Step 2: 跑测试确认失败**

```powershell
npx vitest run src/components/SessionList.test.tsx
```

- [ ] **Step 3: 实现**

替换：

```tsx
{running && (
  <span className="shrink-0 text-xs text-success" title="运行中">✓</span>
)}
```

为：

```tsx
{opened && (
  <AgentActivityIcon
    activity={resolveAgentActivity({
      status: opened.Status,
      hasPermission: opened.kind === 'chat' && !!chatPermissions[opened.ID],
      completed: !!activityCompleted[opened.ID],
    })}
  />
)}
```

订阅 `chatPermissions`、`activityCompleted`；行高亮仍可用「非 exited 已打开」。

- [ ] **Step 4: 跑测试确认通过**

```powershell
npx vitest run src/components/SessionList.test.tsx
```

- [ ] **Step 5: 待提交** — `feat: 会话列表显示 agent 活动图标`

---

### Task 7: 冒烟文档 + 全量前端验证

**Files:**
- Create: `docs/smoke/feat-agent-activity.md`

- [ ] **Step 1: 写冒烟增量**

```markdown
# feat/agent-activity 冒烟

- [ ] 打开 ACP 聊天，发送消息：页签与左栏出现「执行中」转圈
- [ ] 触发权限请求：两处变为「待用户确认」脉冲黄点；确认后恢复执行中或完成
- [ ] 一轮结束后：两处显示「运行完成」对勾；保持到切走该页签或再次发送
- [ ] 切到预览/其他页签后，完成图标消失
- [ ] 内嵌终端运行中显示转圈；退出后显示完成对勾
- [ ] 未打开的历史会话无活动图标；已打开但空闲无图标（无旧 ✓）
```

- [ ] **Step 2: 全量前端验证**

```powershell
cd frontend
npm test
npm run build
```

Expected: 全绿；`tsc && vite build` 成功。

- [ ] **Step 3: 待提交** — `docs: agent 活动状态冒烟清单` + 未提交的实现（若用户要求一次性提交可整理）

---

## Spec 覆盖自检

| 规格要点 | 任务 |
|---|---|
| 四态 + 优先级 | Task 1 |
| activityCompleted 生命周期 | Task 2、4、5 |
| 图标视觉 / a11y | Task 3 |
| 页签挂载 + 切走清除 | Task 5 |
| 会话列表挂载 + 修正 ✓ | Task 6 |
| 终端 mapping | Task 2、5、6 |
| 不改 Go / 不持久化 completed | Global + Task 2 partialize |
| 冒烟 | Task 7 |

## 执行交接

计划已保存到 `docs/plans/2026-10-04-agent-activity-indicators-plan.md`。

**两种执行方式：**

1. **Subagent-Driven（推荐）** — 每任务派生子代理，任务间审阅  
2. **Inline Execution** — 本会话按 `executing-plans` 批量推进并设检查点  

要哪种？
