// 工作区路径归一化测试：会话 cwd 的来源写法并不统一
// （Claude/Codex 是 `D:\a\b`，opencode 的 SQLite 是 `D:/a/b`，大小写也会漂）。
import { describe, expect, it } from 'vitest';
import { normalizeWorkspacePath, sameWorkspacePath } from './workspacePath';

describe('normalizeWorkspacePath', () => {
  it('分隔符统一成 /、末尾分隔符去掉、大小写统一', () => {
    expect(normalizeWorkspacePath('D:\\data\\ws')).toBe('d:/data/ws');
    expect(normalizeWorkspacePath('D:/data/ws')).toBe('d:/data/ws');
    expect(normalizeWorkspacePath('d:\\DATA\\ws\\')).toBe('d:/data/ws');
    expect(normalizeWorkspacePath('')).toBe('');
  });
});

describe('sameWorkspacePath', () => {
  it('反斜杠与前斜杠、大小写差异都视为同一工作区（opencode 会话能进同一个列表）', () => {
    expect(sameWorkspacePath('D:\\data\\workspace\\moxi\\kshell', 'D:/data/workspace/moxi/kshell')).toBe(
      true,
    );
    expect(sameWorkspacePath('d:\\proj-a\\', 'D:\\Proj-A')).toBe(true);
    expect(sameWorkspacePath('D:\\proj-a', 'D:\\proj-b')).toBe(false);
  });

  it('空路径只在两边都为空时算相同（避免把未知工作区的会话混进来）', () => {
    expect(sameWorkspacePath('', '')).toBe(true);
    expect(sameWorkspacePath('', 'D:\\proj-a')).toBe(false);
  });
});
