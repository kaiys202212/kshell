# 内嵌终端 agent TUI 输入区「滚不回来」修复 — 设计

日期：2026-10-04
分支：fix/xterm-viewport

## 问题

桌面端内嵌终端里跑 agent CLI（codebuddy / claude code 等）时，用户拖动 xterm 滚动条翻历史，
之后即使滚到最底部也看不到 TUI 的输入区，打字也没有任何可见反馈（「交互丢失」）；
只有拖动窗口大小（触发重新 fit + PTY resize + 全量重绘）才能恢复。

## 根因

- 这类 agent CLI 是 ink 系 TUI：在**主缓冲区**用「光标上移 + ED2（`\x1b[2J`）全屏擦除 + 重绘」刷新界面，
  并伴随 ED3（`\x1b[3J`）清滚动缓冲。
- xterm.js 5.5 默认语义下 ED2 只擦除视口部分：擦除与用户滚动、流式输出交错时，
  **视口（viewportY）与滚动条/DOM scrollTop 发生失位**（实测复现：滚动条在底部、画面停在别处、
  输入行不可达），只有触发全量刷新的事件（resize → PTY re-emission）才能复位。
- 同族问题公开可查：anthropics/claude-code#33367（确认根因为 cursor-up + ED2/ED3 重绘）、
  xtermjs/xterm.js#5745（Codex CLI on Windows）。

## 修复

1. `@xterm/xterm` 5.5 → **6.0.0**，`@xterm/addon-fit` 0.10 → **0.11.0**。
2. TerminalView 启用 **`scrollOnEraseInDisplay: true`**（xterm 6.0 新选项）：
   ED2 时把旧屏内容滚入滚动缓冲，而非原地擦除——与 Windows Terminal 行为一致。
   实测（xterm 6.0 + 模拟 ink TUI）：
   - 开启前：重绘与滚动交错时视口失位，输入行不可达；
   - 开启后：agent 持续重绘时阅读位置稳定不被拽走（stableAtTop=0）、
     live 区恒在缓冲底部、滚到底必见输入行（viewportY===baseY）。

## 取舍

- 开启后滚动缓冲增长更快（每次全屏重绘入缓冲），由既有 `scrollback: 5000` 上限裁剪兜底；
  换取的是滚动行为正确，与 Windows Terminal 一致。
- 6.0 视口 DOM 重构（`.xterm-scrollable-element`），kshell 依赖的 `.xterm-screen`（IME 锚点修复）
  仍在，无兼容问题；`Terminal` 既有选项全部兼容（tsc + 246 测试通过）。

## 不做的事

- 不在 TUI 侧适配（CLI 不可控）；
- 不自写 ED2 CSI handler 模拟（6.0 已原生提供该语义，避免维护私有实现）。
