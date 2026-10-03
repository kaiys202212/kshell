# 新建会话回填 — 实现计划

日期：2026-10-04
分支：fix/session-attach
设计：docs/plans/2026-10-04-session-attach-design.md

TDD：每步先写失败测试，再实现到绿。

## 步骤

### 1. terminal.Manager.AttachSession（internal/terminal）

- 测试（manager_test.go）：
  a. 运行中 KindNew + 空 SessionID + 工作区/工具匹配 → 绑定，List 返回新 SessionID/Title；
  b. 已有 SessionID 的 KindSession 不被改写；
  c. 工作区不匹配不绑定；ToolID 不匹配不绑定（Info.ToolID 为空视为可绑）；
  d. 已退出（exited）不绑定。
- 实现：`AttachSession(sessionID, workspace, toolID, title string) bool`，锁内遍历 byID。

### 2. chat.Manager.AttachSession（internal/chat）

- 测试（manager_test.go）：KindNew 聊天绑定后 Info.SessionID/Title 更新，Prompt 仍用协议 sessionID
  （用桩 Conn 校验 Prompt 收到的 sessionID 不受绑定影响）。
- 实现：同 1，遍历 order 中 KindNew、未 exited 的会话。

### 3. chat.Open 保留磁盘会话 ID（internal/chat）

- 测试：Open(KindSession, SessionID: "s1", sessionID: "s1") 后 Info.SessionID == "s1"
  即使桩 NewSession 返回不同 ID（对 KindNew 场景）。
  实际上 KindSession 走 LoadSession；补一条：KindSession 打开后 Info.SessionID 仍为传入磁盘 ID。
- 实现：`s.info.SessionID = sessionID` 改为仅当 `info.SessionID == ""` 时写入。

### 4. runScan 绑定（internal/desktop）

- 测试（app_test.go）：装配好终端/聊天管理器后跑一轮扫描，
  结果里有匹配工作区/工具的新会话 → ListTerminals 返回的 Info 已带 SessionID/Title，
  且 scan:done 事件负载 attached == true；无绑定时 attached == false（或缺省）。
- 实现：runScan 成功分支调用 `a.attachDiscoveredSessions(res.Sessions)`；
  绑定计数 >0 时 ev["attached"] = true。

### 5. 前端：scan:done 刷新镜像（frontend/src/pages/WorkspaceTab.tsx）

- 测试（WorkspaceTab.test.tsx）：触发 onScanDone 回调后 listTerminals/listChats 被再次调用。
- 实现：复用既有 onScanDone 订阅，在 refresh 中追加两个重取（store 已有 setTerminals/setChats）。

### 6. 前端：SessionList 激活判断（frontend/src/components/SessionList.tsx）

- 测试（SessionList.test.tsx）：
  a. chat（Status ready）SessionID 匹配 → 显示「切换」+ 行高亮；
  b. terminal Status exited → 不算激活，显示「恢复」。
- 实现：`const running = [...terminals, ...chats].some(x => x.SessionID === s.ID && x.Status !== 'exited')`。

### 7. 全量验证

```powershell
go build ./... ; go vet ./... ; go test ./... -count=1
cd frontend; npm test; npm run build
```

### 8. 自查、合并、收尾

- requesting-code-review 自查改动；
- 合并回 master；`git worktree remove` + `git branch -d`；
- 征得用户同意后 `.\build.ps1 -Desktop` 产出 dist\kshell-desktop.exe。
