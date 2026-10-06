import { describe, expect, it } from 'vitest';
import type { GitSCMEntry } from './api';
import { buildChangeTree, collectPaths } from './gitChangeTree';

const entry = (path: string, over: Partial<GitSCMEntry> = {}): GitSCMEntry => ({
  Path: path,
  X: ' ',
  Y: 'M',
  Staged: false,
  Unstaged: true,
  Untracked: false,
  Conflicted: false,
  ...over,
});

describe('buildChangeTree', () => {
  it('按目录嵌套并收集叶子路径', () => {
    const root = buildChangeTree([
      entry('pkg/a.go'),
      entry('pkg/sub/b.go'),
      entry('root.go'),
    ]);
    expect(root.children.map((c) => c.name)).toEqual(['pkg', 'root.go']);
    const pkg = root.children[0];
    expect(pkg.dir).toBe(true);
    expect(pkg.children.map((c) => c.name)).toEqual(['sub', 'a.go']);
    expect(collectPaths(pkg).sort()).toEqual(['pkg/a.go', 'pkg/sub/b.go']);
    expect(collectPaths(root.children[1])).toEqual(['root.go']);
  });
});
