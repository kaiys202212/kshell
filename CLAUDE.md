# CLAUDE.md

> 沟通约定:与本项目相关的所有回复一律使用**中文**。

> **必读**:本项目的工作流程、确认门禁、提交与验证规范见仓库根的 `AGENTS.md`,本文件只做代码导览;两者冲突时以 `AGENTS.md` 为准。

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

kshell is a lightweight agent workbench: it discovers coding agents installed on the
machine (Claude Code / Codex / Cursor / Gemini / CodeBuddy / OpenCode), lists their
workspaces and sessions, and resumes/creates sessions in one click. It ships a file
tree + editor, git status/diff/log, an embedded terminal, remote SSH, and optional ACP chat.

Two independent entry points, no mode switch:
- **Desktop** — Wails window (React + TypeScript + Vite + Tailwind v4), the main UI.
- **TUI** — Bubbletea full-screen terminal.

Core model: tool-specific session files are persisted by each tool itself; kshell only
scans and displays them. Delivery is either hand the PTY to the native CLI
(`claude --resume`, `codex resume`, …) or, for ACP-capable tools, talk over the Agent
Client Protocol inside the window. Custom tools can be added via `~/.kshell/providers.yaml`
with no code change.

Module path is `github.com/yangk/kshell` (still the old path; do not rely on `go install
…@latest`). Remote is `github.com:kaiys202212/kshell`, default branch `master`.

## Build / Test / Dev

Go 1.23+ is required; `go` may not be on PATH — `build.sh` / `build.ps1` auto-detect it.
Desktop also needs Node.js and the Wails CLI (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`).

- Build TUI:        `./build.sh` (macOS/Linux)  ·  `.\build.ps1` (Windows → `dist\kshell.exe`)  ·  `make build`
- Build desktop:    `./build.sh -Desktop`  ·  `.\build.ps1 -Desktop`  (runs `wails build`, which builds the frontend first)
- Build + test:     add `-Test` to either script, or `make test` (`go test ./... -count=1`)
- Vet:              `go vet ./...`  ·  `make vet`
- Single Go test:   `go test ./internal/<pkg> -run TestName -count=1`
- Frontend:         `cd frontend && npm run build` (tsc + vite) · `npm test` (vitest) · `npm run dev`
- Desktop hot-dev:  `wails dev`
- Cross-compile TUI: `make cross`

## Layout

```
main.go              Wails desktop entry (package main) — embeds frontend/dist; NO build constraints allowed
cmd/kshell           Bubbletea TUI entry
cmd/nodeentry-probe, cmd/syncgitcode   small aux binaries
internal/            all logic (see below)
frontend/            React app (pages: Home, Settings, WorkspaceTab; zustand store; CodeMirror; xterm; i18next)
build.sh / build.ps1 build scripts (vet → test → build; -Desktop wraps wails build)
```

### Key internal packages

- **`internal/providers`** — the central abstraction. `Provider` interface adapts one tool:
  detection (`DetectSpec` → shared `Detect`), session parsing (`SessionRoots` /
  `SessionFilePattern` / `ParseSession`), and new/resume commands. Optional interfaces:
  `PathMatcher` (filter deep files), `SessionEnumerator` (SQLite-backed tools like OpenCode),
  `Themer` (light/dark theme injection). `builtins.go` merges with user `providers.yaml`.
- **`internal/discovery`** — scans workspaces + sessions from the tools' own storage and git
  roots, producing a snapshot; caches under `~/.kshell/cache` for fast startup.
- **`internal/desktop`** — the Wails-bound `App` (exposed to JS as `window.go.desktop.App`);
  file ops, git, SSH panel, chat, terminal, clipboard, tray. Platform split via
  `_windows.go` / `_other.go` files; tray/close behavior lives here, not in main.go.
- **`internal/terminal`** — PTY backend, session attach, OSC escape/notification parsing.
- **`internal/remote`** — SSH over the system `ssh` (`-o BatchMode=yes`, no password, never
  bypass host-key verification). `remote/clients` parse sshconfig/Xshell/PuTTY/MobaXterm/
  SecureCRT; `remote/scanners` find import candidates across a workspace (sshconfig/env/
  spring/deploy/docs).
- **`internal/chat` + `internal/acp`** — ACP (Agent Client Protocol) chat path; tools without
  ACP fall back to the terminal.
- **`internal/launch` / `internal/launcher`** — spawn the agent CLI subprocess (hand over PTY,
  resume args; Windows `.ps1` shim).
- **`internal/workspace`** — file tree, git status/diff/log, search, preview, edit.
- **`internal/ui`** — the Bubbletea TUI model (Sessions / Files / Remote views).
- **`internal/config`** — all `~/.kshell/*` path layout (`paths.go`) and config.yaml loading.

### Hidden sub-commands (dispatched in `main.go` before Wails init)

The desktop binary is also invoked as: SSH ASKPASS (`remote.TryAskPassMain`), `agent-hook`,
cursor-agent proxy (`internal/cursoragent`), `mcp-archive`, `mcp-ssh`. These must return
before any Wails/single-instance-lock init or they'd spawn a second window.

## Conventions / Gotchas

- **Do not add build constraints to repo-root `main.go`** — Wails binding generation drops
  build tags and the root package must compile unconditionally. `frontend/dist` uses a
  `.gitkeep` placeholder so the `//go:embed` always resolves.
- Session files are huge JSONL (tens of MB) — always parse the head only (`providers.ReadHead`).
- Persistence: everything user-facing lives under `~/.kshell/` (`config.yaml`,
  `connections.yaml`, `providers.yaml`, `projects.yaml`, `archived.json`, `cache/`). Each
  tool's own session data is never written by kshell.
- Tests are table-driven `_test.go` files colocated with each package; frontend uses vitest
  + Testing Library.