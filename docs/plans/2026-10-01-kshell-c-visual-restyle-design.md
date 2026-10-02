# kshell 桌面端 UI 重塑 S1：C 高密度工具感视觉底座 设计

**背景：** 桌面端已完成两轮 UI 改造（Tailwind 4 + Radix + shadcn 风格扁平化 token、无边框标题栏、内嵌 ConPTY 终端、三栏可拖动、持久化等）。本轮为「继续美化」的延续，用户选定视觉方向 **C：高密度工具感（IDE 风）**，并额外要求对易用性做诊断、结合产品目标（整合各编程工具与 session、集中使用）给出功能优化建议。

**范围拆分（本次对话确认）：** 需求较大，拆成 5 个子项目各自走 设计 → 计划 → 实现：
- **S1 C 视觉重塑（本文档，基础层，行为零改动）**
- S2 发现层：全局会话中心 + 首页搜索/筛选/排序
- S3 命令面板升级（Ctrl+K：动作 + 最近会话，依赖 S2）
- S4 终端体验（页签快捷键切换、重命名、退出态 + 一键重启）
- S5 预览/编辑代码视图（语法高亮 + 行号 + git diff，含高亮库选型）

**顺序：S1 → S2 → S3 → S4 → S5。** 本文档只覆盖 S1。

---

## 1. 目标与非目标

**目标：** 在**不改变任何行为、数据流、绑定、用户可见文案与 aria 语义**的前提下，把现有 UI 统一到 C 语言：高密度、等宽点缀、3px 圆角、1px 分隔 + 极浅底色差（几乎不用阴影）、语义状态色、工具固定色相。

**非目标：**
- 不加任何新功能（搜索框、会话中心、命令面板、终端快捷键、语法高亮都属 S2–S5）。
- 不引入手动主题开关（继续跟随系统 `prefers-color-scheme`）。
- 不引入图标库（继续内联 SVG）；不引入新的重型依赖。
- 不改响应式断点策略、不改窗口/托盘/终端逻辑。

---

## 2. 设计 Token（`frontend/src/style.css`）

现有 token 层保持不变（`@theme inline` 映射 + `:root` 浅色 + `prefers-color-scheme` 深色），只做以下调整与新增：

**调整：**
- 圆角：`--radius-base` 由 4px → **3px**；`--radius-xs 2px / sm 3px / md 3px / lg 3px / xl 3px / 2xl 4px / 3xl 4px`。
- 字号：`body` `font-size` 13px → **12.5px**；`line-height` 显式 **1.45**。

**新增：**
- 等宽字体 token `--font-mono`：`Consolas, "Cascadia Mono", "SFMono-Regular", ui-monospace, monospace`，在 `@theme inline` 里映射 `--font-mono: var(--font-mono-stack)`（供 `font-mono` 工具类）。
- 语义状态色（浅/深各一套）：
  - `--success`（浅 `#1a7f37` / 深 `#3fb950`）
  - `--warning`（浅 `#9a6700` / 深 `#d29922`）
  - `--danger`（复用现有 `--destructive`，浅 `#c93c37` / 深 `#e5534b`）
  - `--info`（复用现有 `--primary`）
  - 在 `@theme inline` 映射为 `--color-success / --color-warning / --color-danger / --color-info`。
- 工具色相**不进 CSS**，放前端常量（见 §3），因为它是数据驱动的。

**滚动条：** 保持 6px；thumb 用 `--muted-foreground`、hover 用 `--foreground`（现状微调，确认无回归）。

---

## 3. 共享样式与工具色相

**`frontend/src/lib/ui.ts`（新增，纯 className 常量，不新建 React 组件）**

现在 `TitleBar`、`WorkspaceTab` 中心区页签、`WorkspaceTab` 右栏子页签三处各写一份 tab 样式，散落重复。抽成常量统一：

```ts
export const TAB_BASE = 'relative flex shrink-0 items-center gap-1.5 px-2.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground';
export const TAB_ACTIVE = 'font-medium text-foreground';
export const TAB_UNDERLINE = 'absolute inset-x-1.5 bottom-0 h-0.5 bg-primary';
export const LIST_ROW = 'rounded border border-border bg-card transition-colors hover:bg-muted/50';
export const LIST_ROW_ACTIVE = 'border-l-2 border-l-primary bg-primary/5';
export const PANE_HEADER = 'text-[11px] text-muted-foreground';
export const MONO = 'font-mono text-[11px] text-muted-foreground';
```

> 抽取程度以「消除 3 处以上重复」为准，单点出现的样式不抽（YAGNI）。
> `PANE_HEADER` 不套 `uppercase`/`tracking`——分组标题是中文（如「已打开的页签」），加字距反而别扭；仅英文标签（如 SSH）可局部加。

**`frontend/src/lib/toolBadge.ts`（扩展）**

`badgeFor(toolID)` 现有返回 `{ label, className }`，新增返回工具色相：

```ts
export interface ToolBadge { label: string; className: string; color: string; }
```
- Claude → `#d97757`（橙）
- Codex → `#10a37f`（绿）
- Gemini → `#4285f4`（蓝）
- 其它 / 未知 → `var(--muted-foreground)`（灰）

保持既有 `label`/`className` 不变以兼容调用点与测试。

**`frontend/src/components/ui/tool-dot.tsx`（新增）**

`■ + 名称` 的紧凑工具徽标：一个 `1.5×1.5` 圆点（背景 `color`）+ 工具名（等宽小字）。用于会话行、首页卡片、中心区终端页签，替换当前纯色 `Badge`。纯展示组件，接收 `{ toolID }`，内部调 `badgeFor`。

---

## 4. 逐页改动清单

总原则：删除 `rounded-lg`/`rounded-md` 中大于 3px 的用法（统一 `rounded` = 3px）；删除残余 `hover:shadow-*`；路径/时间/数量/git 分支/代码/SSH 目标改等宽；面板标题改 `PANE_HEADER`。

| 文件 | 改动要点 | 行为影响 |
|---|---|---|
| `components/TitleBar.tsx` | 高度 h-9 → **h-8**；页签 padding 收紧（沿用 `TAB_BASE`）；logo 保留 `<span text-primary>k</span>shell`；窗口控件宽度 11 → 10；激活下划线沿用 `TAB_UNDERLINE` | 无 |
| `pages/Home.tsx` | 网格 `gap-3` → `gap-2.5`；卡片 padding `px-3 py-2.5` → `px-2.5 py-2`；时间/会话数改 `MONO`；工具分布换 `ToolDot`；空态/骨架间距收紧 | 无 |
| `pages/WorkspaceTab.tsx` | 三栏 padding 收紧；中心区页签 `h-8` → **h-7** 并用 `TAB_*`；右栏子页签用 `TAB_*`；工具徽标换 `ToolDot`；`ResizeHandle` 视觉对齐 | 无 |
| `components/SessionList.tsx` | 行 padding `p-2.5` → `p-2`、圆角 3px；工具徽标换 `ToolDot`；时间/条数 `MONO`；运行态用 `LIST_ROW_ACTIVE` 并把 `✓` 换成 `--success` 圆点；按钮 `size="sm"` | 无 |
| `components/FileTree.tsx` | 行等高收紧；路径 `font-mono`；git 状态字母按状态着色（M `--warning` / A·U `--success` / D·! `--danger` / R `--info`）；hover 背景更平；篮子按钮保持 hover 显现 | 无 |
| `components/Preview.tsx` | 头部路径 `font-mono`；代码区 `font-mono` + `leading-relaxed` → `leading-[1.55]`；截断 Badge 对齐；编辑器 textarea 字号 12.5px | 无 |
| `components/SshPanel.tsx` | 连接行 padding 收紧；来源标签改小号等宽；输出区等宽 + 行高收紧；`✓` 用 `--success` | 无 |
| `components/BasketBar.tsx` | chips 圆角 3px、密度收紧；标题用 `PANE_HEADER` | 无 |
| `components/QuickSwitcher.tsx` | 条目行 padding 收紧；分组标题改 `PANE_HEADER` | 无 |
| `components/ToolPicker.tsx` | 触发器 h-7 保持、圆角 3px；选项行收紧 | 无 |
| `pages/Settings.tsx` | 分区卡片圆角 3px、padding 收紧；工具列表行密度；yaml 编辑器等宽 | 无 |
| `components/ui/badge.tsx` | 变体圆角统一 3px、字距收紧；新增 `success`/`warning` 变体（供 git/状态） | 无 |
| `components/ui/button.tsx` | 圆角统一 3px；`sm` 尺寸高度微调（h-7） | 无 |
| `components/ui/skeleton.tsx` | 圆角 3px | 无 |
| `components/ui/empty-state.tsx` | 图标与文案间距收紧 | 无 |
| `components/ui/toaster.tsx` | 圆角 3px、密度对齐 | 无 |

---

## 5. 约束

- **行为零改动**：所有 onClick / 数据流 / aria / 文案原样；仅 className 与 token 变化。
- 用户可见文案保持简体中文。
- 深浅两态都要成立（所有新色都同时给浅/深取值）。
- 测试以 role/text 查询为主；历史上有 4 处 className 断言（`style.css`/组件），若断言到具体圆角/字号需同步更新。
- 设计/计划文档按 AGENTS.md 放 `docs/plans/`；**不自动提交**（除非用户明确要求）。

---

## 6. 验证

1. `cd frontend && npm test` —— 全绿（当前基线 100+ 用例）。
2. `cd frontend && npm run build` —— `tsc && vite build` 成功。
3. `.\build.ps1 -Desktop` 出桌面版；按 `docs/smoke-test-desktop.md` 人工检查：
   - 浅色 / 深色两态下，标题栏、三栏、列表、文件树、预览、SSH、设置均符合 C（3px 圆角、无阴影、等宽点缀、状态色）。
   - 文本清晰不糊（12.5px 基准 + 等宽元信息）。
   - 无横向溢出、无截断异常。
4. `git diff --stat` 复核：只应出现 `frontend/src/**` 与 `style.css` 改动。

---

## 7. 风险与备注

- 全局字号 13 → 12.5px 会改变所有页面观感；若真机觉得过小，只需回退 `body` 一处。
- 抽取 `lib/ui.ts` 常量时，务必保证与现有各处的类集合**语义等价**（含 hover/active），否则会引入细微视觉回归。
- `tool-dot.tsx` 替换现有纯色 `Badge` 会牵动若干快照式断言（若测试断言了 Badge 文本/角色）；以「文本仍存在」为准调整，不弱化可访问性。
- C 语言下 `--font-mono` 在 Windows 优先 Consolas，需确认中英文混排不出现发虚。
