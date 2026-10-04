# 预览区命令行 + SSH 可编辑 — 设计

日期：2026-10-04  
分支：`feat/preview-shell-ssh`

## 目标

1. 中心区「预览」页签钉到最右侧，与 agent 会话页签视觉区分。
2. 进入预览后，内容区支持「预览 | 多终端」子页签：本地 shell 可新建多个；SSH 双击也在此打开。
3. SSH 面板支持手动新建/编辑连接（用户名、密钥路径等）；列表双击在预览区命令行打开远程会话。

## 决策

| 项 | 选择 |
|---|---|
| 布局 | agent 页签左侧不动；「预览」`ml-auto` 钉右 |
| 命令行位置 | 预览内容区内子页签，不占 agent 页签位 |
| 终端 Kind | `shell` / `ssh`；agent 仍为 `session` / `new` |
| SSH 主路径 | 双击 → 预览区内嵌终端；「连接」改为「编辑」+「新建」 |
| 密钥 | 只存路径（`identity_file`），不存密钥正文/密码 |
| 本地 shell | Windows：`powershell`；其它：`$SHELL` 或 `/bin/bash` |
| SSH 复用 | 每次双击新开（`ssh:<connID>:<seq>`），允许多会话 |

## 架构

```
中心区页签条: [agent terms/chats ...]          [预览]
预览内容区:   [预览 | 终端1 | SSH·host | +]
右栏:         文件 | SSH（新建/编辑/双击开终端）
```

- `KindSession` / `KindNew` + chat → 左侧 agent 页签
- `KindShell` / `KindSSH` → 仅预览区子页签

## API

| 绑定 | 行为 |
|---|---|
| `OpenShellTerminal(ws, cols, rows)` | 工作区目录起本地 shell |
| `OpenSSHTerminal(connID, cols, rows)` | 内嵌 ssh -t（BatchMode） |
| `UpsertConnection(conn)` | ID 空 Add（Source=manual）；有 ID Update |
| `DeleteConnection(id)` | 删连接 |
| `OpenSSH` | 保留兼容，前端不再调用 |

## 错误与约束

- 私钥字段含 `PRIVATE KEY` 字样拒绝写入（既有 store 规则）。
- Host 必填；Port 默认 22。
- 未装配 store / 终端管理器时返回既有 `errNotReady`。

## 成功标准

- 预览页签在最右且样式可区分。
- 预览区内可开多个本地终端并关闭。
- SSH 可新建/编辑/删除；双击在预览区开远程终端。
- `go test ./...`、前端 `npm test` / `npm run build` 全绿。
