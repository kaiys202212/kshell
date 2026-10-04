# AGENTS.md

本文件为在此仓库中工作的 AI agent 提供指引。**中文注释与提交信息是本项目惯例，请保持一致。**

## 默认工作方式：必须使用 superpowers 技能

开始任何任务前，先判断是否有适用的 superpowers 技能；只要有 **1% 可能相关，就必须调用 `skill` 工具加载后再行动**。不要凭记忆代替技能内容（技能会更新）。

| 场景 | 技能 |
|---|---|
| 对话开始 / 判断用哪个技能 | `using-superpowers` |
| 做新功能、改行为、设计组件前 | `brainstorming` |
| 有需求/规格，写多步实现计划 | `writing-plans` |
| 按计划实现 | 默认 `subagent-driven-development`；任务强耦合时回退 `executing-plans` |
| 写任何实现代码前 | `test-driven-development` |
| 遇到 bug、测试失败、异常行为 | `systematic-debugging` |
| 声称"完成/已修复"前 | `verification-before-completion` |
| 提交/合并前自查 | `requesting-code-review` |
| 收到评审意见时 | `receiving-code-review` |
| 2+ 个互不依赖的任务 | `dispatching-parallel-agents` |
| 需要与当前工作区隔离 | `using-git-worktrees` |
| 实现完成、本地合并主干 | `finishing-a-development-branch`（跳过选项菜单，固定本地合并 `master`） |

流程类技能（brainstorming / debugging）优先于实现类技能。**本文件的用户指令优先于技能默认停顿**（见下节「确认门禁」）。

## 开发流程：worktree 隔离规范

**每个非平凡改动都在独立 worktree + 功能分支上完成，禁止直接在 `master` 上开发或提交。**

### 确认门禁

需求澄清完成后，agent **不得**再为规格、计划、是否开工、如何集成而询问用户。superpowers 技能文本里的设计审批、规格再审、计划写完后的执行方式选择题、`finishing-a-development-branch` 四选一菜单，在本仓库一律跳过。

| 节点 | 是否等待用户 |
|---|---|
| 需求澄清（目的 / 约束 / 成功标准） | 等待。一次一问，确认前不写规格、不写码。 |
| 建 worktree、写规格、写计划 | 不等待。需求确认后立即执行。 |
| 实施 | 不等待。计划一经写出即开始。 |
| 验证与自查 | 不等待用户批准去跑；必须自己跑出全绿证据。 |
| 提交与本地合并 `master` | 不等待。验证与自查均通过后立即做。 |
| `git push` / 开 PR | 等待。默认禁止，除非用户本会话明确要求。 |

仍必须停下的情况：需求未确认；实现出现无法消解的歧义或子代理 `BLOCKED`；验证未全绿；自查（code review）未通过；用户当场明确要求不要合并或中止流程。

### 阶段与技能

流程固定为：需求澄清 → 建 worktree → 写规格 → 写计划 → 子代理 TDD 实现 → 验证 → 自查 → 本地合并收尾。

| 阶段 | 技能 | 动作 | 产物 |
|---|---|---|---|
| 1 需求澄清 | `brainstorming` | 一次一问，确认目的/约束/成功标准 | 设计要点（先不写码） |
| 2 建隔离 | `using-git-worktrees` | 在 `.worktrees/<branch>` 建功能分支 | 干净测试基线 |
| 3 写规格 | 接 brainstorming 收尾 | 规格落 `docs/plans/`；**写完不送用户再审** | `docs/plans/YYYY-MM-DD-<topic>-design.md` |
| 4 写计划 | `writing-plans` | 拆成可执行步骤；**写完不问执行方式** | `docs/plans/YYYY-MM-DD-<topic>-plan.md` |
| 5 实现 | `subagent-driven-development` + `test-driven-development` | 当前会话按任务派发子代理（任务评审 + 整分支评审）；红 → 绿 → 重构 | 代码 + 测试 + 功能分支提交 |
| 6 验证 | `verification-before-completion` | 跑真实命令拿证据 | 全绿输出 |
| 7 自查 | `requesting-code-review` | 合并前审阅改动 | 通过 |
| 8 收尾 | `finishing-a-development-branch` | **跳过菜单**，本地合并 `master`，构建可运行程序，清理 worktree | 合并提交 |

实现回退：任务强耦合、无法按任务派发子代理时，用 `executing-plans` 在本会话连续执行，任务之间同样不问「要不要继续」。

**硬性规则**：

- worktree 固定放仓库根 `.worktrees/`，该目录必须在 `.gitignore` 中（已忽略，勿把 worktree 内容入库）。
- 分支命名：`feat/<topic>` / `fix/<topic>` / `docs/<topic>`，topic 用简短英文或拼音。
- 规格/计划用 `docs/plans/` 命名，日期取当天（沿用项目既有惯例，而非 superpowers 默认目录）。
- 只有在 worktree 内、且验证全绿后，才可声明完成并合并回 `master`；未验证不得声称完成，也不得合并。
- 合并回 `master` 后，在主干上构建出**可运行程序**：`.\build.ps1`（TUI，出 `dist\kshell.exe`）；改桌面端用 `.\build.ps1 -Desktop`（出 `dist\kshell-desktop.exe`）。确认成功产出可执行文件才算收尾完成，普通 `go build ./...` 不算。**仅文档/注释、不改变可运行程序时，跳过二进制构建。**
- 构建**不退出已运行程序**：`.\build.ps1 -Desktop` 不会请求旧实例退出，用户构建期间可继续使用桌面端。若 dist 拷贝因 `kshell-desktop.exe` 被占用失败，脚本会提示从托盘退出后重试；agent 不得写 `~/.kshell/exit.signal` 或以其他方式终止运行中的实例。
- 合并后清理：`git worktree remove` 删除工作区，`git branch -d` 删除已合并分支。
- 破坏性/一次性实验也不在 `master` 上做，先在 worktree 试。
- 本流程内 agent **必须**在功能分支自行提交（含子代理任务提交）；验证与自查均通过后 **必须**本地合并 `master`。默认 **禁止** `git push` 与 `gh pr create`。

**验证命令**（按改动范围全跑，全绿才算通过）：

```powershell
go build ./... ; go vet ./... ; go test ./... -count=1   # Go 侧
```

- 改前端时另跑：`cd frontend; npm test; npm run build`。
- 改 app 行为时在 `docs/smoke/<分支名转文件>.md` 追加增量条目（分支名 `/` → `-`；基线见 `docs/smoke/baseline-*.md`，勿往基线追加功能条目）。
- 仅改 Markdown/流程文档时：确认目标文件无门禁自相矛盾即可，不必为文档改动跑 Go/前端测试套件。

**实操命令**（PowerShell，`<branch>` 为分支名）：

```powershell
git worktree add .worktrees/<branch> -b <branch>   # 阶段 2：建隔离
git worktree list                                  # 查看现有 worktree
# 阶段 3-7：在 .worktrees/<branch> 内完成规格/计划/实现/验证
git -C .worktrees/<branch> status                  # 提交前确认改动
git worktree remove .worktrees/<branch>            # 阶段 8：合并后清理
git branch -d <branch>                             # 删除已合并分支
```

提交信息用中文 `type: 简述`。开发流程内的提交由 agent 自行完成，不必再等用户说「请提交」。

## 项目概述

kshell 是轻量级 shell 管理工具：发现本机已安装的 agent 编程工具（Claude Code / Codex / Gemini 等）及其工作区、会话，一键恢复或新建会话；附带文件树预览/编辑、git 状态与远程 SSH 管理。核心理念：**发现 → 选择 → 交付**（对话能力交由各工具自身 CLI，kshell 不重复实现）。

两个独立入口（无模式开关）：

- TUI：`cmd/kshell/`（Bubbletea，单二进制）
- 桌面端：仓库根 `main.go`（Wails v2，内嵌 `frontend/dist`，绑定在 `internal/desktop/`）。**根 `main.go` 不能加构建约束**——wails 绑定生成会跳过带 tag 的文件。
- 前端：React 19 + Vite + Tailwind v4 + Zustand + xterm，位于 `frontend/`

## 常用命令

Go（在仓库根目录）：

```powershell
go build ./...                        # 编译
go vet ./...                          # 静态检查
go test ./... -count=1                # 测试（CI 用）
go test ./internal/providers -run TestParseSession -count=1   # 跑单个测试
```

Makefile 快捷方式：`make test` / `make vet` / `make lint`（= vet + test）/ `make build` / `make cross`。

Windows 可用 `.\build.ps1`：默认 vet + 编译 TUI 到 `dist\kshell.exe`；`-Test` 先跑全量测试；`-Desktop` 走 `wails build` 出桌面版（自动构建前端，不会退出运行中的旧实例）。

前端（在 `frontend/` 目录）：

```powershell
npm install
npm test                    # vitest run（jsdom 环境）
npm run build               # tsc && vite build
npm run dev                 # vite 开发服务器
```

修改 Go 侧绑定后，`frontend/wailsjs/go/` 为生成产物，勿手改。

桌面端开发调试用 `wails dev`（自动构建前端 + 热重载）；需要先 `go install github.com/wailsapp/wails/v2/cmd/wails@latest`。环境要求：Go 1.23+、Node.js。

## 目录结构

```
main.go             桌面端入口（Wails v2，勿加构建约束）
cmd/kshell/         TUI 入口（Bubbletea）
internal/
  config/           配置加载（~/.kshell/config.yaml 等；缺失用默认值，损坏先备份 .bak 再重建，保存走临时文件原子替换）
  providers/        各 agent 工具适配（claude / codex / gemini / opencode 内置 + generic 由 providers.yaml 声明）
  discovery/        工具、工作区、会话扫描与缓存（~/.kshell/cache/）
  workspace/        文件树、预览/编辑、git 状态、搜索、ignore
  remote/           SSH 连接存储与执行（scanners/ 下为各类候选扫描器）
  terminal/         ConPTY/PTY 终端后端与多会话管理
  launch/ launcher/ 会话恢复与进程启动/包装
  ui/               Bubbletea TUI 视图（Sessions/Files/Remote 三视图）
  desktop/          Wails 绑定层（app/files/ssh/terminal/settings/tray/window/projects/chat）
  acp/ chat/        Agent Client Protocol 对话后端与聊天逻辑（桌面端）
  appearance/       主题（亮/暗色，注入终端环境变量）
  executil/         跨平台进程执行工具
frontend/src/       React 前端（components/ pages/ state/ lib/）
docs/plans/         设计与实现计划文档（改动前可参考/补充）
docs/smoke/          冒烟清单（baseline-* 基线 + 按分支增量）
```

## 架构要点

数据流：`providers`（识别工具、解析会话）→ `discovery`（扫描与缓存）→ `ui` / `desktop`（呈现）。

- **Provider 接口**（`internal/providers/provider.go`）是支持新 agent 工具的核心：`DetectSpec / SessionRoots / ParseSession / NewSessionCmd / ResumeCmd` 等。内置四工具硬编码在 `builtins.go`；用户可在 `~/.kshell/providers.yaml` 声明新工具（glob + 字段映射 + resume 参数）而无需改代码，`MergeProviders` 合并时内置同 ID 优先。
- **会话解析只读文件头部**（codex 256KB / gemini 2MB 上限），不整读多 MB JSONL。
- **OpenCode 会话存 SQLite**，经 `opencode db ... --format json` 枚举；SQL 必须写成**单行**（Windows 上 npm `.cmd` 包装经 cmd.exe 重解析，换行会截断）。
- **缓存**：`~/.kshell/cache/index.json` 按 mtime+size 门控重解析；`snapshot.json` 支撑秒开 + 后台刷新。
- **SSH**：始终走系统 `ssh` 二进制 + `-o BatchMode=yes`，复用 `~/.ssh/config`，不传密码；`connections.yaml` 私钥只存路径。
- **终端退出**：desktop 关闭终端用 drain 机制（80–500ms 延迟收取），立即关闭会丢约 40% 的 ConPTY 尾部输出。勿与 `exit.signal` 混淆（见下）。
- **`~/.kshell/exit.signal`**：桌面版轮询该文件优雅退出（`internal/desktop/signalfile.go`）。构建脚本不使用它——构建不会退出运行中的实例。
- **前端架构**：单一 Zustand store（`state/store.ts`，tab 持久化到 localStorage）；所有后端调用经 `lib/api.ts` → 生成的 `window.go.desktop.App`；后端推送经 `EventsOn`（`terminal:data` / `scan:done` / `chat:update`）。xterm 实例常驻挂载（`terminalRegistry`），切 tab 只隐藏不销毁。

## 代码约定

- Go 遵循标准项目布局，`internal/` 不对外暴露；包内注释用中文，简洁说明"为什么"。
- 平台相关代码用构建约束拆分，例如 `window_win.go`、`hide_windows.go` / `hide_other.go`、`shim_windows.go` / `shim_other.go`。
- 测试与被测文件同目录，命名 `<name>_test.go`；测试数据放 `testdata/`（如 `internal/providers/testdata/`）。
- 前端 TypeScript `strict`，测试同目录 `*.test.ts(x)`，从 `vitest` 显式导入 API（未开 globals）。
- 新功能通常先落一份 `docs/plans/` 设计/计划文档，再实现。
- 不要提交密钥内容；SSH 私钥只存路径引用。构建产物（`dist/`、`build/bin/`、`frontend/dist/`、`frontend/wailsjs/go/`）已 gitignore，勿入库。
- Cursor 会话展示标题：kshell 优先用 transcript 首条用户消息；仓库 `.cursor/rules/session-naming-zh.mdc` 约束会话命名用中文（产品侧 `meta.title` 不保证遵守）。

## 提交

- **开发流程内**（功能分支上的规格、计划、实现）：agent 自行 `git add` / `git commit`，不必等用户再说一次「提交」。
- 提交信息用中文，采用 `type: 简述` 前缀风格（如 `feat:`、`fix:`、`docs:`），参考 `git log`。
- 验证与自查通过后，将功能分支**本地合并**进 `master`（优先 `git merge --ff-only`；不能快进时用一次 merge commit），然后清理 worktree 与已合并分支。
- **默认不 push 远程、不开 PR。** 只有用户在本会话明确要求时才 `git push` 或 `gh pr create`。
- 禁止 force push、禁止改 git config、禁止跳过 hook（除非用户明确要求）。
- 流程外的即兴改动若用户只问「怎么改」而未进入开发流程，仍不要擅自提交。
