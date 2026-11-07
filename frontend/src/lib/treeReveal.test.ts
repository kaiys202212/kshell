// 文件树定位注册表测试：FileTree 挂载登记、跨组件按 wsPath 投递、卸载注销。
import { describe, expect, it, vi } from 'vitest';
import {
  registerTreeRevealer,
  revealInTree,
  unregisterTreeRevealer,
} from './treeReveal';

describe('treeReveal', () => {
  it('revealInTree 投递给已登记的 revealer，返回 true', () => {
    const reveal = vi.fn();
    registerTreeRevealer('D:\\proj', { reveal });
    try {
      expect(revealInTree('D:\\proj', 'D:\\proj\\a\\b.ts')).toBe(true);
      expect(reveal).toHaveBeenCalledWith('D:\\proj\\a\\b.ts');
    } finally {
      unregisterTreeRevealer('D:\\proj');
    }
  });

  it('未登记时返回 false（调用方静默忽略）', () => {
    expect(revealInTree('D:\\none', 'D:\\none\\x.ts')).toBe(false);
  });

  it('重复登记覆盖旧 handle，注销后不可达', () => {
    const first = vi.fn();
    const second = vi.fn();
    registerTreeRevealer('D:\\proj', { reveal: first });
    registerTreeRevealer('D:\\proj', { reveal: second });
    try {
      expect(revealInTree('D:\\proj', 'D:\\proj\\x')).toBe(true);
      expect(first).not.toHaveBeenCalled();
      expect(second).toHaveBeenCalled();
    } finally {
      unregisterTreeRevealer('D:\\proj');
    }
    expect(revealInTree('D:\\proj', 'D:\\proj\\x')).toBe(false);
  });
});
