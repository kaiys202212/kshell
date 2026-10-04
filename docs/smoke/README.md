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
