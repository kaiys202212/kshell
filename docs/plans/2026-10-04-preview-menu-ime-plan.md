# 文件预览增强 + 菜单/Reveal + 终端 IME Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 同分支交付：右键菜单实色与资源管理器 Reveal 可用；终端 IME 预编辑锚在实际输入位置且无横向偏移；文件预览支持 CodeMirror 高亮编辑、Markdown 切换、图片与 PDF 内嵌预览。

**Architecture:** 按风险串行三块——(1) ContextMenu/`explorer` 小修；(2) `imeAnchor` 视觉 caret + 溢出锁定接 `TerminalView`；(3) 桌面 `ReadFileBytes` + Preview `Text` 字段，前端按 `fileKind` 调度 CM6 / react-markdown / img / pdf.js。

**Tech Stack:** Go 1.23+、Wails v2、React 19、CodeMirror 6、pdfjs-dist、react-markdown、@xterm/xterm 6、vitest、PowerShell。

## Global Constraints

- 规格：`docs/plans/2026-10-04-preview-menu-ime-design.md`
- 在 `.worktrees/feat-preview-menu-ime` 的分支 `feat/preview-menu-ime` 上开发，禁止直接改 master 工作区
- 中文注释与提交信息 `type: 简述`；**仅当用户明确要求时才 git commit**
- TUI 预览行为不升级；不接入 Monaco；不改 Chat IME；不向 PTY 伪造预编辑
- 验证：`go build ./... ; go vet ./... ; go test ./... -count=1`；前端 `npm test` + `npm run build`
- 冒烟增量：`docs/smoke/feat-preview-menu-ime.md`
- 构建不写 `exit.signal`、不杀运行中桌面实例

## File map

| 文件 | 职责 |
|---|---|
| `frontend/src/components/ContextMenu.tsx` | 菜单实色背景 |
| `internal/desktop/files_reveal_windows.go` | explorer 参数与启动（无 HideWindow） |
| `internal/desktop/files_reveal_windows_test.go` | explorerArgs 单测 |
| `frontend/src/lib/imeAnchor.ts` (+ test) | 视觉 caret、钳制、溢出锁定纯函数 |
| `frontend/src/components/TerminalView.tsx` | IME 接线 |
| `internal/workspace/preview.go` (+ test) | Preview 增加无行号 `Text` |
| `internal/desktop/files_bytes.go` (+ test) | `ReadFileBytes` |
| `frontend/src/lib/api.ts` | 绑定类型与封装 |
| `frontend/src/lib/fileKind.ts` (+ test) | 扩展名 → 预览种类/语言 |
| `frontend/src/components/CodeEditor.tsx` | CM6 封装 |
| `frontend/src/components/MarkdownPreview.tsx` | MD 渲染 |
| `frontend/src/components/ImagePreview.tsx` | 图片 |
| `frontend/src/components/PdfPreview.tsx` | pdf.js |
| `frontend/src/components/Preview.tsx` (+ test) | 类型调度 |
| `docs/smoke/feat-preview-menu-ime.md` | 冒烟清单 |

---

### Task 1: 右键菜单去掉半透明

**Files:**
- Modify: `frontend/src/components/ContextMenu.tsx`
- Test: `frontend/src/components/ContextMenu.test.tsx`（若无则新建）

**Interfaces:**
- Consumes: 无
- Produces: 菜单根节点 class 含 `bg-card`，不含 `bg-popover`

- [ ] **Step 1: 写失败测试**

新建或扩展 `frontend/src/components/ContextMenu.test.tsx`：

```tsx
import { describe, expect, it, vi } from 'vitest';
import { render } from '@testing-library/react';
import ContextMenu from './ContextMenu';

describe('ContextMenu', () => {
  it('使用不透明 bg-card 而非未定义的 bg-popover', () => {
    const { container } = render(
      <ContextMenu x={10} y={10} items={[{ label: 'x', onSelect: vi.fn() }]} onClose={vi.fn()} />,
    );
    const menu = container.ownerDocument.body.querySelector('[role="menu"]');
    expect(menu?.className).toContain('bg-card');
    expect(menu?.className).not.toContain('bg-popover');
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend; npm test -- ContextMenu`
Expected: FAIL（当前为 `bg-popover`）

- [ ] **Step 3: 改实现**

`ContextMenu.tsx` 根节点 class 将 `bg-popover` 换成 `bg-card`。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend; npm test -- ContextMenu`
Expected: PASS

- [ ] **Step 5: 提交（仅用户要求时）**

```powershell
git add frontend/src/components/ContextMenu.tsx frontend/src/components/ContextMenu.test.tsx
git commit -m "fix: 右键菜单改用不透明 bg-card"
```

---

### Task 2: Windows Reveal——去掉 HideWindow + 空格路径引号

**Files:**
- Modify: `internal/desktop/files_reveal_windows.go`
- Modify: `internal/desktop/files_reveal_windows_test.go`

**Interfaces:**
- Consumes: `revealInOS(abs string, isDir bool) error`；`explorerArgs(abs string, isDir bool) []string`
- Produces: 目录 → `[]string{abs}`；文件 → `[]string{"/select," + quoted}`，其中含空格或 `&` 等特殊字符时对路径加双引号；`revealInOS` **不再**调用 `executil.HideWindow`

- [ ] **Step 1: 写失败测试**

替换/扩展 `TestRevealExplorerArgsWindows`：

```go
func TestRevealExplorerArgsWindows(t *testing.T) {
	got := explorerArgs(`D:\proj\a.go`, false)
	if len(got) != 1 || got[0] != `/select,D:\proj\a.go` {
		t.Fatalf("file args: %v", got)
	}
	got = explorerArgs(`D:\proj\my file.go`, false)
	if len(got) != 1 || got[0] != `/select,"D:\proj\my file.go"` {
		t.Fatalf("spaced file args: %v", got)
	}
	got = explorerArgs(`D:\proj\pkg`, true)
	if len(got) != 1 || got[0] != `D:\proj\pkg` {
		t.Fatalf("dir args: %v", got)
	}
	got = explorerArgs(`D:\proj\my dir`, true)
	if len(got) != 1 || got[0] != `"D:\proj\my dir"` {
		t.Fatalf("spaced dir args: %v", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/desktop/ -run TestRevealExplorerArgsWindows -count=1`
Expected: FAIL（空格路径无引号）

- [ ] **Step 3: 实现**

`files_reveal_windows.go`：

```go
//go:build windows

package desktop

import "os/exec"

func revealInOS(abs string, isDir bool) error {
	cmd := exec.Command("explorer", explorerArgs(abs, isDir)...)
	// 故意不 HideWindow：CREATE_NO_WINDOW 会让 explorer 窗口表现异常（点了像没反应）
	return cmd.Start()
}

func explorerArgs(abs string, isDir bool) []string {
	p := quoteExplorerPath(abs)
	if isDir {
		return []string{p}
	}
	return []string{"/select," + p}
}

// quoteExplorerPath 路径含空格时加双引号；已有引号则原样返回。
func quoteExplorerPath(abs string) string {
	if abs == "" {
		return abs
	}
	if abs[0] == '"' {
		return abs
	}
	for _, r := range abs {
		if r == ' ' {
			return `"` + abs + `"`
		}
	}
	return abs
}
```

删除对 `executil` 的 import。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/desktop/ -run TestReveal -count=1`
Expected: PASS

- [ ] **Step 5: 提交（仅用户要求时）**

```powershell
git add internal/desktop/files_reveal_windows.go internal/desktop/files_reveal_windows_test.go
git commit -m "fix: Windows Reveal 去掉藏窗并为空格路径加引号"
```

---

### Task 3: IME 纯函数——视觉 caret + 溢出锁定

**Files:**
- Modify: `frontend/src/lib/imeAnchor.ts`
- Modify: `frontend/src/lib/imeAnchor.test.ts`

**Interfaces:**
- Consumes: 现有 `ImeAnchorInput`、`computeImeClampStyles`、`IME_MAX_COL_RATIO`
- Produces:
  - `export interface CellHint { x: number; y: number; inverse: boolean }`
  - `export function pickVisualCaret(hints: CellHint[], fallbackX: number, fallbackY: number, cols: number, rows: number): { cursorX: number; cursorY: number }`
    - 优先取视口内 **最后一个** `inverse===true` 且坐标在范围内的单元；若无则用 fallback（再由调用方交给钳制）
  - `export interface OverflowLockStyles { overflowX: 'hidden'; }`
  - `export function imeOverflowLockStyles(): OverflowLockStyles`（恒返回 `{ overflowX: 'hidden' }`，便于单测与接线对称）
  - `export function shouldResetScrollLeft(scrollLeft: number): boolean`（`scrollLeft > 0` 则 true）

- [ ] **Step 1: 写失败测试**

追加到 `imeAnchor.test.ts`：

```ts
import {
  pickVisualCaret,
  imeOverflowLockStyles,
  shouldResetScrollLeft,
} from './imeAnchor';

describe('pickVisualCaret', () => {
  it('优先取最后一个反色单元作为实际输入位置', () => {
    const got = pickVisualCaret(
      [
        { x: 5, y: 10, inverse: true },
        { x: 12, y: 20, inverse: true },
        { x: 90, y: 20, inverse: false },
      ],
      99,
      20,
      100,
      30,
    );
    expect(got).toEqual({ cursorX: 12, cursorY: 20 });
  });

  it('无反色单元时回退 fallback', () => {
    expect(pickVisualCaret([{ x: 1, y: 1, inverse: false }], 99, 20, 100, 30)).toEqual({
      cursorX: 99,
      cursorY: 20,
    });
  });

  it('忽略越界反色单元', () => {
    expect(
      pickVisualCaret([{ x: 100, y: 0, inverse: true }], 3, 4, 100, 30),
    ).toEqual({ cursorX: 3, cursorY: 4 });
  });
});

describe('ime overflow helpers', () => {
  it('组合期锁定 overflow-x', () => {
    expect(imeOverflowLockStyles()).toEqual({ overflowX: 'hidden' });
  });
  it('scrollLeft>0 时应复位', () => {
    expect(shouldResetScrollLeft(12)).toBe(true);
    expect(shouldResetScrollLeft(0)).toBe(false);
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend; npm test -- imeAnchor`
Expected: FAIL（符号未导出）

- [ ] **Step 3: 实现纯函数**

在 `imeAnchor.ts` 追加上述导出实现（中文注释说明：反色单元≈TUI 可见 caret；无则回退 buffer 光标再钳制）。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend; npm test -- imeAnchor`
Expected: PASS

- [ ] **Step 5: 提交（仅用户要求时）**

```powershell
git add frontend/src/lib/imeAnchor.ts frontend/src/lib/imeAnchor.test.ts
git commit -m "feat: IME 视觉 caret 选取与溢出锁定辅助函数"
```

---

### Task 4: TerminalView 接线第三轮 IME

**Files:**
- Modify: `frontend/src/components/TerminalView.tsx`

**Interfaces:**
- Consumes: `pickVisualCaret`、`computeImeClampStyles`、`imeOverflowLockStyles`、`shouldResetScrollLeft`
- Produces: 组合期预编辑锚在视觉 caret（或钳制回退）；锁定 overflow-x；MutationObserver 重应用；end 后恢复

- [ ] **Step 1: 实现扫描反色单元的宿主函数（组件内私有）**

在 `applyImeClamp` 前增加：

```ts
const collectInverseHints = (): CellHint[] => {
  const hints: CellHint[] = [];
  const buf = instance.buffer.active;
  for (let y = 0; y < instance.rows; y++) {
    const line = buf.getLine(y);
    if (!line) continue;
    for (let x = 0; x < instance.cols; x++) {
      const cell = line.getCell(x);
      if (cell && typeof (cell as { isInverse?: () => boolean }).isInverse === 'function') {
        if ((cell as { isInverse: () => boolean }).isInverse()) {
          hints.push({ x, y, inverse: true });
        }
      }
    }
  }
  return hints;
};
```

若当前 `@xterm/xterm` 的 cell API 无 `isInverse`，改用 fg/bg 对调或 `getFgColorMode`/`isAttributeDefault` 等价判定，并在实现注释写明选用的 API；单测仍覆盖纯函数路径。

- [ ] **Step 2: 改写 applyImeClamp**

逻辑顺序：

1. `const caret = pickVisualCaret(collectInverseHints(), buf.cursorX, buf.cursorY, instance.cols, instance.rows)`
2. `computeImeClampStyles({ ...dims, cursorX: caret.cursorX, cursorY: caret.cursorY })`
3. 写 textarea / composition-view 的 left/top/maxWidth/overflow
4. 对 `.xterm`、`.xterm-viewport`、`.xterm-screen` 应用 `overflowX: hidden`（首次记住 `dataset.kshellImePrevOverflowX`）
5. 若 `shouldResetScrollLeft(viewport.scrollLeft)` 则 `viewport.scrollLeft = 0`

- [ ] **Step 3: MutationObserver + 恢复**

- `composing===true` 时 observe composition-view 与 textarea 的 `attributes: style`
- `compositionend`：disconnect observer；恢复三层 overflow 与清除强制 left/top/maxWidth（删 inline 或还原 dataset）

- [ ] **Step 4: 前端测试不回归**

Run: `cd frontend; npm test -- imeAnchor`
Expected: PASS（组件侧以手测/冒烟为主）

- [ ] **Step 5: 提交（仅用户要求时）**

```powershell
git add frontend/src/components/TerminalView.tsx
git commit -m "fix: 终端 IME 锚到视觉 caret 并锁定横向溢出"
```

---

### Task 5: Preview 增加无行号 Text + ReadFileBytes

**Files:**
- Modify: `internal/workspace/preview.go`
- Modify: `internal/workspace/preview_test.go`（若无对应用例则追加）
- Create: `internal/desktop/files_bytes.go`
- Create: `internal/desktop/files_bytes_test.go`
- Modify: `frontend/src/lib/api.ts`

**Interfaces:**
- Consumes: `resolveWorkspaceFile`、`isBinary`、现有 PreviewFile 读取逻辑
- Produces:
  - `workspace.Preview` 新增字段 `Text string`（无行号、`\n` 归一；Binary 时为空）
  - `desktop.FileBytesDTO`：`type FileBytes struct { Base64 string; Mime string; Size int64 }`
  - `func (a *App) ReadFileBytes(wsPath, path string) (FileBytes, error)`
  - 上限：图片/PDF 预览 `20 << 20`（20MiB）；超限返回明确错误
  - Mime：按扩展名映射（`.png→image/png`、`.jpg/.jpeg→image/jpeg`、`.gif→image/gif`、`.webp→image/webp`、`.svg→image/svg+xml`、`.bmp→image/bmp`、`.ico→image/x-icon`、`.pdf→application/pdf`；未知 → `application/octet-stream`）
  - 前端：`export interface FileBytes { Base64: string; Mime: string; Size: number }`；`readFileBytes(wsPath, path)`

- [ ] **Step 1: 写失败测试（Text 字段）**

在 `internal/workspace/preview_test.go` 增加：打开 testdata 文本文件，断言 `p.Text` 不含 `│` 行号前缀，且 `strings.Contains(p.Lines[0], "│")` 仍成立（TUI 兼容）。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/workspace/ -run Preview -count=1`
Expected: FAIL

- [ ] **Step 3: PreviewFile 填充 Text**

在拼 Lines 之前保留纯文本 `text`，赋给 `Preview.Text`；Lines 逻辑不变。

- [ ] **Step 4: ReadFileBytes TDD**

测试：临时目录写入小 png 魔数或假 pdf 字节，经 `App.ReadFileBytes`（或抽出的 `readFileBytes(abs, max)` 纯函数）断言 Mime/Size/Base64 解码长度；路径越界返回 `errPathOutsideWorkspace`。

实现草图：

```go
const maxPreviewBytes = 20 << 20

type FileBytes struct {
	Base64 string `json:"Base64"`
	Mime   string `json:"Mime"`
	Size   int64  `json:"Size"`
}

func (a *App) ReadFileBytes(wsPath, path string) (FileBytes, error) {
	resolved, err := a.resolveWorkspaceFile(wsPath, path)
	if err != nil {
		return FileBytes{}, err
	}
	return readFileBytesLimited(resolved, maxPreviewBytes)
}
```

- [ ] **Step 5: 更新 api.ts**

`FilePreview` 增加可选 `Text?: string`；增加 `FileBytes` 与 `readFileBytes`。

- [ ] **Step 6: 跑 Go 测试**

Run: `go test ./internal/workspace/ ./internal/desktop/ -count=1`
Expected: PASS

- [ ] **Step 7: 提交（仅用户要求时）**

```powershell
git add internal/workspace/preview.go internal/workspace/preview_test.go internal/desktop/files_bytes.go internal/desktop/files_bytes_test.go frontend/src/lib/api.ts
git commit -m "feat: 预览原文 Text 与 ReadFileBytes 绑定"
```

---

### Task 6: fileKind 扩展名分流

**Files:**
- Create: `frontend/src/lib/fileKind.ts`
- Create: `frontend/src/lib/fileKind.test.ts`

**Interfaces:**
- Produces:
  - `export type PreviewKind = 'text' | 'markdown' | 'image' | 'pdf' | 'binary'`
  - `export function previewKind(path: string): PreviewKind`
  - `export function codeLanguage(path: string): string`（返回 CM6 语言 id：`javascript`/`typescript`/`tsx`/`json`/`go`/`python`/`markdown`/`html`/`css`/`yaml`/`toml`/`shell`/`plaintext` 等）

规则（小写扩展名）：

- markdown: `md|markdown|mdx`
- image: `png|jpg|jpeg|gif|webp|svg|bmp|ico`
- pdf: `pdf`
- text: 常见源码扩展（`ts|tsx|js|jsx|json|go|py|rs|java|c|h|cpp|cs|rb|php|html|css|scss|less|vue|svelte|yaml|yml|toml|xml|sh|bash|zsh|ps1|bat|cmd|sql|graphql|dockerfile|txt|log|env|gitignore|editorconfig`）及无扩展名但 basename 为 `Dockerfile`/`Makefile`/`CMakeLists.txt`
- 其余：若稍后后端报 Binary 则 binary；前端 `previewKind` 对未知扩展先返回 `text`（由后端 Binary 标志最终裁定）

- [ ] **Step 1–4: TDD 实现**

覆盖：`a.ts→text/typescript`、`README.md→markdown`、`x.PNG→image`、`doc.pdf→pdf`、`foo.unknown→text`。

Run: `cd frontend; npm test -- fileKind`
Expected: PASS

- [ ] **Step 5: 提交（仅用户要求时）**

```powershell
git add frontend/src/lib/fileKind.ts frontend/src/lib/fileKind.test.ts
git commit -m "feat: 文件预览类型与语言扩展名映射"
```

---

### Task 7: 安装依赖并实现 CodeEditor

**Files:**
- Modify: `frontend/package.json`（通过 npm install）
- Create: `frontend/src/components/CodeEditor.tsx`
- Create: `frontend/src/components/CodeEditor.test.tsx`

**Interfaces:**
- Consumes: `codeLanguage(path)`
- Produces: `<CodeEditor value onChange? readOnly path theme />`
- 依赖：`@codemirror/view` `@codemirror/state` `@codemirror/lang-javascript` `@codemirror/lang-json` `@codemirror/lang-markdown` `@codemirror/lang-html` `@codemirror/lang-css` `@codemirror/lang-python` `@codemirror/lang-go` `@codemirror/language` `@codemirror/commands` `@codemirror/language-data`（或按需子集）+ 一行一语言的动态 import 表

- [ ] **Step 1: 安装**

Run（在 `frontend/`）：

```powershell
npm install @codemirror/view @codemirror/state @codemirror/commands @codemirror/language @codemirror/lang-javascript @codemirror/lang-json @codemirror/lang-markdown @codemirror/lang-html @codemirror/lang-css @codemirror/lang-python @codemirror/lang-go @codemirror/theme-one-dark
```

- [ ] **Step 2: 实现 CodeEditor**

- `useEffect` 创建 `EditorView`，销毁时 `destroy()`
- `readOnly` 时用 `EditorState.readOnly.of(true)`
- 暗色用 `oneDark`，亮色用默认
- 行号：`lineNumbers()`
- `onChange`：`EditorView.updateListener`
- 语言：按 `codeLanguage(path)` switch 加载对应 lang 扩展；未知 → 无语言扩展

- [ ] **Step 3: 组件烟测**

用 Testing Library 渲染只读编辑器，断言容器存在；变更 `value` 不抛错。

Run: `cd frontend; npm test -- CodeEditor`
Expected: PASS

- [ ] **Step 4: 提交（仅用户要求时）**

```powershell
git add frontend/package.json frontend/package-lock.json frontend/src/components/CodeEditor.tsx frontend/src/components/CodeEditor.test.tsx
git commit -m "feat: CodeMirror 6 文件编辑器组件"
```

---

### Task 8: Markdown / Image / Pdf 子组件

**Files:**
- Create: `frontend/src/components/MarkdownPreview.tsx`
- Create: `frontend/src/components/ImagePreview.tsx`
- Create: `frontend/src/components/PdfPreview.tsx`
- Modify: `frontend/package.json`（`pdfjs-dist`）

**Interfaces:**
- `MarkdownPreview({ markdown: string })` — `react-markdown` + `remark-gfm`，样式用现有 prose/文本类，跟随主题
- `ImagePreview({ wsPath, path })` — `readFileBytes` → `data:${Mime};base64,${Base64}` → `<img alt={basename} className="max-h-full max-w-full object-contain" />`
- `PdfPreview({ wsPath, path })` — 动态 `import('pdfjs-dist')`，设 `GlobalWorkerOptions.workerSrc` 为 pdfjs worker（Vite：`?url` 或 cdn 同源打包路径）；渲染当前页 canvas；按钮「上一页」「下一页」+ 页码

- [ ] **Step 1: 安装 pdfjs**

```powershell
npm install pdfjs-dist
```

锁定与 API 匹配的大版本；worker 路径写进组件注释。

- [ ] **Step 2: 实现三组件**

错误态：加载失败显示 `text-destructive` 文案；PDF 页码钳在 `1..numPages`。

- [ ] **Step 3: 最小测试**

- MarkdownPreview：渲染 `# Hi` 出现 `Hi` heading
- ImagePreview：mock `readFileBytes` 后 img src 以 `data:image/png;base64,` 开头

Run: `cd frontend; npm test -- MarkdownPreview ImagePreview`
Expected: PASS

- [ ] **Step 4: 提交（仅用户要求时）**

```powershell
git add frontend/package.json frontend/package-lock.json frontend/src/components/MarkdownPreview.tsx frontend/src/components/ImagePreview.tsx frontend/src/components/PdfPreview.tsx
git commit -m "feat: Markdown/图片/PDF 预览子组件"
```

---

### Task 9: Preview.tsx 调度整合

**Files:**
- Modify: `frontend/src/components/Preview.tsx`
- Modify: `frontend/src/components/Preview.test.tsx`

**Interfaces:**
- Consumes: `previewKind`、`CodeEditor`、`MarkdownPreview`、`ImagePreview`、`PdfPreview`、`FilePreview.Text`、`readFileBytes`
- Produces: 按路径种类渲染；MD 顶栏增加「源码 | 预览」Toggle；文本编辑用 CodeEditor 替换 textarea

行为细则：

1. `kind===image'` → 不调 `previewFile` 文本预览，直接 `ImagePreview`（可仍调一次 preview 拿 Info，或只 ReadFileBytes）
2. `kind==='pdf'` → `PdfPreview`
3. `kind==='markdown'` → 默认 `mode='preview'`；切换到 `source` 显示 CodeEditor（readOnly 或编辑态）
4. `kind==='text'` → 只读 CodeEditor，`value={data.Text ?? stripLinePrefix(data.Lines)}`；编辑态 CodeEditor `readOnly={false}`
5. 后端 `Binary===true` 且非 image/pdf → 元信息（防扩展名伪装）
6. 保留保存/取消/Ctrl+S/截断 Badge/切文件丢草稿

`stripLinePrefix`：若无 `Text`，用正则去掉每行 `^\s*\d+\s*│\s?`。

- [ ] **Step 1: 扩展 Preview.test.tsx**

- mock api：对 `.md` 断言出现「预览」按钮且渲染 markdown
- 对 `.ts` 断言挂载 CodeEditor（可通过 testid `code-editor`）
- 编辑保存链路原有用例不破

- [ ] **Step 2: 改 Preview.tsx 调度**

按上表接线；顶栏在 markdown 非编辑时显示：

```tsx
<div className="ml-auto flex gap-1">
  <Button size="sm" variant={mdMode==='source'?'default':'secondary'} onClick={()=>setMdMode('source')}>源码</Button>
  <Button size="sm" variant={mdMode==='preview'?'default':'secondary'} onClick={()=>setMdMode('preview')}>预览</Button>
  {showEditButton && <Button ...>编辑</Button>}
</div>
```

- [ ] **Step 3: 跑前端测试与 build**

Run:

```powershell
cd frontend; npm test; npm run build
```

Expected: 全绿；build 成功

- [ ] **Step 4: 提交（仅用户要求时）**

```powershell
git add frontend/src/components/Preview.tsx frontend/src/components/Preview.test.tsx
git commit -m "feat: Preview 按类型调度 CM6/MD/图/PDF"
```

---

### Task 10: 冒烟清单与全量验证

**Files:**
- Create: `docs/smoke/feat-preview-menu-ime.md`

- [ ] **Step 1: 写冒烟条目**

```markdown
# 冒烟：feat/preview-menu-ime

## 右键菜单 / Reveal
- [ ] 目录树右键菜单底板不透明
- [ ] 右键文件「在资源管理器中打开」弹出并选中该文件（含空格路径）
- [ ] 右键目录打开该目录

## IME
- [ ] 内嵌 agent TUI、光标 park 行尾：拼音出现在实际输入区附近
- [ ] 确认后变为中文；画面不横向偏移
- [ ] 组合结束后布局/滚动正常

## 预览
- [ ] `.ts`/`.go` 语法高亮；可编辑保存
- [ ] `.md` 源码/预览切换
- [ ] `.png` 显示图片
- [ ] `.pdf` 可翻页
- [ ] 未知二进制仍只显示元信息
```

- [ ] **Step 2: 全量命令**

```powershell
go build ./... ; go vet ./... ; go test ./... -count=1
cd frontend; npm test; npm run build
```

Expected: 全绿

- [ ] **Step 3: 提交文档（仅用户要求时）**

```powershell
git add docs/smoke/feat-preview-menu-ime.md docs/plans/2026-10-04-preview-menu-ime-plan.md docs/plans/2026-10-04-preview-menu-ime-design.md
git commit -m "docs: 预览/菜单/IME 冒烟与计划"
```

---

## Spec coverage（自检）

| 规格项 | Task |
|---|---|
| ContextMenu 实色 | 1 |
| Reveal 去 HideWindow + 空格引号 | 2 |
| IME 视觉 caret + 溢出锁 + Observer | 3–4 |
| Preview Text / ReadFileBytes | 5 |
| CM6 高亮编辑 | 6–7, 9 |
| MD 源码/预览切换 | 8–9 |
| 图片 / pdf.js | 8–9 |
| 冒烟与验证 | 10 |
| 不做 Monaco/TUI/Chat/伪造 PTY | Global Constraints |

## 执行说明

实现前先按 `using-git-worktrees` 在仓库根创建：

```powershell
git worktree add .worktrees/feat-preview-menu-ime -b feat/preview-menu-ime
```

后续所有改动在该 worktree 内完成。
