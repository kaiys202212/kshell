# AGENTS.md

本文件为在此仓库中工作的 AI agent 提供指引。**中文注释与提交信息是本项目惯例，请保持一致。**

## 默认工作方式：必须使用 superpowers 技能

开始任何任务前，先判断是否有适用的 superpowers 技能；只要有 **1% 可能相关，就必须调用 `skill` 工具加载后再行动**。不要凭记忆代替技能内容（技能会更新）。

| 场景 | 技能 |
|---|---|
| 对话开始 / 判断用哪个技能 | `using-superpowers` |
| 做新功能、改行为、设计组件前 | `brainstorming` |
| 有需求/规格，写多步实现计划 | `writing-plans` |
| 按计划实现 | `executing-plans` / `subagent-driven-development` |
| 写任何实现代码前 | `test-driven-development` |
| 遇到 bug、测试失败、异常行为 | `systematic-debugging` |
| 声称"完成/已修复"前 | `verification-before-completion` |
| 提交/合并前自查 | `requesting-code-review` |
| 收到评审意见时 | `receiving-code-review` |
| 2+ 个互不依赖的任务 | `dispatching-parallel-agents` |
| 需要与当前工作区隔离 | `using-git-worktrees` |
| 实现完成、决定如何集成 | `finishing-a-development-branch` |

流程类技能（brainstorming / debugging）优先于实现类技能。用户显式指令优先于技能。

## 项目概述

kshell 是轻量级 shell 管理工具：发现本机已安装的 agent 编程工具（Claude Code / Codex / Gemini 等）及其工作区、会话，一键恢复或新建会话；附带文件树预览/编辑、git 状态与远程 SSH 管理。核心理念：**发现 → 选择 → 交付**（对话能力交由各工具自身 CLI，kshell 不重复实现）。

- Go 后端：`cmd/kshell`（Bubbletea TUI 入口）+ `internal/`（业务逻辑）
- 桌面端：Wails v2，绑定在 `internal/desktop/`
- 前端：React 19 + Vite + Tailwind v4 + Zustand + xterm，位于 `frontend/`

## 常用命令

Go（在仓库根目录）：

```powershell
go build ./...              # 编译
go vet ./...                # 静态检查
go test ./... -count=1      # 测试（CI 用）
```

Makefile 快捷方式：`make test` / `make vet` / `make lint`（= vet + test）/ `make build` / `make cross`。

Windows 可用 `.\build.ps1`：默认 vet + 编译 TUI 到 `dist\kshell.exe`；`-Test` 先跑全量测试；`-Desktop` 走 `wails build` 出桌面版（自动构建前端，并会先请求运行中的旧实例退出）。

前端（在 `frontend/` 目录）：

```powershell
npm install
npm test                    # vitest run（jsdom 环境）
npm run build               # tsc && vite build
npm run dev                 # vite 开发服务器
```

修改 Go 侧绑定后，`frontend/wailsjs/go/` 为生成产物，勿手改。

## 目录结构

```
cmd/kshell/         TUI 入口
internal/
  config/           配置加载（~/.kshell/config.yaml 等）
  providers/        各 agent 工具适配（claude / codex / gemini / generic）
  discovery/        工具、工作区、会话扫描与缓存
  workspace/        文件树、预览/编辑、git 状态、搜索、ignore
  remote/           SSH 连接存储与执行（scanners/ 下为各类候选扫描器）
  terminal/         ConPTY/PTY 终端后端与多会话管理
  launch/ launcher/ 会话恢复与进程启动/包装
  ui/               Bubbletea TUI 视图
  desktop/          Wails 绑定层（app/files/ssh/terminal/settings/tray/window）
frontend/src/       React 前端（components/ pages/ state/ lib/）
docs/plans/         设计与实现计划文档（改动前可参考/补充）
docs/smoke-test*.md 冒烟清单
```

## 代码约定

- Go 遵循标准项目布局，`internal/` 不对外暴露；包内注释用中文，简洁说明"为什么"。
- 平台相关代码用构建约束拆分，例如 `window_win.go`、`hide_windows.go` / `hide_other.go`、`shim_windows.go` / `shim_other.go`。
- 测试与被测文件同目录，命名 `<name>_test.go`；测试数据放 `testdata/`（如 `internal/providers/testdata/`）。
- 前端 TypeScript `strict`，测试同目录 `*.test.ts(x)`，从 `vitest` 显式导入 API（未开 globals）。
- 新功能通常先落一份 `docs/plans/` 设计/计划文档，再实现。
- 不要提交密钥内容；SSH 私钥只存路径引用。构建产物（`dist/`、`build/bin/`、`frontend/dist/`、`frontend/wailsjs/go/`）已 gitignore，勿入库。

## 提交

- 只在用户明确要求时才提交。
- 提交信息用中文，采用 `type: 简述` 前缀风格（如 `feat:`、`fix:`、`docs:`），参考 `git log`。
