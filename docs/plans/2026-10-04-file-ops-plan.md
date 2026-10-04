# 文件区域操作增强 实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 桌面端文件树支持右键菜单（新建/重命名/删除/复制路径）、树内拖拽移动，并支持把文件（树内或外部）拖入终端/聊天页签自动插入全路径。

**Architecture:** Go 侧在 `internal/desktop/files.go` 新增 `CreateEntry`/`DeleteEntry`/`MoveEntry` 三个绑定（复用 `nodeAt`/`underPath`/缓存作废逻辑）；前端新增 api 包装、拖拽工具库、聊天输入注册表，`FileTree` 加右键菜单与 HTML5 DnD，`TerminalView`/`ChatView`/页签条接收落点，`App.tsx` 注册 Wails `OnFileDrop` 处理外部文件拖入。设计文档：`docs/plans/2026-10-04-file-ops-design.md`。

**Tech Stack:** Go（Wails 绑定）、React 19 + TypeScript strict、Tailwind v4、Radix dialog、vitest + jsdom、原生 HTML5 DnD、Wails runtime OnFileDrop。

**工作区：** 本计划在 `.worktrees/file-ops`（分支 `feat/file-ops`）内执行。所有路径均相对该目录。

**基线（已验证全绿）：** Go 17 包全过；前端 240 测试全过。

**约定：**
- 提交信息中文 `type: 简述`（项目惯例）。
- 每个任务红→绿→重构，验证命令必须真实执行并确认输出。
- jsdom 不实现 DataTransfer，拖拽测试用合成事件对象 `{ types: [...], getData, setData, preventDefault, stopPropagation }` 代替（现有测试无先例，注意 mock 完整）。

---

### Task 1: Go — `CreateEntry` 绑定（TDD）

**Files:**
- Modify: `internal/desktop/files.go`（新增方法 + 抽 `invalidateTree` helper）
- Modify: `internal/desktop/files_ops_test.go`（追加测试）

**Step 1: 写失败测试**（追加到 `files_ops_test.go`，复用现有 `newFilesEnv(t)` 助手）

```go
func TestCreateEntry(t *testing.T) {
	env := newFilesEnv(t)
	app := env.app

	// 根层新建文件
	if _, err := app.CreateEntry(env.root, "", "new.go", false); err != nil {
		t.Fatalf("CreateEntry file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.root, "new.go")); err != nil {
		t.Fatalf("新文件不存在: %v", err)
	}
	// 子目录新建目录
	if _, err := app.CreateEntry(env.root, "pkg", "sub", true); err != nil {
		t.Fatalf("CreateEntry dir: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(env.root, "pkg", "sub")); err != nil || !fi.IsDir() {
		t.Fatalf("新目录不存在或类型不对: %v", err)
	}
	// 目标已存在
	if _, err := app.CreateEntry(env.root, "", "main.go", false); !errors.Is(err, errTargetExists) {
		t.Fatalf("重名应报 errTargetExists，got %v", err)
	}
	// 名称非法（含分隔符）
	if _, err := app.CreateEntry(env.root, "", "a/b", false); !errors.Is(err, errInvalidName) {
		t.Fatalf("含分隔符应报 errInvalidName，got %v", err)
	}
	// dirRel 逃逸
	if _, err := app.CreateEntry(env.root, "../esc", "x.go", false); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("目录逃逸应报 errPathOutsideWorkspace，got %v", err)
	}
	// 树缓存已作废：ListFiles 能看到新文件
	nodes, err := app.ListFiles(env.root, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range nodes {
		if n.Name == "new.go" {
			found = true
		}
	}
	if !found {
		t.Fatal("ListFiles 未反映新建文件（缓存未作废？）")
	}
}
```

**Step 2: 跑测试确认失败**

Run: `go test ./internal/desktop/ -run TestCreateEntry -count=1`
Expected: FAIL（`CreateEntry` 未定义，编译错误）

**Step 3: 最小实现**（`internal/desktop/files.go`）

先抽公共 helper（`RenameEntry` 末尾的缓存作废三行替换为调用它）：

```go
// invalidateTree 作废工作区的文件树缓存并推进树代数
// （见 treeFor 注释：防旧快照树写回缓存）。所有改动文件树的绑定共用。
func (a *App) invalidateTree(wsPath string) {
	a.treeMu.Lock()
	delete(a.trees, wsPath)
	a.treeMu.Unlock()
	atomic.AddUint64(&a.treeGen, 1)
}
```

`RenameEntry` 中 `a.treeMu.Lock() ... atomic.AddUint64(&a.treeGen, 1)` 一段替换为 `a.invalidateTree(wsPath)`（行为不变）。

新增方法（放在 `RenameEntry` 之后）：

```go
// CreateEntry 在工作区 dirRel 目录下新建文件/目录（isDir 区分）。
// name 只允许最后一段名字；目标已存在报错；成功后作废树缓存。
func (a *App) CreateEntry(wsPath, dirRel, name string, isDir bool) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." ||
		strings.ContainsAny(name, `/\`) {
		return "", errInvalidName
	}

	tree, err := a.treeFor(wsPath)
	if err != nil {
		return "", err
	}
	node, err := a.nodeAt(tree, wsPath, dirRel)
	if err != nil {
		return "", err
	}
	if !node.IsDir {
		return "", errNotDir
	}

	abs := filepath.Join(node.Path, name)
	if !underPath(wsPath, abs) {
		return "", errPathOutsideWorkspace
	}
	if _, err := filepath.EvalSymlinks(node.Path); err != nil {
		return "", err // 父目录是失效 junction：拒绝新建
	}
	if _, err := os.Lstat(abs); err == nil {
		return "", errTargetExists
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if isDir {
		if err := os.Mkdir(abs, 0o755); err != nil {
			return "", err
		}
	} else {
		f, err := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
		if err != nil {
			return "", err
		}
		if err := f.Close(); err != nil {
			return "", err
		}
	}

	a.invalidateTree(wsPath)
	return abs, nil
}
```

错误变量区（文件顶部 var 块）追加：

```go
errNotDir = errors.New("目标不是目录")
```

**Step 4: 跑测试确认通过**

Run: `go test ./internal/desktop/ -run TestCreateEntry -count=1 && go test ./internal/desktop/ -count=1`（第二条确认 RenameEntry 重构无回归）
Expected: PASS

**Step 5: 提交**

```bash
git add internal/desktop/files.go internal/desktop/files_ops_test.go
git commit -m "feat: 文件树新建文件/目录绑定 CreateEntry"
```

---

### Task 2: Go — `DeleteEntry` 绑定（TDD）

**Files:**
- Modify: `internal/desktop/files.go`
- Modify: `internal/desktop/files_ops_test.go`

**Step 1: 写失败测试**

```go
func TestDeleteEntry(t *testing.T) {
	env := newFilesEnv(t)
	app := env.app

	// 删文件
	if err := app.DeleteEntry(env.root, "main.go"); err != nil {
		t.Fatalf("DeleteEntry file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.root, "main.go")); !os.IsNotExist(err) {
		t.Fatal("文件应已删除")
	}
	// 删目录（含子项）
	if err := app.DeleteEntry(env.root, "pkg"); err != nil {
		t.Fatalf("DeleteEntry dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.root, "pkg")); !os.IsNotExist(err) {
		t.Fatal("目录应已删除")
	}
	// 工作区根不可删
	if err := app.DeleteEntry(env.root, ""); err == nil {
		t.Fatal("根目录不可删除")
	}
	if err := app.DeleteEntry(env.root, "."); err == nil {
		t.Fatal("根目录不可删除")
	}
	// 逃逸拒绝
	if err := app.DeleteEntry(env.root, "../outside"); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("逃逸应报 errPathOutsideWorkspace，got %v", err)
	}
	// ListFiles 反映删除
	nodes, _ := app.ListFiles(env.root, "")
	for _, n := range nodes {
		if n.Name == "readme.md" {
			return
		}
	}
	t.Fatal("readme.md 应仍在树中（未删错）")
}
```

**Step 2: 跑测试确认失败**

Run: `go test ./internal/desktop/ -run TestDeleteEntry -count=1`
Expected: FAIL（`DeleteEntry` 未定义）

**Step 3: 最小实现**（`CreateEntry` 之后追加）

```go
// DeleteEntry 永久删除工作区内的文件/目录（os.RemoveAll，不进回收站）。
// 工作区根不可删；成功后作废树缓存。
func (a *App) DeleteEntry(wsPath, relPath string) error {
	clean := filepath.Clean(strings.TrimSpace(relPath))
	if clean == "." || clean == "" || clean == string(filepath.Separator) {
		return errInvalidName // 根目录不可删
	}

	tree, err := a.treeFor(wsPath)
	if err != nil {
		return err
	}
	node, err := a.nodeAt(tree, wsPath, relPath)
	if err != nil {
		return err
	}
	if _, err := filepath.EvalSymlinks(node.Path); err != nil {
		return err
	}
	if err := os.RemoveAll(node.Path); err != nil {
		return err
	}

	a.invalidateTree(wsPath)
	return nil
}
```

**Step 4: 跑测试确认通过**

Run: `go test ./internal/desktop/ -run TestDeleteEntry -count=1 && go test ./internal/desktop/ -count=1`
Expected: PASS

**Step 5: 提交**

```bash
git add internal/desktop/files.go internal/desktop/files_ops_test.go
git commit -m "feat: 文件树删除绑定 DeleteEntry"
```

---

### Task 3: Go — `MoveEntry` 绑定（TDD）

**Files:**
- Modify: `internal/desktop/files.go`
- Modify: `internal/desktop/files_ops_test.go`

**Step 1: 写失败测试**

```go
func TestMoveEntry(t *testing.T) {
	env := newFilesEnv(t)
	app := env.app

	// 文件移入子目录（保留原名）
	newPath, err := app.MoveEntry(env.root, "main.go", "pkg")
	if err != nil {
		t.Fatalf("MoveEntry: %v", err)
	}
	if want := filepath.Join(env.root, "pkg", "main.go"); newPath != want {
		t.Fatalf("newPath = %q, want %q", newPath, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("移动后文件不存在: %v", err)
	}
	// 目标已存在
	if _, err := app.MoveEntry(env.root, "readme.md", "pkg"); !errors.Is(err, errTargetExists) {
		t.Fatalf("目标已存在应报 errTargetExists，got %v", err)
	}
	// 目录移入自身子孙目录拒绝（先重建一个待移目录）
	if err := os.Mkdir(filepath.Join(env.root, "outer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(env.root, "outer", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := app.MoveEntry(env.root, "outer", filepath.Join("outer", "inner")); err == nil {
		t.Fatal("移入子孙目录应拒绝")
	}
	// 移回原位（同目录同名）应 no-op 成功
	if _, err := app.MoveEntry(env.root, "readme.md", ""); err != nil {
		t.Fatalf("原地移动应 no-op: %v", err)
	}
	// 大小写同名放行（Windows 口径）
	if _, err := app.RenameEntry(env.root, "readme.md", "readme2.md"); err != nil {
		t.Fatal(err)
	}
	// 树缓存已作废
	nodes, _ := app.ListFiles(env.root, "")
	found := false
	for _, n := range nodes {
		if n.Name == "pkg" {
			found = true
		}
	}
	if !found {
		t.Fatal("ListFiles 应含 pkg（缓存作废后重建）")
	}
}
```

**Step 2: 跑测试确认失败**

Run: `go test ./internal/desktop/ -run TestMoveEntry -count=1`
Expected: FAIL（`MoveEntry` 未定义）

**Step 3: 最小实现**（`DeleteEntry` 之后追加）

```go
// MoveEntry 把工作区内 srcRel 移入 dstDirRel 目录（保留原名）。
// 拒绝移入自身子孙目录；目标已存在报错；原地移动视为 no-op；
// 成功后作废树缓存。
func (a *App) MoveEntry(wsPath, srcRel, dstDirRel string) (string, error) {
	tree, err := a.treeFor(wsPath)
	if err != nil {
		return "", err
	}
	src, err := a.nodeAt(tree, wsPath, srcRel)
	if err != nil {
		return "", err
	}
	dstDir, err := a.nodeAt(tree, wsPath, dstDirRel)
	if err != nil {
		return "", err
	}
	if !dstDir.IsDir {
		return "", errNotDir
	}

	dstAbs := filepath.Join(dstDir.Path, src.Name)
	if !underPath(wsPath, dstAbs) {
		return "", errPathOutsideWorkspace
	}
	// 目录不能移进自己或自己的子孙（underPath 含相等，正好覆盖两种情况）
	if underPath(src.Path, dstAbs) && !samePath(src.Path, dstAbs) {
		return "", errors.New("不能把目录移入其自身内部")
	}
	if samePath(src.Path, dstAbs) {
		return src.Path, nil // 原地 drop：no-op
	}
	if _, err := filepath.EvalSymlinks(dstDir.Path); err != nil {
		return "", err // 目标目录是失效 junction：拒绝
	}
	if _, err := os.Lstat(dstAbs); err == nil {
		return "", errTargetExists
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Rename(src.Path, dstAbs); err != nil {
		return "", err
	}

	a.invalidateTree(wsPath)
	return dstAbs, nil
}
```

**Step 4: 跑测试确认通过**

Run: `go test ./internal/desktop/ -count=1 && go vet ./... && go build ./...`
Expected: 全 PASS / 无输出

**Step 5: 提交**

```bash
git add internal/desktop/files.go internal/desktop/files_ops_test.go
git commit -m "feat: 文件树移动绑定 MoveEntry"
```

---

### Task 4: 前端 — api 包装 + 拖拽工具库（TDD）

**Files:**
- Modify: `frontend/src/lib/api.ts`（AppBindings 接口 + 三个包装函数，紧跟 `renameEntry` :370 之后）
- Create: `frontend/src/lib/dragPath.ts` + `frontend/src/lib/dragPath.test.ts`
- Create: `frontend/src/lib/chatInputRegistry.ts` + `frontend/src/lib/chatInputRegistry.test.ts`

**Step 1: 写失败测试**

`frontend/src/lib/dragPath.test.ts`：

```ts
import { describe, expect, it } from 'vitest';
import { DRAG_MIME, REL_MIME, quotePathForShell } from './dragPath';

describe('quotePathForShell', () => {
  it('普通路径不加引号', () => {
    expect(quotePathForShell('D:\\proj\\a.go')).toBe('D:\\proj\\a.go');
  });
  it('含空格路径包双引号', () => {
    expect(quotePathForShell('D:\\my file\\a.go')).toBe('"D:\\my file\\a.go"');
  });
  it('DRAG_MIME 常量稳定', () => {
    expect(DRAG_MIME).toBe('application/x-kshell-path');
    expect(REL_MIME).toBe('application/x-kshell-relpath');
  });
});
```

`frontend/src/lib/chatInputRegistry.test.ts`（仿 `terminalRegistry.test.ts` 风格）：

```ts
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { appendChatInput, registerChatInput, unregisterChatInput } from './chatInputRegistry';

describe('chatInputRegistry', () => {
  beforeEach(() => unregisterChatInput('c1'));
  it('注册后 append 投递到对应聊天输入框', () => {
    const got: string[] = [];
    registerChatInput('c1', { append: (t) => got.push(t) });
    expect(appendChatInput('c1', 'a')).toBe(true);
    expect(appendChatInput('c1', 'b')).toBe(true);
    expect(got).toEqual(['a', 'b']);
  });
  it('未注册的 id 返回 false', () => {
    expect(appendChatInput('nope', 'x')).toBe(false);
  });
  it('注销后不再投递', () => {
    registerChatInput('c1', { append: () => {} });
    unregisterChatInput('c1');
    expect(appendChatInput('c1', 'x')).toBe(false);
  });
});
```

**Step 2: 跑测试确认失败**

Run: `cd frontend && npx vitest run src/lib/dragPath.test.ts src/lib/chatInputRegistry.test.ts`
Expected: FAIL（模块不存在）

**Step 3: 最小实现**

`frontend/src/lib/dragPath.ts`：

```ts
// 文件拖放共享常量与工具：
// - DRAG_MIME 携带绝对路径（拖入终端/聊天时插入用）
// - REL_MIME 携带 '/' 分隔相对路径（树内 drop 定位 MoveEntry 的 src 用）
// 两者在同一个 dragstart 里一起写入 dataTransfer。
export const DRAG_MIME = 'application/x-kshell-path';
export const REL_MIME = 'application/x-kshell-relpath';

// quotePathForShell 路径含空白时包双引号，避免 shell 端被拆词
export function quotePathForShell(path: string): string {
  return /\s/.test(path) ? `"${path}"` : path;
}
```

`frontend/src/lib/chatInputRegistry.ts`（结构对齐 `terminalRegistry.ts`）：

```ts
// 聊天输入框注册表：ChatView 挂载时登记 append 回调，
// 文件拖入聊天页签时经 appendChatInput 投递文本（与 terminalRegistry 同思路）。
interface ChatInputHandle {
  append(text: string): void;
}

const registry = new Map<string, ChatInputHandle>();

export function registerChatInput(id: string, handle: ChatInputHandle): void {
  registry.set(id, handle);
}

export function unregisterChatInput(id: string): void {
  registry.delete(id);
}

// 返回 false 表示该聊天没有注册（已关闭或非聊天页签）
export function appendChatInput(id: string, text: string): boolean {
  const h = registry.get(id);
  if (!h) return false;
  h.append(text);
  return true;
}
```

`frontend/src/lib/api.ts`：`AppBindings` 接口里 `RenameEntry` 一行（:235）后加：

```ts
  CreateEntry(wsPath: string, dirRelPath: string, name: string, isDir: boolean): Promise<string>;
  DeleteEntry(wsPath: string, relPath: string): Promise<void>;
  MoveEntry(wsPath: string, srcRelPath: string, dstDirRelPath: string): Promise<string>;
```

`renameEntry` 函数后加三个包装（口径与 `renameEntry` 一致——先看它现写的守卫再照抄）：

```ts
// createEntry 在工作区 dirRelPath 目录下新建文件/目录，返回绝对路径
export async function createEntry(wsPath: string, dirRelPath: string, name: string, isDir: boolean): Promise<string> {
  const a = app();
  if (!a) throw new Error('未检测到 kshell 桌面端绑定');
  return a.CreateEntry(wsPath, dirRelPath, name, isDir);
}

// deleteEntry 永久删除工作区内文件/目录（后端确认框逻辑在前端完成）
export async function deleteEntry(wsPath: string, relPath: string): Promise<void> {
  const a = app();
  if (!a) throw new Error('未检测到 kshell 桌面端绑定');
  return a.DeleteEntry(wsPath, relPath);
}

// moveEntry 把 srcRelPath 移入 dstDirRelPath 目录（保留原名），返回新绝对路径
export async function moveEntry(wsPath: string, srcRelPath: string, dstDirRelPath: string): Promise<string> {
  const a = app();
  if (!a) throw new Error('未检测到 kshell 桌面端绑定');
  return a.MoveEntry(wsPath, srcRelPath, dstDirRelPath);
}
```

**Step 4: 跑测试确认通过**

Run: `cd frontend && npx vitest run src/lib/dragPath.test.ts src/lib/chatInputRegistry.test.ts && npx tsc -b --noEmit 2>/dev/null || npx tsc --noEmit`
Expected: PASS（tsc 无类型错误）

**Step 5: 提交**

```bash
git add frontend/src/lib/api.ts frontend/src/lib/dragPath.ts frontend/src/lib/dragPath.test.ts frontend/src/lib/chatInputRegistry.ts frontend/src/lib/chatInputRegistry.test.ts
git commit -m "feat: 文件操作 api 包装与拖放工具库"
```

---

### Task 5: 前端 — FileTree 右键菜单（新建/删除/复制路径/重命名触发）

**Files:**
- Create: `frontend/src/components/ContextMenu.tsx`
- Modify: `frontend/src/components/FileTree.tsx`
- Modify: `frontend/src/components/FileTree.test.tsx`（追加）

**要点（实现者自行按现有测试风格写测试先行）：**

1. `ContextMenu.tsx`——通用菜单，`createPortal` 到 `document.body`（FileTree 容器有 overflow，必须 portal）：

```tsx
// 右键菜单：fixed 定位 + portal 到 body；点击外部 / Esc 关闭。
// 菜单项由调用方传入（label + onSelect），danger 项用红色文字。
import { useEffect } from 'react';
import { createPortal } from 'react-dom';
import { cn } from '../lib/cn';

export interface MenuItem {
  label: string;
  onSelect(): void;
  danger?: boolean;
}

export default function ContextMenu({
  x, y, items, onClose,
}: {
  x: number; y: number; items: MenuItem[]; onClose(): void;
}) {
  // 贴边收敛，避免菜单溢出窗口
  const left = Math.min(x, window.innerWidth - 160);
  const top = Math.min(y, window.innerHeight - items.length * 30 - 8);
  useEffect(() => {
    const close = () => onClose();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('pointerdown', close);
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('pointerdown', close);
      window.removeEventListener('keydown', onKey);
    };
  }, [onClose]);
  return createPortal(
    <div
      role="menu"
      className="fixed z-50 min-w-36 rounded-md border border-border bg-popover p-1 shadow-md"
      style={{ left, top }}
      // 菜单自身点击不冒泡触发 window pointerdown 关闭（先于 onSelect？不会——
      // pointerdown 在 click 前触发，因此菜单项用 onPointerDown 执行并阻止冒泡）
    >
      {items.map((it) => (
        <button
          key={it.label}
          role="menuitem"
          className={cn(
            'block w-full rounded px-2 py-1 text-left text-xs transition-colors hover:bg-muted',
            it.danger && 'text-destructive',
          )}
          onPointerDown={(e) => {
            e.stopPropagation();
            it.onSelect();
          }}
        >
          {it.label}
        </button>
      ))}
    </div>,
    document.body,
  );
}
```

注意：菜单项用 `onPointerDown` 而非 `onClick`（window 的 pointerdown 关闭监听先于 click 触发）。

2. `FileTree.tsx` 改动：

- 新增状态：`menu`（`{x, y, item: TreeItem} | null`）、`creating`（`{dirRel: string, isDir: boolean} | null`）、`deleting`（`TreeItem | null`）、`renamingPath`（`string | null`，行内重命名状态上提——TreeRow 的 `renaming` 改为由 `renamingPath === item.relPath` 控制，铅笔按钮改为调 `onRenameStart(item.relPath)`，`draft` 用 `useEffect` 在 `renaming` 变 true 时重置为 `node.Name`）。
- TreeRow / 空白区 `onContextMenu={(e) => { e.preventDefault(); setMenu({ x: e.clientX, y: e.clientY, item }); }}`（空白处 item 为 null → 只出新建两项）。
- 抽 `refreshRoot()`（现 `handleRename` 里的 `listFiles(wsPath,'').then(...)` 逻辑），rename/create/delete/move 四处共用。
- 菜单项组装：
  - 文件行：重命名 / 删除 / 复制路径
  - 目录行：新建文件 / 新建文件夹 / 重命名 / 删除 / 复制路径
  - 空白：新建文件 / 新建文件夹（根目录）
- 复制路径：`navigator.clipboard.writeText(item.node.Path).then(() => notify('已复制路径', 'success')).catch(() => notify('复制失败', 'error'))`
- 删除：`setDeleting(item)` → 渲染确认 `Dialog`（用 `components/ui/dialog`，标题「删除确认」，正文含 `item.node.Name`，确认按钮 danger）→ `deleteEntry(wsPath, item.relPath)` → `notify('已删除 ' + name, 'success')` + `refreshRoot()` + `refreshGitStatus(wsPath)`。
- 新建：`setCreating({dirRel, isDir})`；目标目录未展开先展开（有 children 直接 `updateItems` 展开；无 children 调 `handleDirToggle`）；渲染行内 `Input`（同重命名行样式）在该目录子层首位（根目录则在树列表顶部），placeholder 区分「文件名」/「文件夹名」；Enter 提交 → `createEntry(wsPath, dirRel, name, isDir)` → `notify` + `refreshRoot()` + `refreshGitStatus`；Esc/失焦取消。
- 重命名菜单项：`setRenamingPath(item.relPath); setMenu(null)`（TreeRow 内 commit/cancel 后调 `onRenameEnd()` 清空）。

3. 测试（`FileTree.test.tsx` 追加，沿用现有 mock api 方式；jsdom 下 Clipboard 需 stub `navigator.clipboard.writeText`）：

- 右键目录行 → 菜单出现且含「新建文件夹」「删除」；右键文件行 → 无「新建文件夹」。
- 点「复制路径」→ clipboard 收到 `node.Path`。
- 点「删除」→ Dialog 出现；确认 → `deleteEntry` 被以 `(wsPath, relPath)` 调用。
- 新建：右键根空白 → 新建文件 → 输入名回车 → `createEntry` 被调用。

**验证**：`cd frontend && npx vitest run src/components/FileTree.test.tsx src/components/ContextMenu` → PASS；`npx tsc --noEmit` 无错误。
**提交**：`git commit -m "feat: 文件树右键菜单（新建/删除/复制路径/重命名）"`

---

### Task 6: 前端 — FileTree 树内拖拽移动

**Files:**
- Modify: `frontend/src/components/FileTree.tsx`
- Modify: `frontend/src/components/FileTree.test.tsx`（追加）

**要点：**

1. TreeRow 外层行 div 加（`renaming` 时 `draggable={false}`）：

```tsx
draggable
onDragStart={(e) => {
  e.dataTransfer.setData(DRAG_MIME, node.Path);
  e.dataTransfer.setData(REL_MIME, item.relPath);
  e.dataTransfer.effectAllowed = 'move';
}}
```

2. 目录行的行 div 加 drop 接收（文件行不加）：

```tsx
const [dropActive, setDropActive] = useState(false);
// dir 行：
onDragOver={(e) => {
  if (!e.dataTransfer.types.includes(DRAG_MIME)) return;
  e.preventDefault();
  e.dataTransfer.dropEffect = 'move';
  setDropActive(true);
}}
onDragLeave={() => setDropActive(false)}
onDrop={(e) => {
  setDropActive(false);
  if (!e.dataTransfer.types.includes(DRAG_MIME)) return;
  e.preventDefault();
  e.stopPropagation();
  const srcRel = e.dataTransfer.getData(REL_MIME);
  if (srcRel) onMoveInto(srcRel, item.relPath);
}}
```

行样式追加高亮：`cn(..., dropActive && 'ring-1 ring-primary')`。

3. FileTree 顶层：

```tsx
const handleMoveInto = (srcRel: string, dstDirRel: string) => {
  if (srcRel === dstDirRel || dstDirRel.startsWith(`${srcRel}/`)) return; // 移入自身/子孙忽略
  moveEntry(wsPath, srcRel, dstDirRel)
    .then(() => {
      useAppStore.getState().notify(`已移动到 ${dstDirRel || '根目录'}`, 'success');
      refreshRoot();
      void refreshGitStatus(wsPath);
    })
    .catch((e: unknown) => {
      useAppStore.getState().notify(e instanceof Error ? e.message : String(e), 'error');
    });
};
```

经 props 传给 TreeRow（`onMoveInto`）。

4. 测试（合成 dataTransfer）：

- 对目录行派发 drop（`{ types: [DRAG_MIME, REL_MIME], getData: (t) => t === REL_MIME ? 'main.go' : 'D:\\root\\main.go', preventDefault, stopPropagation }`）→ `moveEntry` 以 `(wsPath, 'main.go', 'pkg')` 调用。
- 拖到自己的子孙目录（`srcRel='pkg'`, `dstDirRel='pkg/sub'`）→ `moveEntry` 不被调用。

**验证**：`cd frontend && npx vitest run src/components/FileTree.test.tsx` → PASS。
**提交**：`git commit -m "feat: 文件树内拖拽移动"`

---

### Task 7: 前端 — TerminalView / ChatView 接收拖入

**Files:**
- Modify: `frontend/src/components/TerminalView.tsx`
- Modify: `frontend/src/components/ChatView.tsx`
- Modify: `frontend/src/components/TerminalView.test.tsx`（追加）

**TerminalView 要点：**

- 根 div（已有 `relative`）加 `data-drop-zone={`terminal:${termId}`}` 与：

```tsx
const [dropHint, setDropHint] = useState(false);
// 根 div：
onDragOver={(e) => {
  if (!e.dataTransfer.types.includes(DRAG_MIME) || term.Status === 'exited') return;
  e.preventDefault();
  setDropHint(true);
}}
onDragLeave={() => setDropHint(false)}
onDrop={(e) => {
  setDropHint(false);
  if (!e.dataTransfer.types.includes(DRAG_MIME) || term.Status === 'exited') return;
  e.preventDefault();
  const p = e.dataTransfer.getData(DRAG_MIME);
  if (!p) return;
  void writeTerminal(termId, encodeTerminalInput(quotePathForShell(p)));
  termRef.current?.focus();
}}
```

- 遮罩（根 div 内、xterm host 之后）：`{dropHint && <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center rounded border-2 border-dashed border-primary bg-primary/10 text-sm">松开插入文件路径</div>}`

**ChatView 要点：**

- 组件内注册输入追加（`id` 为 chat.ID）：

```tsx
useEffect(() => {
  registerChatInput(id, { append: (text) => setDraft((d) => (d ? `${d} ${text}` : text)) });
  return () => unregisterChatInput(id);
}, [id]);
```

- 根 div 加 `data-drop-zone={`chat:${id}`}`、`onDragOver`（含 DRAG_MIME 才 preventDefault）与 `onDrop` → `appendChatInput(id, quotePathForShell(p))`，未注册（返回 false）时 `notify('该会话不可接收文件', 'error')`。

**测试（TerminalView.test.tsx 追加，沿用现有 mock api）：**

- drop 携带 `DRAG_MIME='D:\\my file\\a.go'` → `writeTerminal` 以 `(id, encodeTerminalInput('"D:\\my file\\a.go"'))` 调用（引号包裹生效）。
- drop 无 DRAG_MIME → `writeTerminal` 不被调用。
- `term.Status='exited'` → drop 不写终端。

**验证**：`cd frontend && npx vitest run src/components/TerminalView.test.tsx src/components/ChatView.test.tsx` → PASS。
**提交**：`git commit -m "feat: 终端/聊天页签支持拖入文件插入路径"`

---

### Task 8: 前端 — 页签条落点 + App 全局外部拖入

**Files:**
- Modify: `frontend/src/pages/WorkspaceTab.tsx`
- Modify: `frontend/src/App.tsx`

**WorkspaceTab 要点：**

- 导入 `DRAG_MIME, quotePathForShell`（lib/dragPath）、`appendChatInput`（lib/chatInputRegistry）、`writeTerminal, encodeTerminalInput`。
- 终端页签 div（:236 的 map 内）加：

```tsx
onDragOver={(e) => {
  if (e.dataTransfer.types.includes(DRAG_MIME)) e.preventDefault();
}}
onDrop={(e) => {
  const p = e.dataTransfer.getData(DRAG_MIME);
  if (!p) return;
  e.preventDefault();
  e.stopPropagation();
  setCenterTab(t.ID); // 先切页签，插入后用户立即看到
  if (t.Status !== 'exited') void writeTerminal(t.ID, encodeTerminalInput(quotePathForShell(p)));
}}
```

- 聊天页签 div（:284 的 map 内）同理：`setCenterTab(c.ID); appendChatInput(c.ID, quotePathForShell(p));`。

**App.tsx 要点：**

- 导入 `import { OnFileDrop, OnFileDropOff } from '../wailsjs/runtime/runtime';`（相对层级按 App.tsx 实际位置调整为 `./wailsjs/runtime/runtime`）与 `quotePathForShell`、`writeTerminal`、`encodeTerminalInput`、`appendChatInput`。
- App 组件内追加（跟随现有 useEffect 区域）：

```tsx
// 外部文件拖入（资源管理器）：Wails 全窗口回调，按落点路由到终端/聊天页签。
// useDropTarget=false 关闭 Wails 自带遮罩；内部 HTML5 拖拽（无真实文件）不走这里。
useEffect(() => {
  OnFileDrop((x, y, paths) => {
    if (!paths || paths.length === 0) return;
    const el = document.elementFromPoint(x, y)?.closest('[data-drop-zone]');
    const zone = el?.getAttribute('data-drop-zone') ?? '';
    const text = quotePathForShell(paths[0]);
    if (zone.startsWith('terminal:')) {
      void writeTerminal(zone.slice('terminal:'.length), encodeTerminalInput(text));
    } else if (zone.startsWith('chat:')) {
      appendChatInput(zone.slice('chat:'.length), text);
    }
  }, false);
  return () => OnFileDropOff();
}, []);
```

注意：隐藏页签是 `display:none`，`elementFromPoint` 天然不会命中，无需额外过滤。

**验证**：`cd frontend && npx vitest run && npx tsc --noEmit` → 全 PASS（App/WorkspaceTab 现有测试不回归）。
**提交**：`git commit -m "feat: 页签条与外部文件拖入支持"`

---

### Task 9: 全量验证 + 冒烟清单

**Files:**
- Modify: `docs/smoke/feat-file-ops.md`（Desktop 分区追加本轮冒烟项）

**Step 1: 全量验证（必须全绿）**

```powershell
go build ./... ; go vet ./... ; go test ./... -count=1
cd frontend; npm test; npm run build
```

Expected: Go 无失败；vitest 全过；`tsc && vite build` 成功。

**Step 2: 冒烟清单追加条目**（人工验证项，标注「需桌面端运行」）

- 文件树右键：目录行五项、文件行三项、空白两项均出现
- 新建文件 / 新建文件夹（行内输入，Enter 提交后树刷新）
- 删除：确认框显示文件名；确认后消失
- 复制路径：剪贴板内容为绝对路径
- 树内拖拽：文件拖到目录高亮、松手移动成功；拖到自身子孙无反应
- 拖入终端：文件树拖入 → 终端出现带引号（含空格时）路径；外部资源管理器拖入同样生效
- 拖入聊天：路径出现在聊天输入框
- 拖到未激活页签标题：自动切换并插入

**Step 3: 提交**

```bash
git add docs/smoke/feat-file-ops.md
git commit -m "docs: 文件操作冒烟清单"
```

---

### Task 10: 合并收尾（finishing-a-development-branch）

- 用 `finishing-a-development-branch` 技能收尾：确认验证全绿 → 合并回 `master` → 清理 worktree 与分支。
- 合并后在 master 上 `.\build.ps1 -Desktop` 出 `dist\kshell-desktop.exe`——**构建前必须先征得用户同意**（脚本会经 `~/.kshell/exit.signal` 请求运行中的旧实例退出，用户可能正在使用）。
- 产出可执行文件确认成功后收尾完成。
