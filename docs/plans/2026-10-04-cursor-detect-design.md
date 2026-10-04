# 设计：重启后 Cursor 检测不到

日期：2026-10-04  
分支：`fix/cursor-detect`  
状态：已定稿

## 症状

重启桌面端后：设置页 Cursor 行出现「安装」；新建会话下拉没有 Cursor。

本机对照：`%LOCALAPPDATA%\cursor-agent\cursor-agent.cmd` 存在；官方布局还有 `versions\<日期-commit>\cursor-agent.cmd`。用户 PATH 通常只含安装根目录，不含 `versions\`。

## 根因

两条叠加：

1. **工具表写得太晚。** `runScan` 开头就 `DetectAllCached`，但 `a.tools` 要等后面整轮会话扫描结束才赋值。`GetTools` 在 `a.tools` 非空（快照灌入）时不等待。快照若仍是「仅配置目录、BinPath 空」，设置页按 BinPath 显示「安装」，新建会话过滤掉 Cursor。会话扫描在本机可达数分钟，`scan:done` 之前 UI 一直当没装。
2. **探测不认 versioned 布局。** `InstallDirs` 只看 `%LOCALAPPDATA%\cursor-agent\` 根下的 `cursor-agent.{cmd,exe,...}`。卸载残留或只留下 `versions\` 时 LookPath 与 InstallDirs 都落空，只剩 `~/.cursor` → `Installed=true` 且 `BinPath=""`。

## 决策

| 项 | 选择 |
|---|---|
| 工具表何时可见 | DetectAll 一结束就写入 `a.tools` 并推 `tools:updated`；会话扫描仍推 `scan:done` |
| GetTools | 等到本进程完成过一次 DetectAll（超时仍 3s），不再仅因快照非空立刻返回 |
| Cursor 安装目录 | 根目录 + `versions\` 下最新一档（目录名与官方脚本相同） |
| 安装按钮规则 | 仍看 BinPath，不改 |

## 非目标

- 把 Cursor IDE 本身当成可启动 CLI
- 改 npm 类工具的探测
- 缩短会话扫描本身
