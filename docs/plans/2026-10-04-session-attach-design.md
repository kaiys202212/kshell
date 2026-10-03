# 新建会话回填：页签标题与「恢复/切换」状态修复 — 设计

日期：2026-10-04
分支：fix/session-attach

## 问题

1. **页签标题不更新**：新建会话（终端或 ACP 聊天）创建时磁盘上还没有会话记录，
   Info 只有占位标题「工作区 · 工具名」、SessionID 为空；3 秒后重扫发现了新会话，
   但没有任何机制把发现结果回填到运行中的终端/聊天，页签永远是占位标题，无法区分任务。
2. **激活会话仍显示「恢复」**：`SessionList` 的激活判断
   `terminals.some(t => t.SessionID === s.ID && t.Status === 'running')` 有三处漏洞：
   - `KindNew` 终端 SessionID 为空且无人回填 → 永远匹配不上；
   - 只查 `terminals` 不查 `chats` → ACP 聊天打开的会话永远显示「恢复」；
   - `chat.Manager.Open` 会把 `Info.SessionID` 覆盖成 agent 返回的 ACP 会话 ID
     （与磁盘会话 ID 不同体系）→ 恢复的聊天也匹配不上。

## 设计要点

### 语义收敛

`terminal.Info.SessionID` / `chat.Info.SessionID` 统一表示
**「绑定的磁盘会话 ID（未绑定为空）」**。
协议内部的 ACP 会话 ID 只存在于 `chat.session.sessionID`（私有字段），不再污染 Info。

### Go 侧：绑定方法

- `terminal.Manager.AttachSession(sessionID, workspace, toolID, title) bool`：
  遍历运行中（未退出）且 `Kind == KindNew`、`SessionID == ""` 的会话，
  归一化工作区路径相等且 ToolID 匹配（或 Info.ToolID 为空）时，回填
  `SessionID` 与 `Title`，返回是否发生了绑定。
- `chat.Manager.AttachSession(...)`：同语义，遍历 `KindNew` 聊天；
  只改 `info.SessionID/Title`，不动协议层 `sessionID`。

匹配规则（desktop 层组装）：

- 工作区路径经 `discovery.NormalizePath` 归一化后相等；
- ToolID 相等（终端 Info.ToolID 为空表示由 launch 选首选，允许绑定该工作区任意工具）；
- **只绑本轮扫描新发现的会话**：desktop 层记住上一轮扫描结果的会话 ID 集合，
  其中的会话一律跳过——新建动作发生在上轮扫描之后，旧会话不可能对应运行中的新建终端/聊天，
  不加这条会把工作区里任意旧会话误绑上去；
- 同一目标候选多个时按 List 顺序（创建顺序）绑第一个，先到先得；
- 目标会话未被占用由「未绑定（SessionID 为空）」天然保证（一条会话绑上一个终端后即不再匹配）。

### Go 侧：扫描完成后回填

`App.runScan` 在扫描成功后：对 `res.Sessions` 逐条尝试绑定终端与聊天；
任一绑定发生时，`scan:done` 事件负载带 `attached: true`，
前端据此重取终端/聊天镜像。

### chat.Open 不再覆盖 SessionID

`chat.Manager.Open` 中 `s.info.SessionID = sessionID` 仅在
`info.Kind == KindSession && info.SessionID == ""` 时兜底写入；
`KindNew` 保持为空，等扫描回填。协议调用继续用 `s.sessionID`。

### 前端

- `WorkspaceTab`：订阅 `onScanDone` 时除刷新工具列表外，
  同时重取 `listTerminals()` / `listChats()` 重建镜像（事件负载 `attached` 不必感知，
  幂等整体重建即可，代价是两次轻量绑定调用）。
- `SessionList`：激活判断改为
  `[...terminals, ...chats].some(x => x.SessionID === s.ID && x.Status !== 'exited')`；
  按钮文案与行高亮沿用 `running` 布尔。

## 不做的事

- 不给终端 Info 加创建时间字段做时间窗校验（边缘误绑可接受，避免过度设计）。
- 不改 TUI（internal/ui），本问题只存在于桌面端。
- 不引入新的推送事件，复用既有 `scan:done`。
