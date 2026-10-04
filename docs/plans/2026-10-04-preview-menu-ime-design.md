# 设计：文件预览增强 + 右键菜单/Reveal + 终端 IME

日期：2026-10-04  
分支（拟定）：`feat/preview-menu-ime`  
范围：三项同分支串行交付（先修菜单/Reveal → 再 IME → 最后预览增强）

## 背景与目标

桌面端工作区存在三处独立问题/缺口，本次一并处理：

1. **文件预览过弱**：中心区预览仍是纯文本 `<pre>` / `<textarea>`，无语法高亮、无 Markdown 渲染、图片/PDF 仅显示二进制元信息。
2. **目录树右键菜单**：菜单半透明；「在资源管理器中打开」点击无反应。
3. **终端 IME**：中文输入时画面仍向左偏移；期望与 Windows Terminal / VS Code 一致——拼音临时出现在实际输入位置，确认后替换为中文。

## 已确认决策

| 项 | 选择 |
|---|---|
| 交付方式 | 三项同一分支 |
| 文本编辑器 | CodeMirror 6（非 Monaco） |
| Markdown | 顶栏「源码 / 预览」二选一切换 |
| PDF | 应用内嵌 pdf.js |
| IME 场景 | 仅终端（xterm），不改 Chat |
| IME 体验 | 预编辑锚在实际输入位置 + 防横向偏移 |
| 推进顺序 | 菜单/Reveal → IME → 预览增强 |

## 1. 文件预览 / 编辑器

### 目标

按文件类型提供合适查看与编辑体验，替换纯文本预览。

### 按类型分流

| 类型 | 判定 | 行为 |
|---|---|---|
| 文本源码 | 非二进制；扩展名映射语言 | CodeMirror 6 只读高亮；「编辑」进入可写（保存/取消/Ctrl+S，≤1MB） |
| Markdown | `.md` / `.markdown` / `.mdx` | 顶栏 **源码 / 预览** 切换；源码=CM6；预览复用 `react-markdown` |
| 图片 | `.png/.jpg/.jpeg/.gif/.webp/.svg/.bmp/.ico` | `<img>` 居中预览；不可编辑 |
| PDF | `.pdf` | pdf.js 内嵌翻页；工具栏上一页/下一页/页码 |
| 其他二进制 | 现有 Binary | 仍只显示元信息 |

### 架构

- **前端调度**：`Preview.tsx` 改为按类型调度；拆出 `CodeEditor`、`MarkdownPreview`、`ImagePreview`、`PdfPreview`。
- **后端（桌面绑定）**：
  - 新增 `ReadFileBytes(wsPath, path) → { Base64, Mime, Size }`（仍走路径穿越校验 + 合理大小上限，PDF/图片专用）；前端拼 `data:` URL 或交给 pdf.js。
  - 文本预览：桌面侧改为提供**无行号前缀**的原文（新建绑定或扩展现有预览结构加 `Text` 字段）；行号只由 CM6 渲染。TUI 继续用 `workspace.PreviewFile`（带行号），互不影响。
- **语言映射**：扩展名 → CM6 language 包；未知扩展名 → `plaintext`。
- **主题**：跟随现有亮/暗色（CM6 与 MD 预览共用 appearance）。

### 约束（不做）

- 不做 diff、多文件 tab、未保存草稿跨文件保留
- 不接入 Monaco；TUI 侧预览不升级
- PDF 不做注解/全文搜索；图片不做专用缩放工具条

### 成功标准

- 打开 `.ts` 等源码有高亮，可编辑保存
- `.md` 可在源码与渲染间切换
- `.png` 等能看图
- `.pdf` 能翻页
- 原有截断提示、二进制元信息、路径穿越防护不回归

## 2. 右键菜单不透明 + 在资源管理器中打开

### 半透明

- **原因**：`ContextMenu` 使用 `bg-popover`，主题未定义对应 token，背景无效导致透底。
- **改法**：改为与 `NewSessionMenu` 一致的不透明 `bg-card`（或补齐 `--color-popover` 且等于 card 实色）。
- **成功标准**：菜单底板实色，不透出下层内容。

### Reveal 无反应

链路：`FileTree` → `revealInExplorer` → `RevealInExplorer` → Windows `explorer`。

改法：

1. **去掉**对 `explorer` 的 `HideWindow` / `CREATE_NO_WINDOW`（GUI shell 不应藏窗）。
2. 路径含空格时按惯例加引号（如 `/select,"C:\path with space\file"`）。
3. 失败继续 toast；路径校验失败错误可见。
4. 补强 Go 单测：`explorerArgs`（目录 / 文件 / 含空格）。

### 不做

- 不改菜单项集合与 `onPointerDown` 交互模型
- 不引入自绘文件管理器

### 成功标准

右键文件/目录 →「在资源管理器中打开」能弹出并选中/打开对应项；菜单视觉不透明。

## 3. 终端 IME（第三轮）

### 目标体验

与常见终端一致：

1. 输入拼音时，英文字母**临时出现在实际输入位置**（预编辑）
2. 选字确认后，预编辑被**中文替换**
3. 组合过程中终端画面**不横向偏移**

### 根因（沿用既有结论）

agent TUI 常把 buffer 硬件光标 park 在行尾；xterm 把 `.composition-view`（`white-space: nowrap`）与 helper textarea 钉在该处，拼音贴右缘挤布局，甚至带动横向滚动/偏移。既有两轮仅钳 `left/top/maxWidth`，未解决「锚在错误光标」与「viewport 被撑开」。

### 方案

保留 `computeImeClampStyles` 作为回退，主策略改为「锚在实际输入位置」：

1. **定位实际输入位置**  
   组合开始时，优先在视口内查找**可见光标 / 反色单元**（视觉 caret）；找到则作为预编辑锚点；找不到再回退到视口约 60% 宽度钳制。

2. **预编辑落在输入位置**  
   将 helper `textarea` 与 `.composition-view` 同步到该锚点；组合期限制 `maxWidth`，并对 `.xterm` / `.xterm-viewport` / `.xterm-screen` 锁定 `overflow-x: hidden`；必要时将 `viewport.scrollLeft` 拉回 0。`compositionend` 后恢复原 inline 样式。

3. **对抗 xterm 重定位**  
   组合期用 `onRender` + `requestAnimationFrame` + `MutationObserver`（监听 composition-view / textarea 的 style 被改写）持续重应用锚点。

4. **可测纯函数**  
   扩展 `frontend/src/lib/imeAnchor.ts`：视觉 caret 选取、回退钳制、溢出锁定辅助；`TerminalView.tsx` 仅负责接线。

### 不做

- 不向 PTY 写入伪造预编辑字符（避免弄脏会话）
- 不改 Chat 输入框
- 不升级 xterm 大版本作为主修复路径
- 不改 Go / ConPTY / 构建脚本

### 成功标准

内嵌终端跑 agent TUI、光标 park 行尾时打拼音：预编辑字母出现在实际输入区附近；确认后变为中文；画面不横向偏移；组合结束后滚动与布局恢复正常。

## 测试策略

| 层 | 内容 |
|---|---|
| Go | `explorerArgs` 单测；文件读取绑定路径校验（若新增 API） |
| 前端单测 | `imeAnchor` 扩展；Preview 类型分流 / MD 切换（组件测）；ContextMenu 样式 class |
| 前端构建 | `npm test`；`npm run build` |
| Go 全量 | `go build ./...`；`go vet ./...`；`go test ./... -count=1` |
| 冒烟 | `docs/smoke/feat-preview-menu-ime.md`：菜单不透明、Reveal、IME、各类型预览 |

## 风险与缓解

| 风险 | 缓解 |
|---|---|
| 视觉 caret 启发式在部分 TUI 失效 | 明确回退到 60% 钳制 + 溢出锁定 |
| pdf.js / CM6 增大包体 | 按需动态 import；仅打开对应文件时加载 |
| Reveal 在非 Windows 行为差异 | 保持现有 other 实现；重点验 Windows |
| 预览 API 改动影响 TUI | TUI 仍用 `workspace.PreviewFile`；桌面绑定单独演进 |

## 不做（总表）

- Monaco、TUI 预览升级、PDF 注解、跨文件草稿
- Chat IME、xterm 大版本升级、伪造 PTY 预编辑
- 自绘资源管理器、改菜单信息架构
