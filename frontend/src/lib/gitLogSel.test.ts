import { describe, expect, it, beforeEach, afterEach } from 'vitest';
import { loadLogSel, resolveLogFilter, saveLogSel, logSelKey } from './gitLogSel';

describe('resolveLogFilter', () => {
  it('映射当前 / 全部 / 指定分支', () => {
    expect(resolveLogFilter('current')).toEqual({ mode: 'current', ref: '' });
    expect(resolveLogFilter('all')).toEqual({ mode: 'all', ref: '' });
    expect(resolveLogFilter('topic')).toEqual({ mode: 'ref', ref: 'topic' });
    expect(resolveLogFilter('origin/main')).toEqual({ mode: 'ref', ref: 'origin/main' });
  });
});

describe('logSel 持久化', () => {
  beforeEach(() => {
    localStorage.clear();
  });
  afterEach(() => {
    localStorage.clear();
  });

  it('记住最新选择', () => {
    saveLogSel('D:\\a', '', 'topic');
    expect(loadLogSel('D:\\a', '')).toBe('topic');
    expect(localStorage.getItem(logSelKey('D:\\a', ''))).toBe('topic');
    expect(loadLogSel('D:\\b', '', 'current')).toBe('current');
  });
});
