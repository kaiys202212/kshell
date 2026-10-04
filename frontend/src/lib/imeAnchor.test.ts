// IME 锚点计算测试：对应 xtermjs#5734（agent TUI 把光标 park 在行尾时，
// 候选窗贴屏幕右缘把窗口挤动）的应用层修复。坐标规则与 xterm 内部
// _syncTextArea 对齐：textarea 位置 = 光标单元格的像素坐标。
import { describe, expect, it } from 'vitest';
import { computeImeAnchor, IME_MAX_COL_RATIO } from './imeAnchor';

const base = {
  cols: 100,
  rows: 30,
  cursorX: 10,
  cursorY: 20,
  viewportY: 0,
  viewportWidth: 1000,
  viewportHeight: 300,
};

describe('computeImeAnchor', () => {
  it('光标在行中时锚到光标单元格像素坐标', () => {
    expect(computeImeAnchor(base)).toEqual({ left: 100, top: 200 });
  });

  it('光标超过 60% 宽度时横向钳制，候选窗不贴右缘', () => {
    const maxCol = Math.floor(base.cols * IME_MAX_COL_RATIO);
    const got = computeImeAnchor({ ...base, cursorX: 90 });
    expect(got).not.toBeNull();
    expect(got!.left).toBe(maxCol * (base.viewportWidth / base.cols));
    expect(got!.left).toBeLessThan(base.viewportWidth * IME_MAX_COL_RATIO + 1);
  });

  it('有滚动时按可视行计算 top', () => {
    // cursorY=20、viewportY=5 → 可视第 15 行
    expect(computeImeAnchor({ ...base, viewportY: 5 })).toEqual({ left: 100, top: 150 });
  });

  it('光标滚出视口上方时钳到第 0 行，下方钳到最后一行', () => {
    // cursorY=20、viewportY=25 → 20-25=-5，钳到可视第 0 行
    expect(computeImeAnchor({ ...base, viewportY: 25 })!.top).toBe(0);
    // cursorY=35、viewportY=10 → 可视第 25 行超出 rows-1=29？25<29 不越界，用 45 制造越界
    expect(computeImeAnchor({ ...base, cursorY: 45, viewportY: 10 })!.top).toBe(29 * 10);
  });

  it('退化输入（cols/rows/视口尺寸非正）返回 null', () => {
    expect(computeImeAnchor({ ...base, cols: 0 })).toBeNull();
    expect(computeImeAnchor({ ...base, rows: 0 })).toBeNull();
    expect(computeImeAnchor({ ...base, viewportWidth: 0 })).toBeNull();
    expect(computeImeAnchor({ ...base, viewportHeight: 0 })).toBeNull();
  });
});
