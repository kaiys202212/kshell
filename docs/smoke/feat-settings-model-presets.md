# 冒烟：feat/settings-model-presets

基线见 `baseline-desktop.md`。本文件仅增量。

## 设置页布局

- [ ] 打开设置：左侧有「通用 / 模型 / 工具」分区，点击可切换右侧内容
- [ ] 默认外观为深色（新配置或未写 appearance 时）

## 会话模式 / 权限

- [ ] 通用 → 默认会话模式为 TUI；切到 ACP 后，对支持 ACP 的工具新建走聊天
- [ ] TUI 模式下，新建菜单对 ACP 可用工具显示「xxx（ACP）」项，点选可强制开聊天
- [ ] 权限 Bypass：新开 Claude 终端应带跳过权限参数；ACP 聊天权限不再弹窗自动放行
- [ ] 权限切回默认后，ACP 权限弹窗恢复

## 模型预设

- [ ] 模型区选 DeepSeek / MiniMax 等预设，自动填入 OpenAI 与 Anthropic Base URL
- [ ] 保存后新开 Claude 会话走 Anthropic URL；新开 Codex 走 OpenAI URL
- [ ] 「应用推荐模型」覆盖各 agent 模型名
- [ ] 密钥留空保存不清除已有密钥
