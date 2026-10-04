# 文件树实时刷新 / 显示全部 / 资源管理器打开 设计

日期：2026-10-04  
状态：已与用户确认  
范围：kshell 桌面端（Wails v2），仅本地工作区；TUI 不改。

## 背景与目标

桌面端右栏文件树存在三类问题：

1. **不刷新**：顶部刷新按钮只调 `refreshGitStatus`，不重建文件树；Go 侧 `Tree` 按目录 `Loaded` 缓存，外部改盘后重复 `ListFiles` 仍返回旧快照。
2. **未跟踪/忽略不可见或不区分**：桌面端 `NewMatcher(..., showAll=false)` 写死；未忽略的未跟踪文件在缓存陈旧时看不到；无「显示全部」；git 标记无 `ignored`，未跟踪徽章为 `U`。
3. **右键缺少资源管理器入口**：菜单仅有新建/重命名/删除/复制路径。

目标：手动刷新 + fsnotify 近实时刷新；默认可见未忽略的未跟踪文件，并提供 showAll；未跟踪/忽略样式区分；右键在系统资源管理器中打开（文件选中定位，目录打开自身）。

## 需求清单（已确认）

| # | 需求 | 选择 |
|---|---|---|
| 1 | 未跟踪展示 | 默认显示未忽略的未跟踪；另提供「显示全部」看 ignore/内置排除项（`.git` 永不显示） |
| 2 | 刷新策略 | 刷新按钮重建树 + git；fsnotify 防抖近实时自动刷新 |
| 3 | 资源管理器 | 文件：`explorer /select,`；目录：打开该目录 |
| 4 | 样式 | 未跟踪徽章 **`N`**（绿色）；忽略徽章 **`I`** + 行文字半透明 |

## 方案

采用 **fsnotify + 防抖整树重建**（相对轮询更省、相对目录级 patch 更简单，与现有 `refreshRoot`/`invalidateTree` 模型契合）。

## 后端设计

### 1. 强制刷新

新增绑定 `RefreshFiles(wsPath string) error`：

- 调用已有 `invalidateTree(wsPath)`（推进 `treeGen`、删除该工作区树缓存）
- 本方法不读盘；前端随后按「根 + 已展开路径」再调 `ListFiles` 重建镜像
- 手动刷新与 fsnotify 防抖回调共用同一前端 `reloadTree()` 路径（后端只负责作废 + 发事件）

### 2. 文件系统监听

新增（可同文件或 `files_watch.go`）：

| 绑定 | 说明 |
|---|---|
| `StartFileWatch(wsPath string) error` | 为工作区启动递归 fsnotify；引用计数 +1 |
| `StopFileWatch(wsPath string)` | 引用计数 -1，归零时停 watcher |

行为：

- 递归监听工作区；新建目录时补 watch
- 忽略 `.git` 目录内事件（减少噪声）；其它路径变更均计入
- 同一 `wsPath` **约 300ms 防抖**后 `EventsEmit("files:changed", map[string]string{"path": wsPath})`；前端用 `p.path === wsPath` 过滤（与 `terminal:data` 等对象 payload 风格一致）
- App 退出时停掉全部 watcher
- 启动失败不致命：返回错误供前端 toast 一次，手动刷新仍可用

依赖：引入 `github.com/fsnotify/fsnotify`（若尚未在 go.mod）。

### 3. showAll

- `ListFiles(wsPath, relPath string, showAll bool)`、`SearchFiles(wsPath, query string, showAll bool)` 扩展签名（破坏性：同步改全部 Go 调用方/测试与 `frontend` api 封装；Wails 绑定随下次桌面构建/`wails generate` 更新生成物）
- **缓存键**：`trees` 改为按 `wsPath+"\x00"+strconv.FormatBool(showAll)`（或独立 `treeKey`）区分，避免 true/false 共用一棵树；`RefreshFiles` / 切换 showAll 仍 `invalidateTree` 该工作区两种键
- `treeFor` 使用 `workspace.NewMatcher(wsPath, exclude, showAll)`；`.git` 仍永远跳过（Matcher 内置）
- 默认 `showAll=false`：修好缓存后，磁盘上未忽略的未跟踪文件正常出现

### 4. RevealInExplorer

新增 `RevealInExplorer(wsPath, path string) error`（与 `PreviewFile` 等同层，带工作区根）：

- 复用 `resolveWorkspaceFile` / `underPath` 校验：`path` 必须落在 `wsPath` 下且存在
- Windows：目录 → `explorer <dir>`；文件 → `explorer /select,<file>`（路径含空格时按 Windows 惯例加引号）
- 非 Windows：`xdg-open` / `open` 打开目录（文件则打开父目录；选中定位仅 Windows）
- 失败返回可读错误

### 5. GitStatus 扩展 ignored

- 命令增加忽略信息：`git status --porcelain=v1 -z --untracked-files=all --ignored=matching`（或平台等价）
- `statusFromXY`：`!!` → `"ignored"`；`??` 仍为 `"untracked"`
- 前端类型 `GitStatusCode` 增加 `'ignored'`；徽章映射见下

## 前端设计

### 1. reloadTree（按钮 + 事件共用）

`FileTree` 抽出 `reloadTree()`：

1. 收集当前已展开 `relPath` 列表  
2. `refreshFiles(wsPath)` 作废 Go 缓存  
3. `listFiles(wsPath, '', showAll)` 拉根，再按展开列表逐层重载并恢复 `expanded`  
4. `refreshGitStatus(wsPath)`  

顶部刷新按钮 title/aria 改为「刷新文件树」，`onClick` → `reloadTree()`。

挂载：`startFileWatch(wsPath)` + `onFilesChanged`；cleanup：`stopFileWatch` + 取消监听。  
收到 `files:changed` 且 path 匹配当前 `wsPath` 时调用 `reloadTree()`。

### 2. 显示全部

- 搜索框旁切换控件（勾选或 toggle 按钮），状态组件本地，不持久化
- 所有 `listFiles` / `searchFiles` 传入当前 `showAll`
- 切换后立即 `reloadTree()`

### 3. 样式

| 状态 | 徽章 | 颜色 | 行样式 |
|---|---|---|---|
| untracked | **N** | `text-success` | 默认（与现 U 同行高） |
| ignored | **I** | `text-muted-foreground` | 文件名/目录名半透明（`opacity` 或 muted 文本色） |

- 目录：仅当 git 对该路径本身报 `??`/`!!` 时打标并半透明；**不做**子孙聚合冒泡
- 现有测试中 `U` 断言改为 `N`；新增 `ignored` 用例

### 4. 右键菜单

文件行、目录行增加「在资源管理器中打开」→ `revealInExplorer(wsPath, item.node.Path)`；空白处不加。失败 toast。

### 5. api.ts

- `refreshFiles` / `startFileWatch` / `stopFileWatch` / `revealInExplorer`
- `listFiles` / `searchFiles` 增加 `showAll` 参数（默认 `false`）
- `onFilesChanged(cb)` 封装 `EventsOn('files:changed', ...)`
- `GitStatusCode` 含 `'ignored'`；`GIT_BADGES` / 类型同步

## 错误处理

- watcher 启动失败：toast 一次，不阻断树与手动刷新
- `RevealInExplorer` 失败：toast
- `reloadTree` 某层失败：该层行内 error，其它层保留
- git 刷新失败：继续静默（`lib/git.ts` 现行为）

## 测试

**Go**

- `RefreshFiles` 后 `ListFiles` 能看到外部新建文件（缓存已作废）
- `showAll=true` 列出被 ignore 的条目；`false` 不列出；`.git` 永不列出
- watcher：临时目录写文件 → 收到防抖后的 `files:changed`（可用测试 hook/channel 代替真实 EventsEmit 断言）
- `RevealInExplorer`：路径校验（不存在/越界）报错；Windows 命令构造可单测（不强制真开 explorer）
- `parsePorcelainZ` / `statusFromXY`：`!!` → ignored；`??` → untracked

**前端**

- 刷新按钮触发作废 + 重拉并恢复展开
- `files:changed` 触发重载
- `showAll` 切换后请求带新参数
- 徽章：`N` / `I`；忽略行有淡化 class
- 右键含「在资源管理器中打开」并调用绑定

**验证命令**

```powershell
go build ./... ; go vet ./... ; go test ./... -count=1
cd frontend; npm test; npm run build
```

冒烟：在 `docs/smoke/<分支名>.md` 追加增量条目。

## 明确不做

- 目录级 git 状态聚合（父目录因子孙变更变色）
- SSH/远程工作区文件监听
- TUI 改动；`showAll` 持久化
- 第三方文件树组件；目录级局部 patch（首版整树重建）

## 成功标准

1. 外部新建未忽略文件后约 1s 内树中出现；点刷新立即与磁盘一致  
2. `showAll` 可看见 ignore/内置排除项；关闭后恢复过滤（`.git` 始终隐藏）  
3. 未跟踪显示 `N`；忽略显示 `I` 且行更淡  
4. 右键「在资源管理器中打开」：文件选中定位，目录打开自身  
5. 相关 Go/前端测试与全量验证命令全绿  
