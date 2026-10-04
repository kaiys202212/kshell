# 冒烟文档按分支拆分 — 设计

## 问题

`docs/smoke-test.md`（TUI）与 `docs/smoke-test-desktop.md`（桌面）是共享总清单。各功能分支都往同一文件追加条目，多分支合并时频繁冲突。

## 目标

- 功能分支追加冒烟条目时互不冲突
- 发布时仍有清晰的「基线 + 增量」可跑路径
- 约定写进 `AGENTS.md`，新人可循

## 非目标

- 不改代码、测试、构建脚本
- 不做「增量自动并入基线」的工具
- 不批量改写已完成的历史 `docs/plans/` 文档
- 不为旧路径保留跳转 stub

## 目录与命名

```
docs/smoke/
  README.md                 # 约定与怎么跑
  baseline-tui.md           # 自 docs/smoke-test.md 迁入
  baseline-desktop.md       # 自 docs/smoke-test-desktop.md 迁入
  feat-file-ops.md          # 示例：分支 feat/file-ops 的增量
```

| 规则 | 说明 |
|---|---|
| 基线 | `baseline-tui.md` / `baseline-desktop.md`，内容为现有两份总清单原样迁入 |
| 增量文件名 | 分支名 `/` → `-`（如 `feat/foo-bar` → `feat-foo-bar.md`） |
| 每分支一份 | 文内用 `## TUI` / `## Desktop` 分区；未改动的区可省略 |
| 增量生命周期 | 合并后长期保留，不并回 baseline、不删除归档 |
| 旧路径 | 删除 `docs/smoke-test.md`、`docs/smoke-test-desktop.md`，不留跳转页 |

## 工作流

### 功能开发

1. 在对应 worktree/分支上新建或追加 `docs/smoke/<branch-file>.md`
2. 只写本分支**新增或行为变更**相关条目
3. 条目格式沿用现有：`- [ ] **标题**` + 前置 / 步骤 / 预期
4. 默认不改 baseline；仅当基线条目明显过时或错误时才改（单独、谨慎）

### 增量文件模板

```markdown
# 冒烟增量：feat/foo-bar

> 对应分支：`feat/foo-bar`
> 合并日期：YYYY-MM-DD（合并后补）

## Desktop

- [ ] **某能力**
  - 前置：...
  - 步骤：...
  - 预期：...

## TUI

- [ ] **某能力**
  - ...
```

### 发布 / 全量冒烟

1. 先跑对应 `baseline-*.md`
2. 再扫 `docs/smoke/` 下所有非 `baseline-*`、非 `README.md` 的增量文件，按产品面执行相关条目

## 迁移范围

**改：**

- 新建 `docs/smoke/`，迁入 baseline，写 `README.md`
- 删除旧两份冒烟文件
- 更新 `AGENTS.md`：验证约定、目录结构说明改为新路径与增量规则
- 进行中的 `docs/plans/` 若写死旧路径，顺手改成新约定

**不改：**

- 已完成的历史计划文档不批量改写
- 代码与测试不动

## 成功标准

- 两分支可同时追加各自 `docs/smoke/feat-*.md` 而不互相冲突
- 读 `docs/smoke/README.md` + `AGENTS.md` 能知道写哪里、跑哪些
- 纯文档改动，不影响 `go build` / `go vet` / `go test`
