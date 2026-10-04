# Agent 等待态 + Cursor 中文标题 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 中心区 tab / 会话列表在 agent 停下时显示「等待用户」、真正执行时明显转圈；Cursor 会话标题优先中文用户消息，并加 Cursor 规则尽力约束。

**Architecture:** 前端扩展 `resolveAgentActivity` 为 `awaiting|running|waiting|idle`；终端用 `terminalBusy` + 2.5s 输出静默启发区分执行/等待；删除 `activityCompleted`。Go 侧 `enrichCursorSession` 用 transcript 标题覆盖 meta；`indexVersion` 升至 7。

**Tech Stack:** React 19 + Zustand + Vitest；Go providers/discovery；Cursor rules。

**设计文档:** `docs/plans/2026-10-04-agent-waiting-cursor-title-design.md`

## Global Constraints

- 工作区：在 `.worktrees/feat/agent-waiting-cursor-title`（分支 `feat/agent-waiting-cursor-title`）内执行；禁止在 `master` 上直接改代码。
- 中文注释与提交信息；TDD：先红后绿。
- **提交**：仅当用户明确要求时才 `git commit`（下列 Commit 步骤在用户要求前跳过）。
- 不扩展 Go `Status`；不改写 Cursor `meta.json`；不做英译中；不改 TUI。
- 终端静默阈值固定 **2500ms**。
- 验证：前端改动跑 `cd frontend; npm test; npm run build`；Go 改动跑 `go test ./internal/providers ./internal/discovery -count=1`（合并前按范围全跑）。

---

## File Structure

| 文件 | 职责 |
|---|---|
| `frontend/src/state/agentActivity.ts` | 四态派生纯函数 |
| `frontend/src/state/terminalBusy.ts` | busy 置位 + 静默定时器（不入 persist） |
| `frontend/src/state/store.ts` | `terminalBusy`；删除 `activityCompleted` |
| `frontend/src/components/AgentActivityIcon.tsx` | 三态图标（running/awaiting/waiting） |
| `frontend/src/lib/api.ts` | `writeTerminal` 成功后 bump busy |
| `frontend/src/App.tsx` | `terminal:data` 时 bump busy |
| `frontend/src/pages/WorkspaceTab.tsx` | 页签派生；去掉 completed/切签清除 |
| `frontend/src/components/SessionList.tsx` | 列表派生 |
| `frontend/src/components/ChatView.tsx` | 去掉 clearActivityCompleted |
| `internal/providers/cursor_chats.go` | transcript 标题优先 |
| `internal/discovery/index.go` | `indexVersion = 7` |
| `.cursor/rules/session-naming-zh.mdc` | 会话中文命名规则 |
| `AGENTS.md` | 一句交叉说明 |
| `docs/smoke/feat-agent-waiting-cursor-title.md` | 冒烟增量 |

---

### Task 1: `resolveAgentActivity` 四态

**Files:**
- Modify: `frontend/src/state/agentActivity.ts`
- Modify: `frontend/src/state/agentActivity.test.ts`

**Interfaces:**
- Produces:
  ```ts
  export type AgentActivity = 'awaiting' | 'running' | 'waiting' | 'idle';
  export function resolveAgentActivity(input: {
    status: string;
    hasPermission: boolean;
    kind: 'chat' | 'terminal';
    busy?: boolean;
  }): AgentActivity;
  ```

- [ ] **Step 1: 重写失败测试**

替换 `agentActivity.test.ts` 全文：

```ts
import { describe, expect, it } from 'vitest';
import { resolveAgentActivity } from './agentActivity';

describe('resolveAgentActivity', () => {
  it('权限优先于 running / waiting', () => {
    expect(
      resolveAgentActivity({
        status: 'running',
        hasPermission: true,
        kind: 'chat',
      }),
    ).toBe('awaiting');
    expect(
      resolveAgentActivity({
        status: 'ready',
        hasPermission: true,
        kind: 'chat',
      }),
    ).toBe('awaiting');
  });

  it('聊天 running/starting → running；ready → waiting', () => {
    expect(
      resolveAgentActivity({ status: 'running', hasPermission: false, kind: 'chat' }),
    ).toBe('running');
    expect(
      resolveAgentActivity({ status: 'starting', hasPermission: false, kind: 'chat' }),
    ).toBe('running');
    expect(
      resolveAgentActivity({ status: 'ready', hasPermission: false, kind: 'chat' }),
    ).toBe('waiting');
  });

  it('聊天 exited → idle', () => {
    expect(
      resolveAgentActivity({ status: 'exited', hasPermission: false, kind: 'chat' }),
    ).toBe('idle');
  });

  it('终端 starting 或 running+busy → running；running 非 busy → waiting', () => {
    expect(
      resolveAgentActivity({
        status: 'starting',
        hasPermission: false,
        kind: 'terminal',
        busy: false,
      }),
    ).toBe('running');
    expect(
      resolveAgentActivity({
        status: 'running',
        hasPermission: false,
        kind: 'terminal',
        busy: true,
      }),
    ).toBe('running');
    expect(
      resolveAgentActivity({
        status: 'running',
        hasPermission: false,
        kind: 'terminal',
        busy: false,
      }),
    ).toBe('waiting');
  });

  it('终端 exited → idle', () => {
    expect(
      resolveAgentActivity({
        status: 'exited',
        hasPermission: false,
        kind: 'terminal',
        busy: true,
      }),
    ).toBe('idle');
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

```powershell
cd frontend; npx vitest run src/state/agentActivity.test.ts
```

Expected: FAIL（`kind` / `waiting` 尚未实现，或仍认 `completed`）

- [ ] **Step 3: 实现派生**

替换 `agentActivity.ts`：

```ts
export type AgentActivity = 'awaiting' | 'running' | 'waiting' | 'idle';

export function resolveAgentActivity(input: {
  status: string;
  hasPermission: boolean;
  kind: 'chat' | 'terminal';
  busy?: boolean;
}): AgentActivity {
  if (input.hasPermission) return 'awaiting';
  if (input.kind === 'chat') {
    if (input.status === 'running' || input.status === 'starting') return 'running';
    if (input.status === 'ready') return 'waiting';
    return 'idle';
  }
  if (input.status === 'starting') return 'running';
  if (input.status === 'running') return input.busy ? 'running' : 'waiting';
  return 'idle';
}
```

- [ ] **Step 4: 跑测试确认通过**

```powershell
cd frontend; npx vitest run src/state/agentActivity.test.ts
```

Expected: PASS

- [ ] **Step 5: Commit**（仅用户要求时）

```powershell
git add frontend/src/state/agentActivity.ts frontend/src/state/agentActivity.test.ts
git commit -m "feat: agent 活动态改为 waiting 四态派生"
```

---

### Task 2: `terminalBusy` + 静默定时器；删除 `activityCompleted`

**Files:**
- Create: `frontend/src/state/terminalBusy.ts`
- Create: `frontend/src/state/terminalBusy.test.ts`
- Modify: `frontend/src/state/store.ts`
- Modify: `frontend/src/state/store.test.ts`

**Interfaces:**
- Consumes: `useAppStore`（`setTerminalBusy` / `clearTerminalBusy`）
- Produces:
  ```ts
  export const TERMINAL_SILENCE_MS = 2500;
  export function bumpTerminalBusy(id: string): void;
  export function clearTerminalBusy(id: string): void;
  // store:
  terminalBusy: Record<string, true>;
  setTerminalBusy(id: string, busy: boolean): void;
  ```

- [ ] **Step 1: 先改 store 测试（删 completed，加 busy）**

在 `store.test.ts` 的 `beforeEach`/`reset` 中：去掉 `activityCompleted: {}`，改为 `terminalBusy: {}`。

删除所有 `activityCompleted` / `markActivityCompleted` / `clearActivityCompleted` / `turn_done 标记 activityCompleted` / `markTerminalExited 标记 completed` 相关用例。

追加：

```ts
it('setTerminalBusy 置位与清除；removeTerminal 去掉键', () => {
  useAppStore.getState().setTerminalBusy('t1', true);
  expect(useAppStore.getState().terminalBusy.t1).toBe(true);
  useAppStore.getState().setTerminalBusy('t1', false);
  expect(useAppStore.getState().terminalBusy.t1).toBeUndefined();
  useAppStore.getState().setTerminalBusy('t1', true);
  useAppStore.getState().removeTerminal('t1');
  expect(useAppStore.getState().terminalBusy.t1).toBeUndefined();
});

it('turn_done 将 Status 置 ready（不再写 activityCompleted）', () => {
  const chat = {
    ID: 'c1',
    SessionID: '',
    Workspace: 'D:\\w',
    Title: 't',
    ToolID: 'claude',
    Status: 'running',
    ExitCode: 0,
    Error: '',
  };
  useAppStore.setState({ chats: [chat], chatSeq: {}, chatItems: {} });
  useAppStore.getState().applyChat('c1', {
    Seq: 1,
    Type: 'turn_done',
    StopReason: 'end_turn',
  });
  expect(useAppStore.getState().chats[0].Status).toBe('ready');
  expect(useAppStore.getState()).not.toHaveProperty('activityCompleted');
});
```

（若 persist 用例断言 `not.toHaveProperty('activityCompleted')`，改为同时断言 `partialize` 不含 `terminalBusy`。）

- [ ] **Step 2: 跑 store 测试确认失败**

```powershell
cd frontend; npx vitest run src/state/store.test.ts
```

Expected: FAIL

- [ ] **Step 3: 改 store 实现**

在 `store.ts`：

1. 删除 `activityCompleted`、`markActivityCompleted`、`clearActivityCompleted` 类型与实现。
2. 从 `upsertChat` / `upsertTerminal` / `removeChat` / `removeTerminal` / `markTerminalExited` / `applyChat` 中移除所有 `activityCompleted` 读写；`markTerminalExited` 只改 Status/ExitCode；`applyChat` 的 `turn_done` 只把 Status 置 `ready`。
3. 新增：

```ts
terminalBusy: Record<string, true>;
setTerminalBusy(id: string, busy: boolean): void;
```

实现：

```ts
terminalBusy: {},
setTerminalBusy: (id, busy) =>
  set((s) => {
    if (busy) {
      if (s.terminalBusy[id]) return {};
      return { terminalBusy: { ...s.terminalBusy, [id]: true } };
    }
    if (!(id in s.terminalBusy)) return {};
    return { terminalBusy: omitKey(s.terminalBusy, id) };
  }),
```

`removeTerminal` / `markTerminalExited` 时 `omitKey(s.terminalBusy, id)`。

确认 `partialize` **不含** `terminalBusy`。

- [ ] **Step 4: 实现 `terminalBusy.ts` + 测试**

创建 `frontend/src/state/terminalBusy.ts`：

```ts
import { useAppStore } from './store';

export const TERMINAL_SILENCE_MS = 2500;

const timers = new Map<string, ReturnType<typeof setTimeout>>();

export function bumpTerminalBusy(id: string): void {
  if (!id) return;
  useAppStore.getState().setTerminalBusy(id, true);
  const prev = timers.get(id);
  if (prev) clearTimeout(prev);
  timers.set(
    id,
    setTimeout(() => {
      timers.delete(id);
      useAppStore.getState().setTerminalBusy(id, false);
    }, TERMINAL_SILENCE_MS),
  );
}

export function clearTerminalBusy(id: string): void {
  const prev = timers.get(id);
  if (prev) clearTimeout(prev);
  timers.delete(id);
  useAppStore.getState().setTerminalBusy(id, false);
}
```

在 `store` 的 `removeTerminal` / `markTerminalExited` 中调用 `clearTerminalBusy(id)`（避免循环依赖：可在这两个方法末尾动态 `import('./terminalBusy')` **不要**——改由调用方 clear，或把 timer map 放在 store 文件同目录并由 `markTerminalExited`/`removeTerminal` 直接调 `clearTerminalBusy`；若循环依赖，把 timer 逻辑放进 `store.ts` 底部同文件）。

**推荐避免循环：** `terminalBusy.ts` 只依赖 store；在 `App.tsx` 的 `onTerminalExit` 里先 `clearTerminalBusy(id)` 再 `markTerminalExited`；`removeTerminal` 包装一层或在 SessionList/WorkspaceTab 关闭路径调用 `clearTerminalBusy`。更干净：在 `setTerminalBusy(id,false)` 之外，让 `removeTerminal`/`markTerminalExited` 内部只清 Record，并在 `terminalBusy.ts` 导出 `clearTerminalBusy`，于 `App` exit 与关闭终端处调用。

创建 `terminalBusy.test.ts`：

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useAppStore } from './store';
import { TERMINAL_SILENCE_MS, bumpTerminalBusy, clearTerminalBusy } from './terminalBusy';

describe('terminalBusy', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    useAppStore.setState({ terminalBusy: {} });
  });
  afterEach(() => {
    clearTerminalBusy('t1');
    vi.useRealTimers();
  });

  it('bump 置 busy；静默后清除', () => {
    bumpTerminalBusy('t1');
    expect(useAppStore.getState().terminalBusy.t1).toBe(true);
    vi.advanceTimersByTime(TERMINAL_SILENCE_MS);
    expect(useAppStore.getState().terminalBusy.t1).toBeUndefined();
  });

  it('连续 bump 重置计时', () => {
    bumpTerminalBusy('t1');
    vi.advanceTimersByTime(TERMINAL_SILENCE_MS - 100);
    bumpTerminalBusy('t1');
    vi.advanceTimersByTime(200);
    expect(useAppStore.getState().terminalBusy.t1).toBe(true);
    vi.advanceTimersByTime(TERMINAL_SILENCE_MS);
    expect(useAppStore.getState().terminalBusy.t1).toBeUndefined();
  });
});
```

- [ ] **Step 5: 跑测试通过**

```powershell
cd frontend; npx vitest run src/state/store.test.ts src/state/terminalBusy.test.ts
```

Expected: PASS

- [ ] **Step 6: Commit**（仅用户要求时）

```powershell
git add frontend/src/state/store.ts frontend/src/state/store.test.ts frontend/src/state/terminalBusy.ts frontend/src/state/terminalBusy.test.ts
git commit -m "feat: 终端 busy 静默启发并移除 activityCompleted"
```

---

### Task 3: `AgentActivityIcon` 更新

**Files:**
- Modify: `frontend/src/components/AgentActivityIcon.tsx`
- Modify: `frontend/src/components/AgentActivityIcon.test.tsx`

**Interfaces:**
- Consumes: `AgentActivity`（含 `waiting`，无 `completed`）

- [ ] **Step 1: 改测试**

```ts
it('idle 不渲染', () => {
  const { container } = render(<AgentActivityIcon activity="idle" />);
  expect(container.firstChild).toBeNull();
});

it('running / awaiting / waiting 带对应 aria-label', () => {
  const { rerender } = render(<AgentActivityIcon activity="running" />);
  expect(screen.getByLabelText('执行中')).toBeInTheDocument();
  expect(screen.getByLabelText('执行中').querySelector('.animate-spin')).toBeTruthy();
  rerender(<AgentActivityIcon activity="awaiting" />);
  expect(screen.getByLabelText('待用户确认')).toBeInTheDocument();
  rerender(<AgentActivityIcon activity="waiting" />);
  expect(screen.getByLabelText('等待用户')).toBeInTheDocument();
  expect(screen.getByLabelText('等待用户').querySelector('.animate-spin')).toBeNull();
});
```

- [ ] **Step 2: 跑测试确认失败**

```powershell
cd frontend; npx vitest run src/components/AgentActivityIcon.test.tsx
```

Expected: FAIL（仍为「运行完成」）

- [ ] **Step 3: 实现图标**

- `running`：容器改为 `h-3.5 w-3.5`，svg `viewBox="0 0 14 14"`，circle `r=5`，保留 `animate-spin` + `text-success`
- `awaiting`：不变
- `waiting`：静态空心圆（无 spin、无 pulse）：

```tsx
if (activity === 'waiting') {
  return (
    <span
      className={cn('inline-flex h-3 w-3 shrink-0 text-muted-foreground', className)}
      role="img"
      aria-label="等待用户"
      title="等待用户"
    >
      <svg viewBox="0 0 12 12" className="h-3 w-3" aria-hidden="true">
        <circle cx="6" cy="6" r="4.5" fill="none" stroke="currentColor" strokeWidth="1.5" />
      </svg>
    </span>
  );
}
```

删除对勾分支；未知态返回 `null`。

- [ ] **Step 4: 跑测试通过**

```powershell
cd frontend; npx vitest run src/components/AgentActivityIcon.test.tsx
```

Expected: PASS

- [ ] **Step 5: Commit**（仅用户要求时）

```powershell
git add frontend/src/components/AgentActivityIcon.tsx frontend/src/components/AgentActivityIcon.test.tsx
git commit -m "feat: 活动图标支持等待用户并强化转圈"
```

---

### Task 4: 挂钩 terminal data / write；接线页签与列表

**Files:**
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/lib/api.ts`（`writeTerminal`）
- Modify: `frontend/src/pages/WorkspaceTab.tsx`
- Modify: `frontend/src/pages/WorkspaceTab.test.tsx`
- Modify: `frontend/src/components/SessionList.tsx`
- Modify: `frontend/src/components/SessionList.test.tsx`
- Modify: `frontend/src/components/ChatView.tsx`
- Modify: `frontend/src/components/ChatView.test.tsx`
- Modify: 其它仍引用 `activityCompleted` / `completed:` / `clearActivityCompleted` 的测试（`App.test.tsx` 等，全局搜一遍）

**Interfaces:**
- Consumes: `bumpTerminalBusy`、`clearTerminalBusy`、`resolveAgentActivity`、`terminalBusy`

- [ ] **Step 1: App 挂钩 data/exit**

在 `App.tsx` 的 `onTerminalData` 回调中，`dispatchTerminalData` 之前或之后：

```ts
import { bumpTerminalBusy, clearTerminalBusy } from './state/terminalBusy';

const offData = onTerminalData(({ id, data }) => {
  if (id) bumpTerminalBusy(id);
  dispatchTerminalData(id, data);
});
const offExit = onTerminalExit(({ id, exitCode }) => {
  clearTerminalBusy(id);
  // ...现有 markTerminalExited + notify
});
```

- [ ] **Step 2: writeTerminal 挂钩**

在 `api.ts`：

```ts
import { bumpTerminalBusy } from '../state/terminalBusy';

export async function writeTerminal(id: string, data: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.WriteTerminal(id, data);
  bumpTerminalBusy(id);
}
```

（写入失败不要 bump；仅 await 成功后。）

- [ ] **Step 3: WorkspaceTab / SessionList / ChatView**

`WorkspaceTab.tsx`：

- 去掉 `activityCompleted`、`clearActivityCompleted`、`selectCenterTab` 里清 completed 的逻辑（可保留 `selectCenterTab` 仅 `setCenterTab`）。
- 订阅 `terminalBusy`。
- 终端页签：

```ts
const activity = resolveAgentActivity({
  status: t.Status,
  hasPermission: false,
  kind: 'terminal',
  busy: !!terminalBusy[t.ID],
});
```

- 聊天页签：

```ts
const activity = resolveAgentActivity({
  status: c.Status,
  hasPermission: !!chatPermissions[c.ID],
  kind: 'chat',
});
```

- 删除 `activity === 'idle' && t.Status === 'exited'` 旁的独立灰点（若与 waiting 重复可只留 exited 无图标）。

`SessionList.tsx`：同样改 `resolveAgentActivity` 入参；打开实例为 terminal 时传 `busy: !!terminalBusy[opened.ID]`。

`ChatView.tsx`：删除 `clearActivityCompleted` 调用与相关测试。

- [ ] **Step 4: 更新测试断言**

- 「运行完成」→「等待用户」
- chat `Status=ready` → 出现「等待用户」（不再依赖 `activityCompleted`）
- 删除「切页签清除 activityCompleted」用例，或改为断言切签不改 Status
- terminal：可单测 `bumpTerminalBusy` + fake timers 后页签文案（可选，核心已在 Task 2）
- `SessionList`：ready chat →「等待用户」；running →「执行中」

- [ ] **Step 5: 跑前端相关测试**

```powershell
cd frontend; npx vitest run src/state src/components/AgentActivityIcon.test.tsx src/components/SessionList.test.tsx src/components/ChatView.test.tsx src/pages/WorkspaceTab.test.tsx src/App.test.tsx
```

Expected: PASS。若有残留 `activityCompleted` / `completed:` 编译错误，全局搜索并改完。

- [ ] **Step 6: 全量前端**

```powershell
cd frontend; npm test; npm run build
```

Expected: 全绿

- [ ] **Step 7: Commit**（仅用户要求时）

```powershell
git add frontend/src
git commit -m "feat: 页签与列表接入等待用户活动态"
```

---

### Task 5: Cursor transcript 标题优先 + indexVersion 7

**Files:**
- Modify: `internal/providers/cursor_chats.go`（`enrichCursorSession`）
- Modify: `internal/providers/cursor_test.go`（`TestCursorEnumerateFromChats`）
- Modify: `internal/discovery/index.go`（`indexVersion`）

**Interfaces:**
- Consumes: `(Cursor{}).ParseSession`、`ReadHead`
- Produces: `Session.Title` 在 transcript 有用户消息时覆盖 meta

- [ ] **Step 1: 改失败测试**

将 `TestCursorEnumerateFromChats` 中 transcript 正文改为中文，并期望标题为中文：

```go
body := `{"role":"user","message":{"content":[{"type":"text","text":"<user_query>修复登录页空指针</user_query>"}]}}` + "\n"
// ...
if got[0].Title != "修复登录页空指针" {
	t.Fatalf("title=%q, want transcript 优先于 meta", got[0].Title)
}
```

追加用例（可同文件）：meta 有标题、无 transcript → 仍用 meta：

```go
func TestCursorEnumerateKeepsMetaWhenNoTranscript(t *testing.T) {
	home := t.TempDir()
	ws := filepath.Join(home, "ws")
	os.MkdirAll(ws, 0o755)
	id := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeffff0099"
	writeMeta(t, filepath.Join(home, ".cursor", "chats", "h", id), true, "OnlyMeta", ws)
	got, err := (Cursor{}).EnumerateSessions(home, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "OnlyMeta" {
		t.Fatalf("%+v", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```powershell
go test ./internal/providers -run "TestCursorEnumerateFromChats|TestCursorEnumerateKeepsMeta" -count=1
```

Expected: FAIL（仍 FromMeta）

- [ ] **Step 3: 改 `enrichCursorSession`**

```go
func enrichCursorSession(home string, s *Session) {
	path := resolveCursorTranscript(home, s.Workspace, s.ID)
	if path == "" {
		return
	}
	s.Path = path
	if n, err := CountLines(path); err == nil {
		s.Messages = n
	}
	// transcript 首条用户消息优先于 Cursor 自动英文 meta.title
	if head, err := ReadHead(path, 256*1024); err == nil {
		if parsed, err := (Cursor{}).ParseSession(path, head); err == nil && parsed.Title != "" {
			s.Title = parsed.Title
		}
	}
}
```

- [ ] **Step 4: `indexVersion = 7`**

```go
indexVersion = 7 // v7：Cursor 会话标题优先 transcript 用户消息
```

- [ ] **Step 5: 跑 Go 测试**

```powershell
go test ./internal/providers ./internal/discovery -count=1
```

Expected: PASS

- [ ] **Step 6: Commit**（仅用户要求时）

```powershell
git add internal/providers/cursor_chats.go internal/providers/cursor_test.go internal/discovery/index.go
git commit -m "feat: Cursor 会话标题优先中文用户消息"
```

---

### Task 6: Cursor 规则 + AGENTS + 冒烟

**Files:**
- Create: `.cursor/rules/session-naming-zh.mdc`
- Modify: `AGENTS.md`（项目概述或代码约定附近加一句）
- Create: `docs/smoke/feat-agent-waiting-cursor-title.md`

- [ ] **Step 1: 写规则文件**

`.cursor/rules/session-naming-zh.mdc`：

```markdown
---
description: 会话与聊天标题使用简洁中文
alwaysApply: true
---

# 会话命名

- 为 Agent 会话 / 聊天起名或更新标题时，使用简洁中文主题短语（如「修复登录空指针」「终端等待态」）。
- 避免英文 Title Case（如 `Bug Fixes And Features`）、避免整句英文摘要当标题。
- 若产品 UI 自动生成英文名，在可编辑时改为中文。
```

- [ ] **Step 2: AGENTS.md 交叉说明**

在「代码约定」或「项目概述」末加一句：

```markdown
- Cursor 会话展示标题：kshell 优先用 transcript 首条用户消息；仓库 `.cursor/rules/session-naming-zh.mdc` 约束会话命名用中文（产品侧 `meta.title` 不保证遵守）。
```

- [ ] **Step 3: 冒烟清单**

`docs/smoke/feat-agent-waiting-cursor-title.md`：

```markdown
# feat/agent-waiting-cursor-title 冒烟

- [ ] ACP 聊天发送后 tab/列表「执行中」转圈；结束后变为「等待用户」空心圆（非对勾、非一直转圈）
- [ ] 权限请求时两处为「待用户确认」黄点；确认后恢复执行中或等待用户
- [ ] 内嵌终端：有输出/输入时转圈；停约 2.5s 后变「等待用户」；进程仍在
- [ ] 终端退出后无活动图标（idle）
- [ ] Cursor 会话列表标题优先为首条中文用户消息（刷新/重扫后）；非 meta 英文自动名
- [ ] 未打开的历史会话无活动图标
```

- [ ] **Step 4: Commit**（仅用户要求时）

```powershell
git add .cursor/rules/session-naming-zh.mdc AGENTS.md docs/smoke/feat-agent-waiting-cursor-title.md
git commit -m "docs: Cursor 中文会话命名规则与冒烟清单"
```

---

### Task 7: 全量验证

- [ ] **Step 1: Go**

```powershell
go build ./... ; go vet ./... ; go test ./... -count=1
```

Expected: 全绿

- [ ] **Step 2: 前端**

```powershell
cd frontend; npm test; npm run build
```

Expected: 全绿

- [ ] **Step 3: 自查**

对照设计文档成功标准逐条过一遍；全局搜索确认无 `activityCompleted`、`运行完成`、`completed:` 入参残留（测试里历史用例除外——应已改完）。

---

## Spec Coverage Checklist

| 规格要求 | Task |
|---|---|
| 四态 awaiting/running/waiting/idle | Task 1 |
| 终端 busy + 2.5s 静默 | Task 2、4 |
| 删除 activityCompleted / 对勾 | Task 2、3、4 |
| 强化转圈 + 等待用户空心圆 | Task 3 |
| 页签 + SessionList 接线 | Task 4 |
| write/data 挂钩 busy | Task 4 |
| 聊天依赖 turn_done→ready（卡住则修） | Task 4 验证；若复现另开修复提交于本分支 |
| Cursor transcript 标题优先 | Task 5 |
| indexVersion 7 | Task 5 |
| Cursor 规则 + AGENTS | Task 6 |
| 冒烟 | Task 6 |
| 全量验证 | Task 7 |

## Self-Review Notes

- 无 TBD/placeholder。
- `resolveAgentActivity` 签名在 Task 1 定义，Task 3–4 一致使用 `kind` + `busy`。
- `bumpTerminalBusy` / `clearTerminalBusy` 在 Task 2 定义，Task 4 消费。
- 提交步骤默认跳过，直至用户明确要求。
