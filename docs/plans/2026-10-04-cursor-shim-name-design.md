# 设计：Cursor 官方安装器 shim 改名导致重启后探测不到

日期：2026-10-04
分支：`fix/cursor-shim-name`
状态：已定稿

## 症状

重启桌面端后，设置页 Cursor 行显示「安装」，新建会话下拉没有 Cursor。会话能扫出来（`~/.cursor` 目录在），但工具被当成未安装。

## 根因

2026-10-01 起官方 Windows 安装脚本把根目录 shim 命名为 `agent.cmd` / `agent.ps1`
（脚本内 `CURSOR_INVOKED_AS` 取自文件名，同一脚本兼容两种调用名），
`%LOCALAPPDATA%\cursor-agent\` 根与 `versions\<ver>\` 内**不再有任何 `cursor-agent.*` 文件**
（已用 find 全目录确认）。

kshell 探测链（`internal/providers/cursor.go` DetectSpec + `internal/providers/detect.go` Detect）：

1. `exec.LookPath("cursor-agent")` → 落空（`where.exe cursor-agent` 确认不在 PATH）；
2. `InstallDirs`（根目录 + versions 子目录）内按 `BinName` 拼 `.{exe,cmd,bat,ps1}` 找 → 全落空；
3. 兜底 `~/.cursor` 存在 → `Installed=true` 但 `BinPath=""`。

设置页安装按钮与新建会话下拉都按 `BinPath` 判定，于是 Cursor 显示成未安装。
此前能看到是旧布局时代缓存/快照残留。

## 决策

| 项 | 选择 |
|---|---|
| DetectSpec 扩展 | 新增 `AltBinNames []string`：备选可执行名 |
| PATH 探测 | 仍只用 `BinName`；`agent` 太通用，绝不进 PATH 探测，避免误命中无关二进制 |
| InstallDirs 探测 | `BinName` 与 `AltBinNames` 全部尝试（`binCandidates` 按名拼 Windows 后缀） |
| FindBins（卸载） | 同样覆盖 `AltBinNames`，卸载要删掉每一份拷贝 |
| Cursor 声明 | `AltBinNames: ["agent"]`，全平台声明（unix 安装脚本若同样改名也能命中，无害） |

## 非目标

- 不改其它工具（claude/codex/gemini/opencode）的探测
- 不处理 `CURSOR_INVOKED_AS` 或安装脚本本身
- 不改缓存/快照逻辑
