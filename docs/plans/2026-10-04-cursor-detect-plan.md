# Cursor 检测修复实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 重启后只要本机有 cursor-agent（根目录或 versions 档），设置页显示已装/卸载，新建会话出现 Cursor。

**Architecture:** DetectAll 与会话扫描解耦写回；Cursor DetectSpec 增加 versioned 目录；前端订阅 `tools:updated`。

**Tech Stack:** Go 测试 + Vitest；桌面绑定事件。

---

### Task 1: versions 目录探测

**Files:**
- Modify: `internal/providers/cursor.go`
- Modify: `internal/providers/cursor_test.go`

- [ ] **Step 1: 写失败测试** `TestCursorDetectFindsVersionedInstallDir`：PATH 隔离，根目录无 shim，只在 `versions/2026.10.01-e373342/` 放 `cursor-agent.cmd`，`Detect` 应 Installed 且 BinPath 指向该文件。

- [ ] **Step 2: 跑测试确认失败**

- [ ] **Step 3: DetectSpec 把最新 versions 子目录列入 InstallDirs**

- [ ] **Step 4: 测试通过后提交** `fix: 探测 cursor-agent 的 versions 安装目录`

### Task 2: DetectAll 结束后立刻写工具表

**Files:**
- Modify: `internal/desktop/app.go`
- Modify: `internal/desktop/settings.go`
- Modify: `internal/desktop/app_test.go`

- [ ] **Step 1: 写失败测试** 慢 Scan（阻塞）期间，DetectAll 能命中的工具应已能被 GetTools 读到；并推送 `tools:updated`。

- [ ] **Step 2: runScan 在 Scan 前写入 tools（尊重 keepInstallTools）、置 toolsReady、Emit tools:updated；GetTools 等 toolsReady**

- [ ] **Step 3: 修正只断言「恰好一次 scan:done」的测试**

- [ ] **Step 4: 提交** `fix: 会话扫描完成前先发布工具探测结果`

### Task 3: 前端刷新

**Files:**
- Modify: `frontend/src/lib/api.ts`
- Modify: `frontend/src/pages/Settings.tsx` / `Settings.test.tsx`
- Modify: `frontend/src/pages/WorkspaceTab.tsx` / `components/WorkspaceTab.test.tsx`

- [ ] **Step 1: onToolsUpdated；设置页与新建会话在该事件上重拉 getTools**

- [ ] **Step 2: 测试：tools:updated 后重刷列表**

- [ ] **Step 3: 提交** `fix: 工具探测完成后立即刷新设置与新建会话`

### Task 4: 冒烟与收尾

- [ ] 追加 `docs/smoke/fix-cursor-detect.md`
- [ ] 验证命令全绿，本地合并 master，构建桌面端
