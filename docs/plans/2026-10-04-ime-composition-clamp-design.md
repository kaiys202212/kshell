# 设计：IME 组合全程钳制（composition-view + textarea）

日期：2026-10-04　分支：`fix/ime-composition-clamp`

## 背景与目标

上次修复（`imeAnchor` + `compositionstart` 钳制 textarea）后，中文输入法仍会让内嵌 agent TUI 横向偏移。根因：xterm 在组合期间持续把 `.composition-view`（拼音预编辑）和 `textarea` 重定位到 **buffer 硬件光标**；agent TUI 常把光标 park 在行尾，拼音 `white-space: nowrap` 贴右缘挤布局。

目标：组合全程把预编辑层与 IME 锚点钳在终端视口约 60% 宽度处，并限制预编辑最大宽度，避免右侧溢出。

## 方案（已确认：A）

不改 xterm 内部、不找反色真光标、不升级 xterm 大版本。

1. 扩展 `frontend/src/lib/imeAnchor.ts`：在现有 `computeImeAnchor` 之上，提供应用样式所需的计算结果（钳制后的 left/top，以及 composition-view 的 maxWidth = viewportWidth − left）。
2. `TerminalView.tsx` 在组合全程应用：`compositionstart` / `compositionupdate` / `onRender`（仅组合中）同时设置 `.composition-view` 与 `textarea`。
3. `compositionend` 后停止强制定位，交回 xterm 默认。

## 接口

```ts
export interface ImeClampStyles {
  left: number;
  top: number;
  maxWidth: number; // composition-view：视口剩余宽度，至少 1
}

export function computeImeClampStyles(input: ImeAnchorInput): ImeClampStyles | null;
```

`computeImeAnchor` 保持；`computeImeClampStyles` 复用其 left/top，再算 maxWidth。

## 接线要点

- 查询 `.composition-view`（与 `.xterm-helper-textarea` / `instance.textarea`）。
- 每次应用：`compositionView.style.left/top/maxWidth/overflow`；`textarea.style.left/top`。
- 监听：textarea 的 compositionstart/update/end；xterm `onRender` 在 composing 时重应用（对抗 xterm `updateCompositionElements`）。
- 卸载移除全部监听。

## 测试

- `imeAnchor.test.ts`：右缘光标 left 钳制；maxWidth = viewportWidth − left；退化输入返回 null。
- TerminalView：以纯函数覆盖为主；组件侧保持现有 IME 相关行为不回归即可。

## 成功标准

内嵌终端跑 agent TUI，光标 park 行尾打拼音：预编辑与候选锚点约在视口 60% 宽处，TUI 不再因右侧拼音横向偏移。

## 不做

- 反色单元格启发式（方案 B）
- 单独升级 xterm 依赖（方案 C，不解决行尾 park）
- Go / 构建脚本改动
