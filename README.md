# kshell

轻量级 shell 管理工具：在终端里集成各类 agent 编程工具，自动发现本机已安装的工具、它们的工作区与会话，一键恢复或新建会话；附带工作区文件树预览与远程 SSH 管理。全屏 TUI，单二进制，Windows / macOS / Linux。

## 核心理念

kshell 只做三件事：**发现 → 选择 → 交付**。

- 对话能力一律由 agent 工具自己的 CLI 提供（`claude --resume`、`codex resume`…），kshell 不重复实现，也就不存在协议适配腐烂的问题；
- 选中会话后 kshell 把终端交给原生 CLI，退出后自动回到 kshell。

## 安装

```powershell
# 需 Go 1.23+
go install github.com/yangk/kshell/cmd/kshell@latest
```

或从源码构建：

```powershell
git clone <repo> && cd kshell
go build -o kshell.exe ./cmd/kshell
```

## 使用

直接运行：

```powershell
kshell
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
| `Tab` / `1` `2` `3` | 切换 Sessions / Files / Remote 视图 |
| `↑↓` / `j` `k` | 移动光标 |
| `⏎` | 进入（工作区→会话；会话→恢复；目录→展开） |
| `/` | 搜索过滤（作用在当前聚焦的列表上） |
| `n` | 新建会话（Files 视图带上上下文篮） |
| `r` | 重新扫描 |
| `space` | Files：加入上下文篮；Remote：勾选候选连接 |
| `a` | Files：切换「显示全部」 |
| `i` `x` `s` `t` `b` `d` | Remote：扫描导入 / 执行命令 / 交互式 shell / 连通性测试 / 绑定 / 删除 |
| `?` | 帮助 |
| `q` | 退出 |

## 支持的工具

| 工具 | 会话存储 | 状态 |
|---|---|---|
| Claude Code | `~/.claude/projects/*/*.jsonl` | 已验证（resume 参数实测） |
| Codex CLI | `~/.codex/sessions/**/*.jsonl` | 已验证（resume 参数实测） |
| Gemini CLI | `~/.gemini/tmp/` | 未实测（本机未安装，路径按已知默认值） |
| CodeBuddy / OpenCode / Cline 等 | `~/.kshell/providers.yaml` 声明 | 预置猜测值，标注未验证 |

其他工具在 `~/.kshell/providers.yaml` 里加一段即可，无需改代码：

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

- 连接基于系统 `ssh` 二进制，自动复用 `~/.ssh/config`、ssh-agent、ProxyJump 与 known_hosts（kshell 不绕过主机密钥校验，也永远不传密码）；
- `i` 会扫描工作区里的连接候选：`~/.ssh/config`（高置信）、`.env*`（中）、Spring `application*`（中）、`docker-compose` / `Makefile` / `deploy*.sh` / ansible inventory（低）、README / CLAUDE.md（低）；
- 候选一律**勾选确认后才入库**，低置信度默认不勾选；
- 连接配置在 `~/.kshell/connections.yaml`，每条带工作区绑定；私钥只存路径引用，结构上杜绝密钥内容落盘。

## 配置

`~/.kshell/config.yaml`：

```yaml
scan_roots:            # git 工作区补充扫描的根目录
  - ~
max_depth: 4
exclude: [".git", "node_modules", "vendor", "dist"]
ssh:
  connect_timeout: 5
  command_timeout_seconds: 60
  extra_args: []
scanners:              # 各连接扫描器的开关
  sshconfig: true
  env: true
  spring: true
  deploy: true
  docs: true
```

## 开发

```powershell
make test    # go test ./...
make vet
make build
make cross   # 交叉编译三大平台
```

实现计划与设计文档见 `docs/plans/`。

## 明确不做（一期）

内嵌对话（ACP）、文件编辑、SFTP、端口转发、云同步、GUI。
