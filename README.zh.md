> [English](README.md) | 简体中文

# kshell

轻量级 agent 工作台：发现本机已安装的编程 agent（Claude Code / Codex / Cursor / Gemini 等）及其工作区与会话，一键恢复或新建。附带文件树预览/编辑、git 状态、内嵌终端、远程 SSH，以及可选的 ACP 聊天。

两个独立入口，没有模式开关：

| 入口 | 说明 | 产物 |
|---|---|---|
| 桌面端 | Wails 窗口（React），日常主界面 | `kshell-desktop` |
| TUI | Bubbletea 全屏终端 | `kshell` |

Windows / macOS / Linux。

## 核心理念

kshell 做 **发现 → 选择 → 交付**：

- 会话记录由各工具自己落盘，kshell 只扫描与展示，不复制一套对话协议；
- **终端路径**：把 ConPTY/PTY 交给原生 CLI（`claude --resume`、`codex resume`…），退出后回到 kshell；
- **聊天路径（ACP）**：对已接入的工具走 Agent Client Protocol，在窗口内对话（需对应 ACP 适配器）。工具不支持 ACP 时仍走终端。

## 安装

### 预编译桌面版

从 [GitHub Releases](https://github.com/kaiys202212/kshell/releases) 或 [GitCode Releases](https://gitcode.com/abraveheart2023/kshell/releases) 下载当前平台的 `kshell-desktop-<os>-<arch>.zip`，解压后运行。

桌面端可在 **设置 → 通用 → 关于** 检查更新；升级优先走 GitCode 国内源，GitHub 为后备。

### 从源码构建

需要 **Go 1.23+**。桌面端另需 **Node.js** 与 [Wails CLI](https://wails.io)：

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

```powershell
git clone https://github.com/kaiys202212/kshell.git
cd kshell

# TUI → dist\kshell.exe
.\build.ps1

# 桌面端 → dist\kshell-desktop.exe（会构建 frontend）
.\build.ps1 -Desktop
```

Unix 上 TUI 也可用 `make build` / `make cross`。Go module 路径仍是 `github.com/yangk/kshell`，请以本仓库源码或 Release 包安装，不要依赖过时的 `go install …@latest` 地址。

## 桌面端

启动后是项目网格（会话数、工具分布、最后活动时间）。点卡片打开工作区页签。

工作区三栏：

- **左**：会话列表、新建会话（终端或 ACP，视工具与设置而定）
- **中**：agent 聊天/终端页签 + 文件/会话预览
- **右**：文件树（搜索、重命名、删除、保存编辑、git 脏标记）与 SSH 面板

其它能力：

- 标题栏页签常挂载，切走不拆 xterm / 聊天状态；`Ctrl+K` 快速切换，`Ctrl+F` 聚焦搜索
- 本地 shell 与 SSH 交互终端挂在预览区
- 项目可手动添加；卡片删除是逻辑删除，可从回收站还原
- 会话可归档（不改工具自己的会话文件，名单在 `~/.kshell/archived.json`）；Claude Code 可通过内置 MCP 工具建议归档
- 关闭窗口默认收入系统托盘（可改为直接退出）；单实例，再次启动会唤起已有窗口
- 设置里可安装/卸载内置工具、编辑自定义 `providers.yaml`、注入模型/端点、切换外观字号

## TUI

```powershell
kshell
# 或
.\dist\kshell.exe
```

```
┌ kshell  ●claude ●codex ○gemini        ws: ~/projects/demo ─┐
│ [Sessions] Files  Remote                    (Tab 切换)      │
├──────────────────────┬──────────────────────────────────────┤
│ WORKSPACES           │ PREVIEW                              │
│ ▸ demo          12   │ 会话摘要 / 文件内容 / ssh 输出        │
├──────────────────────┴──────────────────────────────────────┤
│ ↑↓ 移动  ⏎ 进入  / 搜索  n 新建  r 重扫  ? 帮助  q 退出     │
└─────────────────────────────────────────────────────────────┘
```

### 键位

| 键 | 作用 |
|---|---|
| `Tab` / `1` `2` `3` | 切换 Sessions / Files / Remote |
| `↑↓` / `j` `k` | 移动光标 |
| `⏎` | 进入（工作区→会话；会话→恢复；目录→展开） |
| `Esc` | 返回上一层 / 取消 |
| `/` | 搜索过滤（当前聚焦列表） |
| `n` | 新建会话 |
| `r` | 重新扫描 |
| `a` | Files：显示全部（忽略 `.gitignore`） |
| `i` | Remote：扫描导入候选 |
| `space` | Remote：勾选候选 |
| `x` `s` `t` `b` `d` | Remote：执行命令 / 交互 shell / 连通性测试 / 绑定 / 删除 |
| `?` | 帮助 |
| `q` | 退出 |

## 支持的工具

| 工具 | 会话存储 | 终端 resume | ACP 聊天 |
|---|---|---|---|
| Claude Code | `~/.claude/projects/<slug>/*.jsonl` | 已验证 | `claude-agent-acp` / `claude-code-acp` |
| Codex CLI | `~/.codex/sessions/**/*.jsonl` | 已验证 | 无（走终端） |
| Cursor | `~/.cursor/projects/*/agent-transcripts/`（`cursor-agent` / `agent`） | 内置 | `cursor-acp` |
| CodeBuddy | `~/.codebuddy/projects/*/*.jsonl` | 已验证 | CLI `--acp` |
| Gemini CLI | `~/.gemini/tmp/` | 按公开默认值推断，未实测 | 无 |
| OpenCode | SQLite（`opencode db … --format json`） | 内置（`--session`） | 无 |

其它 CLI 在 `~/.kshell/providers.yaml` 声明即可，无需改代码（与内置同 ID 时以内置为准）：

```yaml
providers:
  - id: mytool
    name: MyTool
    detect:
      command: mytool
      dirs: ["~/.mytool"]
    sessions:
      glob: ~/.mytool/projects/*/*.jsonl
      format: jsonl
    fields:
      cwd: cwd
      id: sessionId
      timestamp: timestamp
      title: message.content
    resume:
      args: ["--resume", "{id}"]
    verified: false
```

## 远程 SSH

- 始终走系统 `ssh`，加 `-o BatchMode=yes`，复用 `~/.ssh/config`、ssh-agent、ProxyJump、known_hosts；不传密码，不绕过主机密钥校验
- 可从工作区扫描候选：`~/.ssh/config`（高置信）、`.env*` / Spring `application*`（中）、`docker-compose` / `Makefile` / `deploy*.sh` / ansible（低）、README 等文档（低）
- 候选勾选确认后才写入 `~/.kshell/connections.yaml`；低置信度默认不勾选
- 私钥只存路径，不把密钥内容落盘

## 配置

`~/.kshell/config.yaml`（缺失则用默认值；语法损坏会先备份 `.bak` 再重建）：

```yaml
scan_roots:            # git 工作区补充扫描根
  - ~
max_depth: 4
exclude: [".git", "node_modules", "vendor", "dist"]
ssh:
  connect_timeout: 5
  command_timeout_seconds: 60
  extra_args: []
scanners:
  sshconfig: true
  env: true
  spring: true
  deploy: true
  docs: true
appearance:
  mode: dark           # system | light | dark
  font_size: 13        # 10–20
close_behavior: tray   # tray | exit
session_mode: tui      # tui | acp
permission_mode: default  # default | bypass（仅建议可信环境）
model:
  enabled: false
  preset: ""
  openai_base_url: ""
  anthropic_base_url: ""
  api_key: ""
  agents: {}           # toolID -> 模型名
```

其它本机文件：`connections.yaml`、`providers.yaml`、`projects.yaml`、`archived.json`、`cache/`（扫描快照与工具探测缓存）。

## 开发

```powershell
go build ./...
go vet ./...
go test ./... -count=1

cd frontend
npm test
npm run build
```

Windows：`.\build.ps1`（可加 `-Test`、`-Desktop`）。桌面调试：`wails dev`。

目录要点：`cmd/kshell` 为 TUI；仓库根 `main.go` 为桌面入口（**不要加构建约束**，否则 Wails 绑定生成会跳过）；`internal/providers` 适配各工具；`internal/desktop` 为 Wails 绑定；`frontend/` 为 React 界面。

## 明确不做

SFTP、端口转发、云同步、把各家对话协议再实现一遍。

## 许可证

本项目采用 [MIT License](LICENSE)。

Copyright (c) 2026 kaiys202212
