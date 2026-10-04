# 冒烟增量：fix/ime-composition-clamp

> 对应分支：`fix/ime-composition-clamp`
> 设计：`docs/plans/2026-10-04-ime-composition-clamp-design.md`

## Desktop

- [ ] **终端打中文：拼音预编辑与候选窗不贴右缘、TUI 不横向偏移**
  - 前置：桌面端；内嵌终端能跑 agent TUI（如 Claude Code）
  - 步骤：等光标 park 在行尾后用中文输入法连续输入拼音（未上屏）
  - 预期：`.composition-view` 拼音与 IME 候选锚点约在终端视口 60% 宽度处；agent TUI 画面不因拼音横向偏移；选字上屏后布局恢复正常
