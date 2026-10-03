# UI 美观度优化实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 按已确认设计（`2026-10-03-ui-polish-design.md`）完成 token 层换 Teal、基础组件收敛、视觉提升与动效。

**Architecture:** 全部改动限于 `frontend/src`：style.css token 层 → lib 层（toolBadge/appearance/ui 常量）→ components/ui 新增 input+dialog → 调用点收编 → 页面级视觉提升。样式改动用 `npm run build` + 现有测试验证；逻辑改动严格 TDD。

**Tech Stack:** React 19 + Tailwind v4 (CSS-first) + Radix + vitest/jsdom + cva。

**基线：** 23 测试文件 / 229 用例全绿（worktree 内已验证）。

---

### Task 1: Token 层改造（style.css）

**Files:**
- Modify: `frontend/src/style.css`

**Step 1: 改写主题 token**

亮色块（`style.css:41-56`）替换为（Teal + 青调中性色）：

```css
:root,
[data-theme="light"] {
  color-scheme: light;
  --background: #f3f7f6;  --foreground: #1c2a27;
  --card: #ffffff;        --card-foreground: #1c2a27;
  --muted: #e6ecea;       --muted-foreground: #5a6b66;
  --border: #d7e0dd;      --input: #c9d5d1;
  --primary: #0d9488;     --primary-hover: #0f766e;  --primary-foreground: #ffffff;
  --secondary: #e6ecea;   --secondary-foreground: #1c2a27;
  --accent: #ccfbf1;      --accent-foreground: #0f766e;
  --destructive: #c93c37; --destructive-foreground: #ffffff;
  --success: #1a7f37;     --warning: #9a6700;
  --ring: #0d948866;
  --overlay: rgba(23, 32, 30, 0.4);
  --shadow-card: 0 1px 3px rgba(15, 35, 30, 0.08), 0 4px 12px rgba(15, 35, 30, 0.06);
  --radius-base: 3px;
  --font-mono-stack: Consolas, "Cascadia Mono", "SFMono-Regular", ui-monospace, monospace;
  --tool-codebuddy: #6f42c1; --tool-codex: #0c8a6c; --tool-claude: #bc4c25;
  --tool-gemini: #1a73e8;   --tool-opencode: #0d8f80;
}
```

暗色块（`style.css:58-70`）替换为：

```css
[data-theme="dark"] {
  color-scheme: dark;
  --background: #0c1310;  --foreground: #dce8e4;
  --card: #131b18;        --card-foreground: #dce8e4;
  --muted: #1c2622;       --muted-foreground: #8ba39c;
  --border: #2a3733;      --input: #2a3733;
  --primary: #2dd4bf;     --primary-hover: #5eead4;  --primary-foreground: #04201c;
  --secondary: #1c2622;   --secondary-foreground: #dce8e4;
  --accent: #0e2521;      --accent-foreground: #5eead4;
  --destructive: #e5534b; --destructive-foreground: #ffffff;
  --success: #3fb950;     --warning: #d29922;
  --ring: #2dd4bf99;
  --overlay: rgba(2, 6, 5, 0.6);
  --shadow-card: 0 1px 3px rgba(0, 0, 0, 0.4), 0 4px 12px rgba(0, 0, 0, 0.3);
  --tool-codebuddy: #a371f7; --tool-codex: #26a583; --tool-claude: #e8875f;
  --tool-gemini: #6ba4f7;   --tool-opencode: #2cc8b4;
}
```

**Step 2: @theme inline 增加映射**（`style.css:7-38` 内追加）：

```css
  --color-primary-hover: var(--primary-hover);
```

**Step 3: 动效 token 与 reduced-motion**（`:root` 之外、暗色块后追加）：

```css
:root {
  --duration-fast: 120ms;
  --duration-base: 180ms;
  --ease-out: cubic-bezier(0.16, 1, 0.3, 1);
}

@media (prefers-reduced-motion: reduce) {
  *,
  ::before,
  ::after {
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: 0.01ms !important;
  }
}
```

**Step 4: 密度放宽**（`style.css:82-83`）：`font-size: 13px; line-height: 1.5;`

**Step 5: 验证 + 提交**

Run: `cd frontend && npm test && npm run build` → 全绿。
```bash
git add frontend/src/style.css && git commit -m "feat(ui): token 层换青绿主色并新增遮罩/阴影/动效 token"
```

### Task 2: 工具品牌色入 token（TDD）

**Files:**
- Test: `frontend/src/lib/toolBadge.test.ts`（先改）
- Modify: `frontend/src/lib/toolBadge.ts:11-17`

**Step 1: 改测试期望为 CSS 变量**：`color` 期望值改 `var(--tool-codebuddy)` 等（未知工具仍是 `var(--muted-foreground)`）。

**Step 2: 跑测试确认红**：`npm test -- toolBadge` → FAIL。

**Step 3: 实现**：`TOOL_BADGES` 各 `color` 字段改为 `'var(--tool-xxx)'`。

**Step 4: 绿** → **Step 5: 提交** `fix(ui): 工具品牌色接入亮暗 token`

### Task 3: xterm 主题从 token 派生（TDD）

**Files:**
- Test: `frontend/src/lib/appearance.test.ts`（先改）
- Modify: `frontend/src/lib/appearance.ts`

**Step 1: 改测试**：`terminalTheme(theme)` 在 jsdom（getComputedStyle 拿不到值）下返回回退常量，且回退值与 token 同源（`#131b18`/`#dce8e4`、`#ffffff`/`#1c2a27`）；新增「显式传入 getter 时用 getter 返回值」的用例。

**Step 2: 红**（`npm test -- appearance`）。

**Step 3: 实现**：

```ts
function cssVar(name: string): string | null {
  try {
    return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || null;
  } catch { return null; }
}

export function terminalTheme(theme: ResolvedTheme, resolve: (n: string) => string | null = cssVar) {
  const bg = resolve('--card');
  const fg = resolve('--foreground');
  if (bg && fg) return { background: bg, foreground: fg };
  return theme === 'dark'
    ? { background: '#131b18', foreground: '#dce8e4' }
    : { background: '#ffffff', foreground: '#1c2a27' };
}
```

**Step 4: 绿** → **Step 5: 提交** `fix(ui): xterm 主题色从 token 派生`

### Task 4: 新增 input.tsx 并收编 4 处输入框

**Files:**
- Create: `frontend/src/components/ui/input.tsx`
- Test: `frontend/src/components/ui/input.test.tsx`
- Modify: `QuickSwitcher.tsx:123`、`WorkspaceSearch.tsx:24`、`SshPanel.tsx:212`、`FileTree.tsx:466`、`Preview.tsx:169`

**Step 1: 写失败测试**（渲染输出含 `focus-visible:ring-2`、`border-input`）。

**Step 2: 红**。

**Step 3: 实现**：

```tsx
import { cva, type VariantProps } from 'class-variance-authority';
import type { ComponentProps } from 'react';
import { cn } from '../../lib/cn';

const inputVariants = cva(
  'rounded-[3px] border border-input bg-card text-foreground placeholder:text-muted-foreground transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50',
  {
    variants: {
      size: { default: 'h-8 px-2 text-[13px]', sm: 'h-7 px-2 text-xs' },
    },
    defaultVariants: { size: 'default' },
  },
);

export function Input({ className, size, ...props }: ComponentProps<'input'> & VariantProps<typeof inputVariants>) {
  return <input className={cn(inputVariants({ size }), className)} {...props} />;
}
```

**Step 4: 绿** → **Step 5: 五处调用点替换**为 `<Input>`（原 class 保留必要的布局类如 w-full/flex-1；FileTree 行内重命名用 `size="sm"`）。同时清理各处残留 `focus:border-primary`。

**Step 6: 全量测试 + 提交** `refactor(ui): 统一输入框为 Input 组件`

### Task 5: 新增 dialog.tsx 并收编 3 处弹层

**Files:**
- Create: `frontend/src/components/ui/dialog.tsx`
- Test: `frontend/src/components/ui/dialog.test.tsx`
- Modify: `Home.tsx:224-268`（回收站）、`QuickSwitcher.tsx`（弹层壳）、`ChatView.tsx:148-171`（权限弹窗）

**Step 1: 写失败测试**：Esc 触发 onOpenChange(false)；遮罩点击关闭；内容渲染。

**Step 2: 红**。

**Step 3: 实现**（Radix Dialog 薄封装）：

```tsx
import * as DialogPrimitive from '@radix-ui/react-dialog';
import type { ReactNode } from 'react';
import { cn } from '../../lib/cn';

export function Dialog({ open, onOpenChange, children, className }: {
  open: boolean; onOpenChange: (open: boolean) => void; children: ReactNode; className?: string;
}) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay
          className="fixed inset-0 bg-[var(--overlay)] backdrop-blur-[2px]"
          style={{ animation: `kshell-fade-in var(--duration-base) var(--ease-out)` }}
        />
        <DialogPrimitive.Content
          className={cn(
            'fixed left-1/2 top-[18%] z-50 -translate-x-1/2 rounded-md border border-border bg-card p-4 shadow-lg',
            className,
          )}
          style={{ animation: `kshell-pop-in var(--duration-base) var(--ease-out)` }}
        >
          {children}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
```

配套在 `style.css` 追加 keyframes：

```css
@keyframes kshell-fade-in { from { opacity: 0; } }
@keyframes kshell-pop-in { from { opacity: 0; transform: translateX(-50%) scale(0.98); } }
```

**Step 4: 绿** → **Step 5: 三处调用点替换**（Home 回收站删手拼 `DialogPrimitive`；QuickSwitcher 弹层壳换 `<Dialog>`；ChatView 权限弹窗从纯 div 换 `<Dialog>`，获得 Esc 关闭与焦点圈定）。

**Step 6: 验收扫描 + 提交**：`grep -rn "bg-black/" frontend/src` 与 `bg-foreground/25` 应为 0 处。提交 `refactor(ui): 统一弹层为 Dialog 组件`

### Task 6: 按钮统一

**Files:**
- Modify: `frontend/src/components/ui/button.tsx:11`、`ChatView.tsx:142-143`、`NewSessionMenu.tsx:74-76`

**Step 1:** button.tsx default variant 改 `'bg-gradient-to-b from-primary to-primary-hover text-primary-foreground hover:brightness-105'`。
**Step 2:** ChatView 停止/发送改用 `<Button>`（发送 = default，停止 = outline/destructive 语义）。
**Step 3:** NewSessionMenu 手抄 default 样式改用 `<Button>`。
**Step 4:** 测试全绿 + 提交 `refactor(ui): ChatView/NewSessionMenu 按钮统一走 Button`

### Task 7: 视觉提升与动效

**Files:**
- Modify: `frontend/src/lib/ui.ts`、`pages/Home.tsx`、`pages/WorkspaceTab.tsx:253`、`components/TitleBar.tsx`、`components/ui/empty-state.tsx`、`components/SessionList.tsx`、`style.css`

**Step 1: lib/ui.ts**：`LIST_ROW_ACTIVE` 改 `'border-l-2 border-l-primary bg-primary/8'`；`TAB_UNDERLINE` 改 `'absolute bottom-0 left-1/2 h-0.5 w-[60%] -translate-x-1/2 rounded-full bg-primary'`。
**Step 2: Home 卡片**：hover 加 `hover:-translate-y-px hover:border-primary/40`，`boxShadow: var(--shadow-card)`（hover 时）；卡片左缘 3px 工具色竖条（用 `badgeFor().color`）；入场 stagger fade-up（keyframes `kshell-rise-in`，`animationDelay: i * 30ms`）。
**Step 3: TitleBar**：页签 active/hover 用新 TAB_UNDERLINE（hover 半透明 `bg-primary/40` 预览条）。
**Step 4: empty-state**：图标包一层 `flex h-10 w-10 items-center justify-center rounded-full bg-primary/10 text-primary`。
**Step 5: 面板切换 fade**：WorkspaceTab 中心区容器加 `kshell-fade-in var(--duration-fast)`（key 驱动重放）。
**Step 6:** 全量测试 + `npm run build` + 提交 `feat(ui): 卡片/页签/空态视觉提升与入场动效`

### Task 8: 验证与收尾

**Step 1:** `cd frontend && npm test && npm run build` 全绿；`go build ./...` 全绿。
**Step 2:** 按 AGENTS.md 补 `docs/smoke-test.md` 条目（主题切换、弹层 Esc、输入框 focus、reduced-motion）。
**Step 3:** requesting-code-review 自查，修复问题。
**Step 4:** 合并回 master、`.\build.ps1 -Desktop` 产出可执行文件、清理 worktree（finishing-a-development-branch）。
