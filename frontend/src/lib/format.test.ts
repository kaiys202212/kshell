// 相对时间 / 输出尾部 / 时长格式化测试。
import { afterEach, describe, expect, it, vi } from 'vitest';
import { formatDuration, formatRelativeTime, tailLines } from './format';
import { tt } from '../test/i18n';

// time.* 用命名插值 {{n}}，断言按 en 资源拼出期望串，与具体语言解耦
const rel = (key: string, n: number) => tt(key).replace('{{n}}', String(n));

afterEach(() => {
  vi.useRealTimers();
});

describe('formatRelativeTime', () => {
  it('1 分钟内显示「刚刚」', () => {
    vi.setSystemTime(new Date('2026-01-01T12:00:00Z'));
    expect(formatRelativeTime('2026-01-01T11:59:30Z')).toBe(tt('time.just_now'));
  });

  it('1 小时内显示「N 分钟前」', () => {
    vi.setSystemTime(new Date('2026-01-01T12:00:00Z'));
    expect(formatRelativeTime('2026-01-01T11:30:00Z')).toBe(rel('time.minutes_ago', 30));
  });

  it('1 天内显示「N 小时前」', () => {
    vi.setSystemTime(new Date('2026-01-01T12:00:00Z'));
    expect(formatRelativeTime('2026-01-01T05:00:00Z')).toBe(rel('time.hours_ago', 7));
  });

  it('30 天内显示「N 天前」', () => {
    vi.setSystemTime(new Date('2026-01-15T12:00:00Z'));
    expect(formatRelativeTime('2026-01-10T12:00:00Z')).toBe(rel('time.days_ago', 5));
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

describe('tailLines', () => {
  it('不超过 maxLines 时原样返回', () => {
    expect(tailLines('a\nb\nc', 5)).toBe('a\nb\nc');
  });

  it('超过 maxLines 时只保留尾部 N 行', () => {
    const text = Array.from({ length: 60 }, (_, i) => `line-${i + 1}`).join('\n');
    expect(tailLines(text, 50)).toBe(
      Array.from({ length: 50 }, (_, i) => `line-${i + 11}`).join('\n'),
    );
  });

  it('恰好等于 maxLines 时不截断', () => {
    expect(tailLines('a\nb', 2)).toBe('a\nb');
  });

  it('空串返回空串', () => {
    expect(tailLines('', 50)).toBe('');
  });
});

describe('formatDuration', () => {
  it('Go time.Duration 的 JSON 纳秒值：毫秒级', () => {
    expect(formatDuration(20_000_000)).toBe('20ms');
  });

  it('秒级保留两位小数', () => {
    expect(formatDuration(1_500_000_000)).toBe('1.50s');
  });

  it('不足 1 毫秒归零展示 0ms', () => {
    expect(formatDuration(500)).toBe('0ms'); // 低于展示精度，归零毫秒
  });

  it('非法值（undefined/负数）兜底 0ms', () => {
    expect(formatDuration(Number.NaN)).toBe('0ms');
  });
});
