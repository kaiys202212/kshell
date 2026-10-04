import { describe, expect, it } from 'vitest';
import { DRAG_MIME, REL_MIME, quotePathForShell } from './dragPath';

describe('quotePathForShell', () => {
  it('普通路径不加引号', () => {
    expect(quotePathForShell('D:\\proj\\a.go')).toBe('D:\\proj\\a.go');
  });
  it('含空格路径包双引号', () => {
    expect(quotePathForShell('D:\\my file\\a.go')).toBe('"D:\\my file\\a.go"');
  });
  it('DRAG_MIME 常量稳定', () => {
    expect(DRAG_MIME).toBe('application/x-kshell-path');
    expect(REL_MIME).toBe('application/x-kshell-relpath');
  });
});
