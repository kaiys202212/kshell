# UI 美观度诊断与优化设计（桌面端前端）

日期：2026-10-03
状态：已经用户确认（三段逐段确认）
诊断依据：ui-ux-pro-max 技能数据检索 + 全量前端代码审查

## 背景与结论

kshell 桌面端前端（React 19 + Vite + Tailwind v4）底子好：`@theme inline` + `data-theme` 亮/暗双主题 token 体系完整，扁平化取向明确（1px 边框、2-4px 圆角、几乎无阴影）。问题集中在**一致性与记忆点**：

| # | 问题 | 位置 | 严重度 |
|---|---|---|---|
| 1 | 弹层遮罩语义分裂，暗色下 `bg-black/30` 几乎不可见 | ChatView.tsx:148 vs Home.tsx:225 | 高 |
| 2 | Focus 风格分裂，部分控件无 focus-visible 焦点环 | Preview.tsx:169、FileTree.tsx:466、WorkspaceSearch.tsx:24 | 高 |
| 3 | ChatView「停止/发送」手写按钮，无 hover/disabled/focus | ChatView.tsx:142-143 | 中 |
| 4 | 输入框样式 4 处各写一遍 | QuickSwitcher:123、WorkspaceSearch:24、SshPanel:212、FileTree:466 | 中 |
| 5 | 弹层无统一 Dialog 封装，混用 3 套方案 | Home:224-268 等 | 中 |
| 6 | 工具品牌色硬编码且亮暗不区分对比度 | lib/toolBadge.ts:12-16 | 中 |
| 7 | xterm 主题色与 CSS token 同值但硬编码复制 | lib/appearance.ts:11-15 | 低 |
| 8 | 视觉单调，缺记忆点 | 全局 | 低 |

## 用户决策

- 范围：修一致性债（问题 1-7）+ 全局均衡的视觉提升
- 幅度：**大胆升级**（允许 accent 渐变、卡片阴影层次、动画编排）
- 配色：主色由 GitHub 蓝 → **青绿 Teal**（技能色板：Productivity Tool，Primary `#0D9488`）
- 密度：字号 12.5px → 13px，间距放宽一档
- 不采纳技能的「OLED 黑客风 + JetBrains Mono」整盘换血方案（与项目刻意保留的扁平工程风冲突）

## 设计

### 1. Token 层（frontend/src/style.css）

1. **主色换 Teal**：亮 `--primary: #0D9488`（teal-600），暗 `--primary: #2DD4BF`（teal-400）；派生 `--primary-hover`、`--primary-foreground`、`--ring` 同步；保证文本对比 ≥4.5:1。
2. **中性色微调**：背景/卡片/边框灰阶带入青调（如暗色背景 `#0d1117` → `#0c1310`），与主色呼应。
3. **新增 token**：
   - `--overlay`：弹层遮罩（亮暗各自定值），修问题 1
   - `--shadow-card`：卡片微阴影（暗色下与边框组合）
   - 动效 token：`--duration-fast: 120ms`、`--duration-base: 180ms`、统一 easing
4. **工具品牌色入 token**：`--tool-codebuddy/codex/claude/gemini/opencode` 亮暗两套（亮色加深、暗色提亮），`lib/toolBadge.ts` 改读 token。
5. **密度**：正文 13px / 行高 1.5；面板 padding、列表行高整体 +2~4px；圆角保持 2-4px。
6. **xterm 主题从 token 派生**：`lib/appearance.ts` 不再硬编码 hex，运行时读 `getComputedStyle` 的 token 值。

### 2. 基础组件收敛（frontend/src/components/ui/）

1. **`input.tsx`**（新增）：统一输入框，`h-8 rounded-[3px] border border-input bg-card`，focus 统一 `focus-visible:ring-2 ring-ring`；收编 QuickSwitcher / WorkspaceSearch / SshPanel / FileTree 四处。
2. **`dialog.tsx`**（新增）：Radix Dialog 薄封装。统一遮罩 `bg-[--overlay] + backdrop-blur-[2px]`、容器 `rounded border border-border bg-card shadow-lg`、进出动画 scale 0.98→1 + fade 180ms。收编 Home 回收站、QuickSwitcher、ChatView 权限弹窗（纯 div 版补齐 Esc 关闭与焦点圈定）。
3. **按钮**：`button.tsx` default variant 改 Teal 渐变（`bg-gradient-to-b from-primary to-primary-hover`）；ChatView「停止/发送」改用 `<Button>`；NewSessionMenu 手抄样式删除。
4. tooltip / badge 不动，tool-badge 颜色接新 token。

### 3. 视觉提升与动效

1. **Home 卡片**：hover 时 `hover:border-primary/40` + `shadow-[--shadow-card]` + `translate-y-[-1px]`（120ms）；卡片左侧 3px 工具色竖条；卡片背景加极浅顶部渐变。
2. **激活态**：会话列表 active 行底色 `bg-primary/5` → `bg-primary/8`；TitleBar 页签下划线改 2px 圆头短条（宽 60% 居中），hover 半透明预览条。
3. **空状态**：`empty-state.tsx` 图标加 primary/10 底圆 + primary 描边。
4. **动效**（走 token，`prefers-reduced-motion` 全禁）：
   - 弹层 scale+fade 180ms
   - Home 卡片入场 stagger fade-up（间隔 30ms，总量 <300ms）
   - WorkspaceTab 中心区切换 fade 120ms
   - hover 统一 `transition-colors` 用 `--duration-fast`

### 明确不做

布局重构、页面增删、大圆角大阴影玻璃拟态、xterm 内部配色重设计。

## 验收标准

- 全 src 内 `bg-black/`、`bg-foreground/25` 遮罩为 0 处
- 输入框手写样式仅存在于 input.tsx 一处
- `focus:border-primary`（无 ring）为 0 处
- ChatView 无手写按钮
- 亮/暗两主题下正文对比 ≥4.5:1、非文本 UI ≥3:1
- `prefers-reduced-motion` 下无入场/stagger 动画
- `npm test`、`npm run build` 全绿；补 `docs/smoke-test*.md` 条目
