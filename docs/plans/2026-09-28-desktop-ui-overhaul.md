# 桌面端 UI 大幅美化 + UX 全修 实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** kshell 桌面端前端从手写 CSS 迁移到 Tailwind CSS 4 + Radix（shadcn/ui 风格）并完成视觉重塑，同时修复诊断清单中的全部 UX 问题；黑窗修复（executil）已在本次会话完成。

**Architecture:** Tailwind 4 经 `@tailwindcss/vite` 插件接入（零配置文件），设计 token 用 shadcn 风格 CSS 变量（`:root` 浅色 + `prefers-color-scheme: dark` 深色，`@theme inline` 映射给 Tailwind）；Radix 只引入 Toast / Tooltip / Dialog(命令面板) 三个原语；组件样式全部重写为 Tailwind class，行为契约不变（测试走 role/text 查询，不依赖 className）。

**Tech Stack:** Tailwind CSS 4、@tailwindcss/vite、@radix-ui/react-toast、@radix-ui/react-tooltip、@radix-ui/react-dialog、clsx + tailwind-merge + class-variance-authority、Zustand（含 persist）、React 19 / Vite 7 / Vitest。

**约束：**
- 主题跟随系统（无手动开关）：浅深两套 token 全走 `prefers-color-scheme`。
- 所有用户可见文案保持简体中文。
- 不改 Go 绑定的既有 API 形状；新增绑定只能加方法（RestartApp）。
- 每个任务完成后：`npm test`（前端）或 `go test ./...`（Go）必须全绿再提交。

---

## Task 0: Tailwind 4 基建 + 设计 token

**Files:**
- Modify: `frontend/package.json`（装依赖）
- Modify: `frontend/vite.config.ts`（加 tailwindcss 插件）
- Rewrite: `frontend/src/style.css`（token 层）
- Create: `frontend/src/lib/cn.ts`

**要点：**
1. `npm install tailwindcss @tailwindcss/vite @radix-ui/react-toast @radix-ui/react-tooltip @radix-ui/react-dialog clsx tailwind-merge class-variance-authority`
2. vite.config.ts: `plugins: [react(), tailwindcss()]`
3. style.css 结构（完整重写，替代原 11 个旧变量）：

```css
@import "tailwindcss";

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
  --color-ring: var(--ring);
  --radius: var(--radius-base);
}

/* 浅色 token：延续 GitHub 蓝主色，整体提亮、加层次 */
:root {
  --background: #f6f8fa;  --foreground: #1f2328;
  --card: #ffffff;        --card-foreground: #1f2328;
  --muted: #eef1f4;       --muted-foreground: #59636e;
  --border: #d8dee4;      --input: #d8dee4;
  --primary: #0969da;     --primary-foreground: #ffffff;
  --secondary: #eef1f4;   --secondary-foreground: #1f2328;
  --accent: #ddf4ff;      --accent-foreground: #0969da;
  --destructive: #c93c37; --destructive-foreground: #ffffff;
  --ring: #0969da99;
  --radius-base: 8px;
}

@media (prefers-color-scheme: dark) {
  :root {
    --background: #0d1117;  --foreground: #e6edf3;
    --card: #161b22;        --card-foreground: #e6edf3;
    --muted: #21262d;       --muted-foreground: #8b949e;
    --border: #2d333b;      --input: #2d333b;
    --primary: #4493f8;     --primary-foreground: #0d1117;
    --secondary: #21262d;   --secondary-foreground: #e6edf3;
    --accent: #121d2f;      --accent-foreground: #4493f8;
    --destructive: #e5534b; --destructive-foreground: #ffffff;
    --ring: #4493f899;
  }
}
```

4. body 保留中文字体栈；加 `@custom-variant` 不需要（Tailwind 4 默认 dark: 即 media 策略）。全局滚动条样式（细滚动条、hover 加深）写在 style.css。
5. `src/lib/cn.ts`：`export function cn(...inputs: ClassValue[]) { return twMerge(clsx(inputs)) }`
6. 验证：`npm test` 全绿（Tailwind 是构建期，不影响测试）；`npm run build` 成功。
7. 提交。

## Task 1: UI 原语 + Toast 体系

**Files:**
- Create: `frontend/src/components/ui/button.tsx`、`badge.tsx`、`skeleton.tsx`、`tooltip.tsx`、`toaster.tsx`
- Modify: `frontend/src/state/store.ts`（message → toasts 数组）、`frontend/src/App.tsx`（挂 `<Toaster />`）、`frontend/src/components/BasketBar.tsx`（删行内 message）
- Test: `frontend/src/state/store.test.ts` 增补 toast 用例

**要点：**
1. Button 用 cva：`variant: default | secondary | ghost | destructive | outline`，`size: default | sm | icon`；`asChild` 不需要（YAGNI）。
2. store：删 `message/messageTimer`，改为 `toasts: Toast[]`（`{id: number; title: string; tone: 'info'|'success'|'error'}`）+ `notify(title, tone?)`（默认 info，id 自增）+ `dismissToast(id)`；`notify` 不再自动清空——超时由 Radix Toast `duration={3000}` 控制并在 `onOpenChange(false)` 时 dismiss。注意保留 store.notify 签名兼容旧调用点（第二参可选）。
3. toaster.tsx：Radix Provider + Viewport（右下角，`z-50`），error 用 destructive 配色、success 用 accent。
4. BasketBar 删除 `{message && ...}` 行；所有原 `notify('xxx')` 调用点检查语气（失败类传 'error'）。
5. 验证 + 提交。

## Task 2: 壳层与页面视觉重塑（行为不变）

**Files:** `App.tsx`、`pages/Home.tsx`、`pages/Settings.tsx`、`pages/WorkspaceTab.tsx`、`components/SessionList.tsx`、`FileTree.tsx`、`Preview.tsx`、`SshPanel.tsx`、`BasketBar.tsx`、`WorkspaceSearch.tsx`；删除 `frontend/src/App.css`（样式全部迁 Tailwind，main.tsx 去掉 import）

**视觉规格（统一贯彻）：**
- 壳层：顶栏 `h-11 border-b bg-card`，页签为圆角胶囊式（激活态 `bg-accent text-accent-foreground`），固定「首页」「设置」与工作区页签之间加视觉分组；应用标题 "kshell" 小 logo 字样放最左。
- 卡片语言：工作区列表/会话列表/设置分区一律 `rounded-lg border bg-card`，hover `border-ring` + 轻阴影。
- 会话条目：卡片式（工具徽标右移为彩色 Badge，标题加粗，时间/条数 muted 小字）；打开态左侧 2px 主色竖条。
- 文件树：目录/文件加 SVG 图标（内联小组件，不引图标库），行 hover `bg-muted`，篮子按钮 hover 才显现（篮中常显）。
- 预览：等宽 `font-mono text-[13px]`，头部 path 用 `truncate` + 复制按钮。
- SSH：连接行卡片化，输出区 `bg-muted rounded-md p-3 font-mono`。
- 加载态：全部换 Skeleton（本任务先换骨架形状，不带动效逻辑之外的逻辑）。
- 空态：图标 + 主文案 + muted 副文案的统一 `EmptyState` 小组件（`components/ui/empty-state.tsx`）。
- **行为零改动**：所有 onClick/数据流/aria 保持原样。

## Task 3: UX 功能（清单全修）

**Files:** store.ts、App.tsx、Home/Settings/WorkspaceTab、SessionList、SshPanel、Preview、WorkspaceSearch、`lib/api.ts`、`main.go`、`internal/desktop/app.go`（+新 `restart_other.go`/`restart_windows.go` 或直接复用 executil）

**3.1 反馈补全：**
- SessionList.handleResume：失败 `notify('恢复会话失败：' + err, 'error')`（resumeSession 需把错误透出——检查 api.ts 是否吞错）；成功 `notify('已在外部终端打开', 'success')`。
- WorkspaceTab.handleNewSession：成功 `notify('已创建会话', 'success')`；失败改用 notify（error）替代行内 newErr。

**3.2 页签持久化：** store 加 zustand `persist`（只持久化 `openTabs`、`activeTabId`，key `kshell-tabs`，partialize 排除其余字段）。测试补：rehydrate 后 openTabs 恢复。

**3.3 右栏状态保持：** WorkspaceTabView 改为双面板常挂载（`hidden` class 切换显示），FileTree/SshPanel 不再因切页签卸载重载。

**3.4 首页重扫按钮：** Home 头部加「重新扫描」按钮（scanning 中 disabled + 旋转图标），点击 `scanSessions()`，文案随 scanState 切换。

**3.5 设置页重启按钮：**
- Go：`internal/desktop/app.go` 加 `RestartApp(ctx context.Context) error`——`os.Executable()` 后 `executil.HideWindow` + `Start()`，成功即调 `a.quitApp(ctx)`。为可测性抽出包级变量 `spawnSelf = func(exe string) error`，测试替换后断言 quitting 被置位（BeforeClose 放行）。需处理单实例语义冲突吗？不需要（当前无单实例锁）。
- api.ts 加 `restartApp()`；Settings 保存成功提示旁加「立即重启」按钮，点击后禁用并提示「正在重启……」。

**3.6 快捷键：**
- `Ctrl+K`：命令面板（Radix Dialog），列出全部工作区 + 已开页签（分两组），输入过滤，Enter/点击执行 openTab/setActiveTab，Esc 关闭。数据源复用 store.workspaces（无数据时提示先回首页扫描）。
- `Ctrl+F`：当 activeTab 是工作区时聚焦会话搜索——用 `window.dispatchEvent(new CustomEvent('kshell:focus-search'))`，WorkspaceSearch 挂载时监听并 focus()（简单可靠，不引 ref 链）。
- App.tsx 全局 keydown 监听（`e.ctrlKey && e.key === 'k'/'f'`，preventDefault）。页脚或首页提示快捷键。

**3.7 会话按工具筛选：** SessionList 搜索框下加一排 chip（全部 + 按 badgeFor 去重的工具），选中态高亮，与文字搜索 AND 叠加。

**3.8 SSH 命令历史：** SshPanel 组件内 `history: string[]`（最多 20 条，去重），输入框 ArrowUp/Down 逐条回填，回车执行后入栈；输入框下方最近 5 条以 chip 展示点击回填 + 「清空」。

**3.9 预览加入篮子：** Preview 头部加「加入篮子/移出篮子」按钮（复用 FileTree.handleBasketToggle 的同步逻辑——抽 `lib/basket.ts` 共用 `toggleAndSync(path)`）。

**3.10 窗口最小尺寸：** main.go options 加 `MinWidth: 960, MinHeight: 640`。

**3.11 页签中键关闭：** tab 容器 `onAuxClick` 中键调 closeTab。

## Task 4: 测试补全与回归

- 更新受影响测试（BasketBar 删 message、store message→toasts、WorkspaceTab 双面板挂载可能影响 WorkspaceTab.test 的查询）。
- 新增：store 持久化、toast 队列、工具筛选、SSH 历史、RestartApp（Go，spawnSelf 注入）。
- 全量：`cd frontend && npm test`（现 85 用例基线）、`go test ./...`、`go vet ./...`。

## Task 5: 构建 + 冒烟

1. `cd d:\data\workspace\moxi\kshell\frontend; npm run build`
2. `.\build.ps1 -Desktop`（经 cmd /c 跑 wails build，PowerShell 5.1 会误报 stderr，看产物时间戳为准）
3. 产物：`dist/kshell.exe`（或 build 输出路径，以 build.ps1 为准），人工冒烟清单追加到 `docs/smoke-test-desktop.md`：黑窗不闪（文件树懒加载/预览/重扫）、Ctrl+K/Ctrl+F、主题跟随系统、重启按钮、页签持久化。
4. 最终提交（可分任务多次提交，本次会话统一整理 commit 信息）。

---

**风险与备注：**
- Tailwind 4 dark: 默认就是 `prefers-color-scheme` media 策略，无需配置。
- Wails WebView2 渲染无障碍；Radix 在 Wails 环境正常（纯 DOM）。
- RestartApp 在测试中严禁真 spawn：必须注入 spawnSelf。
- 旧 App.css 删除前确认 main.tsx 中 import 一并移除。
- 黑窗修复（executil）已在本会话完成并测试通过，不属本计划范围。
