# 实现计划：优化轮（IME 锚定 / 默认最大化 / Cursor provider）

日期：2026-10-04　分支：`feat/ime-maximize-cursor`　设计：`2026-10-04-ime-maximize-cursor-design.md`

每步完成后跑对应验证；全部完成后跑全量验证命令。

## Task 1：默认最大化（最小改动，先落地）

- [ ] `main.go`：Wails options 增加 `WindowStartState: options.Maximised`。
- [ ] 验证：`go build ./... && go vet ./...`。

## Task 2：Cursor provider（TDD）

- [ ] **红**：新建 `internal/providers/cursor_test.go`，用 `internal/providers/testdata/cursor/` 下的样例 transcript（真实格式裁剪）覆盖：
  1. `ParseSession`：从路径取 uuid 作 ID；首条 user 消息剥 `<user_query>` 作标题；mtime 作时间；Messages=行数。
  2. `ParseSession`：空壳（无 user 文本）返回 errCursorEmpty。
  3. `MatchSessionRel`：`<slug>/agent-transcripts/<uuid>/<uuid>.jsonl` 通过；3 段/5 段/其它子目录拒绝。
  4. `SlugToWorkspace`：`d-data-workspace-moxi-agent` → `d:\data\workspace\moxi\agent`。
  5. `ResumeCmd`：`--resume <id>` + Dir=Workspace。
- [ ] **绿**：实现 `internal/providers/cursor.go`（含 `WorkspaceToSlug`/`SlugToWorkspace`），`builtins.go` 注册 `Cursor{}`。
- [ ] 全量：`go test ./internal/providers -count=1`、`go build ./... && go vet ./...`。

## Task 3：IME 锚定修复（TDD，前端）

- [ ] **红**：新建 `frontend/src/lib/imeAnchor.test.ts`，覆盖 `computeImeAnchor`：
  - 光标在行中 → left=cursorX*cellW；
  - 光标超出 60% 宽度阈值 → 钳制到阈值列；
  - 有滚动（viewportY>0）时 top 取可视行；
  - 退化输入（cols=0）返回 null。
- [ ] **绿**：实现 `frontend/src/lib/imeAnchor.ts` 纯函数。
- [ ] 接线 `TerminalView.tsx`：实例创建后查 `.xterm-helper-textarea`，监听 `compositionstart`，用 `computeImeAnchor` + buffer 光标同步 textarea 位置；卸载时移除监听。
- [ ] 验证：`cd frontend && npm test && npm run build`。

## Task 4：收尾验证

- [ ] `go build ./... ; go vet ./... ; go test ./... -count=1`
- [ ] `cd frontend; npm test; npm run build`
- [ ] 补 `docs/smoke-test-desktop.md`：默认最大化、cursor 会话恢复、终端打中文候选窗位置三项。
- [ ] 提交（中文 `type: 简述`），自查后合并回 master，构建 `.\build.ps1 -Desktop`（**构建前须征得用户同意**，会请求旧实例退出）。
