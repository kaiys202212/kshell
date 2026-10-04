// IME 锚点计算测试：对应 xtermjs#5734（agent TUI 把光标 park 在行尾时，
// 候选窗贴屏幕右缘把窗口挤动）的应用层修复。坐标规则与 xterm 内部
// _syncTextArea 对齐：textarea 位置 = 光标单元格的像素坐标。
// 注意 xterm API 语义：cursorY 是相对视口顶行的行号（0..rows-1），不含滚动偏移。
import { describe, expect, it } from 'vitest';
import {
  computeImeAnchor,
  computeImeClampStyles,
  IME_MAX_COL_RATIO,
  pickVisualCaret,
  imeOverflowLockStyles,
  shouldResetScrollLeft,
} from './imeAnchor';

const base = {
  cols: 100,
  rows: 30,
  cursorX: 10,
  cursorY: 20,
  viewportWidth: 1000,
  viewportHeight: 300,
};

describe('computeImeAnchor', () => {
  it('光标在视口内时锚到光标单元格像素坐标', () => {
    expect(computeImeAnchor(base)).toEqual({ left: 100, top: 200 });
  });

  it('光标超过 60% 宽度时横向钳制，候选窗不贴右缘', () => {
    const maxCol = Math.floor(base.cols * IME_MAX_COL_RATIO);
    const got = computeImeAnchor({ ...base, cursorX: 90 });
    expect(got).not.toBeNull();
    expect(got!.left).toBe(maxCol * (base.viewportWidth / base.cols));
    expect(got!.left).toBeLessThan(base.viewportWidth * IME_MAX_COL_RATIO + 1);
  });

  it('光标坐标异常时钳制在视口内（防御值）', () => {
    expect(computeImeAnchor({ ...base, cursorY: 50 })!.top).toBe(29 * 10);
    expect(computeImeAnchor({ ...base, cursorY: -3 })!.top).toBe(0);
    expect(computeImeAnchor({ ...base, cursorX: -1 })!.left).toBe(0);
  });

  it('退化输入（cols/rows/视口尺寸非正）返回 null', () => {
    expect(computeImeAnchor({ ...base, cols: 0 })).toBeNull();
    expect(computeImeAnchor({ ...base, rows: 0 })).toBeNull();
    expect(computeImeAnchor({ ...base, viewportWidth: 0 })).toBeNull();
    expect(computeImeAnchor({ ...base, viewportHeight: 0 })).toBeNull();
  });
});

describe('computeImeClampStyles', () => {
  it('复用钳制后的 left/top，并给出剩余视口 maxWidth', () => {
    const got = computeImeClampStyles({ ...base, cursorX: 90 });
    expect(got).not.toBeNull();
    const maxCol = Math.floor(base.cols * IME_MAX_COL_RATIO);
    expect(got!.left).toBe(maxCol * (base.viewportWidth / base.cols));
    expect(got!.top).toBe(base.cursorY * (base.viewportHeight / base.rows));
    expect(got!.maxWidth).toBe(base.viewportWidth - got!.left);
    expect(got!.maxWidth).toBeGreaterThan(0);
  });

  it('退化输入返回 null', () => {
    expect(computeImeClampStyles({ ...base, cols: 0 })).toBeNull();
  });
});

describe('pickVisualCaret', () => {
  it('优先取最后一个反色单元作为实际输入位置', () => {
    const got = pickVisualCaret(
      [
        { x: 5, y: 10, inverse: true },
        { x: 12, y: 20, inverse: true },
        { x: 90, y: 20, inverse: false },
      ],
      99,
      20,
      100,
      30,
    );
    expect(got).toEqual({ cursorX: 12, cursorY: 20 });
  });

  it('无反色单元时回退 fallback', () => {
    expect(pickVisualCaret([{ x: 1, y: 1, inverse: false }], 99, 20, 100, 30)).toEqual({
      cursorX: 99,
      cursorY: 20,
    });
  });

  it('忽略越界反色单元', () => {
    expect(
      pickVisualCaret([{ x: 100, y: 0, inverse: true }], 3, 4, 100, 30),
    ).toEqual({ cursorX: 3, cursorY: 4 });
  });
});

describe('ime overflow helpers', () => {
  it('组合期锁定 overflow-x', () => {
    expect(imeOverflowLockStyles()).toEqual({ overflowX: 'hidden' });
  });
  it('scrollLeft>0 时应复位', () => {
    expect(shouldResetScrollLeft(12)).toBe(true);
    expect(shouldResetScrollLeft(0)).toBe(false);
  });
});
