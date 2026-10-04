import { describe, expect, it } from 'vitest';
import { emptyCustomProvider, sessionGlobFromDir } from './providersForm';

describe('sessionGlobFromDir', () => {
  it('把 Windows 会话目录收成 jsonl glob', () => {
    expect(sessionGlobFromDir('C:\\Users\\a\\.foo\\projects')).toBe(
      'C:/Users/a/.foo/projects/*/*.jsonl',
    );
  });

  it('去掉尾部分隔符', () => {
    expect(sessionGlobFromDir('/home/a/.foo/projects/')).toBe('/home/a/.foo/projects/*/*.jsonl');
  });

  it('空目录得到空串', () => {
    expect(sessionGlobFromDir('')).toBe('');
  });
});

describe('emptyCustomProvider', () => {
  it('默认 jsonl 与 resume 占位', () => {
    const s = emptyCustomProvider();
    expect(s.Sessions.Format).toBe('jsonl');
    expect(s.Resume.Args).toEqual(['--resume', '{id}']);
    expect(s.Verified).toBe(false);
  });
});
