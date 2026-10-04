# 设计：设置页工具检测加「重新扫描」按钮

日期：2026-10-04
分支：`feat/settings-rescan-button`
状态：已定稿

## 背景

工具检测（设置 → 工具）目前只能被动等 `tools:updated` / `scan:done` 事件刷新；用户排查工具探测问题时（如 Cursor shim 缺失重装后），需要去首页点「重新扫描」才能触发一次新的 DetectAll。首页已有该按钮（`Home.tsx` handleRescan：置 scanning 态 + `scanSessions()`），设置页缺一个同等入口。

## 决策

| 项 | 选择 |
|---|---|
| 位置 | 工具检测 section 标题行右侧，与「工具检测」标题同行 |
| 行为 | 与首页一致：点击 → `scanSessions()`（触发后端 `ScanSessions`，DetectAll 结果经 `tools:updated` 早于 `scan:done` 回流） |
| 忙状态 | 本地 `rescanning` state：点击置 true，收到 `tools:updated` 即恢复（工具检测只关心 DetectAll，不必等整轮会话扫描）；按钮禁用防连点 |
| 文案 | 「重新扫描」；扫描中显示「扫描中…」（沿用首页文案） |
| 刷新 | 复用现有 `onToolsUpdated` / `onScanDone` 订阅，不新增刷新通道 |

## 非目标

- 不改首页按钮
- 不改后端（`ScanSessions` 已存在且幂等，进行中扫描会排队复扫）
