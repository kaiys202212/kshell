import { afterEach, describe, expect, it } from 'vitest';
import { clampSplit, loadSplit, saveSplit, splitKey } from './gitSplit';

afterEach(() => {
  localStorage.clear();
});

describe('gitSplit', () => {
  it('key 含工作区与仓库相对路径', () => {
    expect(splitKey('D:\\p', 'sub')).toContain('kshell-git-split:');
    expect(splitKey('D:\\p', 'sub')).toContain('sub');
  });

  it('clamp 限制在 0.22–0.8', () => {
    expect(clampSplit(0.1)).toBe(0.22);
    expect(clampSplit(0.9)).toBe(0.8);
    expect(clampSplit(0.4)).toBe(0.4);
  });

  it('读写记忆比例', () => {
    saveSplit('D:\\p', '', 0.33);
    expect(loadSplit('D:\\p', '')).toBe(0.33);
  });

  it('非法值回退默认 0.55', () => {
    localStorage.setItem(splitKey('D:\\p', ''), 'nope');
    expect(loadSplit('D:\\p', '')).toBe(0.55);
  });
});
