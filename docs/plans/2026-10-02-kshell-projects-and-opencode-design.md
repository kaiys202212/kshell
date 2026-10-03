# kshell 项目化管理 + opencode 会话发现 设计

日期：2026-10-02
状态：已确认（用户拍板 4 项决策）

## 1. 背景与目标

本轮提出 5 项改动：

1. **去掉上下文篮**（TUI + 桌面 + 新建会话注入）
2. **首页支持新建项目**（选定 base 项目目录）
3. **支持删除项目**（逻辑删除、可还原、重新扫描忽略已删除）
4. **重新扫描时自动排除文件路径不存在的目录**
5. **解决 opencode 项目扫描不到的问题**

已确认决策：

| 决策点 | 结论 |
|---|---|
| opencode 数据源 | 走 CLI 查询 `opencode db "<sql>" --format json`（零新依赖，失败降级为空并记录扫描告警） |
| 上下文篮范围 | 彻底移除：两端 UI/绑定/状态 + `providers.ContextPrompt` + `Provider.NewSessionCmd` 的 ctx 参数 |
| 还原入口 | 首页右上角「回收站 (N)」弹层，列出已删除项目（名称/路径/删除时间）逐项还原 |
| 新建项目交互 | Wails 原生目录选择器，项目名取目录名 |

## 2. 现状与根因

### 2.1 opencode 扫不到

- 旧实现把 opencode 当「D 类通用工具」放在 `~/.kshell/providers.yaml`：`sessions.glob: ~/.local/share/opencode/storage/*/*.json`。
- 新版 opencode（本机 1.18.34）已把会话迁移到 **SQLite**：`~/.local/share/opencode/opencode.db`（206MB，`session` 表 135 行、18 个项目目录、88 个根会话、0 归档、0 空目录/空标题）。
- `storage/` 下只剩 `session_diff/ses_*.json`（diff 快照，无 `id` 字段）→ 全部解析失败。
- 结论：既扫不到 opencode 的会话/工作区，还往扫描失败计数里灌了一堆噪音。

实测可用通道（本机验证）：

```
opencode db 'select id, directory, title, time_created, time_updated from session \
  where parent_id is null and time_archived is null order by time_updated desc' --format json
```

- 输出：标准 JSON 数组，键名即列名；耗时 840ms（Node 冷启动）。
- 恢复语法：`opencode --session <id>`（`-s` 同义），已用 `opencode --help` 实测确认。
- 时间字段是**毫秒** epoch。

### 2.2 上下文篮

- Go：`internal/desktop/files.go`（`App.basket`、`ToggleBasket`、`GetBasket`、`basketSnapshot`、`maxBasket`、`RenameEntry` 同步）、`internal/desktop/terminal.go`（新建会话时取 ctxFiles）、`internal/ui/{app,files}.go`（TUI 一套篮：space 入篮、渲染标记、状态行计数）。
- 注入：`providers.ContextPrompt(files)`（`internal/providers/provider.go`）→ 各 provider 的 `NewSessionCmd(ws, bin, ctx []string)` → `internal/launch/launch.go` 的 `ForWorkspace/ForWorkspaceTool`。
- 前端：`components/BasketBar.tsx`、`lib/basket.ts`（`toggleAndSync`）、`state/store.ts`（篮状态 + `syncBasket`）、`lib/api.ts`（`toggleBasket/getBasket`）、`pages/WorkspaceTab.tsx`（挂载 BasketBar）、`FileTree/Preview`（入篮按钮与标记）、`App.tsx`（启动拉篮）。

### 2.3 工作区发现与缓存

- 会话 → `discovery.GroupSessions` 聚合出工作区（`Source: "sessions"`），`ScanGitRepos` 追加 git 仓库（`Source: "git"`）。
- 索引缓存 `~/.kshell/cache/index.json`（按文件路径 + mtime/size 复用解析结果），快照 `~/.kshell/cache/snapshot.json`。
- 现状**没有**「手动项目」概念，也没有任何删除/隐藏入口；不存在的目录靠 `ScanGitRepos` 自身跳过，但会话聚合出的工作区不做存在性校验。

## 3. 设计

### 3.1 上下文篮彻底移除

- `providers.Provider.NewSessionCmd(ws, bin string) Launch`：**删掉 `ctx []string` 参数**；删除 `providers.ContextPrompt`。
- `launch.ForWorkspace(ps, tools, ws)` / `launch.ForWorkspaceTool(ps, tools, ws, toolID)`：同步去掉 ctx 参数。
- 桌面端：删 `App.basket`/`basketSnapshot`/`maxBasket`/`ToggleBasket`/`GetBasket` 与 `RenameEntry` 里的同步逻辑、`terminal.go` 的 ctxFiles 取值。
- TUI：删 `Model.basket`、space 入篮/渲染标记、状态行「篮 N」、帮助文案。
- 前端：删 `BasketBar.tsx`、`lib/basket.ts`、store 的篮字段与 `syncBasket`、api 的两个绑定、`WorkspaceTab` 挂载点、`FileTree/Preview` 入篮按钮与标记、`App.tsx` 启动拉篮，以及相关测试。
- 新建会话从此不带任何注入。

### 3.2 项目表 `~/.kshell/projects.yaml`

新增 `internal/discovery/projects.go`，承载「手动添加」与「逻辑删除」两类叠加信息：

```yaml
# kshell 项目表：手动添加的项目目录 + 逻辑删除（隐藏）的项目
projects:
  - path: D:/data/workspace/moxi/kshell
deleted:
  - path: D:/data/workspace/old
    at: 2026-10-02T10:00:00+08:00
```

- `ProjectStore`：`Load/Save`、`Manual() []string`、`Deleted() []DeletedProject`、`Add(path)`、`Hide(path)`、`Restore(path)`。
- 路径一律用 `NormalizePath` 归一化后比较（大小写/分隔符不敏感，与聚合 key 同口径）。
- 写盘原子（临时文件 + rename），内存 `sync.RWMutex`，文件缺失视为空表，损坏则退回空表并返回错误供日志。
- **Hide 不移除 manual 记录**，只往 `deleted` 加一条（带时间）——删除手动项目后再还原，才能凭 manual 记录复活。
- `Add` 遇到该路径在 `deleted` 里时，顺手移出 `deleted`（重新添加即还原）。

### 3.3 叠加规则 `ApplyProjects`

```go
// ApplyProjects 把手动项目并进扫描结果，并剔除「已逻辑删除」与「目录已不存在」的项。
func ApplyProjects(ws []Workspace, st *ProjectStore, exists func(string) bool) []Workspace
```

- 过滤：`st.IsDeleted(path)` 或 `!exists(path)` → 丢弃（满足需求 4；对会话/git/手动来源一视同仁）。
- 追加：manual 里未出现在结果中的路径 → 造 `Workspace{Path, Name: filepath.Base, Source: "manual", SessionCount: 0}`，排在末尾。
- 已在结果中的手动路径保持原样（不覆盖 `Source`），避免「手动添加已有会话的目录」把卡片降级。
- 纯函数 + `exists` 注入 → 好测；`nil` 时用 `os.Stat` + `IsDir`。
- 幂等：重复 Apply 不产生重复项——因此可以在每次 `GetWorkspaces`/扫描收尾时安全重放（删除/还原无需重新扫描）。

### 3.4 桌面端绑定与事件

`internal/desktop/projects.go`（新文件）：

| 绑定 | 语义 |
|---|---|
| `CreateProject() (string, error)` | 弹原生目录选择器（`runtime.OpenDirectoryDialog`），取消返回 `""`；选中则校验目录存在 → `Add` → 触发刷新 → 返回归一化路径 |
| `HideProject(path string) error` | 逻辑删除 → 刷新 |
| `RestoreProject(path string) error` | 还原（从 deleted 移出）→ 刷新 |
| `ListDeletedProjects() []DeletedProject` | 回收站数据源 |

- 目录选择器通过包级变量 `pickDirectory` 注入（测试可替换，参照既有 `spawnSelf/quitRuntime` 惯例）。
- 「刷新」= 重新 `ApplyProjects` 覆盖 `a.result.Workspaces` + `wailsruntime.EventsEmit(a.ctx, "projects:changed")` + 更新快照，前端收到事件后重拉工作区/回收站。
- `runScan` 收尾处也跑一次 `ApplyProjects`：`scan:done` 的 payload 与快照口径一致（满足「重新扫描时自动排除不存在目录」）。

### 3.5 首页 UI

`frontend/src/pages/Home.tsx`：

- 顶部按钮区新增 **「新建项目」**（主按钮）与 **「回收站 (N)」**（次要按钮，N=0 时仍在但禁用/不显示计数）。
- 工作区卡片：`<li class="relative group">` 内放原卡片 button + 绝对定位的删除按钮（不能嵌套 button）。hover 显现，点击 → 直接逻辑删除 + toast「已删除「x」，可在回收站还原」。
- 回收站弹层：复用 `components/ui/dialog`（Radix）列出已删除项目（名称/路径/删除时间）逐项「还原」；空态「回收站是空的」。
- `projects:changed` 事件订阅 → 重拉工作区 + 若回收站开着则重拉列表；同时关闭工作区已消失的页签（避免页签指向被隐藏的项目）。
- 新增/删除/还原都用既有 toast 反馈。

### 3.6 opencode 会话枚举

**内置 provider**：`internal/providers/opencode.go` 的 `Opencode{}`

- `DetectSpec`: `command: opencode`、`dirs: ["~/.local/share/opencode"]`
- `SessionRoots` 返回 `nil`、`SessionFilePattern` 返回 `""`（不走文件遍历；`ParseSession` 不会被调用）
- `NewSessionCmd(ws, bin)` → 在 ws 目录起 opencode；`ResumeCmd(s, bin)` → `opencode --session <id>`
- 实现可选接口：

```go
// SessionEnumerator 给「会话不在文件里」的工具用（如 opencode 的 SQLite 库）：
// 扫描时直接调用工具自身 CLI 枚举会话，绕过文件遍历与解析缓存。
type SessionEnumerator interface {
    EnumerateSessions(home, bin string) ([]Session, error)
}
```

- 实现要点：
  - `bin` 由 `discovery.Scan` 通过 `providers.Detect(p.DetectSpec(home), home)` 解析；`Detect` 只做路径解析（不探版本，快），`BinPath == ""`（只命中配置目录、CLI 不在 PATH）时跳过枚举。
  - 执行：`executil.ResolveShim(bin, args)` 后 `exec.CommandContext`，独立 20s 超时（不占用文件扫描 deadline）。Windows 下 `executil.HideWindow` 防黑窗。
  - SQL 固定为：`select id, directory as cwd, title, time_created as created, time_updated as updated from session where parent_id is null and time_archived is null order by time_updated desc`（`--format json`）。
  - 行 → `Session{ID, ToolID: "opencode", Workspace: NormalizePath(cwd), Title, CreatedAt/UpdatedAt: ms→time, Path: <db 文件路径>}`；`Messages` 留 0（表里没有）。
  - 失败（CLI 缺失/版本不支持/DB 被独占）→ 返回错误，`Scan` 记入 `Result.Failed`（文案 `opencode 会话查询失败: ...`），**不**中断整个扫描。
- 已知取舍：**不再支持旧版 opencode 的 JSON 会话目录**（`storage/*/*.json`）。理由：新版 opencode 升级时自动迁移到 SQLite；旧 glob 在新版上只会捞到 `session_diff` 垃圾并制造解析失败。设计上以「新版为准」。
- `discovery.Scan` 在文件收集之后追加枚举结果，仍走 `dedupeSessions` 去重（按 ToolID+ID），缓存与快照不含 DB 会话（每次扫描重查，840ms 可接受）。

### 3.7 内置 provider 与 yaml 去重

- 新增 `providers.Builtins() []Provider`（Claude/Codex/Gemini/Opencode）与 `providers.MergeProviders(builtins, specs, home)`：**按 ID 去重，内置优先**，yaml 里同 ID 的条目跳过。
- 原因：`EnsureProvidersFile` 只在文件缺失时写模板，老用户的 `~/.kshell/providers.yaml` 里仍有 `opencode` 段；不去重会出现两个 `ID=opencode` 的 Tool（工具列表重复）。
- `DefaultProvidersYAML` 删掉 opencode 段（新用户不再重复定义），并把注释里的 opencode 说明挪到内置清单。
- 三处装配统一改为 `providers.MergeProviders(providers.Builtins(), specs, home)`：`internal/desktop/app.go` 的 `initRealDeps`、`cmd/kshell/main.go`、`internal/ui/app.go` 的 `NewModelWith` 默认值。

## 4. 兼容与回滚

- `projects.yaml` 是新文件，缺失即空表 → 老用户升级后行为不变（除「不存在的目录不再显示」这一预期修复）。
- 老 `providers.yaml` 里的 `opencode` 段被内置实现取代（去重跳过），用户无需手改；若用户自定义过 opencode 的 resume 参数，需自行删掉该段才能生效——在 README/docs 里留一句说明。
- 上下文篮移除会让「新建会话携带文件清单」能力消失，属预期产品决策。
- 回滚：单条 `git revert` 即可（无数据迁移、无新文件格式的破坏性变更）。

## 5. 验证

- Go：`go vet ./...` + `go test ./... -count=1`（新增 ProjectStore/ApplyProjects/opencode 枚举/provider 去重的单测；篮子相关测试同步删除或改写）。
- 前端：`npm test`（删除篮子用例，新增首页新建/删除/回收站用例）+ `npm run build`。
- 真机：`.\build.ps1 -Desktop` 出桌面版，按 `docs/smoke-test-desktop.md` 新增项冒烟：
  1. 首页 opencode 会话与项目出现（本机 ≥18 个目录、88 个根会话）
  2. 新建项目 → 选目录 → 卡片立即出现
  3. 删除项目 → 卡片消失、回收站计数 +1 → 还原 → 卡片回来
  4. 删除一个真实存在的目录 → 重新扫描后仍不出现（逻辑删除优先）
  5. 手删磁盘上一个项目目录 → 重新扫描后该卡片消失
  6. 新建会话不再注入任何文件清单
