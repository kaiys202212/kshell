> [English](README.md) | [简体中文](README.zh.md)

# kshell

Lightweight agent workbench: discover the coding agents already installed on your machine (Claude Code / Codex / Cursor / Gemini, etc.) together with their workspaces and sessions, then resume or create one in a single click. Ships with a file-tree preview/editor, git status, an embedded terminal, remote SSH, and optional ACP chat.

Two independent entry points, no mode switch:

| Entry | Description | Artifact |
|---|---|---|
| Desktop | Wails window (React), the day-to-day main UI | `kshell-desktop` |
| TUI | Bubbletea full-screen terminal | `kshell` |

Windows / macOS / Linux.

## Core Idea

kshell does **Discover → Choose → Deliver**:

- Session records are persisted by each tool itself; kshell only scans and displays them, without reimplementing a chat protocol;
- **Terminal path**: hand ConPTY/PTY over to the native CLI (`claude --resume`, `codex resume`, …), and return to kshell when it exits;
- **Chat path (ACP)**: for integrated tools, talk over the Agent Client Protocol inside the window (requires the matching ACP adapter). When a tool doesn't support ACP, it still goes through the terminal.

## Installation

### Prebuilt Desktop

Download the `kshell-desktop-<os>-<arch>.zip` for your platform from [GitHub Releases](https://github.com/kaiys202212/kshell/releases) or [GitCode Releases](https://gitcode.com/abraveheart2023/kshell/releases), then unzip and run.

On the desktop app you can check for updates under **Settings → General → About**; upgrades prefer the GitCode domestic mirror, with GitHub as a fallback.

### Build from Source

**Go 1.23+** is required. The desktop app additionally needs **Node.js** and the [Wails CLI](https://wails.io):

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

```powershell
git clone https://github.com/kaiys202212/kshell.git
cd kshell

# TUI → dist\kshell.exe
.\build.ps1

# Desktop → dist\kshell-desktop.exe (also builds the frontend)
.\build.ps1 -Desktop
```

On Unix, the TUI is also available via `make build` / `make cross`. The Go module path is still `github.com/yangk/kshell`; install from this repository's source or a Release package, and don't rely on the outdated `go install …@latest` address.

## Desktop

On launch you get a project grid (session counts, tool distribution, last activity time). Click a card to open a workspace tab.

The workspace has three columns:

- **Left**: session list, new session (terminal or ACP, depending on the tool and settings)
- **Center**: agent chat/terminal tabs + file/session preview
- **Right**: file tree (search, rename, delete, save edits, git dirty markers) and the SSH panel

Other capabilities:

- Title-bar tabs stay mounted, so switching away doesn't tear down xterm / chat state; `Ctrl+K` switches quickly, `Ctrl+F` focuses search
- Local shell and interactive SSH terminals live in the preview area
- Projects can be added manually; deleting a card is a soft delete, restorable from the recycle bin
- Sessions can be archived (without touching the tool's own session files; the list lives in `~/.kshell/archived.json`); Claude Code can suggest archiving through a built-in MCP tool
- Closing the window minimizes to the system tray by default (configurable to exit directly); single instance — launching again brings up the existing window
- Settings let you install/uninstall built-in tools, edit the custom `providers.yaml`, inject models/endpoints, and switch appearance and font size

## TUI

```powershell
kshell
# or
.\dist\kshell.exe
```

```
┌ kshell  ●claude ●codex ○gemini        ws: ~/projects/demo ─┐
│ [Sessions] Files  Remote                    (Tab switch)    │
├──────────────────────┬──────────────────────────────────────┤
│ WORKSPACES           │ PREVIEW                              │
│ ▸ demo          12   │ session / file / ssh output          │
├──────────────────────┴──────────────────────────────────────┤
│ ↑↓ move  ⏎ enter  / search  n new  r rescan  ? help  q quit │
└─────────────────────────────────────────────────────────────┘
```

### Keybindings

| Key | Action |
|---|---|
| `Tab` / `1` `2` `3` | Switch Sessions / Files / Remote |
| `↑↓` / `j` `k` | Move cursor |
| `⏎` | Enter (workspace → session; session → resume; directory → expand) |
| `Esc` | Go back one level / cancel |
| `/` | Search filter (current focused list) |
| `n` | New session |
| `r` | Rescan |
| `a` | Files: show all (ignore `.gitignore`) |
| `i` | Remote: scan for import candidates |
| `space` | Remote: toggle candidate |
| `x` `s` `t` `b` `d` | Remote: run command / interactive shell / connectivity test / bind / delete |
| `?` | Help |
| `q` | Quit |

## Supported Tools

| Tool | Session storage | Terminal resume | ACP chat |
|---|---|---|---|
| Claude Code | `~/.claude/projects/<slug>/*.jsonl` | Verified | `claude-agent-acp` / `claude-code-acp` |
| Codex CLI | `~/.codex/sessions/**/*.jsonl` | Verified | None (uses terminal) |
| Cursor | `~/.cursor/projects/*/agent-transcripts/` (`cursor-agent` / `agent`) | Built-in | `cursor-acp` |
| CodeBuddy | `~/.codebuddy/projects/*/*.jsonl` | Verified | CLI `--acp` |
| Gemini CLI | `~/.gemini/tmp/` | Inferred from public defaults, not yet tested | None |
| OpenCode | SQLite (`opencode db … --format json`) | Built-in (`--session`) | None |

Any other CLI can be declared in `~/.kshell/providers.yaml` without code changes (a built-in with the same ID wins):

```yaml
providers:
  - id: mytool
    name: MyTool
    detect:
      command: mytool
      dirs: ["~/.mytool"]
    sessions:
      glob: ~/.mytool/projects/*/*.jsonl
      format: jsonl
    fields:
      cwd: cwd
      id: sessionId
      timestamp: timestamp
      title: message.content
    resume:
      args: ["--resume", "{id}"]
    verified: false
```

## Remote SSH

- Always uses the system `ssh` with `-o BatchMode=yes`, reusing `~/.ssh/config`, ssh-agent, ProxyJump, and known_hosts; never passes a password, never bypasses host-key verification
- Can scan a workspace for candidates: `~/.ssh/config` (high confidence), `.env*` / Spring `application*` (medium), `docker-compose` / `Makefile` / `deploy*.sh` / ansible (low), and README-style docs (low)
- Candidates are written to `~/.kshell/connections.yaml` only after you tick and confirm them; low-confidence ones are unticked by default
- Private keys are stored as paths only; key contents never hit disk

## Configuration

`~/.kshell/config.yaml` (defaults are used when missing; a syntactically broken file is first backed up to `.bak` and then rebuilt):

```yaml
scan_roots:            # extra scan roots for git workspaces
  - ~
max_depth: 4
exclude: [".git", "node_modules", "vendor", "dist"]
ssh:
  connect_timeout: 5
  command_timeout_seconds: 60
  extra_args: []
scanners:
  sshconfig: true
  env: true
  spring: true
  deploy: true
  docs: true
appearance:
  mode: dark           # system | light | dark
  font_size: 13        # 10–20
close_behavior: tray   # tray | exit
session_mode: tui      # tui | acp
permission_mode: default  # default | bypass (trusted environments only)
model:
  enabled: false
  preset: ""
  openai_base_url: ""
  anthropic_base_url: ""
  api_key: ""
  agents: {}           # toolID -> model name
```

Other local files: `connections.yaml`, `providers.yaml`, `projects.yaml`, `archived.json`, `cache/` (scan snapshots and tool-detection cache).

## Development

```powershell
go build ./...
go vet ./...
go test ./... -count=1

cd frontend
npm test
npm run build
```

On Windows: `.\build.ps1` (add `-Test`, `-Desktop`). Desktop debugging: `wails dev`.

Layout highlights: `cmd/kshell` is the TUI; the repo-root `main.go` is the desktop entry point (**don't add build constraints**, or Wails binding generation will skip it); `internal/providers` adapts each tool; `internal/desktop` holds the Wails bindings; `frontend/` is the React UI.

## Non-Goals

SFTP, port forwarding, cloud sync, and reimplementing each vendor's chat protocol.

## License

This project is licensed under the [MIT License](LICENSE).

Copyright (c) 2026 kaiys202212
