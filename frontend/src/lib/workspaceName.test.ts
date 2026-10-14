import { describe, expect, it } from 'vitest';
import { workspaceName } from './workspaceName';

describe('workspaceName', () => {
  it('取 Windows 路径最后一段', () => {
    expect(workspaceName('D:\\a\\b\\proj')).toBe('proj');
  });

  it('取 POSIX 路径最后一段', () => {
    expect(workspaceName('/home/u/proj')).toBe('proj');
  });

  it('末尾分隔符忽略', () => {
    expect(workspaceName('D:\\a\\proj\\')).toBe('proj');
    expect(workspaceName('/home/u/proj/')).toBe('proj');
  });

  it('根路径返回去分隔符后的原串', () => {
    expect(workspaceName('D:\\')).toBe('D:');
  });

  it('空串返回空', () => {
    expect(workspaceName('')).toBe('');
  });
});
