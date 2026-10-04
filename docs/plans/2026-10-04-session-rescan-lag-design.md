# 新建会话列表/页签标题滞后 — 设计

日期：2026-10-04  
分支：`fix/session-rescan-lag`

## 问题

新建会话后：

1. 左侧 session 列表不出现最新会话；
2. 中心区页签标题停在占位（「工作区 · 工具名」）；
3. 再新建下一个时，上一个才进列表；切页签时才注意到标题变化。

## 根因

1. **忙碌时重扫被静默丢弃**：`ScanSessions` 在 `scanning==true` 时直接 no-op。新建后的延迟重扫若撞上首页首扫，永远不会补扫。
2. **单次 3s 重扫过早**：CLI 会话落盘常晚于 3s（尤其要等首条用户消息才有可解析标题），没有后续重试。
3. **已绑定会话标题不同步**：`prevIDs` 跳过旧会话后不再更新标题；首轮绑上空标题后页签永久占位。

## 方案

| 层 | 改动 |
|---|---|
| Go `ScanSessions` | 忙碌时置 `pendingScan`，本轮结束后自动再扫一轮 |
| Go `attachDiscoveredSessions` | 新会话仍只绑一次；全部会话走 `UpdateSessionTitle` 同步标题 |
| 前端 `WorkspaceTab` | 新建后按 `[3s, 8s, 15s]` 退避多次 `scanSessions` |

## 不做

- 不引入文件监视/长轮询；
- 不改 TUI；
- 不把 `KindNew` 改成 `KindSession`（激活判断已按 SessionID）。
