# Changelog

## v0.1.32 (2026-11-18)

- More reliable updates: after detecting a new version, kshell first downloads and verifies this platform's package and only then shows "version ready"; choosing "Later" keeps the downloaded package and reuses it on the next launch or check instead of downloading again.
- Upgrade prompts now appear only when the package is actually available: if the latest release does not provide a package for this platform, kshell shows no prompt and never falls back to an older version.
- "Check for updates" in Settings now distinguishes "Download and install" from "Install and restart": the package is downloaded and verified first when needed, and installed directly once ready; a ready package can even be installed offline.
- The npx installer falls back to the previous release when the latest one has no package for the current platform.
- Fixed "Restart app" on Windows failing to start the new process under PowerShell 5.1 and leaving no running app.

## v0.1.31 (2026-11-18)

- AgentHub manager questions can now be answered inline: when an ask card appears in the thinking chain, answer it in the card (single choice, multi-select or free text) and the manager continues automatically — no manual follow-up in the session; progress updates live and can be interrupted, and the WeChat bridge works the same way.
- Ask-form improvements: multi-step questionnaires (a confirm step without options falls back to Yes/No); choosing "Other" expands its text box on demand; card titles show the project and session; cards collapse on click, answered cards show a clear state, and submitting an empty multi-select is disabled.
- The manager's thinking chain is grouped and collapsible per round, so long tasks no longer fill the chat with one endless block; the manager also considers recent session messages plus project and session context, reducing idle repeated reasoning.
- Fixed in-progress tools staying in "running" after a cancelled or abnormally stopped round (refusal, max tokens, etc.), which froze the frontend queue.
- Fixed AgentHub's new-session entry offering tools that don't support ACP and therefore could not chat; unsupported tools are filtered out automatically.
- Bare numbers typed into AgentHub are no longer treated as commands: while an answer is pending, replying with a number counts as the answer.
- WeChat binding is now part of AgentHub settings: turning AgentHub off unbinds WeChat and updates the home page; a confirmation is shown before closing, late scan callbacks can no longer write credentials back, and unbind/scan flows are fully ordered, fixing leftover bindings and orphan polling.
- The Git commit box gains AI-generated commit messages: generate a single-line `type: summary` from staged changes and fill it in with one click, for both local and SSH remote repos; malformed diffs or remote rate limits now produce clear errors instead of sending git error text to the model.
- Chat elicitation forms now understand `anyOf` enums, so union-typed options returned by agents parse and display correctly.

## v0.1.30 (2026-11-16)

- The file tree now supports copy/cut/paste: Ctrl+C copies selected files to the system clipboard (paste them in Explorer directly), Ctrl+X cuts within the tree and pasting moves them; name conflicts on paste are auto-renamed with a numeric suffix instead of overwriting, and moving a folder into its own subtree is rejected.
- The settings page gains an AgentHub section: master switch, heartbeat threshold and host sync in one place; WorkBuddy syncs kshell capabilities with one click, while other hosts get a self-install guide (a generated `~/.kshell/host-install.md` — send the one-line command shown in the UI to the platform's AI and it installs itself).
- The AgentHub manager is now a model-driven looping agent: tool calls with ask-based follow-up confirmations, long-term memory (a persisted journal with search), and automatic injection of recent session messages as context; both the desktop Hub and the WeChat bridge are wired in, the manager's thinking and conclusions are recorded and visible, and "continue"-type shortcuts go straight to the waiting session.
- New `kshell agent` command family and local HTTP API: discover/create sessions, deliver messages and subscribe to output, so external hosts like WorkBuddy can drive kshell programmatically.
- New WorkBuddy model platform: after importing WorkBuddy credentials in Settings, chat connects to the WorkBuddy gateway through a local proxy and can use its models directly (protocol translation and SSE stitching are handled automatically).
- Model platforms can now be chosen per tool: codex / gemini and other tools can each point at their own platform, and every model dropdown is unified as "pick a platform first, then a model"; switching models inside a chat only affects that session and no longer rewrites the global default.
- AgentHub gains group-send and overview commands: `#g` sends to multiple sessions, `#st` shows per-session status, `#gx` replays recent messages of a session; with no explicit target a local model routes the message, long tasks are aggregated into a heartbeat line and summarized automatically once everything finishes — the WeChat bridge behaves the same.
- The ACP chat window's bottom action row now hosts the model and parameter selectors; messages sent from the window are delivered directly and no longer blocked by AgentHub routing.
- Fixed ACP long-task timelines stalling (an unclosed seed window dropped subsequent updates).
- Chat page performance: the timeline is memoized, so typing in long sessions no longer re-renders every message and input stutter is gone.
- Fixed disk sessions being wrongly re-bound to ownerless old chats in the same workspace during scanning.
- Commit log loading failures now show an error toast instead of a silent blank panel.
- Removed the "rephrase" quick reply in chat; the Claude platform no longer injects a default pseudo-model into the dropdown.
- AgentHub sending reworked: messages with an explicit target (sent from a session's chat page, @ a session, replying with a number, quoting a message) are now delivered directly instead of first being handed to the local model to "decide whom it belongs to" — previously those messages cost an extra model round-trip and could even be rerouted to a different session. The local tool loop and its write-confirmation cards are gone as well (requirement/session tools are now only reachable from the intent router used for free text with no target).
- Input routing reduced to two modes: "Auto" and "Agent only". The former "Force tools" no longer had a distinct meaning; old configs are normalized to "Auto".
- Immediate feedback in AgentHub: when there is no explicit target, your bubble plus a "thinking" bubble appear at once and step through the current stage (deciding / delivering / listing sessions), and the routing can be stopped with a "Stop" button — previously nothing showed until the model's first token arrived, which looked like the message had not been sent.
- The file tree no longer inserts checkboxes while Shift is held (typing capitals used to shift the whole tree sideways); multi-select is now Shift range-select plus Ctrl/Cmd-click to add/remove rows.
- Model sync is now configured per tool: the "Sync platform models" toggle in the tools list becomes a "Model settings" dialog — Claude Code maps models to the 4 preset slots (default / Sonnet / Opus / Haiku), OpenCode picks models per platform (multi-platform supported), CodeBuddy picks models from the default platform. Nothing is injected when disabled or with no models checked, and previously injected entries are cleaned up. Note: after upgrading, previously auto-injected models are removed — please re-check models in each tool's "Model settings".
- Setting the default platform and the default model in Settings now takes effect immediately, without pressing the global save button; the resulting model sync is debounced, so rapid toggles collapse into one sync.
- The chat model dropdown now mirrors what is actually injected per tool (claude: default platform models; opencode/codebuddy: checked models) — fake options that could not work are gone.
- The model settings page now uses a two-column layout: platform list on the left, the add/edit platform form expands on the right with its own save button (auto-collapses after saving); the "Enable sync" toggle and platform deletion also take effect immediately — the standalone save button is gone.

## v0.1.29 (2026-11-09)

- The file tree supports locating by path: expanding layer by layer, selecting and scrolling to the target; activating a file tab now auto-locates the file in the tree, and clicking a folder attachment in chat also locates it in the file tree.
- Dragging files into the terminal now inserts a path relative to the workspace root first, keeping paths shorter.
- The file tree "show all" toggle takes effect immediately: watchers restart with the toggle, so changes to hidden files are no longer missed.
- Open now supports directories: opening a directory launches the system file manager.
- AgentHub panel polish: no more right-edge overflow, and the panel can be collapsed (collapsed by default, state remembered).
- Fixed truncated openings in the last agent turn for AgentHub and WeChat: replies are now stitched fully by turn boundaries, and boundary sentinels no longer leak into chat history.
- Fixed silent failure of hubtools internal model resolution after the platform rework (now resolves from the default platform).
- Fixed new AgentHub sessions being wrongly marked read-only when no tool was specified (the preferred tool is filled in automatically).

## v0.1.28 (2026-11-06)

- New one-click install: no manual download & unzip anymore — run `npx github:kaiys202212/kshell` (Node 18+) or `pip install kshell-install @ git+https://github.com/kaiys202212/kshell.git#subdirectory=tools/pypi` to install the desktop app in one line: it resolves the latest release, downloads the platform zip, verifies SHA-256, installs to `~/.kshell/bin` and launches it asynchronously; downloads prefer the GitCode mirror and fall back to GitHub. See "One-click Install" in the README.
- The README (EN / zh-CN) gains matching one-click install sections.

## v0.1.27 (2026-11-03)

- Easier custom tool onboarding: the settings page now offers an onboarding template download and yml import — fill in the standard template and import it with one click to register a custom agent tool, no manual config editing needed; if the imported file fails to read, existing config is preserved instead of being wiped.
- Fixed Claude chat failing to start with error -32603: settings are now passed as an object, and agent form validation details are forwarded.
- Fixed windows becoming visible but unclickable after maximizing on high-DPI displays (e.g. 200% scaling) on Windows: the frameless window now uses the pure frameless mode so the input hit area no longer misaligns.

## v0.1.26 (2026-11-01)

- OpenCode session experience overhaul: the session list now shows a conversation preview snippet (full text restored from SQLite), and sessions matched via `#find` can be previewed directly; missing titles fall back to the first user message with the message count filled in; empty "New session" sessions with zero messages are filtered out automatically.
- OpenCode model memory and injection improvements: resuming a history session replays the last-used model, every tool remembers the last-used model and applies it to new sessions; injected models now enable attachment / reasoning / tool_call by default; switching config models resolves by native ID, so the dropdown no longer shows duplicate bare-name copies.
- Model sync is now per-tool controllable: the tool list is split into installed / not-installed sections, and tools that support platform models can have sync turned off individually — turning it off also removes previously injected models; sync failures are logged instead of silently swallowed.
- Model sync fixes: fixed codebuddy models.json containing a UTF-8 BOM causing endless rewrites (freeze); dropdown merging now dedupes by ID so platform models colliding with built-in IDs are no longer lost; TRAE detection updated to the new `trae-cli` binary name and requires valid artifacts in its config dir.
- Session resume guards against double clicks: an inline spinner shows while resuming and repeated triggers are ignored; the activating state is propagated to the session list and preview panel.
- Fixed chat attachments failing to open files via relative paths due to a false out-of-bounds check.

## v0.1.25 (2026-10-31)

- Chat now supports agent config options and interactive forms: config dropdowns and form confirmations (elicitation) raised by the agent can be completed directly in the chat panel.
- Built-in opencode is promoted to a full ACP chat tool: the login entry works, launching no longer passes `--auto`, and login detection tolerates missing credentials on fresh installs.
- The Git panel gains manual refresh: one refresh button on the changes toolbar and one on the commit-log toolbar for when data fails to auto-update; overly long branch names show the full name on chip hover, and the row-hover detail card now lists all branch references of the commit.
- The AgentHub help dialog adds an OpenCode explanation.
- Model sync fixes: Claude Sonnet alias no longer points to an unselected model when the active model is not first in the list; one-time cleanup when all platforms are deleted and disabled; each tool's own model config is no longer rewritten when no model has ever been configured; dangling ActiveModel is cleared when no platform remains, avoiding wiping third-party config on the next sync.
- Fixed ACP session header detection not matching paths without a trailing slash.

## v0.1.24 (2026-10-29)

- Fixed agent tools being unusable under Windows install paths containing spaces: the CLI launch entry is unified with automatic `.cmd` batch wrapping — version detection, opencode session enumeration and TUI session launch for tools under `C:\Program Files` all work again; ACP and CodeBuddy / TRAE detection now go through the same entry.
- Fixed the platform polluting Claude's configuration when only an OpenAI-protocol platform is configured: model keys, top-level `model` and the ACP token are written only when an Anthropic endpoint is configured.
- Settings keeps a single "set as default" platform badge, and the Active model dropdown now refreshes after saving, so the state is no longer misleading.
- TUI now syncs the platform model into each tool's own configuration at startup, matching the desktop behavior.

## v0.1.23 (2026-10-27)

- Fixed the one-click update failure: the update script in v0.1.21 / v0.1.22 always errored on Windows PowerShell 5.1, so the downloaded new version could never be swapped into place (leaving a leftover `kshell-desktop.exe.new` in the install directory) and auto-update kept failing. Updates work normally from this version on.
- The update script is now fully compatible with both the bundled Windows PowerShell 5.1 and PowerShell 7, with real-execution tests added to prevent this class of bug from slipping through again.
- Note: if you are stuck on v0.1.21 or v0.1.22, auto-update cannot heal itself — please download this version's package manually and overwrite-install once with the app closed.

## v0.1.22 (2026-10-27)

- Claude model sync is smoother: newly installed Claude Code is initialized automatically, no need to go through the onboarding wizard; syncing now clears leftover display-name keys from newer versions, so `/model` shows the real model instead of being overridden.
- Fixed WeChat image attachment transfer failures.

## v0.1.21 (2026-10-25)

- Chat attachments are fully usable: image and file reference blocks in agent replies are recovered into the timeline, attachments can be clicked to preview or open with the system, and images are cached on disk.
- Configurable center layout: tabbed mode or left-right split (agent on the left, files + terminal on the right) with a draggable divider, switchable from both Settings and the tab bar; terminal remount replays scrollback history.
- New auto-start on boot: a Settings toggle that silently launches to tray when the system starts.
- Brand refresh to Capybara: capybara icon and display name, plus an open-source repository link in the About section.
- Chat bubbles and session previews now decode percent-encoded file names for display; stored content is untouched.
- Markdown preview supports relative-path images, loaded securely through the restricted file channel.
- Fixed syntax highlighting loss in the source editor under the light theme.
- Upgrade replacement now uses a rename-exchange with automatic rollback; upgrades still auto-relaunch the new version while the assistant is alive, fully undisturbed.
- Orphan processes are systematically eliminated: ACP adapters, login probes, and other subprocesses run in their own process groups and are terminated as a group on exit.
- macOS fixes: follow-system light/dark now reads AppleInterfaceStyle, and file reveal uses `open -R` to select the target.
- Fixed the image lightbox preview state not being reset when switching tabs.

## v0.1.20 (2026-10-24)

- Built-in TRAE CLI support: discovery, session parsing and resume, ACP chat, skills, and installation all work end to end; login state is detected via `traecli login status`.
- Session tab improvements: overflow tabs collapse into a "more" menu, drag-to-reorder is supported, project tabs are wider, and a spinning indicator shows while working.
- Tools that are not signed in no longer appear in the session creation entry (CodeBuddy / TRAE login detection); the list refreshes automatically when the login dialog closes.
- Model sync robustness: dangling default models self-heal, sync no longer evicts built-in models, ACP dropdowns dedupe same-name entries; Claude sync clears dangling top-level model, and CodeBuddy sync cleans up old-platform mirrored models and only injects the default platform.
- Uninstalling a tool now closes its kshell sessions first; deletion failures clearly report the process-in-use cause.
- CodeBuddy login detection now uses `_codebuddy.ai/getUserInfo` for better reliability; the tool-section refresh button was renamed with an action hint.

## v0.1.19 (2026-10-23)

- Model sync gains a Claude target: switching the default platform or installing Claude now auto-injects `settings.json` statically, syncing the active model and platform model aliases.
- Platform model sync now also writes OpenCode / CodeBuddy native configs.
- Fixed upgrade check version comparison across sources — the latest of all sources is picked — and corrected GitCode latest-tag detection.
- Fixed the global shortcut Alt+Shift+K occasionally failing to register.
- AgentHub welcome bubbles no longer show quick-reply buttons.

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
