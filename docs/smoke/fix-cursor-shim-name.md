# 冒烟：Cursor agent shim 改名探测

- [ ] 重启桌面端后设置 → 工具：Cursor 行显示已安装（版本/「卸载」），不是「安装」按钮
- [ ] `%LOCALAPPDATA%\cursor-agent\` 根目录只有官方新布局 `agent.cmd` / `agent.ps1`、无任何 `cursor-agent.*` 时仍能检出，BinPath 指向 agent.cmd
- [ ] 工作区「新建会话」下拉出现 Cursor，可正常新建与 `--resume` 恢复会话
- [ ] 设置页对 Cursor 卸载时，新旧两份 shim（cursor-agent.* 与 agent.*）都会被删除
