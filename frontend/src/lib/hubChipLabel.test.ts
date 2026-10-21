import { describe, expect, it } from 'vitest';
import { chipSessionLabel, projectPrefix } from './hubChipLabel';

describe('projectPrefix', () => {
  it('取路径末段前两字（英文 / 中文）', () => {
    expect(projectPrefix('D:\\ws\\kshell')).toBe('ks');
    expect(projectPrefix('/home/u/strategy')).toBe('st');
    expect(projectPrefix('D:\\ws\\项目甲')).toBe('项目');
  });

  it('末段不足两字时原样返回', () => {
    expect(projectPrefix('D:\\ws\\a')).toBe('a');
  });

  it('空路径返回空串', () => {
    expect(projectPrefix('')).toBe('');
  });
});

describe('chipSessionLabel', () => {
  it('有标题时始终带项目前缀', () => {
    expect(chipSessionLabel('Agent Name Bug', 'D:\\ws\\kshell')).toBe('ks · Agent Name Bug');
    expect(chipSessionLabel('修登录', 'D:\\ws\\strategy')).toBe('st · 修登录');
  });

  it('无标题回落完整项目基名', () => {
    expect(chipSessionLabel('', 'D:\\ws\\kshell')).toBe('kshell');
  });

  it('无工作区时只显示标题或 empty', () => {
    expect(chipSessionLabel('仅标题', '')).toBe('仅标题');
    expect(chipSessionLabel('', '', '空')).toBe('空');
  });
});
