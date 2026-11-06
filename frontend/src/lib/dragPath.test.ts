import { describe, expect, it } from 'vitest';
import { DRAG_MIME, DRAG_ROOT_MIME, REL_MIME, pickTerminalDropPath, quotePathForShell } from './dragPath';

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
    expect(DRAG_ROOT_MIME).toBe('application/x-kshell-dragroot');
  });
});

describe('pickTerminalDropPath', () => {
  // 构造 dataTransfer.getData 的替身：mime -> 值
  const getter = (data: Record<string, string>) => (mime: string) => data[mime] ?? '';

  it('拖拽源与终端同工作区：返回相对路径', () => {
    const get = getter({
      [DRAG_MIME]: 'D:\\proj\\src\\a.go',
      [REL_MIME]: 'src/a.go',
      [DRAG_ROOT_MIME]: 'D:\\proj',
    });
    expect(pickTerminalDropPath(get, 'D:/proj')).toBe('src/a.go');
  });

  it('拖拽源与终端异工作区：回退绝对路径', () => {
    const get = getter({
      [DRAG_MIME]: 'D:\\other\\src\\a.go',
      [REL_MIME]: 'src/a.go',
      [DRAG_ROOT_MIME]: 'D:\\other',
    });
    expect(pickTerminalDropPath(get, 'D:\\proj')).toBe('D:\\other\\src\\a.go');
  });

  it('缺 REL_MIME：回退绝对路径', () => {
    const get = getter({
      [DRAG_MIME]: 'D:\\proj\\a.go',
      [DRAG_ROOT_MIME]: 'D:\\proj',
    });
    expect(pickTerminalDropPath(get, 'D:\\proj')).toBe('D:\\proj\\a.go');
  });

  it('缺 DRAG_ROOT_MIME：回退绝对路径', () => {
    const get = getter({
      [DRAG_MIME]: 'D:\\proj\\a.go',
      [REL_MIME]: 'a.go',
    });
    expect(pickTerminalDropPath(get, 'D:\\proj')).toBe('D:\\proj\\a.go');
  });
});
