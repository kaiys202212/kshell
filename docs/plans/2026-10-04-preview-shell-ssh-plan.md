# 预览区命令行 + SSH 可编辑 — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans. 步骤用 TDD。

**Goal:** 预览钉右 + 预览区内多终端 + SSH 可编辑并双击内嵌打开。

**Architecture:** 扩展 `terminal.Kind` 为 shell/ssh；desktop 暴露 OpenShell/OpenSSHTerminal 与 Upsert/Delete；前端 WorkspaceTab 分区页签，PreviewToolPane 管子页签，SshPanel 改编辑流。

**Tech Stack:** Go (Wails 绑定)、React + Vitest、既有 TerminalView / remote.Store。

---

### Task 1: 终端 Kind + OpenShell/OpenSSHTerminal

**Files:**
- Modify: `internal/terminal/manager.go`
- Modify: `internal/desktop/terminal.go`（或新增 shell 相关）
- Test: `internal/desktop/*_test.go`

步骤：写失败测试 → 实现 KindShell/KindSSH、ConnID、两个 Open API → 绿。

### Task 2: SSH Upsert/Delete 绑定

**Files:**
- Modify: `internal/desktop/ssh.go`
- Test: `internal/desktop/files_ssh_settings_test.go` 或新测

步骤：写失败测试 → UpsertConnection / DeleteConnection → 绿。

### Task 3: 前端 API + PreviewToolPane + WorkspaceTab

**Files:**
- Modify: `frontend/src/lib/api.ts`
- Create: `frontend/src/components/PreviewToolPane.tsx` (+ test)
- Modify: `frontend/src/pages/WorkspaceTab.tsx` (+ test)

### Task 4: SshPanel 新建/编辑/双击

**Files:**
- Modify: `frontend/src/components/SshPanel.tsx` (+ test)

### Task 5: 验证 + smoke

- `go build ./... ; go vet ./... ; go test ./... -count=1`
- `cd frontend; npm test; npm run build`
- `docs/smoke/feat-preview-shell-ssh.md`
