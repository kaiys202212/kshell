// IME 候选窗 / 拼音预编辑锚点计算（应用层移植 xtermjs#5759 思路 + 边缘钳制）。
// 背景：xterm 把隐藏 textarea 与 .composition-view 锚在 buffer 光标处；
// agent TUI 常把光标 park 在行尾，拼音 nowrap 贴右缘挤布局，候选窗也贴屏幕右缘。
// 组合全程按本模块结果重设两者位置，并限制 composition-view 最大宽度。
// 坐标规则与 xterm 内部 _syncTextArea 对齐：位置 = 光标单元格的像素坐标。

/** 横向钳制阈值：光标列超过视口宽度该比例时候选窗贴右缘，钳到该列。 */
export const IME_MAX_COL_RATIO = 0.6;

export interface ImeAnchorInput {
  cols: number;
  rows: number;
  cursorX: number; // 光标列（0..cols-1）
  cursorY: number; // 光标行（xterm API 语义：相对视口顶行的 0..rows-1）
  viewportWidth: number;
  viewportHeight: number;
}

export interface ImeAnchor {
  left: number;
  top: number;
}

/** 应用到 .composition-view / textarea 的钳制样式。 */
export interface ImeClampStyles {
  left: number;
  top: number;
  maxWidth: number; // composition-view：视口剩余宽度，至少 1
}

export function computeImeAnchor(input: ImeAnchorInput): ImeAnchor | null {
  const { cols, rows, cursorX, cursorY, viewportWidth, viewportHeight } = input;
  if (cols <= 0 || rows <= 0 || viewportWidth <= 0 || viewportHeight <= 0) return null;

  const cellW = viewportWidth / cols;
  const cellH = viewportHeight / rows;

  // cursorY 本身就是视口相对行（0..rows-1），无需再减滚动偏移；钳制防御异常值
  const row = Math.min(Math.max(cursorY, 0), rows - 1);
  // 横向钳制：不让候选窗出现在视口右侧 40% 区域，避免贴屏幕右缘
  const maxCol = Math.floor(cols * IME_MAX_COL_RATIO);
  const clampedX = Math.min(Math.max(cursorX, 0), maxCol);

  return { left: clampedX * cellW, top: row * cellH };
}

export function computeImeClampStyles(input: ImeAnchorInput): ImeClampStyles | null {
  const anchor = computeImeAnchor(input);
  if (!anchor) return null;
  const maxWidth = Math.max(input.viewportWidth - anchor.left, 1);
  return { left: anchor.left, top: anchor.top, maxWidth };
}

/** 终端单元格渲染提示：反色单元 ≈ TUI 可见 caret（相对视口行列）。 */
export interface CellHint {
  x: number;
  y: number;
  inverse: boolean;
}

/**
 * 从渲染提示中选取 IME 锚点用的光标列/行。
 * 优先视口内最后一个反色单元；若无则回退 buffer 光标（fallback，由调用方再钳制）。
 */
export function pickVisualCaret(
  hints: CellHint[],
  fallbackX: number,
  fallbackY: number,
  cols: number,
  rows: number,
): { cursorX: number; cursorY: number } {
  let cursorX = fallbackX;
  let cursorY = fallbackY;

  for (const h of hints) {
    if (!h.inverse) continue;
    if (h.x < 0 || h.x >= cols || h.y < 0 || h.y >= rows) continue;
    cursorX = h.x;
    cursorY = h.y;
  }

  return { cursorX, cursorY };
}

/** 组合输入期间锁定横向滚动时的样式（便于单测与 TerminalView 对称接线）。 */
export interface OverflowLockStyles {
  overflowX: 'hidden';
}

export function imeOverflowLockStyles(): OverflowLockStyles {
  return { overflowX: 'hidden' };
}

/** 视口 scrollLeft 非零时应在组合期复位，避免候选窗错位。 */
export function shouldResetScrollLeft(scrollLeft: number): boolean {
  return scrollLeft > 0;
}
