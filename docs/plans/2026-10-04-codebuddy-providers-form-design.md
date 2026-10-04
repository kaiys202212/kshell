# 设计：CodeBuddy 内置 + 自定义工具表单

日期：2026-10-04  
分支：`feat/codebuddy-providers-form`  
状态：已定稿

## 背景与目标

1. 把 CodeBuddy 做成与 Cursor 同级的内置 agent：硬编码检测 / 会话 / 恢复，设置 → 工具检测可一键安装 / 卸载。
2. 自定义工具（`~/.kshell/providers.yaml`）以表单为主编辑未知 agent，保留整份 YAML 源码模式；目录与可执行文件走系统选择框。
3. 保存后热重载 provider 清单并刷新工具检测 / 会话扫描，不必重启应用。

## 已确认决策

| 项 | 选择 |
|---|---|
| CodeBuddy 实现 | 独立 `CodeBuddy` 类型进 `Builtins()`；会话解析委托现有 Generic（字段与已实测 yaml 一致） |
| 安装 | `npm install -g @tencent-ai/codebuddy-code`；卸载同包名；`PurgeDirs: ~/.codebuddy` |
| 可执行名 | `codebuddy`；`AltBinNames: ["cbc"]`（官方短命令） |
| 内置顺序 | Claude、Codex、Cursor、CodeBuddy、Gemini、OpenCode |
| yaml 中的 codebuddy | 默认模板删除该段；`MergeProviders` 仍按 ID 丢弃（内置优先） |
| 表单 vs 源码 | 表单为主；切换「源码」编辑整份 yaml；双向：表单→`FormatProvidersYAML`，源码→`ParseProvidersYAML` |
| 路径选择 | 检测目录 / 会话目录用文件夹对话框；command 用文件对话框（写入绝对路径，`LookPath` 对含分隔符路径可直接命中） |
| 会话 glob | 选中会话目录后写入 `{dir}/*/*.jsonl`（反斜杠改 `/`），用户仍可手改 |
| 保存生效 | 写盘成功后替换 `opts.Providers`、`DetectAll` 推 `tools:updated`，并 `ScanSessions` |
| 注释 | 表单↔源码往返不保留 yaml 注释（按结构重新生成） |

## 非目标

- TUI 设置页表单
- 为 CodeBuddy 做 ACP
- 把 Cline 也改成内置
- 热改内置 provider 行为
- 保留用户 yaml 里的 codebuddy 段并合并字段（内置覆盖）

## 1. CodeBuddy 内置

`internal/providers/codebuddy.go`：

- `ID() == "codebuddy"`，`DisplayName() == "CodeBuddy"`
- `DetectSpec`：`BinName: codebuddy`，`AltBinNames: [cbc]`，`ConfigDirs: [~/.codebuddy]`
- 会话：委托内部 `Generic{Spec: codeBuddySpec, Home}`，spec 与当前已实测模板同构：
  - glob `~/.codebuddy/projects/*/*.jsonl`，format jsonl
  - fields：cwd / sessionId / timestamp / title=summary / titleFallbacks=[aiTitle]
  - resume `["--resume", "{id}"]`
- `MatchSessionRel` 走 Generic，排除 `subagents` 深层文件
- `InstallRecipe`：`npmInstall("@tencent-ai/codebuddy-code")` + `PurgeDirs: []string{"~/.codebuddy"}`

`TestBuiltinsImplementInstaller` 会自动要求新内置实现 `Installer`。

## 2. 自定义工具表单

后端：

- `FormatProvidersYAML(specs []GenericSpec) (string, error)`：缩进 2 的 yaml 编码
- `App.PickDirectory(title)` / `App.PickFile(title)`：Wails 原生对话框；取消返回 `""`；测试注入 `pickDirectory` / `pickFile`
- `SaveProvidersYAML` 成功后 `reloadProviders()`：读 yaml → `MergeProviders(Builtins(), specs, home)` 写回 `opts.Providers` → `DetectAll` + `publishDetectedTools` → `ScanSessions()`

前端（设置 → 工具，可抽 `ProvidersEditor`）：

- 模式切换：表单 | 源码
- 表单：agent 列表（增/删）+ 当前项字段（id、name、command+选文件、detect.dirs[]+选文件夹、sessions.glob+选会话目录、format、fields.*、resume.args 逗号或逐条、verified）
- 源码：现有 textarea（`aria-label` 仍为 `providers.yaml 编辑器`）
- 保存：源码模式直接存 textarea；表单模式先 format 再存
- 成功文案改为「已保存，已重新加载」，去掉「立即重启」
- 自定义工具仍无安装/卸载按钮；CodeBuddy 作为内置应出现安装/卸载（前端测试改 mock recipe）

## 3. 测试与冒烟

- Go：CodeBuddy 检测 / 解析 / resume / 配方；默认 yaml 不含 `id: codebuddy`；保存后 `GetTools` 含新自定义 ID（或 providers 切片含该 ID）
- 前端：表单增删、选路径 mock、源码切换、保存不再提示重启；CodeBuddy 行有卸载按钮（给 recipe）
- `docs/smoke/feat-codebuddy-providers-form.md` 增量条目
