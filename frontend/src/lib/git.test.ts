import { describe, expect, it } from 'vitest';
import { subtreeDirty } from './git';

describe('subtreeDirty', () => {
  it('虚拟根：任意未忽略改动即脏', () => {
    expect(subtreeDirty({ 'a.ts': 'modified' }, '')).toBe(true);
    expect(subtreeDirty({ 'a.ts': 'ignored' }, '')).toBe(false);
    expect(subtreeDirty({}, '')).toBe(false);
  });

  it('文件夹：子路径有改动即脏', () => {
    expect(subtreeDirty({ 'src/a.ts': 'untracked' }, 'src')).toBe(true);
    expect(subtreeDirty({ 'src/a.ts': 'untracked' }, 'lib')).toBe(false);
    expect(subtreeDirty({ src: 'modified' }, 'src')).toBe(true);
  });
});
