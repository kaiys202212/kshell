import { describe, expect, it } from 'vitest';
import { isNestedGitParent, resolveGitCode, subtreeDirty } from './git';

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

  it('虚拟根不把嵌套仓库改动算作外层脏', () => {
    const map = { 'wt/app/dirty.txt': 'untracked' };
    const dirs = { '': 'main', 'wt/app': 'feat/x' };
    expect(subtreeDirty(map, '', dirs)).toBe(false);
    expect(subtreeDirty(map, 'wt', dirs)).toBe(false);
    expect(subtreeDirty(map, 'wt/app', dirs)).toBe(true);
    expect(
      subtreeDirty({ 'a.ts': 'modified', 'wt/app/x.txt': 'untracked' }, '', dirs),
    ).toBe(true);
  });
});

describe('resolveGitCode', () => {
  it('祖先 ignored 时子路径继承 ignored', () => {
    const map = { vendor: 'ignored' };
    expect(resolveGitCode(map, 'vendor/pkg/a.go', false)).toBe('ignored');
    expect(resolveGitCode(map, 'vendor/pkg', true)).toBe('ignored');
  });

  it('仅部分子项 ignored 时父目录不推断为 ignored（porcelain 不含干净跟踪文件）', () => {
    const map = {
      'website/node_modules': 'ignored',
      'website/dist': 'ignored',
    };
    expect(resolveGitCode(map, 'website', true)).toBeUndefined();
    expect(resolveGitCode(map, 'website/src', true)).toBeUndefined();
    expect(resolveGitCode(map, 'website/package.json', false)).toBeUndefined();
    expect(resolveGitCode(map, 'website/node_modules', true)).toBe('ignored');
  });

  it('目录自身 untracked 时不因 ignored 子项覆盖为 ignored', () => {
    const map = {
      '.cursor': 'untracked',
      '.cursor/rules.md': 'ignored',
    };
    expect(resolveGitCode(map, '.cursor', true)).toBe('untracked');
    expect(resolveGitCode(map, '.cursor/rules.md', false)).toBe('ignored');
    expect(resolveGitCode(map, '.cursor/extra', false)).toBeUndefined();
  });

  it('目录下有非 ignored 子条目时保持自身码', () => {
    const map = {
      src: 'untracked',
      'src/a.ts': 'ignored',
      'src/b.ts': 'modified',
    };
    expect(resolveGitCode(map, 'src', true)).toBe('untracked');
  });

  it('自身明确改动态不被子路径 ignored 覆盖', () => {
    const map = { src: 'modified', 'src/a.ts': 'ignored' };
    expect(resolveGitCode(map, 'src', true)).toBe('modified');
  });

  it('文件行不因子路径推断', () => {
    const map = { 'skip.log': 'ignored' };
    expect(resolveGitCode(map, 'skip.log', false)).toBe('ignored');
    expect(resolveGitCode({ 'a/b': 'ignored' }, 'a', false)).toBeUndefined();
  });

  it('忽略继承不跨越嵌套 git 根', () => {
    const map = {
      feat: 'ignored',
      'feat/app': 'ignored',
      'feat/app/node_modules': 'ignored',
    };
    const dirs = { '': 'main', 'feat/app': 'feat/x' };
    expect(resolveGitCode(map, 'feat', true, dirs)).toBe('ignored');
    expect(resolveGitCode(map, 'feat/app', true, dirs)).toBeUndefined();
    expect(resolveGitCode(map, 'feat/app/README.md', false, dirs)).toBeUndefined();
    expect(resolveGitCode(map, 'feat/app/node_modules', true, dirs)).toBe('ignored');
  });
});

describe('isNestedGitParent', () => {
  it('嵌套仓库路径的严格前缀为中间层', () => {
    const dirs = { 'ext/lib': 'dev', '': 'main' };
    expect(isNestedGitParent(dirs, 'ext')).toBe(true);
    expect(isNestedGitParent(dirs, 'ext/lib')).toBe(false);
    expect(isNestedGitParent(dirs, '')).toBe(false);
    expect(isNestedGitParent(dirs, 'other')).toBe(false);
  });
});
