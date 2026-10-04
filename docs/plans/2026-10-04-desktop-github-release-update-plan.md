# 桌面端 GitHub Release 自动升级 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans（任务强耦合，本会话连续执行）。Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 桌面端 zip 随 `v*` 标签发到 GitHub Release，启动/设置检查更新时优先国内代理，确认后校验并替换 exe。

**Architecture:** `internal/update` 负责代理链、Release JSON、版本比较、SHA-256；`internal/version` 注入当前版本；`internal/desktop` 暴露 Check/Apply 并在启动后延迟检查；前端设置「关于」与 `update:available` 通知。CI 在 tag 时构建 zip + SHA256SUMS。

**Tech Stack:** Go 1.23+、Wails 绑定、React 设置页、GitHub Actions `windows-latest`。

## Global Constraints

- 仅桌面端；产物 `kshell-desktop-windows-amd64.zip` + `SHA256SUMS`
- 仓库 `kaiys202212/kshell`；模块路径仍为 `github.com/yangk/kshell`
- 启动静默检查，用户确认才下载；`dev`/空版本跳过
- 代理顺序：ghfast.top → gh-proxy.com → ghproxy.net → mirror.ghproxy.com → 官方
- 提交信息中文 `type: 简述`；不 push

---

### Task 1: 版本与代理检查

**Files:**
- Create: `internal/version/version.go`
- Create: `internal/update/check.go`
- Create: `internal/update/check_test.go`
- Create: `internal/update/sums.go`
- Create: `internal/update/sums_test.go`

**Interfaces:**
- Produces: `version.Current() string`；`update.Client.Check`；`ParseSHA256SUMS`；`ShouldSkip`；`Newer`

- [ ] TDD：代理链失败回退、版本比较、dev 跳过、SHA256 解析
- [ ] Commit: `feat: 增加 GitHub Release 代理优先的更新检查`

### Task 2: 下载校验与 Windows 替换

**Files:**
- Create: `internal/update/apply.go`
- Create: `internal/update/replace_windows.go`
- Create: `internal/update/replace_other.go`
- Create: `internal/update/apply_test.go`

**Interfaces:**
- Produces: `Apply(ctx, ApplyOptions) error`（下载、校验、解压、写 `.new`、调度替换）

- [ ] TDD：哈希不匹配失败；匹配则写出 `.new`
- [ ] Commit: `feat: 校验 zip 并在退出后替换桌面端`

### Task 3: 桌面绑定与启动检查

**Files:**
- Create: `internal/desktop/update.go`
- Create: `internal/desktop/update_test.go`
- Modify: `internal/desktop/app.go` Startup
- Modify: `internal/desktop/restart_spawn_windows.go`（可选复用 Wait-Process）

**Interfaces:**
- Produces: `GetAppVersion()`；`CheckForUpdate()` `UpdateInfo`；`ApplyUpdate()`；事件 `update:available`

- [ ] TDD：dev 跳过；Available 时 Emit
- [ ] Commit: `feat: 桌面端绑定检查更新与应用升级`

### Task 4: 设置页关于区

**Files:**
- Modify: `frontend/src/lib/api.ts`
- Modify: `frontend/src/pages/Settings.tsx`
- Modify: `frontend/src/pages/Settings.test.tsx`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/App.test.tsx`

- [ ] TDD 前端后实现
- [ ] Commit: `feat: 设置页检查更新并提示新版本`

### Task 5: Release 工作流与构建

**Files:**
- Modify: `.gitignore`（放行 `docs/plans`、`docs/smoke`、`release.yml`）
- Create: `.github/workflows/release.yml`
- Modify: `build.ps1`（可选 `-Version`）
- Create: `docs/smoke/feat-desktop-github-release-update.md`

- [ ] Commit: `ci: 按标签发布桌面 zip 到 GitHub Release`

---
