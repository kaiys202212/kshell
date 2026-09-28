// 相对时间格式化测试。
import { afterEach, describe, expect, it, vi } from 'vitest';
import { formatRelativeTime } from './format';

afterEach(() => {
  vi.useRealTimers();
});

describe('formatRelativeTime', () => {
  it('1 分钟内显示「刚刚」', () => {
    vi.setSystemTime(new Date('2026-01-01T12:00:00Z'));
    expect(formatRelativeTime('2026-01-01T11:59:30Z')).toBe('刚刚');
  });

  it('1 小时内显示「N 分钟前」', () => {
    vi.setSystemTime(new Date('2026-01-01T12:00:00Z'));
    expect(formatRelativeTime('2026-01-01T11:30:00Z')).toBe('30 分钟前');
  });

  it('1 天内显示「N 小时前」', () => {
    vi.setSystemTime(new Date('2026-01-01T12:00:00Z'));
    expect(formatRelativeTime('2026-01-01T05:00:00Z')).toBe('7 小时前');
  });

  it('30 天内显示「N 天前」', () => {
    vi.setSystemTime(new Date('2026-01-15T12:00:00Z'));
    expect(formatRelativeTime('2026-01-10T12:00:00Z')).toBe('5 天前');
  });

  it('更久直接给本地日期', () => {
    vi.setSystemTime(new Date('2026-03-01T12:00:00Z'));
    expect(formatRelativeTime('2026-01-01T12:00:00Z')).toBe(
      new Date('2026-01-01T12:00:00Z').toLocaleDateString(),
    );
  });

  it('非法时间串返回空字符串', () => {
    expect(formatRelativeTime('not-a-date')).toBe('');
  });
});
