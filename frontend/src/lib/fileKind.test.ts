// 文件预览类型与 CM6 语言映射测试。
import { describe, expect, it } from 'vitest';
import { codeLanguage, previewKind } from './fileKind';

describe('previewKind', () => {
  it.each([
    ['src/a.ts', 'text'],
    ['README.md', 'markdown'],
    ['assets/x.PNG', 'image'],
    ['doc.pdf', 'pdf'],
    ['foo.unknown', 'text'],
    ['Dockerfile', 'text'],
    ['path/Makefile', 'text'],
    ['CMakeLists.txt', 'text'],
  ])('%s → %s', (path, kind) => {
    expect(previewKind(path)).toBe(kind);
  });
});

describe('codeLanguage', () => {
  it('TypeScript 源文件', () => {
    expect(codeLanguage('a.ts')).toBe('typescript');
  });

  it('Markdown 文件', () => {
    expect(codeLanguage('README.md')).toBe('markdown');
  });

  it('未知扩展名默认为 plaintext', () => {
    expect(codeLanguage('foo.unknown')).toBe('plaintext');
  });
});
