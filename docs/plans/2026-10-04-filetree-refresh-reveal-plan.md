# 文件树实时刷新 / 显示全部 / 资源管理器打开 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复桌面端文件树不刷新、补齐 showAll 与未跟踪/忽略样式，并支持右键在系统资源管理器中打开。

**Architecture:** Go 侧扩展 `ListFiles`/`SearchFiles`（`showAll`）、新增 `RefreshFiles`/`StartFileWatch`/`StopFileWatch`/`RevealInExplorer`；`GitStatus` 解析 `!!`→`ignored`；fsnotify 防抖后 `Emit("files:changed", {path})`。前端 `FileTree` 共用 `reloadTree()`（按钮 + 事件），切换 showAll，徽章 `N`/`I`，右键 Reveal。设计文档：`docs/plans/2026-10-04-filetree-refresh-reveal-design.md`。

**Tech Stack:** Go 1.23+、`github.com/fsnotify/fsnotify`、Wails v2 EventsEmit、React 19 + TypeScript strict、vitest + jsdom、Tailwind v4。

## Global Constraints

- 范围：仅桌面端本地工作区；不改 TUI；不做 SSH 文件监听；不做目录级 git 聚合；不持久化 showAll。
- 工作区隔离：在 `.worktrees/feat-filetree-refresh-reveal`（分支 `feat/filetree-refresh-reveal`）内执行；禁止在 `master` 上直接改代码。
- 提交：中文 `type: 简述`；**仅当用户明确要求时才 `git commit`**（计划中的 Commit 步骤在未授权时跳过，改记「待提交」）。
- TDD：每个任务先红后绿；验证命令必须真实跑通。
- `.git` 永远不进文件树（即使 showAll）。
- 未跟踪徽章 **`N`**（绿）；忽略 **`I`**（muted）+ 行名半透明。
- 事件 payload：`EventsEmit("files:changed", map[string]string{"path": wsPath})`。

## 文件结构（将创建/修改）

| 文件 | 职责 |
|---|---|
| `internal/workspace/gitstatus.go` | porcelain 加 `--ignored=matching`；`!!`→ignored |
| `internal/workspace/gitstatus_test.go` | 解析与集成断言 |
| `internal/desktop/files.go` | ListFiles/SearchFiles showAll；RefreshFiles；tree 缓存键；invalidate 双键 |
| `internal/desktop/files_reveal.go` + `_windows.go` / `_other.go` | RevealInExplorer 平台命令 |
| `internal/desktop/files_watch.go` | fsnotify 引用计数 + 防抖 Emit |
| `internal/desktop/files_*_test.go` | 上述绑定测试 |
| `frontend/src/lib/api.ts` | 新封装 + 签名 + GitStatusCode |
| `frontend/src/components/FileTree.tsx` | reloadTree、watch、showAll、样式、菜单 |
| `frontend/src/components/FileTree.test.tsx` | 前端行为测试 |
| `docs/smoke/feat-filetree-refresh-reveal.md` | 冒烟增量 |

---

### Task 0: 建立 worktree

**Files:** 无代码

- [ ] **Step 1: 建隔离工作区**

```powershell
git worktree add .worktrees/feat-filetree-refresh-reveal -b feat/filetree-refresh-reveal
```

- [ ] **Step 2: 确认基线**

```powershell
cd .worktrees/feat-filetree-refresh-reveal
go test ./... -count=1
cd frontend; npm test
```

Expected: 全绿。后续所有路径相对该 worktree 根。

- [ ] **Step 3: 拷入已写好的设计/计划（若 worktree 创建时尚未包含）**

从主工作区复制两份 md 到 worktree 的 `docs/plans/`（或创建分支前先把 docs 提交/带上）。确保 worktree 内可读设计文档。

---

### Task 1: Go — GitStatus 解析 `ignored`

**Files:**
- Modify: `internal/workspace/gitstatus.go`
- Modify: `internal/workspace/gitstatus_test.go`

**Interfaces:**
- Consumes: 现有 `parsePorcelainZ` / `statusFromXY`
- Produces: status 码 `"ignored"`；命令含 `--ignored=matching`

- [ ] **Step 1: 写失败测试**（追加到 `gitstatus_test.go`）

```go
func TestStatusFromXYIgnored(t *testing.T) {
	if got := statusFromXY('!', '!'); got != "ignored" {
		t.Fatalf("!! → ignored, got %q", got)
	}
}

func TestParsePorcelainZIgnored(t *testing.T) {
	// !! path\0
	data := []byte("!! skip.log\x00?? new.go\x00")
	got := parsePorcelainZ(data, `/ws`, `/ws`)
	if got["skip.log"] != "ignored" {
		t.Fatalf("skip.log: %v", got)
	}
	if got["new.go"] != "untracked" {
		t.Fatalf("new.go: %v", got)
	}
}
```

（Windows 测试里路径用 `filepath` 风格时按现有用例习惯；`parsePorcelainZ` 的 path 段是 `/` 分隔相对仓库根。）

- [ ] **Step 2: 跑测确认失败**

```powershell
go test ./internal/workspace/ -run "TestStatusFromXYIgnored|TestParsePorcelainZIgnored" -count=1
```

Expected: FAIL（`ignored` 未实现或仍为空）

- [ ] **Step 3: 最小实现**

在 `statusFromXY` 最前：

```go
if x == '!' && y == '!' {
	return "ignored"
}
```

在 `GitStatus` 的 status 命令参数追加 `"--ignored=matching"`：

```go
cmd := gitCmd(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignored=matching")
```

更新 `gitstatus_windows_test.go` 里若断言完整 args，同步加上该 flag。

- [ ] **Step 4: 跑测确认通过**

```powershell
go test ./internal/workspace/ -run "GitStatus|Porcelain|StatusFromXY" -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**（仅用户要求时）

```powershell
git add internal/workspace/gitstatus.go internal/workspace/gitstatus_test.go internal/workspace/gitstatus_windows_test.go
git commit -m "feat: git status 解析 ignored（!!）"
```

---

### Task 2: Go — `showAll` + `RefreshFiles` + 树缓存键

**Files:**
- Modify: `internal/desktop/files.go`（`treeFor`/`ListFiles`/`SearchFiles`/`invalidateTree`/`RefreshFiles`）
- Modify: `internal/desktop/files_ssh_settings_test.go`、`files_ops_test.go`（所有 `ListFiles`/`SearchFiles` 调用加 `showAll`）
- Modify: 其它引用这些方法的测试

**Interfaces:**
- Produces:
  - `ListFiles(wsPath, relPath string, showAll bool) ([]workspace.Node, error)`
  - `SearchFiles(wsPath, query string, showAll bool) ([]workspace.SearchHit, error)`
  - `RefreshFiles(wsPath string) error`
  - `treeCacheKey(wsPath string, showAll bool) string`（包内）
  - `invalidateTree` 删除 `wsPath` 与 `wsPath+"\x00all"` 两键

- [ ] **Step 1: 写失败测试**（`files_ssh_settings_test.go` 或新 `files_refresh_test.go`）

```go
func TestRefreshFilesSeesExternalCreate(t *testing.T) {
	env := newFilesEnv(t)
	if _, err := env.app.ListFiles(env.root, "", false); err != nil {
		t.Fatal(err)
	}
	// 外部写入（不经 CreateEntry，模拟资源管理器新建）
	writeFile(t, filepath.Join(env.root, "ext.go"), "package x\n")
	// 缓存未作废前：可能看不到
	before, _ := env.app.ListFiles(env.root, "", false)
	for _, n := range before {
		if n.Name == "ext.go" {
			t.Fatal("作废前不应依赖幸运命中；若已可见说明缓存未生效，调整断言策略")
		}
	}
	if err := env.app.RefreshFiles(env.root); err != nil {
		t.Fatal(err)
	}
	after, err := env.app.ListFiles(env.root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range after {
		if n.Name == "ext.go" {
			found = true
		}
	}
	if !found {
		t.Fatal("RefreshFiles 后应看到 ext.go")
	}
}

func TestListFilesShowAllRevealsIgnored(t *testing.T) {
	env := newFilesEnv(t)
	// newFilesEnv 已有 node_modules；再写 .gitignore 条目
	writeFile(t, filepath.Join(env.root, ".gitignore"), "*.tmp\n")
	writeFile(t, filepath.Join(env.root, "x.tmp"), "x")
	hidden, err := env.app.ListFiles(env.root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range hidden {
		if n.Name == "x.tmp" || n.Name == "node_modules" {
			t.Fatalf("showAll=false 不应出现 %s", n.Name)
		}
	}
	shown, err := env.app.ListFiles(env.root, "", true)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, n := range shown {
		names = append(names, n.Name)
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "x.tmp") || !strings.Contains(joined, "node_modules") {
		t.Fatalf("showAll=true 应含 x.tmp 与 node_modules: %v", names)
	}
	if strings.Contains(joined, ".git") {
		t.Fatal(".git 即使 showAll 也不应出现")
	}
}
```

注意：`TestRefreshFilesSeesExternalCreate` 里「作废前不可见」依赖 `Loaded` 缓存——先 `ListFiles` 根层把树缓存起来，再外部写文件，再 `ListFiles` 仍走缓存故看不到；若实现已改变此语义，保留「Refresh 后可见」为主断言，前置断言可改为软检查。

- [ ] **Step 2: 跑测确认失败**

```powershell
go test ./internal/desktop/ -run "TestRefreshFilesSeesExternalCreate|TestListFilesShowAllRevealsIgnored" -count=1
```

Expected: 编译失败或 FAIL

- [ ] **Step 3: 最小实现**

```go
func treeCacheKey(wsPath string, showAll bool) string {
	if showAll {
		return wsPath + "\x00all"
	}
	return wsPath
}

func (a *App) invalidateTree(wsPath string) {
	a.treeMu.Lock()
	delete(a.trees, wsPath)
	delete(a.trees, wsPath+"\x00all")
	a.treeMu.Unlock()
	atomic.AddUint64(&a.treeGen, 1)
}

func (a *App) RefreshFiles(wsPath string) error {
	cleaned := filepath.Clean(strings.TrimSpace(wsPath))
	if cleaned == "." || cleaned == "" {
		return errWorkspaceNotFound
	}
	a.invalidateTree(cleaned)
	return nil
}
```

`treeFor(wsPath string, showAll bool)`：用 `treeCacheKey` 读写 `a.trees`；`NewMatcher(wsPath, exclude, showAll)`。

`ListFiles` / `SearchFiles` 增加 `showAll bool` 并传入。全库测试调用处补第三个参数（默认 `false`）。

- [ ] **Step 4: 跑测确认通过**

```powershell
go test ./internal/desktop/ -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**（仅用户要求时）

```powershell
git commit -m "feat: 文件树 RefreshFiles 与 showAll 列表"
```

---

### Task 3: Go — `RevealInExplorer`

**Files:**
- Create: `internal/desktop/files_reveal.go`（校验 + 分发）
- Create: `internal/desktop/files_reveal_windows.go`（`explorer`）
- Create: `internal/desktop/files_reveal_other.go`（`xdg-open`/`open`）
- Create: `internal/desktop/files_reveal_test.go`

**Interfaces:**
- Produces: `RevealInExplorer(wsPath, path string) error`
- Consumes: `underPath` / 存在性检查；包内 `revealInOS(abs string, isDir bool) error`

- [ ] **Step 1: 写失败测试**

```go
func TestRevealInExplorerRejectsOutside(t *testing.T) {
	env := newFilesEnv(t)
	if err := env.app.RevealInExplorer(env.root, filepath.Join(env.root, "..", "x")); err == nil {
		t.Fatal("越界应失败")
	}
}

func TestRevealInExplorerRejectsMissing(t *testing.T) {
	env := newFilesEnv(t)
	if err := env.app.RevealInExplorer(env.root, filepath.Join(env.root, "no-such")); err == nil {
		t.Fatal("不存在应失败")
	}
}

func TestRevealExplorerArgsWindows(t *testing.T) {
	// 仅 windows 构建；测试 explorerArgs(abs, isDir) 纯函数
	got := explorerArgs(`D:\proj\a.go`, false)
	if len(got) != 1 || got[0] != `/select,D:\proj\a.go` {
		t.Fatalf("file args: %v", got)
	}
	got = explorerArgs(`D:\proj\pkg`, true)
	if len(got) != 1 || got[0] != `D:\proj\pkg` {
		t.Fatalf("dir args: %v", got)
	}
}
```

- [ ] **Step 2: 跑测确认失败**

```powershell
go test ./internal/desktop/ -run Reveal -count=1
```

Expected: FAIL

- [ ] **Step 3: 最小实现**

`files_reveal.go`：

```go
func (a *App) RevealInExplorer(wsPath, path string) error {
	cleanedWS := filepath.Clean(strings.TrimSpace(wsPath))
	abs := filepath.Clean(strings.TrimSpace(path))
	if !underPath(cleanedWS, abs) {
		return errPathOutsideWorkspace
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return err
	}
	return revealInOS(abs, fi.IsDir())
}
```

Windows：`exec.Command("explorer", explorerArgs(abs, isDir)...)`；**忽略 explorer 的退出码**（Windows 上成功也可能非 0），仅保证 `Start` 成功。用 `executil.HideWindow`。

other：目录 `xdg-open`/`open` abs；文件对其父目录。

- [ ] **Step 4: 跑测通过**

```powershell
go test ./internal/desktop/ -run Reveal -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**（仅用户要求时）

```powershell
git commit -m "feat: RevealInExplorer 在资源管理器中打开"
```

---

### Task 4: Go — fsnotify 文件监视

**Files:**
- Create: `internal/desktop/files_watch.go`
- Create: `internal/desktop/files_watch_test.go`
- Modify: `internal/desktop/app.go`（`App` 增加 watch 字段；`Startup`/退出清理可选）
- Modify: `go.mod` / `go.sum`（`go get github.com/fsnotify/fsnotify`）

**Interfaces:**
- Produces:
  - `StartFileWatch(wsPath string) error`
  - `StopFileWatch(wsPath string)`
  - 防抖后 `a.Emit("files:changed", map[string]string{"path": cleaned})`
- 包级可测变量：`fileWatchDebounce = 300 * time.Millisecond`

- [ ] **Step 1: 依赖**

```powershell
go get github.com/fsnotify/fsnotify@latest
```

- [ ] **Step 2: 写失败测试**

```go
func TestFileWatchEmitsDebounced(t *testing.T) {
	env := newFilesEnv(t)
	fileWatchDebounce = 30 * time.Millisecond
	t.Cleanup(func() { fileWatchDebounce = 300 * time.Millisecond })

	var mu sync.Mutex
	var events []string
	env.app.opts.Emit = func(name string, data ...any) {
		if name != "files:changed" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if len(data) > 0 {
			if m, ok := data[0].(map[string]string); ok {
				events = append(events, m["path"])
			}
		}
	}
	// 测试需能注入 Emit：NewAppWith 后直接设 opts.Emit；若 snapshot 拷贝问题，对照现有 Emit 测试改用同一注入方式
	if err := env.app.StartFileWatch(env.root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { env.app.StopFileWatch(env.root) })

	writeFile(t, filepath.Join(env.root, "watched.go"), "package w\n")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(events)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) < 1 || events[0] != env.root {
		t.Fatalf("期望 files:changed path=%s, got %v", env.root, events)
	}
}
```

若 `newFilesEnv` 的 App 读不到 `opts.Emit`，按 `Emit` 实现改为在 `NewAppWith` 时传入 `Options{Emit: ...}`。

- [ ] **Step 3: 跑测确认失败**

```powershell
go test ./internal/desktop/ -run TestFileWatchEmitsDebounced -count=1
```

Expected: FAIL

- [ ] **Step 4: 最小实现要点**（`files_watch.go`）

```go
type fileWatcher struct {
	refs     int
	w        *fsnotify.Watcher
	cancel   context.CancelFunc
	timer    *time.Timer
	timerMu  sync.Mutex
	root     string
}

// App 字段：
// watchMu sync.Mutex
// watches map[string]*fileWatcher
```

- `StartFileWatch`：refs++；首次创建 `fsnotify.NewWatcher`，`addRecursive(root)`（跳过名为 `.git` 的目录），goroutine 读 `Events`/`Errors`；事件路径在 `.git` 段内则忽略；否则重置 debounce timer，到时 `invalidateTree(root)` + `Emit("files:changed", map[string]string{"path": root})`
- `StopFileWatch`：refs--；0 时 cancel、Close、从 map 删除
- 新建目录事件：对该目录 `Add` watch（若仍存在）
- `Shutdown`/`BeforeClose` 若已有清理钩子：遍历 Stop 全部（可选，防泄漏）

**注意：** watcher 回调里已 `invalidateTree`，前端 `reloadTree` 仍应调 `RefreshFiles`（幂等）或仅重拉——计划约定前端仍调 `refreshFiles` 再 list，双重点击无害。

- [ ] **Step 5: 跑测通过**

```powershell
go test ./internal/desktop/ -run TestFileWatch -count=1
go test ./internal/desktop/ -count=1
```

Expected: PASS

- [ ] **Step 6: Commit**（仅用户要求时）

```powershell
git commit -m "feat: 工作区文件树 fsnotify 监视"
```

---

### Task 5: 前端 — api 封装与类型

**Files:**
- Modify: `frontend/src/lib/api.ts`

**Interfaces:**
- Produces:
  - `listFiles(wsPath, relPath, showAll = false)`
  - `searchFiles(wsPath, query, showAll = false)`
  - `refreshFiles(wsPath)`
  - `startFileWatch(wsPath)` / `stopFileWatch(wsPath)`
  - `revealInExplorer(wsPath, path)`
  - `onFilesChanged(cb: (path: string) => void): () => void`
  - `GitStatusCode` 含 `'ignored'`
  - `DesktopApp` 接口方法签名同步

- [ ] **Step 1: 改 api.ts**（无独立单测时与 Task 6 一起验）

```ts
export type GitStatusCode =
  | 'modified' | 'added' | 'deleted' | 'renamed' | 'untracked' | 'conflicted' | 'ignored';

export async function listFiles(wsPath: string, relPath: string, showAll = false): Promise<FileNode[]> {
  const a = app();
  if (!a) return [];
  return a.ListFiles(wsPath, relPath, showAll);
}

export async function refreshFiles(wsPath: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.RefreshFiles(wsPath);
}

export function onFilesChanged(cb: (path: string) => void): () => void {
  return EventsOn('files:changed', (p: { path?: string }) => {
    if (p?.path) cb(p.path);
  });
}

// startFileWatch / stopFileWatch / revealInExplorer 同风格
```

Wails 生成物 `frontend/wailsjs/go/` 若存在且被引用：以 `api.ts` 的 `DesktopApp` 手写接口为准（项目惯例）；桌面构建时再生成。测试 mock 不依赖生成物。

- [ ] **Step 2: Commit**（仅用户要求时）

```powershell
git commit -m "feat: 前端 api 增加文件树刷新/监视/reveal"
```

---

### Task 6: 前端 — `reloadTree` + 刷新按钮 + 监视生命周期

**Files:**
- Modify: `frontend/src/components/FileTree.tsx`
- Modify: `frontend/src/components/FileTree.test.tsx`

**Interfaces:**
- Consumes: Task 5 API
- Produces: `reloadTree` 行为；按钮不再只刷 git

- [ ] **Step 1: 写失败测试**

```tsx
it('刷新按钮：作废缓存并重建树（恢复展开）', async () => {
  mocks.listFiles
    .mockResolvedValueOnce(root) // 初次
    .mockResolvedValueOnce(srcChildren) // 展开 src
    .mockResolvedValueOnce(root) // refresh 根
    .mockResolvedValueOnce(srcChildren); // refresh 恢复 src
  mocks.refreshFiles = vi.fn().mockResolvedValue(undefined);
  // 在 vi.mock('../lib/api') 中增加 refreshFiles / startFileWatch / stopFileWatch / onFilesChanged
  render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
  await screen.findByText('README.md');
  fireEvent.click(screen.getByText('src'));
  await screen.findByText('main.ts');

  fireEvent.click(screen.getByRole('button', { name: '刷新文件树' }));
  await waitFor(() => {
    expect(mocks.refreshFiles).toHaveBeenCalledWith('D:\\proj');
  });
  await waitFor(() => {
    expect(mocks.listFiles).toHaveBeenCalledWith('D:\\proj', '', false);
    expect(mocks.listFiles).toHaveBeenCalledWith('D:\\proj', 'src', false);
  });
  expect(await screen.findByText('main.ts')).toBeInTheDocument();
});

it('files:changed 匹配 wsPath 时触发重载', async () => {
  let changedCb: ((p: string) => void) | null = null;
  mocks.onFilesChanged.mockImplementation((cb) => {
    changedCb = cb;
    return () => { changedCb = null; };
  });
  mocks.listFiles.mockResolvedValue(root);
  mocks.refreshFiles.mockResolvedValue(undefined);
  render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
  await screen.findByText('README.md');
  const calls = mocks.listFiles.mock.calls.length;
  await act(async () => {
    changedCb?.('D:\\proj');
  });
  await waitFor(() => {
    expect(mocks.refreshFiles).toHaveBeenCalled();
    expect(mocks.listFiles.mock.calls.length).toBeGreaterThan(calls);
  });
});
```

测试文件顶部 mock 扩展：

```ts
refreshFiles: vi.fn().mockResolvedValue(undefined),
startFileWatch: vi.fn().mockResolvedValue(undefined),
stopFileWatch: vi.fn(),
onFilesChanged: vi.fn(() => () => {}),
revealInExplorer: vi.fn().mockResolvedValue(undefined),
```

挂载断言：`startFileWatch` 被调用；unmount 调 `stopFileWatch`。

- [ ] **Step 2: 跑测确认失败**

```powershell
cd frontend; npx vitest run src/components/FileTree.test.tsx
```

Expected: FAIL

- [ ] **Step 3: 实现 `reloadTree` 与生命周期**

要点：

```ts
const collectExpanded = (items: TreeItem[]): string[] => {
  const out: string[] = [];
  const walk = (list: TreeItem[]) => {
    for (const it of list) {
      if (it.expanded && it.node.IsDir) {
        out.push(it.relPath);
        if (it.children) walk(it.children);
      }
    }
  };
  walk(items);
  return out;
};

const reloadTree = async () => {
  const expanded = items ? collectExpanded(items) : [];
  await refreshFiles(wsPath);
  const nodes = await listFiles(wsPath, '', showAll);
  let next = toItems(nodes, '');
  for (const rel of expanded) {
    try {
      const kids = await listFiles(wsPath, rel, showAll);
      next = updateItems(next, rel, (it) => ({
        ...it,
        children: toItems(kids, rel),
        expanded: true,
        error: undefined,
      })) ?? next;
    } catch { /* 单层失败忽略 */ }
  }
  setItems(next);
  void refreshGitStatus(wsPath);
};
```

`useEffect([wsPath])`：`startFileWatch` + `onFilesChanged`；cleanup `stopFileWatch` + unsubscribe。  
按钮：`aria-label="刷新文件树"` `title="刷新文件树"` → `void reloadTree()`。  
所有既有 `listFiles`/`searchFiles` 调用传入 `showAll`（本任务可先恒 `false`，Task 7 接状态）。

- [ ] **Step 4: 跑测通过**

```powershell
cd frontend; npx vitest run src/components/FileTree.test.tsx
```

Expected: PASS（可能需先改旧断言里 `listFiles` 参数含 `false`）

- [ ] **Step 5: Commit**（仅用户要求时）

```powershell
git commit -m "feat: 文件树刷新按钮与 files:changed 重载"
```

---

### Task 7: 前端 — showAll 开关 + 徽章 N/I + 忽略行样式

**Files:**
- Modify: `frontend/src/components/FileTree.tsx`
- Modify: `frontend/src/components/FileTree.test.tsx`

- [ ] **Step 1: 写失败测试**

```tsx
it('显示全部：切换后 listFiles 带 showAll=true', async () => {
  mocks.listFiles.mockResolvedValue(root);
  render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
  await screen.findByText('README.md');
  fireEvent.click(screen.getByRole('checkbox', { name: '显示全部' }));
  // 或 button name 显示全部
  await waitFor(() => {
    expect(mocks.refreshFiles).toHaveBeenCalled();
    expect(mocks.listFiles).toHaveBeenCalledWith('D:\\proj', '', true);
  });
});

it('git 标记：未跟踪为 N，忽略为 I 且行淡化', async () => {
  mocks.gitStatus.mockResolvedValue({
    Status: { 'README.md': 'untracked', 'skip.log': 'ignored' },
    IsRepo: true,
  });
  mocks.listFiles.mockResolvedValue([
    node('README.md', false),
    node('skip.log', false),
  ]);
  render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
  expect(await screen.findByText('N')).toBeInTheDocument();
  expect(screen.getByTitle('git：未跟踪')).toBeInTheDocument();
  expect(screen.getByText('I')).toBeInTheDocument();
  expect(screen.getByTitle('git：已忽略')).toBeInTheDocument();
  // 忽略行带 opacity 类（按实现选 data-attr 或 class 断言）
  expect(screen.getByText('skip.log').className).toMatch(/opacity|muted/);
});
```

把旧测试中期望 `U` 全部改为 `N`。

- [ ] **Step 2: 跑测失败 → 实现**

`GIT_BADGES`：

```ts
untracked: { label: 'N', cls: 'text-success', title: '未跟踪' },
ignored: { label: 'I', cls: 'text-muted-foreground', title: '已忽略' },
```

`TreeRow`：`const gitCode = gitMap?.[item.relPath];`（**目录也查**，去掉 `IsDir ? undefined`）。  
名称 `span`：`gitCode === 'ignored' && 'opacity-60 text-muted-foreground'`。

顶部：`<input type="checkbox" aria-label="显示全部" />` 或等价 toggle；`useState(false)`；切换 → `setShowAll` + `reloadTree`。  
`searchFiles(wsPath, q, showAll)` 同步。

- [ ] **Step 3: 跑测通过**

```powershell
cd frontend; npx vitest run src/components/FileTree.test.tsx
```

Expected: PASS

- [ ] **Step 4: Commit**（仅用户要求时）

```powershell
git commit -m "feat: 文件树显示全部与 N/I 样式"
```

---

### Task 8: 前端 — 右键「在资源管理器中打开」

**Files:**
- Modify: `frontend/src/components/FileTree.tsx`
- Modify: `frontend/src/components/FileTree.test.tsx`

- [ ] **Step 1: 写失败测试**

```tsx
it('右键菜单含在资源管理器中打开并调用 revealInExplorer', async () => {
  mocks.listFiles.mockResolvedValue(root);
  render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
  await screen.findByText('README.md');
  fireEvent.contextMenu(screen.getByText('README.md'));
  const item = await screen.findByRole('menuitem', { name: '在资源管理器中打开' });
  fireEvent.click(item);
  expect(mocks.revealInExplorer).toHaveBeenCalledWith('D:\\proj', expect.stringContaining('README.md'));
});
```

目录行同样测一条（可选）。空白处菜单**不应**出现该项。

- [ ] **Step 2: 实现菜单项**

```ts
{
  label: '在资源管理器中打开',
  onSelect: () => {
    revealInExplorer(wsPath, it.node.Path)
      .then(() => setMenu(null))
      .catch((e: unknown) => {
        useAppStore.getState().notify(e instanceof Error ? e.message : String(e), 'error');
        setMenu(null);
      });
  },
}
```

文件菜单与目录菜单都加入（建议放在「复制路径」旁）。

- [ ] **Step 3: 跑测通过**

```powershell
cd frontend; npx vitest run src/components/FileTree.test.tsx
```

Expected: PASS

- [ ] **Step 4: Commit**（仅用户要求时）

```powershell
git commit -m "feat: 文件树右键在资源管理器中打开"
```

---

### Task 9: 冒烟文档 + 全量验证

**Files:**
- Create: `docs/smoke/feat-filetree-refresh-reveal.md`

- [ ] **Step 1: 写冒烟条目**

```markdown
# feat/filetree-refresh-reveal 冒烟

- [ ] 工作区右栏：外部新建未忽略文件后约 1s 内出现在树中
- [ ] 点「刷新文件树」立即与磁盘一致，已展开目录保持展开
- [ ] 「显示全部」可看见 ignore / node_modules；关闭后隐藏；`.git` 始终无
- [ ] 未跟踪文件徽章为 N；忽略为 I 且名称更淡
- [ ] 右键文件 → 资源管理器选中该文件；右键目录 → 打开该目录
```

- [ ] **Step 2: 全量验证**

```powershell
go build ./...
go vet ./...
go test ./... -count=1
cd frontend
npm test
npm run build
```

Expected: 全绿。

- [ ] **Step 3: 自检对照设计文档成功标准 1–5，全部满足才可进入合并收尾**

- [ ] **Step 4: Commit 文档**（仅用户要求时）

```powershell
git commit -m "docs: 文件树刷新/reveal 冒烟清单"
```

---

## Spec 覆盖自检

| 规格条目 | 任务 |
|---|---|
| RefreshFiles + 按钮重建树 | Task 2, 6 |
| fsnotify 防抖 files:changed | Task 4, 6 |
| showAll List/Search | Task 2, 7 |
| 未跟踪 N / 忽略 I + 淡化 | Task 1, 7 |
| RevealInExplorer 文件选中/目录打开 | Task 3, 8 |
| 错误处理（watcher/reveal/分层失败） | Task 3, 4, 6, 8 |
| 冒烟 + 全量验证 | Task 9 |
| 不做 TUI/SSH/聚合/持久化 | Global Constraints |

## 执行交接

计划已保存到 `docs/plans/2026-10-04-filetree-refresh-reveal-plan.md`。

**两种执行方式：**

1. **Subagent-Driven（推荐）** — 每任务新开子代理，任务间审查  
2. **Inline Execution** — 本会话按 executing-plans 连续做，设检查点  

请选择一种方式继续。
