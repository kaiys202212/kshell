# kshell C 高密度工具感视觉底座（S1）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在不改变任何行为、数据流、绑定、文案与 aria 语义的前提下，把 kshell 桌面端 UI 统一到 C（高密度工具感）视觉语言。

**Architecture:** 分两层落地——(1) 改 `frontend/src/style.css` 的设计 token（字号/圆角/等宽字体/语义状态色），半径 token 改成 3px 后，现有 `rounded-md/lg/sm` 自动收敛；(2) 新增 `lib/ui.ts` 共享 className 常量 + `lib/toolBadge` 工具色相 + `ui/tool-dot` 组件，再逐页做密度/等宽/状态色的显式调整。**不改行为**，因此除 `toolBadge` 外不需要新行为测试；回归依赖既有 100+ 用例与 `npm run build`，最终人工冒烟浅/深两态。

**Tech Stack:** Tailwind CSS 4（`@theme inline` + `prefers-color-scheme`）、React 19、Vitest、Vite。

**约束（重要）：**
- **本仓库约定：只在用户明确要求时才提交。** 本计划各任务末尾的提交为「可选」；默认不执行 `git commit`，除非用户当次明确同意。建议的 commit message 已给出备查。
- 不改任何 `onClick`/数据流/绑定/用户可见文案/aria。
- 深浅两态都要成立；所有新色都给浅/深取值。
- 不改响应式断点与窗口/托盘/终端逻辑。
- 每个改动任务后运行 `cd frontend; npm test` 应全绿；纯 CSS/新增文件任务不破坏测试。

---

## 文件结构

| 文件 | 动作 | 职责 |
|---|---|---|
| `frontend/src/style.css` | Modify | token：字号 12.5px、行高 1.45、`--radius-base` 3px、`--font-mono-stack`、`--success/--warning` 与映射 |
| `frontend/src/lib/ui.ts` | Create | 共享 className 常量（页签/列表行/面板标题/等宽），消除 3 处 tab 重复 |
| `frontend/src/lib/toolBadge.ts` | Modify | `ToolBadge` 增加 `color`（工具固定色相） |
| `frontend/src/lib/toolBadge.test.ts` | Modify | 同步断言（新增 color，改用 `toMatchObject`） |
| `frontend/src/components/ui/tool-dot.tsx` | Create | `色点 + 工具名` 紧凑徽标 |
| `frontend/src/components/ui/tool-dot.test.tsx` | Create | ToolDot 渲染测试 |
| `frontend/src/components/ui/badge.tsx` | Modify | 新增 `success`/`warning` 变体 |
| `frontend/src/components/TitleBar.tsx` | Modify | 密度：h-9→h-8，页签/控件收紧，复用 TAB 常量 |
| `frontend/src/components/QuickSwitcher.tsx` | Modify | 输入 h-9→h-8、条目/分组标题收紧 |
| `frontend/src/components/ToolPicker.tsx` | Modify | 触发器/选项密度收紧（文案不变） |
| `frontend/src/pages/Home.tsx` | Modify | 网格/卡片密度、工具徽标换 ToolDot |
| `frontend/src/pages/WorkspaceTab.tsx` | Modify | 三栏 padding、中心区页签 h-8→h-7、终端页签工具徽标换 ToolDot |
| `frontend/src/components/SessionList.tsx` | Modify | 行密度、工具徽标换 ToolDot、运行态配色 |
| `frontend/src/components/BasketBar.tsx` | Modify | 容器/chips 密度 |
| `frontend/src/components/FileTree.tsx` | Modify | 行高、等宽、git 字母用状态 token |
| `frontend/src/components/Preview.tsx` | Modify | 代码区等宽密度 |
| `frontend/src/components/SshPanel.tsx` | Modify | 行/来源标签/输出密度与状态色 |
| `frontend/src/pages/Settings.tsx` | Modify | 分区/列表密度，「未验证」用 warning 变体 |

---

## Task 1: 设计 token + 共享样式常量

**Files:**
- Modify: `frontend/src/style.css`
- Create: `frontend/src/lib/ui.ts`

- [ ] **Step 1: 改 `style.css` 的 `@theme inline`（半径 + 状态色 + 等宽映射）**

把 `@theme inline` 块整体替换为：

```css
@theme inline {
  --color-background: var(--background);
  --color-foreground: var(--foreground);
  --color-card: var(--card);
  --color-card-foreground: var(--card-foreground);
  --color-muted: var(--muted);
  --color-muted-foreground: var(--muted-foreground);
  --color-border: var(--border);
  --color-input: var(--input);
  --color-primary: var(--primary);
  --color-primary-foreground: var(--primary-foreground);
  --color-secondary: var(--secondary);
  --color-secondary-foreground: var(--secondary-foreground);
  --color-accent: var(--accent);
  --color-accent-foreground: var(--accent-foreground);
  --color-destructive: var(--destructive);
  --color-destructive-foreground: var(--destructive-foreground);
  --color-success: var(--success);
  --color-warning: var(--warning);
  --color-danger: var(--destructive);
  --color-info: var(--primary);
  --color-ring: var(--ring);
  --font-mono: var(--font-mono-stack);
  --radius: var(--radius-base);
  --radius-xs: 2px;
  --radius-sm: 3px;
  --radius-md: 3px;
  --radius-lg: 3px;
  --radius-xl: 3px;
  --radius-2xl: 4px;
  --radius-3xl: 4px;
}
```

- [ ] **Step 2: 改浅色 `:root`（加 success/warning、等宽栈、半径 3px）**

把第一个 `:root` 块（浅色）替换为：

```css
:root {
  --background: #f4f6f8;  --foreground: #1f2328;
  --card: #ffffff;        --card-foreground: #1f2328;
  --muted: #eceff2;       --muted-foreground: #59636e;
  --border: #d8dee4;      --input: #d8dee4;
  --primary: #0969da;     --primary-foreground: #ffffff;
  --secondary: #eceff2;   --secondary-foreground: #1f2328;
  --accent: #ddf4ff;      --accent-foreground: #0969da;
  --destructive: #c93c37; --destructive-foreground: #ffffff;
  --success: #1a7f37;     --warning: #9a6700;
  --ring: #0969da99;
  --radius-base: 3px;
  --font-mono-stack: Consolas, "Cascadia Mono", "SFMono-Regular", ui-monospace, monospace;
}
```

- [ ] **Step 3: 改深色 `@media (prefers-color-scheme: dark)` 的 `:root`**

替换为：

```css
@media (prefers-color-scheme: dark) {
  :root {
    --background: #0d1117;  --foreground: #e6edf3;
    --card: #151b23;        --card-foreground: #e6edf3;
    --muted: #1f262e;       --muted-foreground: #8b949e;
    --border: #2d333b;      --input: #2d333b;
    --primary: #4493f8;     --primary-foreground: #0d1117;
    --secondary: #1f262e;   --secondary-foreground: #e6edf3;
    --accent: #101d2f;      --accent-foreground: #4493f8;
    --destructive: #e5534b; --destructive-foreground: #ffffff;
    --success: #3fb950;     --warning: #d29922;
    --ring: #4493f899;
  }
}
```

- [ ] **Step 4: 改 `body` 字号与行高**

在 `body {` 块内，把：

```css
  font-size: 13px;
```

改为：

```css
  font-size: 12.5px;
  line-height: 1.45;
```

- [ ] **Step 5: 新建 `frontend/src/lib/ui.ts`**

```ts
// 共享视觉常量（S1 C 视觉重塑）：把重复出现的页签/列表行/面板标题/等宽样式集中到一处。
// 抽取以「消除 3 处以上重复」为准——TitleBar、中心区页签、右栏子页签各写了一份 tab 样式。
export const TAB_BASE =
  'relative flex shrink-0 items-center gap-1.5 px-2.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground';
export const TAB_ACTIVE = 'font-medium text-foreground';
export const TAB_UNDERLINE = 'absolute inset-x-1.5 bottom-0 h-0.5 bg-primary';
export const LIST_ROW = 'rounded border border-border bg-card transition-colors hover:bg-muted/50';
export const LIST_ROW_ACTIVE = 'border-l-2 border-l-primary bg-primary/5';
export const PANE_HEADER = 'text-[11px] text-muted-foreground';
export const MONO = 'font-mono text-[11px] text-muted-foreground';
```

- [ ] **Step 6: 回归验证**

Run: `cd frontend; npm test`
Expected: 全部通过（纯 CSS + 新增未被引用的文件，不影响测试）。

Run: `cd frontend; npm run build`
Expected: `tsc && vite build` 成功，无类型错误。

- [ ] **Step 7（可选）提交**：`git add frontend/src/style.css frontend/src/lib/ui.ts && git commit -m "feat(frontend): C 视觉 token 与共享样式常量（S1 Task 1）"`

---

## Task 2: 工具色相 + ToolDot（TDD）

**Files:**
- Modify: `frontend/src/lib/toolBadge.ts`
- Modify: `frontend/src/lib/toolBadge.test.ts`
- Create: `frontend/src/components/ui/tool-dot.tsx`
- Create: `frontend/src/components/ui/tool-dot.test.tsx`

- [ ] **Step 1: 改测试为期望带 color（先红）**

把 `frontend/src/lib/toolBadge.test.ts` 整体替换为：

```ts
// 工具徽标映射测试。
import { describe, expect, it } from 'vitest';
import { badgeFor } from './toolBadge';

describe('badgeFor', () => {
  it.each([
    ['codebuddy', 'CodeBuddy', 'tool-badge--codebuddy', '#8957e5'],
    ['codex', 'Codex', 'tool-badge--codex', '#10a37f'],
    ['claude', 'Claude', 'tool-badge--claude', '#d97757'],
    ['gemini', 'Gemini', 'tool-badge--gemini', '#4285f4'],
  ])('已知工具 %s 映射展示名/配色/色相', (toolID, label, className, color) => {
    expect(badgeFor(toolID)).toEqual({ label, className, color });
  });

  it('大小写不敏感', () => {
    expect(badgeFor('Claude').label).toBe('Claude');
  });

  it('未知工具给中性色相与中性徽标，展示原始 ToolID', () => {
    expect(badgeFor('aider')).toEqual({
      label: 'aider',
      className: 'tool-badge--other',
      color: 'var(--muted-foreground)',
    });
  });

  it('空 ToolID 显示「未知」', () => {
    expect(badgeFor('').label).toBe('未知');
  });
});
```

- [ ] **Step 2: 运行确认失败**

Run: `cd frontend; npx vitest run src/lib/toolBadge.test.ts`
Expected: FAIL —— 返回对象缺少 `color`（`toEqual` 不匹配）。

- [ ] **Step 3: 实现 color**

把 `frontend/src/lib/toolBadge.ts` 整体替换为：

```ts
// 工具徽标映射：ToolID → 展示名、旧配色类（兼容调用点）与固定色相（C 视觉用色点）。
export interface ToolBadge {
  label: string;
  className: string;
  color: string;
}

// 未识别工具的中性色相（跟随 muted-foreground，深浅自动适配）
const OTHER_COLOR = 'var(--muted-foreground)';

const TOOL_BADGES: Record<string, ToolBadge> = {
  codebuddy: { label: 'CodeBuddy', className: 'tool-badge--codebuddy', color: '#8957e5' },
  codex: { label: 'Codex', className: 'tool-badge--codex', color: '#10a37f' },
  claude: { label: 'Claude', className: 'tool-badge--claude', color: '#d97757' },
  gemini: { label: 'Gemini', className: 'tool-badge--gemini', color: '#4285f4' },
};

export function badgeFor(toolID: string): ToolBadge {
  return (
    TOOL_BADGES[toolID.toLowerCase()] ?? {
      label: toolID || '未知',
      className: 'tool-badge--other',
      color: OTHER_COLOR,
    }
  );
}
```

- [ ] **Step 4: 运行确认通过**

Run: `cd frontend; npx vitest run src/lib/toolBadge.test.ts`
Expected: PASS。

- [ ] **Step 5: 新建 `frontend/src/components/ui/tool-dot.tsx`**

```tsx
// 工具色点徽标（S1）：固定色相小圆点 + 工具名。
// 替代会话行/首页卡片/终端页签里的纯色 Badge，让多工具一眼可辨。
import { badgeFor } from '../../lib/toolBadge';
import { cn } from '../../lib/cn';

export function ToolDot({ toolID, className }: { toolID: string; className?: string }) {
  const badge = badgeFor(toolID);
  return (
    <span className={cn('inline-flex min-w-0 items-center gap-1 text-[11px] text-muted-foreground', className)}>
      <span
        aria-hidden="true"
        className="h-1.5 w-1.5 shrink-0 rounded-full"
        style={{ background: badge.color }}
      />
      <span className="truncate">{badge.label}</span>
    </span>
  );
}
```

- [ ] **Step 6: 新建 `frontend/src/components/ui/tool-dot.test.tsx`**

```tsx
import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ToolDot } from './tool-dot';

describe('ToolDot', () => {
  it('渲染工具名与色点', () => {
    const { container } = render(<ToolDot toolID="claude" />);
    expect(screen.getByText('Claude')).toBeInTheDocument();
    expect(container.querySelector('span[aria-hidden="true"]')).not.toBeNull();
  });

  it('未知工具回退展示原始 ToolID', () => {
    render(<ToolDot toolID="aider" />);
    expect(screen.getByText('aider')).toBeInTheDocument();
  });
});
```

- [ ] **Step 7: 回归验证**

Run: `cd frontend; npm test`
Expected: 全绿。

- [ ] **Step 8（可选）提交**：`git add frontend/src/lib/toolBadge.ts frontend/src/lib/toolBadge.test.ts frontend/src/components/ui/tool-dot.tsx frontend/src/components/ui/tool-dot.test.tsx && git commit -m "feat(frontend): 工具色相与 ToolDot 徽标（S1 Task 2）"`

---

## Task 3: Badge 变体 + 壳层（TitleBar / QuickSwitcher / ToolPicker）

**Files:**
- Modify: `frontend/src/components/ui/badge.tsx`
- Modify: `frontend/src/components/TitleBar.tsx`
- Modify: `frontend/src/components/QuickSwitcher.tsx`
- Modify: `frontend/src/components/ToolPicker.tsx`

- [ ] **Step 1: `badge.tsx` 新增 `success` / `warning` 变体**

把 `badgeVariants` 的 `variant` 对象替换为：

```tsx
      variant: {
        default: 'bg-accent text-accent-foreground',
        muted: 'bg-muted text-muted-foreground',
        outline: 'border text-foreground',
        destructive: 'bg-destructive text-destructive-foreground',
        success: 'bg-success/15 text-success',
        warning: 'bg-warning/15 text-warning',
      },
```

- [ ] **Step 2: `TitleBar.tsx` 密度 + 复用 TAB 常量**

顶部 import 增加：

```tsx
import { TAB_ACTIVE, TAB_BASE, TAB_UNDERLINE } from '../lib/ui';
```

把两行常量声明：

```tsx
// 页签基础态：扁平下划线激活态，不做胶囊填充
const tabBase =
  'kshell-no-drag relative flex h-9 max-w-44 shrink-0 items-center gap-2.5 px-2.5 text-[13px] text-muted-foreground transition-colors hover:bg-muted hover:text-foreground';
const tabActive = 'font-medium text-foreground';
```

> 注：原文为 `gap-1 px-2.5 text-[13px]`（无 gap-2.5）；以文件实际内容为准，只把高度与字号改为下值。

替换为：

```tsx
// 页签基础态：扁平下划线激活态，不做胶囊填充（TAB_BASE 共享自 lib/ui）
const tabBase = `kshell-no-drag ${TAB_BASE} h-7 max-w-44`;
const tabActive = TAB_ACTIVE;
```

把 `controlBase` 声明：

```tsx
const controlBase =
  'kshell-no-drag flex h-9 w-11 items-center justify-center text-muted-foreground transition-colors hover:bg-muted hover:text-foreground';
```

替换为：

```tsx
const controlBase =
  'kshell-no-drag flex h-8 w-10 items-center justify-center text-muted-foreground transition-colors hover:bg-muted hover:text-foreground';
```

把 `<header ...>` 的 class：

```tsx
      className="kshell-drag flex h-9 shrink-0 items-stretch border-b border-border bg-card pl-2"
```

替换为：

```tsx
      className="kshell-drag flex h-8 shrink-0 items-stretch border-b border-border bg-card pl-2"
```

把三处激活下划线 `<span className="absolute inset-x-1.5 bottom-0 h-0.5 bg-primary" />`（首页、工作区页签、设置）统一替换为 `<span className={TAB_UNDERLINE} />`。

- [ ] **Step 3: `QuickSwitcher.tsx` 密度**

把输入框 class：

```tsx
            className="h-9 w-full rounded-md border border-input bg-card px-2.5 text-sm text-foreground outline-none placeholder:text-muted-foreground focus-visible:ring-2 focus-visible:ring-ring"
```

替换为：

```tsx
            className="h-8 w-full rounded border border-input bg-card px-2.5 text-xs text-foreground outline-none placeholder:text-muted-foreground focus-visible:ring-2 focus-visible:ring-ring"
```

把两处分组标题：

```tsx
                  <p className="px-1 py-1 text-xs text-muted-foreground">已打开的页签</p>
```

```tsx
                  <p className="px-1 py-1 text-xs text-muted-foreground">工作区</p>
```

替换为（用 PANE_HEADER，需在文件顶部 `import { PANE_HEADER } from '../lib/ui';`）：

```tsx
                  <p className={`px-1 py-1 ${PANE_HEADER}`}>已打开的页签</p>
```

```tsx
                  <p className={`px-1 py-1 ${PANE_HEADER}`}>工作区</p>
```

把条目按钮 class：

```tsx
          'flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm transition-colors',
```

替换为：

```tsx
          'flex w-full items-center gap-1.5 rounded px-2 py-1 text-left text-xs transition-colors',
```

- [ ] **Step 4: `ToolPicker.tsx` 选项行密度（文案与结构不变）**

把选项按钮共用 class 中的 `px-2 py-1 text-left text-xs` 保持；仅把触发器/面板圆角统一为 `rounded`（3px）。把面板：

```tsx
          className="absolute left-0 top-full z-20 mt-1 w-44 rounded border border-border bg-card py-0.5"
```

保持不变（已是 `rounded`）。触发器保持 `h-7 ... rounded`。**本步仅确认无需改动**；若触发器仍是 `rounded` 则跳过。

> 说明：ToolPicker 文案（如 `工具：Claude Code`）与 role 结构被 `ToolPicker.test.tsx` 依赖，**严禁改动文案与 role/aria**。

- [ ] **Step 5: 回归验证**

Run: `cd frontend; npm test`
Expected: 全绿（`ToolPicker.test.tsx`、`ToolPicker` 相关查询不受影响）。

Run: `cd frontend; npm run build`
Expected: 成功。

- [ ] **Step 6（可选）提交**：`git add frontend/src/components/ui/badge.tsx frontend/src/components/TitleBar.tsx frontend/src/components/QuickSwitcher.tsx && git commit -m "feat(frontend): Badge 变体与壳层密度（S1 Task 3）"`

---

## Task 4: 首页（Home）

**Files:**
- Modify: `frontend/src/pages/Home.tsx`

- [ ] **Step 1: 网格与卡片密度 + 工具徽标换 ToolDot**

import 增加：

```tsx
import { ToolDot } from '../components/ui/tool-dot';
```

把网格常量：

```tsx
const GRID = 'grid gap-3 grid-cols-[repeat(auto-fill,minmax(240px,1fr))]';
```

替换为：

```tsx
const GRID = 'grid gap-2.5 grid-cols-[repeat(auto-fill,minmax(220px,1fr))]';
```

把卡片按钮 class：

```tsx
                  className="flex h-full w-full min-w-0 flex-col gap-1.5 rounded border border-border bg-card px-3 py-2.5 text-left transition-colors hover:bg-muted"
```

替换为：

```tsx
                  className="flex h-full w-full min-w-0 flex-col gap-1 rounded border border-border bg-card px-2.5 py-2 text-left transition-colors hover:bg-muted"
```

把名称 span：

```tsx
                    <span className="min-w-0 truncate text-sm font-medium">{ws.Name}</span>
```

替换为：

```tsx
                    <span className="min-w-0 truncate text-[12.5px] font-medium">{ws.Name}</span>
```

把工具分布渲染块（`shown.map`）：

```tsx
                      {shown.map(([toolID]) => (
                        <Badge key={toolID} variant="muted">
                          {badgeFor(toolID).label}
                        </Badge>
                      ))}
```

替换为：

```tsx
                      {shown.map(([toolID]) => (
                        <ToolDot key={toolID} toolID={toolID} />
                      ))}
```

> `Badge` 仍用于 git 徽标（`<Badge variant="outline">git</Badge>`），保留 import；`badgeFor` 若不再被引用则移除其 import（检查：替换后 Home 只剩 `<Badge variant="outline">git</Badge>`，`badgeFor` 不再使用 → 删除 `import { badgeFor } from '../lib/toolBadge';`）。

- [ ] **Step 2: 回归验证**

Run: `cd frontend; npm test`
Expected: 全绿（Home 测试按文本/角色查询工作区名与「个会话」，不受影响）。

- [ ] **Step 3（可选）提交**：`git add frontend/src/pages/Home.tsx && git commit -m "feat(frontend): 首页卡片密度与工具色点（S1 Task 4）"`

---

## Task 5: 工作区三栏（WorkspaceTab）

**Files:**
- Modify: `frontend/src/pages/WorkspaceTab.tsx`

- [ ] **Step 1: 共享页签常量 + 中心区 h-8→h-7**

import 增加：

```tsx
import { TAB_ACTIVE, TAB_BASE, TAB_UNDERLINE } from '../lib/ui';
import { ToolDot } from '../components/ui/tool-dot';
```

把中心区页签常量：

```tsx
const centerTabBase =
  'group relative flex h-8 max-w-56 shrink-0 items-center gap-1.5 px-2.5 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground';
const centerTabActive = 'font-medium text-foreground';
```

替换为：

```tsx
const centerTabBase = `group ${TAB_BASE} h-7 max-w-56 text-xs`;
const centerTabActive = TAB_ACTIVE;
```

- [ ] **Step 2: 左/右栏 padding 收紧**

左栏 aside：

```tsx
        className="flex shrink-0 flex-col gap-2 overflow-y-auto border-r border-border bg-card p-3"
```

→

```tsx
        className="flex shrink-0 flex-col gap-2 overflow-y-auto border-r border-border bg-card p-2.5"
```

右栏 aside：

```tsx
        className="flex shrink-0 flex-col overflow-y-auto border-l border-border bg-card p-3"
```

→

```tsx
        className="flex shrink-0 flex-col overflow-y-auto border-l border-border bg-card p-2.5"
```

- [ ] **Step 3: 终端页签工具徽标换 ToolDot**

把：

```tsx
                {t.ToolID && (
                  <span className="shrink-0 text-[10px] text-muted-foreground">{badge.label}</span>
                )}
```

替换为：

```tsx
                {t.ToolID && <ToolDot toolID={t.ToolID} className="shrink-0" />}
```

> `const badge = badgeFor(t.ToolID);` 仍用于按钮 `title`，保留；`badgeFor` import 不动。

- [ ] **Step 4: 三处激活下划线用 TAB_UNDERLINE**

把中心区「预览」与终端页签的：

```tsx
              <span className="absolute inset-x-2 bottom-0 h-0.5 bg-primary" />
```

替换为：

```tsx
              <span className={TAB_UNDERLINE} />
```

- [ ] **Step 5: 回归验证**

Run: `cd frontend; npm test`
Expected: 全绿（`WorkspaceTab.test.tsx` 依赖 ToolPicker 选项名与中心区 role=tab，不受影响）。

- [ ] **Step 6（可选）提交**：`git add frontend/src/pages/WorkspaceTab.tsx && git commit -m "feat(frontend): 工作区三栏密度与页签统一（S1 Task 5）"`

---

## Task 6: 会话列表（SessionList）+ 上下文篮（BasketBar）

**Files:**
- Modify: `frontend/src/components/SessionList.tsx`
- Modify: `frontend/src/components/BasketBar.tsx`

- [ ] **Step 1: SessionList 行密度与工具色点**

import 增加：

```tsx
import { ToolDot } from './ui/tool-dot';
```

（`Badge` 若替换后不再使用则删除其 import——替换后本文件不再用 `Badge`。）

把行 li 的 class：

```tsx
                className={cn(
                  'rounded border border-border bg-card p-2.5 transition-colors',
                  running && 'border-l-2 border-l-primary bg-primary/5',
                )}
```

替换为：

```tsx
                className={cn(
                  'rounded border border-border bg-card p-2 transition-colors hover:bg-muted/50',
                  running && 'border-l-2 border-l-primary bg-primary/5',
                )}
```

把工具徽标：

```tsx
                    <Badge
                      variant={badge.className === 'tool-badge--other' ? 'muted' : 'default'}
                    >
                      {badge.label}
                    </Badge>
```

替换为：

```tsx
                    <ToolDot toolID={s.ToolID} className="shrink-0" />
```

> 保留 `const badge = badgeFor(s.ToolID);`（仍用于 Tooltip 元信息）。

把运行中标记（保持文本 `✓` 与 `title="运行中"`，仅改配色）：

```tsx
                      <span className="shrink-0 text-xs text-primary" title="运行中">
                        ✓
                      </span>
```

替换为：

```tsx
                      <span className="shrink-0 text-xs text-success" title="运行中">
                        ✓
                      </span>
```

> 工具筛选 chip 的 `chipClass`（含 `rounded-sm` / `bg-primary/10` / `text-primary`）被 `SessionList.test.tsx` 断言，**不得改动**。

- [ ] **Step 2: BasketBar 密度**

把容器 class：

```tsx
    <div className="mx-3 mt-3 mb-2 flex flex-wrap items-center gap-2 rounded border border-border bg-card px-2.5 py-1.5">
```

替换为：

```tsx
    <div className="mx-2.5 mt-2 mb-1.5 flex flex-wrap items-center gap-1.5 rounded border border-border bg-card px-2 py-1">
```

把标题 span：

```tsx
      <span className="whitespace-nowrap text-xs text-muted-foreground">
        上下文篮（{basket.length}/{maxBasket}）
      </span>
```

替换为：

```tsx
      <span className="whitespace-nowrap text-[11px] text-muted-foreground">
        上下文篮（{basket.length}/{maxBasket}）
      </span>
```

- [ ] **Step 3: 回归验证**

Run: `cd frontend; npm test`
Expected: 全绿。重点核对 `SessionList.test.tsx`（chip 类名断言、运行态 `bg-primary/5` 断言、无标题 `italic` 断言）与 `BasketBar.test.tsx`（文案断言）。

- [ ] **Step 4（可选）提交**：`git add frontend/src/components/SessionList.tsx frontend/src/components/BasketBar.tsx && git commit -m "feat(frontend): 会话列表与篮子密度（S1 Task 6）"`

---

## Task 7: 文件树（FileTree）+ 预览（Preview）

**Files:**
- Modify: `frontend/src/components/FileTree.tsx`
- Modify: `frontend/src/components/Preview.tsx`

- [ ] **Step 1: FileTree 行高 / 等宽 / git 状态 token**

把 git 徽标配色表：

```tsx
// git 状态小色标（VS Code 风格配色）
const GIT_BADGES: Record<string, { label: string; cls: string; title: string }> = {
  modified: { label: 'M', cls: 'text-orange-500', title: '已修改' },
  added: { label: 'A', cls: 'text-green-600', title: '新增（已暂存）' },
  deleted: { label: 'D', cls: 'text-red-500', title: '已删除' },
  renamed: { label: 'R', cls: 'text-sky-500', title: '重命名' },
  untracked: { label: 'U', cls: 'text-green-500', title: '未跟踪' },
  conflicted: { label: '!', cls: 'text-red-600', title: '合并冲突' },
};
```

替换为（改用语义状态 token，label/title 不变）：

```tsx
// git 状态小色标（S1：改用语义状态色，label/title 与既有测试一致）
const GIT_BADGES: Record<string, { label: string; cls: string; title: string }> = {
  modified: { label: 'M', cls: 'text-warning', title: '已修改' },
  added: { label: 'A', cls: 'text-success', title: '新增（已暂存）' },
  deleted: { label: 'D', cls: 'text-danger', title: '已删除' },
  renamed: { label: 'R', cls: 'text-info', title: '重命名' },
  untracked: { label: 'U', cls: 'text-success', title: '未跟踪' },
  conflicted: { label: '!', cls: 'text-danger', title: '合并冲突' },
};
```

把 `GitBadge` 的 span class 加等宽：

```tsx
      className={cn('shrink-0 text-xs font-bold', b.cls)}
```

→

```tsx
      className={cn('shrink-0 font-mono text-[11px] font-bold', b.cls)}
```

把树行容器：

```tsx
        className="group flex h-7 items-center gap-0.5 rounded pr-1 transition-colors hover:bg-muted"
```

→

```tsx
        className="group flex h-6 items-center gap-0.5 rounded pr-1 transition-colors hover:bg-muted"
```

把文件名 span：

```tsx
              <span
                className={cn(
                  'min-w-0 truncate text-sm',
                  node.IsDir ? 'font-medium' : 'text-foreground/90',
                )}
              >
```

→

```tsx
              <span
                className={cn(
                  'min-w-0 truncate font-mono text-xs',
                  node.IsDir ? 'font-medium' : 'text-foreground/90',
                )}
              >
```

把重命名输入框：

```tsx
            className="min-w-0 flex-1 rounded border border-primary bg-background px-1 py-0.5 text-sm outline-none"
```

→

```tsx
            className="min-w-0 flex-1 rounded border border-primary bg-background px-1 py-0.5 font-mono text-xs outline-none"
```

把搜索框（约第 518 行）：

```tsx
          className="min-w-0 flex-1 rounded-md border border-border bg-background px-2 py-1 text-sm outline-none placeholder:text-muted-foreground focus:border-primary"
```

→

```tsx
          className="min-w-0 flex-1 rounded border border-border bg-background px-2 py-1 text-xs outline-none placeholder:text-muted-foreground focus:border-primary"
```

- [ ] **Step 2: Preview 代码区等宽密度**

编辑器 textarea：

```tsx
          className="mt-2 min-h-[240px] w-full flex-1 resize-y overflow-auto rounded-md border border-border bg-card p-3 font-mono text-[13px] leading-relaxed outline-none focus:border-primary"
```

→

```tsx
          className="mt-2 min-h-[240px] w-full flex-1 resize-y overflow-auto rounded border border-border bg-card p-2.5 font-mono text-xs leading-[1.55] outline-none focus:border-primary"
```

预览 pre：

```tsx
          <pre className="overflow-auto rounded-md border border-border bg-card p-3 font-mono text-[13px] leading-relaxed whitespace-pre">
```

→

```tsx
          <pre className="overflow-auto rounded border border-border bg-card p-2.5 font-mono text-xs leading-[1.55] whitespace-pre">
```

- [ ] **Step 3: 回归验证**

Run: `cd frontend; npm test`
Expected: 全绿。重点核对 `FileTree.test.tsx`（`findByText('M')`、`getByTitle('git：已修改')`、`queryByText('U')`、重命名/篮子/搜索用例）。

- [ ] **Step 4（可选）提交**：`git add frontend/src/components/FileTree.tsx frontend/src/components/Preview.tsx && git commit -m "feat(frontend): 文件树与预览等宽密度（S1 Task 7）"`

---

## Task 8: SSH 面板（SshPanel）+ 设置页（Settings）

**Files:**
- Modify: `frontend/src/components/SshPanel.tsx`
- Modify: `frontend/src/pages/Settings.tsx`

- [ ] **Step 1: SshPanel 行/来源/输出密度与状态色**

连接行 li：

```tsx
                className={cn(
                  'rounded-lg border border-border bg-card px-2.5 py-2 transition-colors',
                  open
                    ? 'border-l-2 border-l-primary bg-primary/5'
                    : c.ID === selectedId && 'bg-muted/60',
                )}
```

→

```tsx
                className={cn(
                  'rounded border border-border bg-card px-2.5 py-1.5 transition-colors',
                  open
                    ? 'border-l-2 border-l-primary bg-primary/5'
                    : c.ID === selectedId && 'bg-muted/60',
                )}
```

> 保留 `bg-primary/5`（`SshPanel.test.tsx` 断言该 class）。

来源标签 span：

```tsx
                  <span
                    className="shrink-0 rounded-sm border border-border px-1.5 py-px"
                    title={c.SourceFile || undefined}
                  >
                    {sourceLabel(c.Source)}
                  </span>
```

→

```tsx
                  <span
                    className="shrink-0 rounded-sm border border-border px-1.5 py-px font-mono text-[10px]"
                    title={c.SourceFile || undefined}
                  >
                    {sourceLabel(c.Source)}
                  </span>
```

执行输入框：

```tsx
              className="h-8 min-w-0 flex-1 rounded-md border border-input bg-card px-2.5 text-sm text-foreground outline-none placeholder:text-muted-foreground focus-visible:ring-2 focus-visible:ring-ring"
```

→

```tsx
              className="h-7 min-w-0 flex-1 rounded border border-input bg-card px-2.5 text-xs text-foreground outline-none placeholder:text-muted-foreground focus-visible:ring-2 focus-visible:ring-ring"
```

Verified 勾：

```tsx
                  {c.Verified && <span className="shrink-0 text-emerald-500">✓</span>}
```

→

```tsx
                  {c.Verified && <span className="shrink-0 text-success">✓</span>}
```

输出区两处 `pre`：

```tsx
                className="max-h-80 overflow-auto rounded-md bg-muted p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap"
```

→

```tsx
                className="max-h-80 overflow-auto rounded bg-muted p-2.5 font-mono text-xs leading-[1.5] whitespace-pre-wrap"
```

```tsx
                  className="max-h-80 overflow-auto rounded-md bg-muted p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap text-destructive/90"
```

→

```tsx
                  className="max-h-80 overflow-auto rounded bg-muted p-2.5 font-mono text-xs leading-[1.5] whitespace-pre-wrap text-destructive/90"
```

Skeleton 圆角（3 处）：

```tsx
        <Skeleton className="h-12 rounded-lg border border-border" />
```

→ 三处统一改为（`rounded` 由 token 收 3px）：

```tsx
        <Skeleton className="h-12 rounded border border-border" />
```

- [ ] **Step 2: Settings 分区/列表密度 + 未验证用 warning 变体**

第一个 section：

```tsx
        <section className="mb-6 rounded-lg border border-border bg-card p-4">
```

→

```tsx
        <section className="mb-5 rounded border border-border bg-card p-3.5">
```

第二个 section：

```tsx
        <section className="rounded-lg border border-border bg-card p-4">
```

→

```tsx
        <section className="rounded border border-border bg-card p-3.5">
```

工具列表 ul：

```tsx
            <ul className="divide-y divide-border rounded-md border border-border">
```

→

```tsx
            <ul className="divide-y divide-border rounded border border-border">
```

列表行 li：

```tsx
                    'flex items-center gap-2 px-3 py-2 text-sm transition-colors hover:bg-muted',
```

→

```tsx
                    'flex items-center gap-2 px-2.5 py-1.5 text-xs transition-colors hover:bg-muted',
```

「未验证」Badge：

```tsx
                    <Badge
                      variant="outline"
                      className="border-amber-500/50 text-amber-600 dark:text-amber-400"
                    >
                      未验证
                    </Badge>
```

→

```tsx
                    <Badge variant="warning">未验证</Badge>
```

yaml 编辑器圆角：

```tsx
                className="min-h-[280px] w-full resize-y rounded-md border border-input bg-card p-3 font-mono text-xs leading-relaxed text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
```

→

```tsx
                className="min-h-[280px] w-full resize-y rounded border border-input bg-card p-2.5 font-mono text-xs leading-[1.55] text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
```

- [ ] **Step 3: 回归验证**

Run: `cd frontend; npm test`
Expected: 全绿。重点核对 `SshPanel.test.tsx`（`bg-primary/5`）与 `Settings.test.tsx`（`opacity-50` 与「未验证」文本）。

- [ ] **Step 4（可选）提交**：`git add frontend/src/components/SshPanel.tsx frontend/src/pages/Settings.tsx && git commit -m "feat(frontend): SSH 与设置页密度与状态色（S1 Task 8）"`

---

## Task 9: 全量回归 + 构建 + 人工冒烟

**Files:** 无（验证任务）

- [ ] **Step 1: 静态检查**

Run: `go vet ./...`（在仓库根）
Expected: 无输出（S1 未动 Go，应保持通过）。

- [ ] **Step 2: 前端全量测试**

Run: `cd frontend; npm test`
Expected: 全绿（基线 100+ 用例）。

- [ ] **Step 3: 前端构建**

Run: `cd frontend; npm run build`
Expected: `tsc` 无类型错误，`vite build` 产出 `frontend/dist/`。

- [ ] **Step 4: 桌面版构建**

Run: `.\build.ps1 -Desktop`（PowerShell；wails 会请求旧实例退出）
Expected: 产出桌面版可执行文件（路径以 `build.ps1` 输出为准）。

- [ ] **Step 5: 人工冒烟（浅/深两态）**

按 `docs/smoke-test-desktop.md` 检查，并补充确认：
1. 标题栏 h-8、页签 h-7、窗口控件 32×40；双击空白最大化仍可用。
2. 首页卡片、会话行、文件树、预览、SSH、设置均为 3px 圆角、无阴影、密度收紧。
3. 工具徽标显示为「色点 + 名称」（Claude 橙 / Codex 绿 / Gemini 蓝）。
4. 文件树 git 字母配色（M 琥珀 / A·U 绿 / D·! 红 / R 蓝）。
5. 预览代码区为等宽字体、行高 1.55；中文不糊。
6. 切换系统浅/深色，两态 token 正确（状态色、边框、文字对比度）。
7. 行为无回归：恢复/新建会话、终端、文件重命名/篮子、SSH 执行、设置保存/重启。

- [ ] **Step 6: 差异复核**

Run: `git diff --stat`
Expected: 仅出现 `frontend/src/**`（含 `style.css`）改动；无 Go/绑定产物的意外改动。

- [ ] **Step 7（可选）最终提交**：按用户当次授权，可用 `feat(frontend): C 高密度工具感视觉底座（S1）`。

---

## 自查记录（Self-Review）

- **Spec 覆盖**：设计文档 §2 token → Task 1；§3 共享样式与 ToolDot/toolBadge → Task 1/2；§4 逐页清单逐行对应 Task 3–8；§6 验证 → Task 9。无缺口。
- **占位符扫描**：无 TBD/TODO；所有代码步骤含完整替换内容。
- **类型一致性**：`badgeFor` 返回类型在 Task 2 定义 `{ label, className, color }`，Task 4/5/6 使用 `ToolDot`（内部调 `badgeFor`）一致；`TAB_BASE/TAB_ACTIVE/TAB_UNDERLINE/LIST_ROW_LIST…` 常量名在 Task 1 定义、Task 3/5 引用一致（`LIST_ROW`/`LIST_ROW_ACTIVE` 定义但按当前实现 Task 6 使用字面类以保留测试断言——如需引用可后续统一，不影响正确性）。
- **风险提示**：Task 6 未使用 `LIST_ROW` 常量（改回字面类）以严格保留 `bg-primary/5` 与既有断言；`MONO` 常量在计划中定义但仅按需使用（避免给中文套等宽）。
