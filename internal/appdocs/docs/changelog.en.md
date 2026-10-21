# Changelog

## v0.1.18 (2026-10-22)

- AgentHub's session-pick menu is now grouped: a quick "new session" entry on top plus the 5 most recent sessions; the user's original message is visible immediately while picking.
- WeChat quoted delivery improved: it yields to pending picks and sends straight to the target session; failed attribution enters the pick flow instead of misfiring at the current session; outbound messages are attributed by SessionID on disk, so expired quotes still resolve by session.
- A "starting agent" animation shows while activating or creating a session, so waiting is no longer silent.
- AgentHub / WeChat show the full last reply, with automatic splitting for over-length messages.
- AgentHub session chips always show the first two characters of the project name, and messages carry absolute timestamps and the attributed tool name.
- Cursor ACP sessions use the first user question as the title and prefer Cursor's bundled node.exe.
- Fixed AgentHub termKey collisions mixing up session names across tools, cross-client pick clearing, external links now open in the system browser, plus a lighter file-tree watcher with independent git status color sync.

## v0.1.17 (2026-10-21)

- New "Help" section in Settings: built-in bilingual docs for the user guide and changelog, always available in-app.
- The full changelog now pops up automatically after upgrading to a new version, so new features are easy to spot.
- AgentHub WeChat bridge upgrades: voice messages are saved and forwarded with the conversation, attachments are merged with text, quoted replies are attributed to the right session, and outbound messages always start with [session name].
- AgentHub supports `#f` to send directly to the latest session; when there is no exact match, sessions are listed for selection and replying with a number delivers the message.
- AgentHub sessions are now flat chips above the input box — tap one to @ that session; completion notifications are deduplicated by text to avoid replaying history.
- AgentHub gains autonomous tools and `dispatch_task`, plus a routing-mode switch and a confirmation card before write operations.
- Cursor chat now uses the official ACP channel, with login-state detection and a login dialog, plus a fallback hint when not signed in.
- Cursor acp-sessions are discovered automatically; inactive sessions can be activated with a quick reply, and titles and previews are restored from store.db.
- Activating a Claude session over ACP restores the last used model; new sessions sync the selected model and reuse it next time.
- Fixed Markdown table rendering in ACP chat bubbles, frontend crashes when the model list is empty, and model-name matching failures when restoring sessions.

## v0.1.16 (2026-10-18)

- WeChat voice receipts are clearer when the model doesn't support audio input, with guidance to configure a transcription model in Settings.
- Voice transcription now falls back to the default chat model when no dedicated ASR model is configured, instead of failing outright.

## v0.1.15 (2026-10-17)

- Multi-platform model support: configure multiple model platforms (presets / custom dual-protocol endpoints), pick a default with one click, and see all models merged as "platform prefix + model" in session dropdowns.
- AgentHub autonomous tools: agents can register and invoke local capabilities (tool loop), with a confirmation card before write operations; input routing supports Auto / Agent-only / Force-tool modes.
- WeChat bridge: bind via QR code, whitelist who can use it, with context tokens, outbound message mirroring, and attachment forwarding.
- WeChat voice messages: SILK decoding to WAV and transcription through OpenAI-compatible ASR APIs.
- New F1 help panel: a keyboard shortcut cheat sheet available at any time.
- Fixed MCP stdio handshake timeouts and lost output (per-message Flush).

## v0.1.14 (2026-10-13)

- Plain shell terminals now inherit theme environment variables so terminal colors match the app.
- Fetched model lists can be filtered by keyword.
- Fixed incomplete PATH on macOS GUI launches.

## v0.1.13 (2026-10-13)

- Session list supports multi-select with batch archive / restore.
- Model configuration unified into a "list + active model" layout with model list fetching from platforms.
- The right-hand Terminal panel gained a plus button to quickly open a local shell.
- Tray watchdog black-box logs written to disk for diagnosing tray issues.

## v0.1.12 (2026-10-11)

- SSH client scan import: existing connections are read from Xshell and ssh config automatically.
- SSH panel supports an AllowAgent whitelist controlling which agents may reach remote hosts.
- Desktop automatically injects the kshell-ssh MCP and local Hub so remote sessions can use local tools.
- Adapted MCP injection across multiple tools; session tab names are now case-deduplicated.

## v0.1.11 (2026-10-11)

- Released macOS DMG installer.
- Added local build script build.sh for Linux / macOS.
- Fixed several tray menu issues.

## v0.1.10 (2026-10-09)

- HTML files can be rendered in a browser preview tab.
- Markdown preview supports keyword search with highlighting.
- The home page can filter workspaces by name / path.
- The editor integrates the CodeMirror find panel for in-file search.
- Unreachable remote sessions are clearly marked.

## v0.1.9 (2026-10-09)

- Git commit list loads more history automatically when scrolled to the bottom.
- Commit / sync operations show a busy spinner for clear feedback.

## v0.1.8 (2026-10-08)

- Fixed ConPTY launch failures for paths containing spaces.

## v0.1.7 (2026-10-08)

- Compatible with the legacy one-click upgrade entry for smoother upgrades.

## v0.1.6 (2026-10-08)

- Skill Plaza: search skills.sh and install skills to local agents in one click.
- Markdown preview and source mode can be switched freely.
- Clicking a Git commit opens its diff in a tab.
- Independent notification bubbles show agent activity while the main window is hidden.

## v0.1.5 (2026-10-07)

- First-run Agent setup wizard guides you through installing CLI tools.
- The About page in Settings gained a feedback link.
- Fixed session title cleaning.

## v0.1.4 (2026-10-06)

- Agent completion notification bubbles: hook / OSC / chat detection channels plus native Windows toasts.
- Update checks every two hours.
- Desktop shortcut is created automatically on first run.

## v0.1.3 (2026-10-05)

- CodeBuddy and Cursor joined ACP chat mode.

## v0.1.1 (2026-10-05)

- Upgrade source prefers the GitCode China mirror for faster, more reliable downloads.
- Fixed node-entry tool detection and version directory handling.

## v0.1.0 (2026-10-05)

- First public release.
- Desktop and TUI entry points: session discovery / resume / creation, file tree preview and editing, Git status, and SSH remote management.
- Supports many agent coding tools: Claude Code / Codex / Gemini / OpenCode / Cursor / CodeBuddy and more.
- One-click upgrades.
