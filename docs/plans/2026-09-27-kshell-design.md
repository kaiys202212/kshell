# kshell 设计方案

轻量级 shell 管理工具：在命令行中集成各类 agent 编程工具，自动发现本地已安装的工具、工具下的工作区与 session，可选择继续会话或新建会话；附带工作区文件树预览与远程 SSH 管理能力。

## 1. 定位与架构

kshell 不是另一个 agent，而是 agent 的「启动器 + 工作台」，只做三件事：

- **发现**：本地装了哪些工具、有哪些工作区、每个工作区有哪些 session；
- **选择**：TUI 中浏览 / 预览 / 筛选；
- **交付**：exec 原生 CLI 接管终端，退出后回到 kshell。

对话能力一律由官方 CLI 提供，kshell 不重复实现，从而避免协议适配腐烂。

技术选型：**Go + Bubble Tea**（单二进制跨平台分发、SSH 与 IO 能力成熟、冷启动快）。交互形态：全屏 TUI。

```
cmd/kshell          入口：无参数进 TUI；子命令 scan / ws / conn / resume
internal/
  providers/  工具适配器：claude、codex、gemini + 通用 yaml provider
  discovery/  工具检测 → 工作区聚合 → session 索引（带缓存）
  workspace/  文件树（懒加载 + gitignore 过滤）与文件预览
  remote/     连接存储、连接扫描器、ssh 执行（exec 系统 ssh）
  launcher/   启动新会话 / 恢复会话，退出后回到 TUI
  config/     ~/.kshell/ 下配置读写
  ui/         Bubble Tea：布局、面板、键位、状态机
```

约束：所有读盘 / 网络操作异步化（`tea.Cmd`），UI 永不阻塞；扫描结果进内存索引，二次打开秒开。

## 2. 数据模型与存储

| 实体 | 关键字段 |
|---|---|
| Tool | id、二进制路径、版本、会话根目录、resume 命令模板、来源（内置 / 自定义 provider） |
| Workspace | 路径、名称、最后使用时间、各工具 session 数、来源（会话反推 / git 扫描） |
| Session | id、所属 tool、工作区路径、标题摘要、创建/更新时间、消息数、原始文件、resume 参数 |
| Connection | id、名称、host、user、port、identity 引用、绑定工作区、来源（手动 / 扫描+来源文件）、连通状态 |

落盘位置 `~/.kshell/`：

- `config.yaml`：扫描根、排除规则、扫描器开关；
- `connections.yaml`：全局连接，每条带 `workspace` 绑定字段；
- `providers.yaml`：通用 provider 定义；
- `cache/index.json`：扫描索引缓存。

硬规则：**私钥只存路径引用，不复制内容**；**密码默认不落盘**，运行时询问，可选存系统钥匙串。

缓存策略：session 文件按 `path + mtime + size` 做缓存 key，未变化直接复用解析结果（JSONL 会话文件可达数十 MB，冷解析会明显拖慢启动）。首次全量扫（后台跑，先渲染缓存），之后增量。

## 3. 发现层

**工具检测**（三路并行，任一命中即算已安装）：`PATH` 查找（Windows `where` / Unix `which`）→ 常见安装路径兜底（`~/.claude/local/`、`~/.npm-global/`、scoop/choco、`/usr/local/bin`）→ 配置目录存在性（`~/.claude`、`~/.codex`、`~/.gemini`）。版本探测跑 `<bin> --version`，3 秒超时，失败显示 `unknown` 且不阻塞。

**工作区发现**：

- 主列表来自 session 的 `cwd` 聚合，去重后按最后使用时间排序（零噪声）；
- git 扫描作为补充：从配置的根（默认 `~`、`~/workspace`、`~/code`，可改）按 `maxDepth`（默认 4）找 `.git`，硬性排除 `node_modules/vendor/dist/.git`；
- 两种来源分别标记，git 扫描结果不污染主列表。

**Session 解析**：每个 provider 实现统一接口 `Detect / Sessions / NewSessionCmd / ResumeCmd`。解析时**只读文件头 64KB** 提取 cwd、时间戳、标题摘要，消息数用轻量统计，绝不整文件反序列化。命中不到已知格式但目录像某工具时，标记「疑似工具 X·未识别」并提示可在 `providers.yaml` 配置。

各家的 resume 命令以**模板**形式放在 provider 定义中，实现时用本机真实版本逐条实测校准。

**通用 provider（D 类工具）**：`providers.yaml` 中每项声明 `{name, 检测命令, 会话根路径 glob, 会话格式 jsonl|json, 字段映射, resume 命令模板}`，预置 CodeBuddy / OpenCode / Cline-Roo 的默认猜测并标注「未验证」，路径不对时改一行 yaml 即生效。

## 4. 远程 SSH 层

**连接存储**：`~/.kshell/connections.yaml`，每条含 `host / user / port / identityFile（路径引用）/ workspace 绑定 / source + sourceFile / 连通状态 / 最近使用`。列表默认按当前工作区过滤，可随时「绑定到当前工作区」。

**扫描器链**：五个 scanner 各声明 `MatchGlobs + Extract`，输出候选 `{host, user, port, 置信度, 来源文件:行号}`：

| scanner | 置信度 |
|---|---|
| `~/.ssh/config` | 高 |
| `.env` / `.env.*` | 中（字段名可在 yaml 配） |
| Spring `application*.{yml,yaml,properties}` | 中（启发式 host/user/port/key） |
| 部署类：`docker-compose.yml`、`Makefile`、`deploy*.sh`、ansible inventory、`package.json` scripts | 低 |
| 工作区 `.ssh/` 密钥 + `README`/`CLAUDE.md`/`.kshell.yaml` 中的主机名 | 低 |

同 host+user+port 自动折叠去重；扫描限定在工作区内、单文件 <2MB、总数上限，防止大仓库卡顿。

**导入流程**：候选列表按置信度排序，低置信度默认不勾选，`空格` 勾选、`回车` 导入，导入后保留来源标记以便回溯。

**执行**：基于系统 `ssh` 二进制（白嫖 `~/.ssh/config`、ssh-agent、ProxyJump、known_hosts 与 PTY）。

- 单命令：`ssh -o BatchMode=yes -o ConnectTimeout=5 [-i key] [-p port] user@host -- cmd`，输出进可滚动面板（支持复制 / 保存），单独显示返回码；
- 交互式：`ssh -t`，用 `tea.ExecProcess` 让出终端，退出即回 kshell；
- 连通性测试：`ssh -o BatchMode=yes -o ConnectTimeout=5 ... true`；
- Windows：探测 `ssh.exe`，缺失则提示启用 OpenSSH 可选功能；
- **不绕过 known_hosts 校验**，首次连接提示交给系统 ssh。

## 5. TUI 布局与交互

稳定三区：顶栏（工具筛选 + 当前工作区 + 上下文篮计数）、主体（左列表 / 右详情预览）、底栏（键位提示 + 状态消息）。切换视图时**只换左栏语义**，右栏永远是「预览」，布局不跳动。

```
┌ kshell  ●claude ●codex ○gemini        ws: ~/moxi/kshell ─┐
│ [Sessions] Files  Remote                     (Tab 切换)  │
├──────────────────────┬───────────────────────────────────┤
│ WORKSPACES           │ PREVIEW                           │
│ ▸ ~/moxi/kshell  12  │ 会话摘要 / 文件内容 /             │
│   ~/moxi/strategy 8  │ ssh 输出（可滚动、可复制）        │
│                      │                                   │
│ SESSIONS (选中工作区)│                                   │
│  修复上传白名单  2h  │                                   │
├──────────────────────┴───────────────────────────────────┤
│ ↑↓ 移动  ⏎ 进入  / 搜索  n 新建  r 重扫  ? 帮助  q 退出  │
└──────────────────────────────────────────────────────────┘
```

- **Sessions 视图**：左栏上=工作区、下=该工作区 session；`⏎` exec resume 接管终端。
- **Files 视图**：左栏懒加载文件树（尊重 `.gitignore`，可一键切「显示全部」），`space` 加入上下文篮，`n` 新建 session 时注入篮内路径；预览为只读文本（语法高亮、大文件截断、二进制显示元信息）。
- **Remote 视图**：左栏连接列表（按工作区过滤），`x` 执行命令、`s` 进入交互式 shell、`e` 编辑、`d` 删除。

键位：`Tab`/`1`/`2`/`3` 切视图，`↑↓`/`j k` 移动，`/` 模糊搜索，`Esc` 返回，`n` 新建，`r` 重扫，`?` 帮助，`q` 退出。窗口 <80×24 自动折叠为单栏；深色主题，尊重 `NO_COLOR`。

## 6. 错误处理与边界

原则是任何单点失败都不该让 kshell 打不开：

- 工具未安装 / 版本探测失败：顶栏灰显，其余功能照常；
- 会话文件损坏或格式变化：跳过并在状态栏提示「N 条解析失败」，记录路径；
- 工作区路径已删除：标记 `missing` 灰显，可一键移出列表（不动磁盘）；
- 扫描过慢：worker 池并发 + 文件数/深度上限 + 单仓超时，先渲染已有结果；
- ssh 缺失 / 连接超时 / 主机密钥变更：明确错误 + 建议动作，主机密钥变更不自动绕过；
- 权限拒绝（读 `~/.ssh/config`、私钥不可读）：降级跳过并提示原因；
- 启动 session 失败：回 TUI 显示退出码 + 最后若干行输出；
- Windows 特有：路径分隔符与大小写不敏感去重、Git-Bash 路径（`/c/Users/...`）转换、`ssh.exe` 探测、终端 ANSI 能力检测；
- 配置文件写入：临时文件 + 原子 rename，读取时 schema 校验，损坏则备份后重建默认配置。

## 7. 测试策略与里程碑

**测试**：

- 单测为主：provider 解析（脱敏最小 jsonl fixture）、gitignore 匹配、路径规范化与 Windows 路径转换、各 scanner 提取（每类一份 fixture）、ssh 命令拼装（断言参数，不真连）；
- 集成测试：向 PATH 注入假 `ssh` 脚本记录入参，覆盖执行 / 超时 / 失败路径；
- TUI：`teatest` 驱动模型，断言渲染与键位流转；
- 冒烟清单：Windows 本机 + Linux 远程服务器各跑一遍真实 resume 与 ssh。

**里程碑**：

| 阶段 | 内容 |
|---|---|
| M0 | Go module、Bubble Tea 三区空壳、配置读写 |
| M1 | 三家 provider + 工作区聚合 + 索引缓存 + Sessions 视图可浏览/恢复 |
| M2 | 懒加载文件树 + 预览 + 上下文篮 + 新建 session |
| M3 | 连接 CRUD + 五类扫描器 + 候选导入 + 命令执行 + 交互式 shell |
| M4 | 通用 yaml provider（D 类工具）、帮助/主题/小屏适配、发布脚本 |

**一期明确不做**：内嵌对话（ACP）、文件编辑、SFTP、端口转发、云同步、GUI。

## 8. 待实现时确认

- 各家 resume / new-session 命令的真实参数，以本机安装版本实测为准（如 `claude --resume <id>`、`codex resume <id>`、Gemini 对应参数）；
- D 类国产工具的实际会话存储路径，需在本机确认后写入 `providers.yaml`（默认值为猜测，标注未验证）。
