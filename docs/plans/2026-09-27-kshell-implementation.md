# kshell 实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 用 Go + Bubble Tea 实现 kshell——自动发现本机 agent 编程工具（Claude Code / Codex / Gemini CLI / 通用 provider）、聚合工作区与 session 并支持恢复/新建会话，附带工作区文件树预览与基于系统 ssh 的远程连接管理，全部在一个全屏 TUI 中完成。

**Architecture:** kshell 只做「发现 → 选择 → 交付」，不实现对话能力：选中 session 后 exec 原生 CLI 接管终端，退出后回到 TUI。所有读盘/网络操作走 `tea.Cmd` 异步执行，UI 不阻塞；解析结果落内存索引并按 `path+mtime+size` 缓存。远程能力全部委托系统 `ssh` 二进制。

**Tech Stack:** Go 1.23+、Bubble Tea v1（`github.com/charmbracelet/bubbletea`）+ Bubbles + Lipgloss、`gopkg.in/yaml.v3`、`github.com/sabhiram/go-gitignore`、标准库 `os/exec`、系统 OpenSSH。

**设计文档：** `docs/plans/2026-09-27-kshell-design.md`（已确认，实现时以此为准）

## 本机环境事实（已实测，勿凭空猜测）

| 项 | 实测结果 |
|---|---|
| Go | **未安装**，需先装（见 Task 0） |
| ssh | `C:\Windows\System32\OpenSSH\ssh.exe` |
| claude | `C:\Users\yangk\.local\bin\claude.exe` |
| codex | `D:\software\nodejs\codex.ps1`（**npm 的 PowerShell 包装脚本，不能直接 exec**） |
| opencode | `D:\software\nodejs\opencode.ps1`（同类） |
| gemini | 未安装 |
| `~/.claude/projects` | `<slug>/*.jsonl`，slug = 路径 `:`→`-`、`\`→`-`（例 `D--data-workspace`） |
| Claude 记录字段 | `cwd`、`sessionId`、`timestamp`、`type`、`gitBranch`、`version` |
| `~/.codex/sessions` | `YYYY/MM/DD/rollout-<ts>-<uuid>.jsonl`；首行 `type:"session_meta"`，`payload` 含 `session_id`、`cwd`、`timestamp`、`cli_version` |
| `~/.codebuddy/projects` | `d-data-workspace-moxi-strategy`（结构与 Claude 同源，slug 规则为 `:\`→`-`、`\`→`-`） |

**Windows 关键约束**：npm 全局安装的 CLI 在 Windows 上是 `.ps1`（可能还有同名 `.cmd`）。exec 时必须做 shim 解析——优先找同目录同名 `.cmd`，否则用 `powershell -NoProfile -Command "& '<ps1路径>' <args>"` 调用。这个逻辑必须放在 `launcher` 包统一处理，否则 codex/opencode 全部启动失败。

## 目录结构

```
go.mod                       module github.com/yangk/kshell
cmd/kshell/main.go
internal/
  config/       config.go  paths.go  config_test.go
  providers/    provider.go  claude.go  codex.go  gemini.go  generic.go  各自 _test.go + testdata/
  discovery/    tools.go  workspaces.go  index.go  *_test.go
  workspace/    tree.go  ignore.go  preview.go  *_test.go
  remote/       store.go  exec.go  scanner.go
                scanners/{sshconfig,env,spring,deploy,docs}.go  各自 _test.go + testdata/
  launcher/     launcher.go  shim_windows.go  shim_other.go  launcher_test.go
  ui/           app.go  sessions.go  files.go  remote.go  keys.go  theme.go
```

---

## M0 骨架

### Task 0: 安装 Go 工具链

**Files:** 无（环境）

**Step 1: 安装**
```powershell
winget install -e --id GoLang.Go
```
若 winget 不可用：`scoop install go`，或到 https://go.dev/dl/ 下载 Windows MSI 安装。

**Step 2: 验证**
```powershell
go version
```
Expected: `go version go1.23.x windows/amd64`（≥1.23）。若为旧版本，先升级。安装后需**重开终端**让 PATH 生效。

**Step 3: 配置**（可选但推荐，避免把 module 缓存放 C 盘）
```powershell
go env -w GOPATH=D:\software\go
```

---

### Task 1: Go module 与三区空壳 TUI

**Files:**
- Create: `go.mod`
- Create: `cmd/kshell/main.go`
- Create: `internal/ui/app.go`
- Create: `internal/ui/theme.go`

**Step 1: 初始化 module 并加依赖**
```powershell
cd D:\data\workspace\moxi\kshell
go mod init github.com/yangk/kshell
go get github.com/charmbracelet/bubbletea@latest github.com/charmbracelet/bubbles@latest github.com/charmbracelet/lipgloss@latest gopkg.in/yaml.v3@latest github.com/sabhiram/go-gitignore@latest
```

**Step 2: 写冒烟测试**（TUI 起码能渲染出三区）
```go
// internal/ui/app_test.go
func TestAppRendersThreeRegions(t *testing.T) {
    m := NewModel()
    out := m.View()
    for _, want := range []string{"kshell", "Sessions", "Files", "Remote"} {
        if !strings.Contains(out, want) {
            t.Fatalf("view missing %q:\n%s", want, out)
        }
    }
}
```

**Step 3: 运行确认失败**
```powershell
go test ./internal/ui/ -run TestAppRendersThreeRegions -v
```
Expected: FAIL（undefined: NewModel）

**Step 4: 最小实现**
`internal/ui/theme.go`：定义 `Theme`（深色配色，`lipgloss.AdaptiveColor` 或检测 `NO_COLOR` 环境变量后降级为纯色）。
`internal/ui/app.go`：`Model{ width, height, view ViewID }`，`View()` 用 `lipgloss.JoinVertical` 拼顶栏/主体/底栏；主体用 `JoinHorizontal` 拼左列表与右预览；宽度 <80 或高度 <24 时只渲染主体左栏。
`cmd/kshell/main.go`：`tea.NewProgram(ui.NewModel(), tea.WithAltScreen())`。

**Step 5: 运行确认通过**
```powershell
go test ./internal/ui/ -run TestAppRendersThreeRegions -v
go run ./cmd/kshell
```
Expected: PASS；运行能看到空三区界面，`q` 可退出。

**Step 6: 提交**
```powershell
git add -A; git commit -m "feat(ui): Go module 与三区空壳 TUI"
```

---

### Task 2: config 包（~/.kshell 路径与配置读写）

**Files:**
- Create: `internal/config/paths.go`
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Step 1: 写失败测试**
```go
func TestPathsUnderHome(t *testing.T) {
    home := t.TempDir()
    t.Setenv("USERPROFILE", home); t.Setenv("HOME", home)
    p, err := Paths()
    if err != nil { t.Fatal(err) }
    if filepath.Base(p.Root) != ".kshell" { t.Fatalf("got %s", p.Root) }
    if p.Config != filepath.Join(p.Root, "config.yaml") { t.Fatalf("got %s", p.Config) }
}

func TestLoadDefaultWhenMissing(t *testing.T) {
    // 空目录 → 返回默认配置，不报错
}

func TestSaveIsAtomicAndRoundTrips(t *testing.T) {
    // 保存后重新加载，字段相等；且目录内无 .tmp 残留
}
```

**Step 2: 运行确认失败** → `go test ./internal/config/ -v` → FAIL

**Step 3: 实现**
- `Paths()`：`os.UserHomeDir()` 下 `.kshell`，返回 `{Root, Config, Connections, Providers, Cache}`；`EnsureRoot()` 按需 `MkdirAll`。
- `Config` 结构：`ScanRoots []string`、`MaxDepth int`、`Exclude []string`（默认 `node_modules/.git/vendor/dist`）、`SSHOptions {ConnectTimeout, BatchMode, ExtraArgs}`、`Scanners map[string]bool`。
- `Load()`：文件不存在或 YAML 解析失败 → 返回默认值（损坏时先重命名为 `config.yaml.bak` 再重建）；`Save()`：写 `<file>.tmp` 后 `os.Rename` 原子替换。

**Step 4: 运行确认通过** → PASS

**Step 5: 提交** → `git commit -m "feat(config): ~/.kshell 配置读写与原子保存"`

---

## M1 发现与会话

### Task 3: provider 接口与工具检测

**Files:**
- Create: `internal/providers/provider.go`
- Create: `internal/discovery/tools.go`
- Test: `internal/discovery/tools_test.go`

**Step 1: 写失败测试**（用假 PATH 目录 + 假 home）
```go
func TestDetectFindsBinOnPath(t *testing.T)      // 造 fake claude.exe，命中
func TestDetectFallsBackToConfigDir(t *testing.T) // PATH 无，但 ~/.claude 存在 → 标记 installed-but-no-bin
func TestDetectMissingTool(t *testing.T)          // 都没有 → found=false
```

**Step 2: 运行确认失败** → FAIL

**Step 3: 实现**
```go
// internal/providers/provider.go
type Provider interface {
    ID() string                                  // "claude" | "codex" | "gemini" | 自定义 id
    DisplayName() string
    SessionRoots(home string) []string           // 会话根（glob 允许）
    Detect(home string) Detection                // {Installed, BinPath, Version, Source}
    ParseSession(path string, head []byte) (*Session, error)
    NewSessionCmd(ws string, bin string, ctx []string) Launch
    ResumeCmd(s Session, bin string) Launch
}

type Session struct {
    ID, ToolID, Workspace, Title string
    CreatedAt, UpdatedAt time.Time
    Messages int
    Path string           // 原始文件
    ResumeArgs []string
}

type Launch struct { Path string; Args []string; Dir string; ViaShell bool }
```
`discovery/tools.go`：`DetectAll(cfg)` 三路并行——`exec.LookPath`（Windows 额外用 `where`）→ 常见安装路径 → 配置目录存在性；版本探测 `<bin> --version`，3s 超时，失败置 `"unknown"`。返回 `[]Tool`，未安装的也返回（灰显用）。

**Step 4: 运行确认通过** → PASS

**Step 5: 提交** → `git commit -m "feat(discovery): provider 接口与工具检测"`

---

### Task 4: Claude provider

**Files:**
- Create: `internal/providers/claude.go`
- Create: `internal/providers/testdata/claude/basic.jsonl`（脱敏样例，字段与实测一致）
- Test: `internal/providers/claude_test.go`

**Step 1: 写失败测试**
```go
func TestParseSessionExtractsCwdAndID(t *testing.T) // cwd=D:\data\workspace, sessionId=42a6304b..., 时间、消息数
func TestSlugToWorkspace(t *testing.T)              // "D--data-workspace" → `D:\data\workspace`
func TestWorkspaceToSlug(t *testing.T)              // 反向，用于按工作区定位目录
func TestResumeCmd(t *testing.T)                    // 期望 claude --resume <id>
```

**Step 2: 运行 `claude --help` 确认 resume 参数**（在 PowerShell 执行），把真实参数写进 resume 模板；若 `--help` 输出不含 `--resume`，改用 `--continue`/`-r` 并在测试中固定。

**Step 3: 运行测试确认失败** → FAIL

**Step 4: 实现**
- 根：`~/.claude/projects`（glob `*.jsonl`，递归一层）。
- 解析：只读文件头 64KB，按行 `json.Unmarshal` 到 `map[string]any`，取首个含 `cwd` 的记录得工作区、取 `sessionId`、取首/末 `timestamp`、统计行数作消息数；取第一条 `type:"user"` 的文本前 80 字作标题。
- slug↔路径互转：`strings.NewReplacer(":", "-", `\`, "-", "/", "-")`。
- `ResumeCmd`：`claude --resume <sessionID>`，`Dir` 设为 session 的工作区；`NewSessionCmd`：`claude`（可选把上下文文件路径拼进初始 prompt 参数，以 `--help` 实测为准）。
- 解析失败的坏行：**跳过不报错**。

**Step 5: 运行确认通过** → PASS

**Step 6: 提交** → `git commit -m "feat(providers): Claude Code provider"`

---

### Task 5: Codex provider

**Files:**
- Create: `internal/providers/codex.go`
- Create: `internal/providers/testdata/codex/session_meta.jsonl`
- Test: `internal/providers/codex_test.go`

**Step 1: 写失败测试**
```go
func TestParseSessionMeta(t *testing.T)   // payload.session_id / payload.cwd / payload.timestamp
func TestSessionGlobLayout(t *testing.T)  // sessions/YYYY/MM/DD/rollout-*.jsonl 能匹配
func TestResumeCmd(t *testing.T)          // 期望 codex resume <id>（以 --help 实测为准）
```

**Step 2: 运行 `codex --help` 与 `codex resume --help` 确认参数**（PowerShell；注意它是 `.ps1`）

**Step 3: 运行测试确认失败** → FAIL

**Step 4: 实现**
- 根：`~/.codex/sessions/**/*.jsonl`（递归）。
- 解析：首行 `type=="session_meta"`，`payload` 内 `session_id`、`cwd`、`timestamp`、`cli_version`；时间从 `payload.timestamp` 或行 `timestamp` 取；消息数按行数统计。
- 工作区以 `payload.cwd` 为准（比 slug 可靠）。
- `ResumeCmd`：`codex resume <session_id>`（以实测为准），`Dir` = 工作区。

**Step 5: 运行确认通过** → PASS

**Step 6: 提交** → `git commit -m "feat(providers): Codex provider"`

---

### Task 6: Gemini provider

**Files:**
- Create: `internal/providers/gemini.go`
- Test: `internal/providers/gemini_test.go`

**说明**：本机未安装 Gemini CLI，会话目录结构无法实测。按已知默认 `~/.gemini/tmp/<hash>/` 实现，并**明确标注未验证**；若目录不存在，`Detect` 返回未安装即可，不阻塞其他功能。

**Step 1: 写失败测试** → 用 testdata 造一份假 `~/.gemini/tmp/<hash>/session-*.json`，断言能解析出 cwd/id；`Detect` 在目录缺失时返回 `Installed=false`。

**Step 2: 运行确认失败** → FAIL

**Step 3: 实现**（与 Claude 同构，路径与 resume 参数写在 provider 常量里便于一行改）

**Step 4: 运行确认通过** → PASS

**Step 5: 提交** → `git commit -m "feat(providers): Gemini provider（未实测，待校准）"`

---

### Task 7: 工作区聚合与索引缓存

**Files:**
- Create: `internal/discovery/workspaces.go`
- Create: `internal/discovery/index.go`
- Test: `internal/discovery/workspaces_test.go`、`internal/discovery/index_test.go`

**Step 1: 写失败测试**
```go
func TestGroupSessionsIntoWorkspaces(t *testing.T) // 同 cwd（大小写/分隔符归一）聚合，按 UpdatedAt 降序
func TestGitScanFindsRepos(t *testing.T)           // 造 tmp 树含 .git，深度上限生效，node_modules 被排除
func TestIndexCacheSkipsUnchanged(t *testing.T)    // 同 mtime+size 命中缓存不重解析；改 mtime 后重解析
```

**Step 2: 运行确认失败** → FAIL

**Step 3: 实现**
- `workspaces.go`：路径归一（Windows 大小写不敏感、`/`↔`\`、去尾部分隔符）→ 聚合 → `Workspace{Path, Name, LastUsed, ToolCounts, SessionCount, Source}`。`ScanGitRepos(roots, maxDepth, exclude)` 走 `filepath.WalkDir`，遇到 `.git` 记入并 `SkipDir`。
- `index.go`：`Index` 结构 + `cache/index.json`，key 为文件路径，value 为 `{mtimeUnix, size, session}`；`Scan()` 先加载缓存，仅对变化文件重新解析，解析结果合并后写回。并发用固定大小 worker 池（默认 `runtime.NumCPU()`），并设文件数上限（默认 20000）与单目录超时保护。

**Step 4: 运行确认通过** → PASS

**Step 5: 提交** → `git commit -m "feat(discovery): 工作区聚合与增量索引缓存"`

---

### Task 8: Sessions 视图（列表 + 筛选 + 摘要预览）

**Files:**
- Create: `internal/ui/sessions.go`
- Modify: `internal/ui/app.go`
- Test: `internal/ui/sessions_test.go`

**Step 1: 写失败测试**（`teatest` 驱动）
```go
func TestSessionsViewListsSessionsOfSelectedWorkspace(t *testing.T)
func TestFilterNarrowsList(t *testing.T)   // 输入 "/白名单" 后只剩匹配项
func TestPreviewShowsSummary(t *testing.T) // 右栏出现选中 session 的摘要
```

**Step 2: 运行确认失败** → FAIL

**Step 3: 实现**
- 左栏上下两段：工作区列表（`▸` 标记选中）与当前工作区 session 列表（标题 + 相对时间 `2h`/`1d`）。
- `/` 进入筛选态，模糊匹配（子串 + 大小写不敏感即可，别上复杂算法）。
- 右栏：选中 session 的详情（工具、工作区、创建/更新时间、消息数、原始文件路径、resume 命令预览）。
- 数据加载：`tea.Cmd` 调 `discovery.Scan()`，扫描期间底栏显示进度，不阻塞输入。

**Step 4: 运行确认通过** → PASS

**Step 5: 提交** → `git commit -m "feat(ui): Sessions 视图"`

---

### Task 9: launcher（启动/恢复会话 + Windows shim）

**Files:**
- Create: `internal/launcher/launcher.go`
- Create: `internal/launcher/shim_windows.go`
- Create: `internal/launcher/shim_other.go`
- Test: `internal/launcher/launcher_test.go`、`internal/launcher/shim_windows_test.go`

**Step 1: 写失败测试**
```go
func TestResolveShimPrefersCmd(t *testing.T)     // 同目录有 codex.cmd → 用 .cmd
func TestResolveShimWrapsPs1(t *testing.T)       // 只有 .ps1 → ViaShell=true，命令为 powershell -NoProfile -Command "& '...'"
func TestBuildResumeExec(t *testing.T)           // Dir=工作区，Args 与 provider 一致
func TestLaunchFailureReturnsExitCode(t *testing.T) // 假脚本 exit 3 → 返回码 3 + 输出尾部
```

**Step 2: 运行确认失败** → FAIL

**Step 3: 实现**
- `Resolve(binPath)`：**仅 Windows** 生效——后缀为 `.ps1` 时找同目录同名 `.cmd`；没有则用 `powershell -NoProfile -Command "& '<ps1>'"` 并把参数拼进同一命令行，置 `ViaShell=true`。其他平台直接返回原路径。
- `Run(launch)`：`exec.CommandContext`，`Dir` 设工作区，捕获输出与退出码；失败时返回 `Result{ExitCode, Tail}`，供 UI 显示。
- `ExecProcessCmd(launch)`：返回 `tea.ExecProcess` 的 Cmd，用于**交互式接管终端**（`tea.WithAltScreen` 下会挂起 TUI，子进程退出后自动恢复）。
- 关键：交互式路径下 stdin/stdout/stderr 直接接终端，不做管道，否则 TUI 程序（claude/codex 自己也是 TUI）会显示错乱。

**Step 4: 运行确认通过** → PASS

**Step 5: 手工冒烟**
```powershell
go run ./cmd/kshell   # 选中一个 claude session 回车 → 应进入 claude；退出后回到 kshell
```
再选一个 codex session 回车，确认 `.ps1` shim 处理正确（这是最容易翻车的地方）。

**Step 6: 提交** → `git commit -m "feat(launcher): 会话启动/恢复与 Windows shim 解析"`

---

## M2 文件树与预览

### Task 10: gitignore 过滤与懒加载文件树

**Files:**
- Create: `internal/workspace/ignore.go`
- Create: `internal/workspace/tree.go`
- Create: `internal/workspace/testdata/simple/.gitignore`
- Test: `internal/workspace/ignore_test.go`、`internal/workspace/tree_test.go`

**Step 1: 写失败测试**
```go
func TestIgnoreRespectsGitignore(t *testing.T)   // node_modules、dist 被过滤；!important 例外生效
func TestBuiltinExcludes(t *testing.T)           // 无 .gitignore 时 .git 仍被排除
func TestLazyExpandOnlyReadsOneLevel(t *testing.T) // 未展开目录不读取子节点
func TestToggleShowAll(t *testing.T)             // 关闭过滤后隐藏项出现
```

**Step 2: 运行确认失败** → FAIL

**Step 3: 实现**
- `ignore.go`：`Matcher` 组合仓库根 `.gitignore`（`go-gitignore`）+ 内置排除（`.git`、`node_modules`、`vendor`、`dist`、`build`、`*.log`）；提供 `ShowAll` 开关。目录级短路：目录被忽略则整棵跳过。
- `tree.go`：`Node{Name, Path, IsDir, Expanded, Loaded, Children}`；`Expand()` 时调用 `os.ReadDir` 只加载一层并置 `Loaded=true`。大目录（>1000 项）截断并提示。

**Step 4: 运行确认通过** → PASS

**Step 5: 提交** → `git commit -m "feat(workspace): gitignore 过滤与懒加载文件树"`

---

### Task 11: 文件预览

**Files:**
- Create: `internal/workspace/preview.go`
- Test: `internal/workspace/preview_test.go`

**Step 1: 写失败测试**
```go
func TestPreviewTextFile(t *testing.T)      // 正常文本带行号
func TestPreviewTruncatesLargeFile(t *testing.T) // >512KB 截断并附提示行
func TestPreviewBinary(t *testing.T)        // PNG → 显示类型/大小/修改时间，不输出乱码
func TestPreviewPermissionDenied(t *testing.T)   // 返回友好错误，不 panic
```

**Step 2: 运行确认失败** → FAIL

**Step 3: 实现**：`Preview(path, maxBytes=512KB, maxLines)`；二进制按前 8000 字节含 NUL 判定；UTF-8 非法字节替换为 `�`；返回 `Preview{Lines []string, Truncated bool, Info}`，右栏滚动展示。

**Step 4: 运行确认通过** → PASS

**Step 5: 提交** → `git commit -m "feat(workspace): 文件预览"`

---

### Task 12: Files 视图与上下文篮

**Files:**
- Create: `internal/ui/files.go`
- Modify: `internal/ui/app.go`
- Test: `internal/ui/files_test.go`

**Step 1: 写失败测试**
```go
func TestFilesViewExpandsDirectory(t *testing.T)
func TestSpaceAddsToContextBasket(t *testing.T)  // 顶栏计数 +1，再按一次移除
func TestNewSessionInjectsBasket(t *testing.T)   // n → 生成的启动命令包含篮内路径
```

**Step 2: 运行确认失败** → FAIL

**Step 3: 实现**：左栏渲染 `workspace.Tree`（`▸/▾` 展开态），右栏 `Preview`；`space` 切换上下文篮（去重、最多 20 项，超出提示）；`n` 新建 session：工作区为根、篮内路径注入到启动参数（按 provider 的 `NewSessionCmd` 契约）。

**Step 4: 运行确认通过** → PASS

**Step 5: 提交** → `git commit -m "feat(ui): Files 视图与上下文篮"`

---

## M3 远程 SSH

### Task 13: 连接存储

**Files:**
- Create: `internal/remote/store.go`
- Test: `internal/remote/store_test.go`

**Step 1: 写失败测试**
```go
func TestAddAndRoundTrip(t *testing.T)
func TestFilterByWorkspace(t *testing.T)   // 只返回绑定该工作区或全局(workspace 为空)的连接
func TestNoSecretContentPersisted(t *testing.T) // IdentityFile 只存路径；Password 字段不落盘
func TestAtomicWriteNoTempLeft(t *testing.T)
```

**Step 2: 运行确认失败** → FAIL

**Step 3: 实现**：`Connection{ID, Name, Host, User, Port, IdentityFile, Workspace, Source, SourceFile, Verified, LastUsed}`；`~/.kshell/connections.yaml`；`Add/Update/Delete/List(ws)`；写用 tmp+rename。**存盘前校验**：`Password` 非空则拒绝落盘（只保留运行时内存态）。

**Step 4: 运行确认通过** → PASS

**Step 5: 提交** → `git commit -m "feat(remote): 连接存储与工作区绑定"`

---

### Task 14: 扫描器框架 + ssh config scanner

**Files:**
- Create: `internal/remote/scanner.go`
- Create: `internal/remote/scanners/sshconfig.go`
- Create: `internal/remote/scanners/testdata/ssh_config`
- Test: `internal/remote/scanners/sshconfig_test.go`

**Step 1: 写失败测试**
```go
func TestParseSSHConfigHosts(t *testing.T) // Host/HostName/User/Port/IdentityFile；通配符 Host * 不作为独立连接
```

**Step 2: 运行确认失败** → FAIL

**Step 3: 实现**
```go
// internal/remote/scanner.go
type Candidate struct {
    Name, Host, User string
    Port int
    IdentityFile string
    Confidence string   // "high" | "medium" | "low"
    Source string       // "sshconfig" | "env" | "spring" | "deploy" | "docs"
    SourceFile string
    SourceLine int
}
type Scanner interface {
    ID() string
    Match(relPath string) bool
    Extract(root, absPath string) ([]Candidate, error)
}
```
扫描编排：`ScanWorkspace(root, enabled)` 遍历工作区文件（跳过 `exclude`、单文件 >2MB、总数上限 5000），把各 scanner 结果按 `host+user+port` 折叠去重（保留置信度最高者与来源），按置信度排序。**所有 scanner 只读，绝不写文件。**
`sshconfig.go`：解析 `~/.ssh/config`（全局，非工作区内）与工作区内的 `ssh_config`；支持 `Include` 时忽略即可（YAGNI）。置信度 high。

**Step 4: 运行确认通过** → PASS

**Step 5: 提交** → `git commit -m "feat(remote): 扫描器框架与 ssh config scanner"`

---

### Task 15: env 与 spring scanner

**Files:**
- Create: `internal/remote/scanners/env.go`
- Create: `internal/remote/scanners/spring.go`
- Create: `internal/remote/scanners/testdata/{app.env,application.yml,application.properties}`
- Test: 各自 `_test.go`

**Step 1: 写失败测试**
```go
func TestEnvScanner(t *testing.T)   // SSH_HOST/SSH_USER/SSH_PORT/SSH_KEY；缺 host 不产出
func TestSpringScannerYaml(t *testing.T)   // 含 host/user/port/key 的配置段被提取
func TestSpringScannerProperties(t *testing.T)
func TestSpringScannerIgnoresUnrelated(t *testing.T) // datasource.url 之类不误报为 ssh 连接
```

**Step 2: 运行确认失败** → FAIL

**Step 3: 实现**
- `env.go`：匹配 `.env`、`.env.*`；字段名从 config 可配（默认 `SSH_HOST/SSH_HOSTNAME/SSH_USER/SSH_PORT/SSH_KEY/SSH_IDENTITY_FILE`）；识别 `user@host` 合并写法。置信度 medium。
- `spring.go`：匹配 `application*.{yml,yaml,properties}`；用 YAML 遍历（yaml.v3 解析到 `map[string]any`）找同一层级同时含 host 与（user 或 port 或 key）的节点；properties 按 `.` 前缀分组后同样判定；**排除** `datasource`/`redis`/`kafka`/`kafka`/`elasticsearch` 等明显非 ssh 的段。置信度 medium。

**Step 4: 运行确认通过** → PASS

**Step 5: 提交** → `git commit -m "feat(remote): env 与 spring 连接扫描器"`

---

### Task 16: deploy 与 docs/keys scanner

**Files:**
- Create: `internal/remote/scanners/deploy.go`
- Create: `internal/remote/scanners/docs.go`
- Create: `internal/remote/scanners/testdata/{docker-compose.yml,Makefile,deploy.sh,inventory,package.json,README.md}`
- Test: 各自 `_test.go`

**Step 1: 写失败测试**
```go
func TestDeployScannerFindsHosts(t *testing.T) // compose 的 DOCKER_HOST、Makefile 的 ssh 目标、ansible inventory、deploy.sh 里的 ssh 命令行
func TestDeployScannerLowConfidence(t *testing.T)
func TestDocsScanner(t *testing.T)  // README/CLAUDE.md 中 user@host 与 .ssh/ 下私钥文件名；私钥内容绝不入库
```

**Step 2: 运行确认失败** → FAIL

**Step 3: 实现**
- `deploy.go`：正则 `ssh\s+(?:-p\s*(\d+)\s+)?(?:-i\s+(\S+)\s+)?([\w.-]+)@([\w.-]+)` 与 `DOCKER_HOST=ssh://user@host:port`；置信度 low。
- `docs.go`：Markdown 里 `[\w.-]+@[\w.-]+\.[a-z]{2,}` 提取候选，结合同仓库 `.ssh/` 下私钥文件（`id_rsa`/`id_ed25519` 等，仅记路径）补足 identity；置信度 low。**禁止读取私钥内容**。

**Step 4: 运行确认通过** → PASS

**Step 5: 提交** → `git commit -m "feat(remote): deploy 与文档/密钥扫描器"`

---

### Task 17: 候选导入 UI

**Files:**
- Create: `internal/ui/candidates.go`
- Modify: `internal/ui/remote.go`（本任务先建占位）
- Test: `internal/ui/candidates_test.go`

**Step 1: 写失败测试**
```go
func TestCandidatesSortedByConfidence(t *testing.T)
func TestLowConfidenceUncheckedByDefault(t *testing.T)
func TestImportWritesOnlyChecked(t *testing.T)  // 勾选项写入 store，未勾选不写
```

**Step 2: 运行确认失败** → FAIL

**Step 3: 实现**：列表展示 `名称 / user@host:port / 置信度徽标 / 来源文件:行号`；`space` 勾选、`a` 全选当前置信度组、`enter` 导入；导入后提示「已导入 N 条」并保留 `Source/SourceFile` 便于回溯。

**Step 4: 运行确认通过** → PASS

**Step 5: 提交** → `git commit -m "feat(ui): 连接候选导入"`

---

### Task 18: ssh 执行

**Files:**
- Create: `internal/remote/exec.go`
- Test: `internal/remote/exec_test.go`

**Step 1: 写失败测试**（注入假 ssh 脚本）
```go
func TestBuildArgs(t *testing.T)   // 断言参数顺序含 -o BatchMode=yes -o ConnectTimeout=5 -p <port> -i <key> user@host -- cmd
func TestRunCapturesOutputAndExitCode(t *testing.T) // 假 ssh stdout/stderr + exit 7
func TestRunTimeout(t *testing.T)  // 假 ssh sleep → 超时返回错误且进程被杀
func TestMissingSSHBinary(t *testing.T) // 返回明确错误与安装建议
func TestNoPasswordOnCommandLine(t *testing.T) // 存在密码配置时报错拒绝，不拼进 argv
```

**Step 2: 运行确认失败** → FAIL

**Step 3: 实现**
- `BuildArgs(c Conn, cmd string, opts)`：`-o BatchMode=yes -o ConnectTimeout=<n> [-p port] [-i identity] <user@host> -- <cmd>`；额外参数来自 `config.SSHOptions.ExtraArgs`。
- `Run(ctx, conn, cmd)`：`exec.CommandContext(sshBin, args...)`，分别捕获 stdout/stderr，返回 `Result{Stdout, Stderr, ExitCode, Duration}`；超时由 `context.WithTimeout` 控制（默认 60s，可配）。
- `ShellCmd(conn)`：`ssh -t <user@host>`，供 `tea.ExecProcess` 使用。
- `sshBin` 探测：`exec.LookPath("ssh")`，失败时返回「未找到 ssh，Windows 请在可选功能中启用 OpenSSH 客户端」。
- **不绕过 known_hosts / 不传密码**。

**Step 4: 运行确认通过** → PASS

**Step 5: 提交** → `git commit -m "feat(remote): ssh 命令拼装与执行"`

---

### Task 19: Remote 视图

**Files:**
- Create: `internal/ui/remote.go`
- Test: `internal/ui/remote_test.go`

**Step 1: 写失败测试**
```go
func TestRemoteListsConnectionsOfWorkspace(t *testing.T)
func TestExecuteShowsOutputInPreview(t *testing.T)  // x 输入命令 → 输出进右栏 + 返回码
func TestVerifiedBadge(t *testing.T)                // t 连通性测试后显示状态
```

**Step 2: 运行确认失败** → FAIL

**Step 3: 实现**：左栏连接列表（按工作区过滤，显示 `名称 / user@host / 来源 / 连通状态`）；`x` 打开命令输入（带历史上下翻）、输出走 `tea.Cmd` 异步并在右栏可滚动展示；`s` 交互式 shell（`tea.ExecProcess` + `shellCmd`）；`t` 连通性测试；`e` 编辑、`d` 删除、`b` 绑定到当前工作区、`i` 触发扫描导入（跳 Task 17 界面）。

**Step 4: 运行确认通过** → PASS

**Step 5: 手工冒烟**：用一条真实服务器连接执行 `hostname`、`uname -a`，再进交互式 shell 并退出回到 kshell。

**Step 6: 提交** → `git commit -m "feat(ui): Remote 视图与远程命令执行"`

---

## M4 打磨与发布

### Task 20: 通用 yaml provider（D 类工具）

**Files:**
- Create: `internal/providers/generic.go`
- Create: `config/providers.yaml`（模板，随二进制 embed 一份默认）
- Test: `internal/providers/generic_test.go`

**Step 1: 写失败测试**
```go
func TestGenericProviderFromYAML(t *testing.T)     // name/detect/sessionsGlob/format/字段映射/resume 模板
func TestGenericProviderParsesFixture(t *testing.T) // 按映射提取 cwd/id/timestamp
func TestBuiltinCodeBuddyPreset(t *testing.T)      // ~/.codebuddy/projects/<slug>/*.jsonl 能命中（本机实测存在）
```

**Step 2: 运行确认失败** → FAIL

**Step 3: 实现**
```yaml
# ~/.kshell/providers.yaml
providers:
  - id: codebuddy
    name: CodeBuddy
    detect: { command: "codebuddy", dirs: ["~/.codebuddy"] }
    sessions: { glob: "~/.codebuddy/projects/*/*.jsonl", format: jsonl }
    fields: { cwd: "cwd", id: "sessionId", timestamp: "timestamp", title: "message.content" }
    resume: { args: ["--resume", "{id}"] }        # 未验证：需实测校准
    verified: false
```
- `generic.go` 按 YAML 生成 provider；`verified: false` 的项在 UI 中标注「未验证」。
- 预置 CodeBuddy / OpenCode / Cline-Roo 的默认猜测（**全部标 `verified: false`**）。
- 未识别目录提示：扫描时若发现 `~/.<name>` 目录但无 provider 匹配，状态栏提示可配置。

**Step 4: 运行确认通过** → PASS

**Step 5: 提交** → `git commit -m "feat(providers): 通用 yaml provider 与 D 类工具预置"`

---

### Task 21: 帮助、主题、小屏适配、错误收口

**Files:**
- Modify: `internal/ui/keys.go`（帮助面板）、`internal/ui/theme.go`、`internal/ui/app.go`
- Test: `internal/ui/keys_test.go`

**Step 1: 写失败测试**：`?` 打开帮助且列出全部键位；宽度 <80 渲染为单栏；`NO_COLOR=1` 时输出无 ANSI 转义。

**Step 2: 实现**：统一 `Help` 面板数据来自键位定义（DRY，避免帮助与实现漂移）；状态栏消息分级（info/warn/error）；把各处的错误收敛为「状态栏提示 + 日志路径」。

**Step 3: 运行确认通过** → PASS

**Step 4: 提交** → `git commit -m "feat(ui): 帮助面板、主题与小屏适配"`

---

### Task 22: 构建脚本、README、冒烟清单

**Files:**
- Create: `Makefile`（`build` / `test` / `lint` / `install`）
- Create: `README.md`
- Create: `.github/workflows/ci.yml`（`go build` + `go test ./...`，矩阵 windows/linux/macos）
- Create: `docs/smoke-test.md`

**Step 1: 交叉编译验证**
```powershell
go build ./... && go test ./...
GOOS=linux GOARCH=amd64 go build -o dist/kshell-linux-amd64 ./cmd/kshell
```

**Step 2: README**：安装（`go install github.com/yangk/kshell/cmd/kshell@latest`）、键位表、配置说明、provider 自定义指南。

**Step 3: `docs/smoke-test.md`** 冒烟清单（Windows 本机 + Linux）：
- Sessions：claude/codex session 可列出、可恢复、退出回 kshell；
- Files：文件树展开、gitignore 生效、大文件截断、二进制提示；
- Remote：`~/.ssh/config` 扫描导入、命令执行、交互式 shell、`s` 退出回 kshell；
- 异常：删掉一个工作区目录 → 显示 `missing`；破坏一个 jsonl → 提示解析失败数；`PATH` 移除 ssh → 给出安装提示。

**Step 4: 提交** → `git commit -m "chore: 构建脚本、CI 与冒烟清单"`

---

## 执行注意

- **TDD 纪律**：每个 Task 严格按「写测试 → 看它失败 → 最小实现 → 看它变绿 → 提交」五步走，不许跳过失败验证。
- **不要凭猜测写命令参数**：claude/codex/gemini 的 resume 参数必须 `--help` 实测后写死在测试里；D 类工具路径标 `verified: false`。
- **fixtures 必须脱敏**：从 `~/.claude`、`~/.codex` 复制样例时只取 3-5 行并替换绝对路径与内容文本。
- **Windows 一等公民**：任何新命令执行路径都要过一遍 `launcher.Resolve`，别直接 `exec.Command(binPath)`。
