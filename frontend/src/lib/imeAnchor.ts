// IME 候选窗锚点计算（应用层移植 xtermjs#5759 思路 + 边缘钳制）。
// 背景：xterm 把隐藏 textarea 锚在 buffer 光标处，Windows IME 候选窗跟随其屏幕位置；
// agent TUI 等待输入时常把光标 park 在行尾，候选窗贴屏幕右缘，触发原生层的窗口移动。
// compositionstart 时按本函数结果重设 textarea 位置，把候选窗拉回视口内。
// 坐标规则与 xterm 内部 _syncTextArea 对齐：textarea 位置 = 光标单元格的像素坐标。

/** 横向钳制阈值：光标列超过视口宽度该比例时候选窗贴右缘，钳到该列。 */
export const IME_MAX_COL_RATIO = 0.6;

export interface ImeAnchorInput {
  cols: number;
  rows: number;
  cursorX: number; // buffer 光标列
  cursorY: number; // buffer 光标行（含滚动偏移）
  viewportY: number; // 视口顶部对应的 buffer 行
  viewportWidth: number;
  viewportHeight: number;
}

export interface ImeAnchor {
  left: number;
  top: number;
}

export function computeImeAnchor(input: ImeAnchorInput): ImeAnchor | null {
  const { cols, rows, cursorX, cursorY, viewportY, viewportWidth, viewportHeight } = input;
  if (cols <= 0 || rows <= 0 || viewportWidth <= 0 || viewportHeight <= 0) return null;

  const cellW = viewportWidth / cols;
  const cellH = viewportHeight / rows;

  // 光标可视行越界（滚出视口）时钳回屏内，候选窗跟着可见区域走
  const visibleRow = Math.min(Math.max(cursorY - viewportY, 0), rows - 1);
  // 横向钳制：不让候选窗出现在视口右侧 40% 区域，避免贴屏幕右缘
  const maxCol = Math.floor(cols * IME_MAX_COL_RATIO);
  const clampedX = Math.min(Math.max(cursorX, 0), maxCol);

  return { left: clampedX * cellW, top: visibleRow * cellH };
}
