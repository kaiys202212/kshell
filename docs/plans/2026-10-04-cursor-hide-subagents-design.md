# Cursor 会话列表隐藏 Subagent 设计

日期：2026-10-04  
状态：已与用户确认  
范围：桌面端 / TUI 共用的会话发现层（`internal/providers` + `internal/discovery`）；仅改 Cursor provider 行为。

## 背景与目标

kshell 会话列表把 Cursor 的 Task/subagent 也当成独立 session 展示。用户期望与 Cursor IDE 对话列表一致：**只显示用户发起的主会话**。

### 根因

Cursor 有两套相关数据：

| 路径 | 用途 |
|---|---|
| `~/.cursor/chats/<md5(cwd)>/<agentId>/` | IDE 对话列表的真实索引 |
| `~/.cursor/projects/<slug>/agent-transcripts/<uuid>/` | 全量 transcript（含 Task 子代理） |

kshell 当前只扫 transcript（`PathMatcher` 深度过滤只能去掉 `.../subagents/*.jsonl`），挡不住**同级 UUID** 的 Task 子代理。

本机对照（工作区 `D:\data\workspace\moxi\kshell`）：

- `chats` 中带 `meta.json` 且 `hasConversation: true` → 主会话（约 5 条）
- 同目录仅有 `store.db`、无 `meta.json` → 子代理（约 22 条）
- 子代理 `store.db` 的 meta JSON 含 `subagentInfo.parentAgentId` 等字段
- 与 `agent-transcripts` 条目一一对应，无例外

### 已确认需求

| # | 需求 | 选择 |
|---|---|---|
| 1 | 展示策略 | **A**：直接隐藏 subagent，只留主会话 |
| 2 | 识别策略 | **C**：主读 `chats` 索引；无索引时回退 transcript + `subagentInfo` / 无 meta 过滤 |
| 3 | 范围 | 仅 Cursor；Claude / CodeBuddy / OpenCode 现有过滤不动 |

## 方案

采用 **Cursor 实现 `SessionEnumerator`（对齐 OpenCode）**：

- `SessionRoots` 返回 `nil`，不再走 `collectSessionFiles` 全量扫 transcript（避免主源与文件扫重复、子代理漏网）。
- `EnumerateSessions` 内完成：主源 chats → 必要时 transcript 兜底。
- `ResumeCmd` 仍为 `cursor-agent --resume <ID>`；`Dir` 优先会话 `Workspace`（目录存在才 chdir）。

备选（未采用）：继续扫 transcript + 标题启发式——脆弱且与 Cursor 权威字段不一致。

## 数据流

```
EnumerateSessions(home, bin)
  ├─ 主源：读 ~/.cursor/chats/*/<agentId>/meta.json
  │    └─ hasConversation == true → Session{...}
  └─ 若 chats 不可用或结果为空：
       扫 ~/.cursor/projects/*/agent-transcripts/<id>/<id>.jsonl
       （沿用现 MatchSessionRel：恰好 4 段）
       再排除：
         - chats/.../<id>/ 存在但无 meta.json
         - 或 store.db meta 含 subagentInfo
         - store.db 读失败 → 宁可保留（避免误杀）
```

## 字段映射

### 主源（meta.json）

| Session 字段 | 来源 |
|---|---|
| ID | 目录名 `<agentId>` |
| ToolID | `cursor` |
| Workspace | `cwd` |
| Title | `title`；空则尝试 transcript 首条用户消息（现有 `cleanTitle`） |
| CreatedAt / UpdatedAt | `createdAtMs` / `updatedAtMs`（毫秒 → time.Time） |
| Path | 同 ID 的 transcript 路径（若存在）；否则 `""` |
| Messages | 有 transcript 时 `CountLines`；否则 0 |

### 工作区 hash

`chats` 一级目录名 = `md5(cwd 原文字节)`。Windows 实测：`md5("D:\\data\\workspace\\moxi\\kshell")`。  
枚举时**直接遍历** `~/.cursor/chats/*/`，不必先猜路径再算 hash；每个 `meta.json` 自带 `cwd`。

### store.db（仅兜底判别）

- SQLite 表 `meta`，key=`0`，value 为 hex 编码的 JSON。
- 子代理特征：解析后存在 `subagentInfo` 对象（含 `parentAgentId` / `rootParentAgentId` / `typeName` 等）。
- 主会话：有正式 `name`，无 `subagentInfo`。
- **不**解析 `blobs` 对话内容。

## 与 discovery 的交互

- Cursor 改为 `SessionEnumerator` 后，`SessionRoots` 返回 `nil`，不再进入 `collectSessionFiles`。
- **必须改** `discovery.enumerateSessions`：当前在 `Detect(...).BinPath == ""` 时直接 `continue`，会把「只装了 Cursor IDE、未装 `cursor-agent`」的用户整段跳过。调整为：
  - 对实现了 `SessionEnumerator` 的 provider **始终调用** `EnumerateSessions(home, binPath)`（`binPath` 可为空）；
  - 由 provider 自行决定空 `bin` 时是否能枚举（Cursor：可以；OpenCode：保持现有「空 bin → 返回空」）。
- `dedupeSessions` 仍按 `(ToolID, ID)` 去重；因不再双源扫 Cursor，正常无重复。
- **`indexVersion` 递增**（当前为 5 → 6），清掉旧 path 缓存里可能残留的 Cursor 子代理条目。
- Enumerator 会话缓存策略默认对齐 OpenCode：**每次扫描现场枚举**，不写入 path `Index`（避免半套缓存语义）。

## 失败与边界

| 情况 | 行为 |
|---|---|
| `~/.cursor/chats` 不存在 | 走 transcript 兜底 |
| meta.json 损坏 | 跳过该 agentId |
| 有 meta 但无 transcript | 仍进列表；`Path=""`；resume 仅靠 ID |
| 兜底时 store.db 被锁/读失败 | 保留该 transcript 会话 |
| 明确读到 `subagentInfo` | 丢弃 |
| `agent-transcripts/.../subagents/` | 兜底路径继续用深度过滤拒绝 |
| Claude / CodeBuddy / 其它工具 | 不改 |

## 测试

### 单测（`internal/providers`，临时目录夹具）

1. 主源：`chats/<hash>/<id>/meta.json`（`hasConversation: true` + title/cwd/时间）→ 枚举 1 条，字段正确  
2. 同 hash 下仅有 `store.db`（含 `subagentInfo`）→ 不出现  
3. `hasConversation: false` → 不收录  
4. 无 `chats`：扫 transcript；无 meta / 有 `subagentInfo` 的 ID 剔除；`subagents/` 路径拒绝  
5. store.db 读失败 → 兜底路径保留该条  
6. `ResumeCmd`：cwd 存在则 `Dir` 正确，否则空  

### 验收（本机）

对 `D:\data\workspace\moxi\kshell`：Cursor 会话数应接近 IDE 主会话数（当前约 5），不再出现「You are implementing Task… / Review Task…」类条目；其它工具数量与行为不变。

### 冒烟

在功能分支对应的 `docs/smoke/<branch>.md` 增加：Cursor 会话列表不展示 Task 子代理。

## 非目标

- 子代理挂到主会话下的树形 UI  
- 解析 Cursor blobs / 展示完整聊天记录  
- 修改 Claude / CodeBuddy / OpenCode / Gemini / Codex 的发现逻辑  
- 标题启发式过滤（仅作 title 空时的展示回退，不作收录判据）
