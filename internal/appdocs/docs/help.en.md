# kshell User Guide

kshell is a lightweight manager for agent coding tools, built on the idea of "discover → choose → deliver":

- It automatically discovers locally installed agent CLIs (Claude Code / Codex / Gemini / OpenCode / Cursor / CodeBuddy, etc.) along with their workspaces and session history.
- Sessions are resumed or created in one click; conversation capability is delegated to each tool's own CLI — kshell does not reimplement it.
- It ships with both a desktop app and a TUI with consistent features; this guide covers the desktop app.

## Installation & Launch

### Installation

- Windows: download the installer (or upgrade package) and follow the wizard.
- macOS: a DMG installer is available — drag the app into Applications.
- Linux: build locally with the bundled build.sh.

### First launch

- A desktop shortcut is created automatically on first run.
- The Agent Setup wizard opens: it detects which agent CLIs are missing and installs the ones you select in one click; you can skip it and install later under "Settings → Tools".
- Only one instance runs at a time — launching again activates the existing window.

### Main window layout

- Home page (left): the workspace (project) list, the entry point for session discovery and creation.
- Tab area: one tab per workspace, containing the session list, terminals, file tree, Git panel, and more.
- Title bar: home button, tab switching, settings entry, and window controls.

### Closing & updates

- The window-close behavior is configurable in Settings: "minimize to tray" or "exit".
- When minimized to the tray, use the tray icon's right-click menu to restore the window or quit.
- Manual update check: click "Check for updates" on the Settings "About" page.
- Automatic checks: the app checks every two hours and prompts when a new version is found.
- Upgrading: clicking Upgrade downloads and replaces the binary automatically; when finished, the full changelog for the new version pops up.

## Session Management

On startup kshell scans each agent tool's session directory (e.g. Claude Code's `~/.claude/projects`, plus Codex, Gemini, OpenCode, Cursor, etc.), parses session titles and timestamps, and groups them by workspace. Results are cached locally so the app opens instantly, with a background refresh.

### Discovery & resume

- The home page lists historical sessions grouped by workspace, with last-active time and message counts.
- Clicking a session resumes it:
  - Terminal-based tools use their built-in resume argument to restore context.
  - Chat-based (ACP) tools restore the conversation record directly.
- The session list supports keyword search and filtering by tool.

### New sessions

- "New Session" on the home page picks a tool and workspace to start.
- You can also start one from the session list or the Terminal panel on the right.
- The last-used "tool + project" combination is remembered for quick reuse.
- Duplicate session tab names are deduplicated automatically to avoid confusion.

### Batch & archive

- The session list has a multi-select mode — select all and archive / restore in one click, handy for clearing out stale sessions.
- Inactive sessions can be archived to keep the main list clean; archived sessions are hidden but can be restored anytime.
- Archive suggestions appear automatically once a session has been idle for a while.

### Workspace management

- The home page supports adding local project folders and creating projects (local or remote).
- Projects can be deleted or moved to trash; mistakenly removed projects can be restored from the trash in one click.
- The home page can filter workspaces by name / path for quick lookup in long lists.

## Terminal Tabs

- Each workspace has a Terminal panel on the right; click the plus button to open a new local shell tab, with multiple tabs at once.
- Terminals run on ConPTY (Windows) / PTY and behave like a real terminal.
- Agent sessions also appear as terminal tabs with live status.
- Terminal tabs support drag-to-reorder and Ctrl+W to close; after exit, the tab keeps an exit-code hint.
- Plain shell terminals receive theme environment variables so terminal colors match the app theme.
- Files can be dragged into a terminal or chat input and are converted to paths automatically.

## File Tree & Preview / Editing

Each workspace has a file tree on the left and a tabbed file area on the right, with multiple files open at once.

### File tree

- Create / rename / delete files and folders, with confirmation on delete.
- Copy paths and reveal in the system file manager.
- Filter the tree by name, or run a full-text search across the workspace with results grouped by file.
- Files / folders can be added to ignore (writes .gitignore) via the context menu.
- Git status badges (added / modified / deleted / conflicted) update on the tree in real time.

### Preview

- Markdown: a Preview / Source toggle; preview supports keyword search with highlighting and jumping between matches.
- HTML: rendered in an in-app browser tab, or opened in your system browser.
- Images and PDFs preview inline (PDF with pagination); binary files show an "unavailable" notice.

### Editing

- Text files open in a CodeMirror editor with syntax highlighting.
- The find panel supports in-file search / replace (CodeMirror find panel).
- Saving writes to disk; closing with unsaved changes asks for confirmation.
- External changes are watched and refreshed automatically.

## Git Panel

Each workspace has a built-in Git panel for everyday operations without touching the command line.

### Status tab

- Shows uncommitted changes grouped into staged / unstaged.
- Per-file or bulk stage / unstage / discard (discard asks twice).
- In the diff view, changes can be staged / unstaged / discarded per hunk.
- Type a commit message and "Commit" or "Commit & Push".

### Commit graph & diff

- Visualizes the current branch's history, loading older commits automatically on scroll.
- Click any commit to open its full diff in a tab.

### Sync & branches

- The toolbar offers pull, fetch / fetch all, and push.
- Stash: stash changes, pop the latest, or drop a specific stash (with confirmation).
- Branches: see the current branch plus local / remote branch lists, with checkout and branch creation.
- Running operations show a busy spinner with a result toast on completion.
- The panel header shows the branch name and an uncommitted-changes badge; non-Git folders say so explicitly.

## Remote SSH Management

### Connection management

- The SSH panel lists all connections; double-click to open a remote terminal.
- Connections can be created manually: name / host / user / port / key path / password.
- Private keys are stored as path references only — key contents are never saved; the panel notes that passwords are plaintext.
- Connections can be edited and deleted.

### Connection import

- Click "Import" to scan existing local configuration: ssh config, Xshell and other clients.
- Select scan results and import them in bulk instead of typing each by hand.

### Remote sessions & workspaces

- Agent sessions discovered on remote hosts also appear in the session list and can be resumed remotely; unreachable remote sessions are clearly marked.
- "New Session" on the home page can target a remote location: pick a path in the remote directory browser and the workspace opens (file tree / editing / terminal) just like a local one.
- AllowAgent whitelist: each connection can define which agents are allowed to access it.
- The desktop automatically injects the kshell-ssh MCP and the local Hub into remote sessions so remote agents can use local capabilities.

## AgentHub

AgentHub is a multi-agent collaboration panel: run several agent sessions in one view and dispatch tasks in natural language, all visible to each other.

### Creating agents & messaging

- "New Agent" in the panel picks a project and tool; defaults are configured in Settings first.
- The `/easynew` command reuses the last project and tool for a quick start.
- Type into the input box to send to the current target; `@` targets a specific agent (auto-routed when omitted).
- Queued messages show ack indicators (delivered / queued); messages can be recalled.

### Commands

Type `#` to open the command palette:

- `/new` — create an agent.
- `/easynew` — quick-create using the last configuration.
- `/stop` — stop the target agent.
- `/model` — switch models.
- `/clear` — clear this view.
- `/find` — find and activate a discovered session.

### Input routing modes

Three routing modes are switchable per session:

- Auto: short phrase commands prefer built-in tools; everything else goes to the agent.
- Agent-only: everything is handed to the agent to interpret.
- Force tool: built-in commands are matched first.

### Confirmation cards & session management

- When an agent requests a write operation (writing files, running commands, etc.), a "Pending confirmation" card pops up; nothing proceeds until you approve or reject, keeping things safe and controllable.
- The sidebar lists all Hub sessions for jumping and closing.
- The view can auto-scroll to follow output.

## WeChat Bridge

Once bound, you can read agent replies and send instructions from WeChat, with the desktop app remaining the primary view.

### Binding

- In Settings → WeChat, click "Bind WeChat" and scan the QR code to confirm; binding completes automatically.
- The page shows live status and prompts to re-bind when disconnected.

### Whitelist & messaging

- Only whitelisted WeChat users can use the bridge, preventing strangers from abusing it; the binder is allowed by default.
- Messages sent in WeChat are forwarded to the agent and replies are mirrored back (outbound mirroring).
- Attachments such as images are passed to the agent.

### Voice transcription

- WeChat voice messages are decoded (SILK → WAV) and transcribed through an OpenAI-compatible ASR API before being sent to the agent.
- Configure the transcription model (ASR Model) for your platform under "Settings → Models"; without one it falls back to the default chat model.
- If the current model doesn't support audio input, WeChat replies with an explicit notice and configuration guidance.

## Settings

The Settings page has five sections: General / WeChat / Models / Tools / Skill Plaza.

### General

- Appearance: system / light / dark; UI font size; show whitespace.
- Interface language: English / Simplified Chinese / follow system.
- Window-close behavior: minimize to tray / exit.
- Default session mode: TUI (terminal) / ACP (chat) — falls back to terminal when the tool lacks ACP support.
- Permission mode: default (with confirmation) / Bypass. Bypass auto-approves CLI and ACP permission prompts, recommended only in trusted environments.
- Notification sound toggle.

### Models (multi-platform)

- Add multiple model platforms: built-in presets or custom, each with OpenAI / Anthropic dual-protocol Base URLs and an API key.
- Each platform maintains a model list: fetchable from the platform and filterable by keyword, or edited manually.
- One platform is the default; the in-session model dropdown merges all platforms as "platform prefix + model" and switching takes effect immediately.
- A voice transcription model (ASR Model) can also be set per platform for WeChat voice messages.

### Tools

- Lists all detected agent CLIs and their status: ok / not installed / broken / config only.
- One-click install, uninstall, and repair; uninstall can optionally purge configuration too.
- "Rescan" refreshes status after manual installs.

### Skill Plaza

- Search skills.sh for agent skills and install them to local tools in one click.
- Installs show a file preview and trust notice, and allow choosing install targets.
- Uninstall and force overwrite are supported.

### About & updates / Help

- About: shows the current version, manual update check, and immediate upgrade when available; includes a feedback link.
- Help: built-in bilingual docs for this user guide and the changelog.
- After upgrading, the full changelog pops up automatically.

## Keyboard Shortcuts

Press F1 (or the help entry in Settings) to open the shortcut cheat sheet at any time; Esc or F1 again closes it.

| Shortcut | Action |
|---|---|
| Ctrl+K | Toggle the quick switcher for tabs and workspaces |
| Ctrl+F | Focus the current view's search box |
| Ctrl+1..9 | Switch to the Nth open tab (out-of-range ignored) |
| Ctrl+Tab / Ctrl+Shift+Tab | Next / previous tab |
| Ctrl+W | Close the current workspace tab |
| Ctrl+0 | Go home (new-session entry) |
| Ctrl+T | Go home (yields to the terminal when a terminal is focused) |
| F1 | Show / hide the shortcut cheat sheet |

Note: when a terminal (xterm) is focused, shortcuts that would write to the terminal (Ctrl+K, Ctrl+F, Ctrl+W, Ctrl+T) yield to terminal semantics; only tab-switching shortcuts stay active, avoiding one keypress triggering two effects.
