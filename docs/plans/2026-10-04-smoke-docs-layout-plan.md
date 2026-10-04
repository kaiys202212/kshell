# 冒烟文档按分支拆分 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把共享冒烟清单迁到 `docs/smoke/`（基线 + 按分支增量），消除多分支合并冲突。

**Architecture:** 现有两份总清单原样迁为 `baseline-tui.md` / `baseline-desktop.md`；功能分支只写 `docs/smoke/<branch-with-dashes>.md` 增量；`AGENTS.md` 与进行中计划改为新约定；旧路径直接删除。

**Tech Stack:** Markdown 文档；Git `mv`；无代码变更。

## Global Constraints

- 增量文件名：分支名 `/` → `-`（如 `feat/foo-bar` → `feat-foo-bar.md`）
- 增量长期保留，不并回 baseline
- 旧路径删除、不留跳转 stub
- 历史已完成 plans 不批量改写；进行中计划顺手改路径
- 工作目录：`.worktrees/docs-smoke-layout`（分支 `docs/smoke-layout`）
- 仅当用户明确要求时才 `git commit`

---

### Task 1: 迁入 baseline 并建 README

**Files:**
- Create: `docs/smoke/baseline-tui.md`（自 `docs/smoke-test.md`）
- Create: `docs/smoke/baseline-desktop.md`（自 `docs/smoke-test-desktop.md`）
- Create: `docs/smoke/README.md`
- Delete: `docs/smoke-test.md`、`docs/smoke-test-desktop.md`

**Interfaces:**
- Consumes: 现有两份冒烟清单全文
- Produces: `docs/smoke/` 目录约定（README 描述命名与跑法）

- [ ] **Step 1: 创建目录并用 git mv 迁入基线**

```powershell
New-Item -ItemType Directory -Force docs/smoke | Out-Null
git mv docs/smoke-test.md docs/smoke/baseline-tui.md
git mv docs/smoke-test-desktop.md docs/smoke/baseline-desktop.md
```

Expected: `git status` 显示两文件为 rename。

- [ ] **Step 2: 写 `docs/smoke/README.md`**

全文如下：

```markdown
# 冒烟清单

## 结构

| 文件 | 用途 |
|---|---|
| `baseline-tui.md` | TUI 发布前基线清单 |
| `baseline-desktop.md` | 桌面端发布前基线清单 |
| `<branch>.md` | 功能分支增量（`/` → `-`，如 `feat/foo` → `feat-foo.md`） |

## 功能开发时

1. 在本分支新建或追加 `docs/smoke/<branch-file>.md`
2. 只写本分支新增/变更条目；文内用 `## TUI` / `## Desktop` 分区（未改动的区可省略）
3. 默认不要改 baseline（除非修明显过时/错误条目）

增量模板见仓库 `docs/plans/2026-10-04-smoke-docs-layout-design.md`。

## 发布 / 全量冒烟时

1. 先跑对应 `baseline-*.md`
2. 再执行本目录下所有非 baseline、非 README 的增量文件中与目标产品面相关的条目

增量文件合并后长期保留，不自动并入 baseline。
```

- [ ] **Step 3: 确认旧路径已不存在**

```powershell
Test-Path docs/smoke-test.md; Test-Path docs/smoke-test-desktop.md
Get-ChildItem docs/smoke
```

Expected: 前两行为 `False`；`docs/smoke` 含 `README.md`、`baseline-tui.md`、`baseline-desktop.md`。

---

### Task 2: 更新 AGENTS.md 与进行中计划

**Files:**
- Modify: `AGENTS.md`（约 L59、L132）
- Modify: `docs/plans/2026-10-04-ime-maximize-cursor-plan.md`（L38）
- Modify: `docs/plans/2026-10-04-file-ops-plan.md`（L849、L874）— 若仍引用旧路径则改（历史记录性质也可只改未完成步骤）
- Modify: `docs/plans/2026-10-04-file-ops-design.md`（L85）

**Interfaces:**
- Consumes: Task 1 的路径约定
- Produces: agent/开发者指引指向 `docs/smoke/`

- [ ] **Step 1: 改 `AGENTS.md` 验证约定**

将：

```markdown
- 改 app 行为时补 `docs/smoke-test*.md` 条目。
```

改为：

```markdown
- 改 app 行为时在 `docs/smoke/<分支名转文件>.md` 追加增量条目（分支名 `/` → `-`；基线见 `docs/smoke/baseline-*.md`，勿往基线追加功能条目）。
```

- [ ] **Step 2: 改 `AGENTS.md` 目录结构行**

将：

```
docs/smoke-test*.md 冒烟清单
```

改为：

```
docs/smoke/          冒烟清单（baseline-* 基线 + 按分支增量）
```

- [ ] **Step 3: 改进行中计划里的旧路径**

`2026-10-04-ime-maximize-cursor-plan.md` L38：

```markdown
- [ ] 补 `docs/smoke/feat-ime-maximize-cursor.md`（Desktop）：默认最大化、cursor 会话恢复、终端打中文候选窗位置三项。
```

`2026-10-04-file-ops-plan.md` 中涉及 `docs/smoke-test*.md` 的步骤改为：

```markdown
- Modify: `docs/smoke/feat-file-ops.md`（Desktop 分区追加本轮冒烟项）
```

以及对应 `git add` 行改为 `docs/smoke/feat-file-ops.md`。

`2026-10-04-file-ops-design.md` L85：

```markdown
- **手工冒烟**：补 `docs/smoke/feat-file-ops.md` 条目（右键菜单五项、树内移动、拖入终端/聊天、外部拖入）
```

- [ ] **Step 4: 全文检索确认无残留「应写新条目」的旧指引**

```powershell
rg -n "docs/smoke-test" AGENTS.md docs/plans/2026-10-04-*.md
```

Expected: 仅出现在本设计/计划文档的「问题/迁移」叙述中，或已改为 `docs/smoke/...`；`AGENTS.md` 无 `smoke-test`。

---

### Task 3: 验证

**Files:** 无新增

- [ ] **Step 1: 确认目录与内容完整性**

```powershell
# 基线行数应与迁入前一致（tui ~40 行内容 / desktop ~310 行；以 wc 为准）
(Get-Content docs/smoke/baseline-tui.md | Measure-Object -Line).Lines
(Get-Content docs/smoke/baseline-desktop.md | Measure-Object -Line).Lines
Test-Path docs/smoke-test.md   # False
Test-Path docs/smoke-test-desktop.md  # False
```

- [ ] **Step 2: Go 侧验证（文档改动也应确认未误伤）**

```powershell
go build ./... ; go vet ./... ; go test ./... -count=1
```

Expected: 全绿。

- [ ] **Step 3: 向用户汇报并可应要求提交/合并**

汇报：`docs/smoke/` 结构、`AGENTS.md` 变更摘要；询问是否 `git commit` 与合并回 `master`。
