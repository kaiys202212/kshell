# kshell 项目化管理 + opencode 会话发现 实现计划

日期：2026-10-02
设计：[2026-10-02-kshell-projects-and-opencode-design.md](./2026-10-02-kshell-projects-and-opencode-design.md)
执行方式：TDD（先写失败测试，再实现）；每个任务结束跑该包测试 + `go vet`；收尾跑全量。

约定：
- Go 用 `$env:Path='D:\software\go\bin;'+$env:Path` 前缀（Go 不在系统 PATH）。
- 前端在 `frontend/` 目录 `npm test` / `npm run build`；vitest 已钉 3.2.4。
- 中文注释与提交信息。

---

## T1 内置 provider 清单与去重

**问题**：老用户 `~/.kshell/providers.yaml` 里仍有 `opencode` 段（`EnsureProvidersFile` 只在文件缺失时写模板），而我们要把它升级为内置 provider，不去重会出现两个 `ID=opencode`。

**文件**
- `internal/providers/provider.go`：新增
  ```go
  // Builtins 返回内置工具清单（顺序即 UI 展示顺序）。
  func Builtins() []Provider {
      return []Provider{Claude{}, Codex{}, Gemini{}, Opencode{}}
  }

  // MergeProviders 把内置清单与 yaml 里的自定义定义合并，按 ID 去重（内置优先）：
  // 老用户的 providers.yaml 里可能还留着已被内置取代的条目（如 opencode）。
  func MergeProviders(builtins []Provider, specs []GenericSpec, home string) []Provider
  ```
  测试 `internal/providers/provider_test.go`：`TestMergeProvidersSkipsBuiltinIDs`（specs 含 `opencode` 与 `codebuddy` → 结果 ID 集合 = claude/codex/gemini/opencode/codebuddy，且 `opencode` 是内置实现而非 `Generic`）、`TestMergeProvidersEmptySpecs`。
- `internal/providers/generic.go`：`DefaultProvidersYAML` 删掉 `opencode` 段（注释同步：opencode 已是内置 provider，不再需要在此配置）。
- `internal/providers/generic_test.go`：如断言了模板内容/条目数，同步更新。

**装配三处改为同源**
- `internal/desktop/app.go`（`initRealDeps`）：`ps := providers.MergeProviders(providers.Builtins(), specs, home)`（`LoadGenericSpecs` 失败时传 nil）。
- `cmd/kshell/main.go`
- `internal/ui/app.go`（`NewModelWith` 的默认值）

**验收**：`go test ./internal/providers/... ./internal/ui/... ./cmd/... -count=1` 绿。

---

## T2 `executil.ResolveShim` 下沉

**目的**：providers 需要执行工具自身 CLI，但 `providers` 不能 import `launcher`（`launcher` import `providers`，会成环）。shim 解析（Windows 上 npm 装的 `.ps1` 要改走 `powershell -Command`）本质属于「跨平台子进程辅助」，下沉到 `internal/executil` 后两边共用。

**文件**
- `internal/executil/shim_windows.go`（新，从 `internal/launcher/shim_windows.go` 迁移）：导出
  ```go
  // ResolveShim 处理 Windows 上 npm 全局 CLI 的 .ps1 包装：
  // 优先用同目录 .cmd 兄弟文件，否则改走 powershell -Command。
  func ResolveShim(path string, args []string) (string, []string)
  ```
- `internal/executil/shim_other.go`（新）：非 Windows 原样返回。
- `internal/launcher/shim_windows.go` / `shim_other.go`：删除；`launcher.Resolve` 改为调用 `executil.ResolveShim`（`launcher.Spec` 类型与语义不变，调用方零改动）。
- 测试迁移：`internal/launcher/shim_windows_test.go` → `internal/executil/shim_windows_test.go`（用例原样搬）。

**验收**：`go vet ./...` + `go test ./internal/executil/... ./internal/launcher/... -count=1` 绿。

---

## T3 opencode provider（CLI 查询 SQLite）

**文件**
- `internal/providers/provider.go`：新增可选接口
  ```go
  // SessionEnumerator 给「会话不在文件里」的工具用（如 opencode 的 SQLite 库）：
  // 扫描时调用工具自身 CLI 枚举会话，绕过文件遍历与解析缓存。
  type SessionEnumerator interface {
      EnumerateSessions(home, bin string) ([]Session, error)
  }
  ```
- `internal/providers/opencode.go`（新）：
  ```go
  // Opencode 是内置的 opencode 适配：检测/新建/恢复走 CLI，
  // 会话发现走 SQLite（新版 opencode 已把 JSON 会话迁移到 ~/.local/share/opencode/opencode.db，
  // 文件目录里只剩 session_diff 垃圾），因此实现 SessionEnumerator 由 Scan 直接调用。
  type Opencode struct{}

  const opencodeSessionsSQL = `select id, directory as cwd, title, time_created as created, time_updated as updated
  from session where parent_id is null and time_archived is null order by time_updated desc`

  func (Opencode) ID() string { return "opencode" }
  func (Opencode) Name() string { return "OpenCode" }
  func (Opencode) DetectSpec(home string) DetectSpec   // command: opencode, dirs: ~/.local/share/opencode
  func (Opencode) SessionRoots(home string) []string   // nil：不走文件遍历
  func (Opencode) SessionFilePattern() string          // ""
  func (Opencode) ParseSession(...) (Session, error)   // 不会被调用，返回明确错误
  func (Opencode) NewSessionCmd(ws, bin string) Launch // Dir: ws
  func (Opencode) ResumeCmd(s Session, bin string) Launch // ["--session", s.ID], Dir: s.Workspace
  func (Opencode) EnumerateSessions(home, bin string) ([]Session, error)
  ```
  - `EnumerateSessions`：`bin == ""` → 返回 nil,nil（只命中配置目录时静默跳过）；`executil.ResolveShim(bin, []string{"db", sql, "--format", "json"})` → `exec.CommandContext(20s)` → 解析 JSON 数组（行结构 `{id,cwd,title,created,updated}`，`created/updated` 是毫秒 epoch）→ 过滤空 cwd/空标题 → `Session{Path: filepath.Join(home,".local","share","opencode","opencode.db")}`。
  - 查询失败时把 stderr 摘要并入错误（便于排障）。
  - 为了可测，把「执行命令」抽成包级变量 `var runOpencodeQuery = execOpencodeQuery`（返回原始 stdout），或导出内部函数 `parseOpencodeRows([]byte)` 单测直接测解析 + 用 `runOpencodeQuery` 覆盖失败分支。
- 测试 `internal/providers/opencode_test.go`：
  - `TestOpencodeParseSessions`：固定 JSON → 会话字段（ID/ToolID/Workspace 归一化/Title/毫秒时间转换）
  - `TestOpencodeParseSkipsBlankRows`：空 cwd/空 title 行被丢弃
  - `TestOpencodeParseBadJSON`：非 JSON 输出返回错误
  - `TestOpencodeEnumerateWithoutBin`：`bin == ""` → nil,nil 且不执行命令
  - `TestOpencodeResumeCmd`：`["--session", id]` + Dir=工作区

**验收**：`go test ./internal/providers/... -count=1` 绿。

---

## T4 `discovery.Scan` 接入枚举器

**文件**
- `internal/discovery/index.go`：文件收集与 git 扫描之后、`dedupeSessions` 之前：
  ```go
  // 文件遍历之外还有「会话不在文件里」的工具（opencode 的 SQLite）：
  // 逐个解析 CLI 路径后枚举，失败只记 failed 不中断扫描。
  for _, p := range ps {
      en, ok := p.(providers.SessionEnumerator)
      if !ok { continue }
      bin := providers.Detect(p.DetectSpec(home), home).BinPath
      sess, err := en.EnumerateSessions(home, bin)
      if err != nil { failed = append(failed, fmt.Sprintf("%s 会话查询失败: %v", p.Name(), err)); continue }
      sessions = append(sessions, sess...)
  }
  ```
  仍需检查 `Detect` 的返回结构（`Detected{BinPath, Installed, ...}`）与 `Scan` 里 `failed` 的变量名。
- 测试 `internal/discovery/index_test.go`：
  - `TestScanIncludesEnumeratorSessions`：注册一个假 provider（实现 Provider + SessionEnumerator）→ 会话进入 `res.Sessions`，并参与工作区聚合。
  - `TestScanRecordsEnumeratorFailure`：假 provider 返回错误 → `res.Failed` 含一条且扫描成功返回。
- 前端 `SessionList`/状态栏文案：`Failed` 现在是「项」而非纯文件，把 `frontend` 里的「N 个文件解析失败」改成「N 项扫描失败」（`grep -rn "解析失败" frontend/src`）。

**验收**：`go test ./internal/discovery/... ./internal/providers/... -count=1` 绿。

---

## T5 去掉上下文篮（Go 侧）

**文件与改法**
1. `internal/providers/provider.go`
   - `NewSessionCmd(ws, bin string) Launch`（去掉 `ctx []string`）
   - 删除 `ContextPrompt`
2. 各 provider：`claude.go` / `codex.go` / `gemini.go` / `generic.go` 的 `NewSessionCmd` 签名同步收敛（含原先拼 ctx 的代码一并删除）。
3. `internal/launch/launch.go`：`ForWorkspace` / `ForWorkspaceTool` 去掉 `ctx []string` 参数与向下透传。
4. `internal/desktop/files.go`：删 `basket` 字段、`ToggleBasket`、`GetBasket`、`basketSnapshot`、`maxBasket`，以及 `RenameEntry` 里的篮子路径同步（若还有本地/远程路径重命名连带逻辑，逐个甄别后只删篮子部分）。
5. `internal/desktop/terminal.go`：删取 `basketSnapshot` 的调用与传参。
6. `internal/ui/{app,files}.go`：删 `basket` 字段、space 入篮处理、文件树里的入篮标记渲染、状态行「篮 N」、帮助文案里的篮子说明；`internal/ui/launch` 调用点同步。
7. 测试：`internal/desktop/*_test.go`、`internal/ui/{files,sessions}_test.go`、`internal/launch/*_test.go`、`internal/providers/*_test.go` 里篮子相关用例删除或改写（断言「新建会话参数里不再有文件清单」可作为正向用例保留一条）。
8. 文档：`docs/smoke-test.md` / `docs/smoke-test-desktop.md` 里的篮子冒烟项删除。

**验收**：`go vet ./...` 干净 + `go test ./... -count=1` 全绿；`grep -rn basket internal/ cmd/` 无残留。

---

## T6 去掉上下文篮（前端）

**文件与改法**
- 删 `frontend/src/components/BasketBar.tsx`、`frontend/src/lib/basket.ts` 及其测试。
- `frontend/src/state/store.ts`：删篮状态字段与 `syncBasket`/`toggleBasket`（以实际命名与引用为准）。
- `frontend/src/lib/api.ts`：删 `toggleBasket`/`getBasket` 绑定。
- `frontend/src/pages/WorkspaceTab.tsx`：删 `<BasketBar/>` 挂载与相关 props。
- `frontend/src/components/FileTree.tsx` / `Preview.tsx`：删入篮按钮、入篮标记、`basketPaths` 之类的判断与样式。
- `frontend/src/App.tsx`：删启动时拉篮的初始化。
- `frontend/src/styles.css`：删只服务于篮子的样式类（先 grep 确认无其他使用方）。
- 测试：`*.test.tsx` 里篮子用例删除；受影响快照/断言同步。

**验收**：前端 `npm test` 绿 + `npm run build` 通过 + `grep -rni basket frontend/src` 无残留。

---

## T7 项目表 `ProjectStore` + `ApplyProjects`

**文件**
- `internal/discovery/projects.go`（新）
  ```go
  type DeletedProject struct {
      Path string     `yaml:"path" json:"path"`
      At   time.Time  `yaml:"at" json:"at"`
  }

  // ProjectStore 是 ~/.kshell/projects.yaml 的内存镜像：
  // 手动添加的项目目录 + 逻辑删除（隐藏）的项目。
  type ProjectStore struct { path string; mu sync.RWMutex; data projectFile }

  func NewProjectStore(path string) *ProjectStore
  func (s *ProjectStore) Load() error          // 文件缺失→空表；损坏→空表 + 错误
  func (s *ProjectStore) Manual() []string
  func (s *ProjectStore) Deleted() []DeletedProject
  func (s *ProjectStore) IsDeleted(p string) bool
  func (s *ProjectStore) Add(p string) error   // 去重 + 顺手从 deleted 移出
  func (s *ProjectStore) Hide(p string) error  // 记入 deleted（带时间），保留 manual 记录
  func (s *ProjectStore) Restore(p string) error // 从 deleted 移出
  func (s *ProjectStore) Save() error          // 原子写（tmp + rename），0655→0600 权限与既有缓存文件一致
  ```
- 同文件 `ApplyProjects(ws []Workspace, st *ProjectStore, exists func(string) bool) []Workspace`：
  - `st == nil` → 原样返回（TUI/测试可省）。
  - `exists == nil` → 用 `dirExists`（`os.Stat` + `IsDir`）。
  - 过滤 deleted / 不存在的目录；追加 manual 中未出现的项（`Source: "manual"`，`Name: filepath.Base(path)`）。
- 测试 `internal/discovery/projects_test.go`：
  - Load 空/损坏文件；Add/Hide/Restore 往返 + 落盘内容断言（含「Hide 后 manual 记录保留」）
  - Add 一个已 Hide 的路径 → deleted 移除
  - 路径归一化：`D:/x/Y` vs `d:\x\y` 视为同一路径
  - `TestApplyProjectsFiltersDeletedAndMissing`：注入 `exists` 剔除不存在项
  - `TestApplyProjectsAppendsManual`：手动项追加、`Source=manual`、幂等（Apply 两次不重复）
  - `TestApplyProjectsKeepsScannedSource`：手动路径已在扫描结果里时 `Source` 不变

**验收**：`go test ./internal/discovery/... -count=1` 绿。

---

## T8 `projects.yaml` 路径 + 桌面端绑定

**文件**
- `internal/config/paths.go`：`Layout` 加 `Projects string`（`~/.kshell/projects.yaml`），`Paths(home)`/`EnsureRoot` 同步。
- `internal/desktop/projects.go`（新）：
  ```go
  // pickDirectory 是原生目录选择器（注入便于测试）。
  var pickDirectory = func(ctx context.Context) (string, error) {
      return runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{Title: "选择项目目录"})
  }

  func (a *App) CreateProject() (string, error)   // 取消返回 ""
  func (a *App) HideProject(path string) error
  func (a *App) RestoreProject(path string) error
  func (a *App) ListDeletedProjects() []discovery.DeletedProject
  ```
  - `CreateProject`：目录为空 → `("", nil)`；`os.Stat` 校验是目录 → `store.Add` → `applyProjects()` → 返回归一化路径。
  - `HideProject`：`store.Hide` → `applyProjects()`。
  - `RestoreProject`：`store.Restore` → `applyProjects()`。
  - `applyProjects()`：重算 `a.result.Workspaces = discovery.ApplyProjects(a.result.Workspaces, a.projects, nil)`——**注意**：`a.result.Workspaces` 已被覆盖，需要用「原始扫描结果」重算 → App 里保存 `rawWorkspaces []discovery.Workspace`（扫描/快照载入时赋值），`applyProjects` 永远从 raw 派生。
  - 事件：`wailsruntime.EventsEmit(a.ctx, "projects:changed")`；快照同步保存（`SaveSnapshot`）。
- 测试 `internal/desktop/projects_test.go`：
  - `TestCreateProjectAddsAndEmits`（替换 `pickDirectory` 桩 + `t.TempDir()`，断言落盘 + 结果列表包含）
  - `TestCreateProjectCancel`（桩返回 "" → 无写入）
  - `TestHideAndRestoreProject`（隐藏后不在列表、回收站一条、还原后回来）
  - `TestListDeletedProjects`（含删除时间）
- `App.Init/Startup` 装配：`a.projects = discovery.NewProjectStore(paths.Projects)` + `Load()`（失败只记日志）。

**验收**：`go test ./internal/desktop/... -count=1` 绿。

---

## T9 扫描/读取路径应用叠加

**文件**
- `internal/desktop/app.go`
  - `runScan`：扫描完成 → `a.rawWorkspaces = res.Workspaces` → `res.Workspaces = discovery.ApplyProjects(res.Workspaces, a.projects, nil)` → 存快照 → `scan:done` payload 用过滤后的列表。
  - 快照载入（无扫描时先出旧数据）：同样经 `ApplyProjects` 派生。
  - `GetWorkspaces`：返回 `ApplyProjects(a.rawWorkspaces, a.projects, nil)`（幂等，保证删除/还原即时生效）。
- `internal/ui`（TUI 读侧）：`Options` 加 `Projects *discovery.ProjectStore`，扫描回调里 `m.workspaces = discovery.ApplyProjects(res.Workspaces, m.opts.Projects, nil)`；`cmd/kshell/main.go` 载入 store 传入。TUI 不做新建/回收站 UI（无目录选择器）。
- 测试：
  - `internal/desktop/app_test.go`（或新文件）：`TestScanPayloadExcludesMissingWorkspaceDirs`（造一个不存在的路径进 raw → 结果里没有）
  - `internal/ui`：现有测试不受影响即可（可选补一条 Apply 生效断言）

**验收**：`go test ./... -count=1` 全绿。

---

## T10 首页 UI（新建项目 / 删除 / 回收站）

**文件**
- `frontend/src/lib/api.ts`：加 `createProject()`、`hideProject(path)`、`restoreProject(path)`、`listDeletedProjects()`（手写绑定 → `window.go.desktop.App.*`，命名空间必须是 `desktop`）。
- `frontend/src/pages/Home.tsx`：
  - 顶部按钮区加「新建项目」（主按钮，`+` 图标）与「回收站 (N)」。
  - 卡片容器改 `relative group`，hover 显现右上角删除按钮（绝对定位，避免 button 嵌套）。
  - 「新建项目」→ `createProject()`；返回非空 → toast「已添加项目「name」」+ `refresh()`；空串（取消）静默。
  - 删除 → `hideProject(path)` → toast「已删除「name」，可在回收站还原」+ `refresh()`。
  - 回收站弹层（`components/ui/dialog`）→ `listDeletedProjects()` 渲染列表 + 「还原」按钮 → `restoreProject(path)` → toast + 重拉工作区与该列表。
- `frontend/src/App.tsx`：订阅 `projects:changed` → `refreshWorkspaces()`；关闭工作区已消失的页签；回收站开着的场景由 Home 自己重拉（或统一在事件里刷新 store 的 `deletedProjects`）。
- 测试：`Home.test.tsx`（若有）/ 新增
  - 新建项目调用绑定并刷新列表
  - 删除调用绑定 + 卡片消失
  - 回收站列出已删除项、还原后回到列表
  - `projects:changed` 触发刷新

**验收**：`npm test` 绿 + `npm run build` 通过 + `.\build.ps1 -Desktop` 出包。

---

## T11 文档与全量验证

- `docs/smoke-test-desktop.md`：加 4 条（openocde 项目出现 / 新建项目 / 删除-还原 / 目录消失后重扫不出现），删篮子条目。
- `docs/smoke-test.md`（TUI）：删篮子条目与按键说明。
- `README.md`：如提到上下文篮或 D 类工具里的 opencode，同步更新（grep 确认）。
- 全量：`go vet ./...`、`go test ./... -count=1`、`frontend npm test`、`npm run build`、`.\build.ps1 -Desktop`。
- 真机冒烟按设计 §5 的 6 条逐项过。
- 提交：**等用户明确要求**再提交（中文 `type: 简述`）。

---

## 风险与注意

1. `ApplyProjects` 必须从「原始扫描结果」派生（App 保存 `rawWorkspaces`），否则 Hide 之后无法 Restore（已过滤的列表回不来）。
2. 卡片内不能嵌套 button（HTML 非法 + 事件冒泡误触），删除按钮用绝对定位兄弟节点。
3. `Detect` 只命中配置目录时 `BinPath == ""`，这种情况下枚举必须静默跳过而不是记错误。
4. opencode CLI 冷启动 840ms，Scan 是后台异步执行（有 `scan:loading`），不要阻塞首屏。
5. 删除/还原后要落盘 + 更新快照，否则重启回到旧状态。
6. TUI 与桌面共用 `Scan`，注册表/装配口径必须走 `MergeProviders`，避免两端口径分叉。
