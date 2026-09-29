# 文件树可操作 + git 状态标记 + 预览编辑 设计

日期：2026-09-29
范围：kshell 桌面端（Wails v2），仅本地工作区（SSH 面板不受影响）。

## 目标

1. 文件树支持检索（后端递归搜索）与重命名。
2. 文件树同步 git 状态，标记跟踪/未跟踪/变更（VS Code 风格）。
3. 文本文件预览支持编辑保存。

## 后端（Go）

### 新增 internal/workspace/search.go

`SearchFiles(root, query string, max int) ([]Node, error)`：
- walk 全工作区，复用 `Matcher`（.gitignore + 内置排除，`.git` 永不进树）。
- 文件/目录名大小写不敏感子串匹配；query 空白返回空。
- 命中上限 max（调用方传 2000）；返回 `[]Node`，调用方按根路径换算 relPath。

### 新增 internal/workspace/editfile.go

- `EditFile`：整文件读入，上限 1MB（超限返回 TooLarge 标志）；`isBinary`（复用 preview.go）拒二进制；探测行尾（统计 `\r\n` vs 单独 `\n`）得 `eol: "lf"|"crlf"`；返回 `EditContent{Text, EOL, Size}`，Text 保留原行尾。
- `SaveEdit(path, text, eol)`：把编辑器归一的 `\n` 按 eol 还原（crlf 时 `\n` → `\r\n`）；大小上限 1MB；**临时文件 + `os.Rename` 原子写**；临时文件与目标同目录。

### 新增 internal/workspace/gitstatus.go

`GitStatus(root string) (map[string]string, bool, error)`（relPath → code, isRepo, err）：
- 执行 `git -C <root> status --porcelain=v1 -z --untracked-files=all`，context 10s 超时；git 不在 PATH 或非仓库 → `(nil, false, nil)`。
- 解析：`-z` 以 NUL 分隔条目，rename 条目为 `new\0old` 两段；XY 码映射：
  - `??` → untracked；`X/Y` 含 U 或 AA/DD → conflicted；
  - X ∈ {A,C} 或 Y=A → added；X/Y 含 D → deleted；X=R → renamed；其余非空 → modified。

### 扩展 internal/desktop/files.go 绑定

| 绑定 | 说明 |
|---|---|
| `SearchFiles(wsPath, query)` | 走 treeFor 的 Matcher，上限 2000 |
| `RenameEntry(wsPath, relPath, newName)` | newName 单段（无分隔符、非 `.`/`..`）；underPath + EvalSymlinks 双重校验；目标不存在；`os.Rename`；成功后篮子内旧路径原地换新路径，**作废该工作区树缓存**（懒重建） |
| `ReadFileForEdit(wsPath, path)` | underPath + EvalSymlinks → EditFile |
| `SaveFile(wsPath, path, text, eol)` | 同上校验 → SaveEdit |
| `GitStatus(wsPath)` | 归一化 wsPath 后调用 |

写操作成功后由前端调 `GitStatus` 刷新（无后端事件）。

## 前端

### api.ts / store

- 新增封装：`searchFiles` / `renameEntry` / `readFileForEdit` / `saveFile` / `gitStatus`。
- `useAppStore` 增 `gitStatus: Record<wsPath, Record<path, code>>` 与 `refreshGitStatus(wsPath)`；树加载完成、刷新按钮、rename/save 成功后调用。

### FileTree

- 顶部搜索框（可清空）：输入即时过滤已加载节点名；200ms 防抖调 `searchFiles`；有结果时以**平铺结果列表**替换树（文件名主文本 + 目录浅色后缀）；点文件开预览，点目录在树中展开定位；清空恢复树。
- 行内重命名：hover 操作区加「重命名」按钮 → 行内 input，Enter 确认 / Esc 取消，失败 toast；成功后树缓存重建自动反映。
- git 标记：文件名右侧小色标（M 橙 / A 绿 / U 绿 / D 红 / R 蓝 / 冲突红），目录不聚合。

### Preview

- 文本态加「编辑」按钮（二进制或超 1MB 不显示）。
- 编辑态：等宽 textarea + 未保存圆点；Ctrl/S 保存按钮 → `saveFile` → toast → 退出编辑态并重新预览；dirty 时取消需确认；保存成功后刷新 git 标记。
- 预览态 500 行摘要行为不变。

## 测试

- Go：search（ignore/上限/大小写）、editfile（EOL 保留、1MB 上限、二进制拒、原子写）、gitstatus（porcelain -z fixture 解析、超时、非仓库）、desktop 绑定（rename 校验/篮子更新/树缓存作废、保存穿越拦截）。
- 前端：FileTree 搜索/重命名/标记、Preview 编辑模式，扩展现有测试文件，全量绿。

## 明确不做

- git 目录级状态聚合、git 命令面板（add/commit）、文件删除/新建、远程 SSH 文件操作。
