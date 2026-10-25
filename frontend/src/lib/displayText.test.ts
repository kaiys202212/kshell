import { describe, expect, it } from 'vitest';
import { decodePercentText } from './displayText';

describe('decodePercentText', () => {
  it('还原中文文件名编码串', () => {
    // 模块与数据需求说明.md
    const enc = '%E6%A8%A1%E5%9D%97%E4%B8%8E%E6%95%B0%E6%8D%AE%E9%9C%80%E6%B1%82%E8%AF%B4%E6%98%8E.md';
    expect(decodePercentText(enc)).toBe('模块与数据需求说明.md');
  });

  it('还原 markdown 文件链接（标签与 URI 都还原）', () => {
    const enc = '[@%E6%A8%A1%E5%9D%97.md](file:///d:/a/%E6%95%B0%E6%8D%AE%E6%8E%A5%E5%85%A5%E6%B8%85%E5%8D%95.md)';
    expect(decodePercentText(enc)).toBe('[@模块.md](file:///d:/a/数据接入清单.md)');
  });

  it('单组 %HH 不还原（避免误伤正文）', () => {
    expect(decodePercentText('a%20b.md')).toBe('a%20b.md');
  });

  it('非法 UTF-8 序列保留原样', () => {
    expect(decodePercentText('%FF%FE')).toBe('%FF%FE');
    expect(decodePercentText('%E4%B8')).toBe('%E4%B8'); // 截断
  });

  it('解码出控制字符时保留原样（≥2 组才进守卫）', () => {
    expect(decodePercentText('a%0A%0Ab')).toBe('a%0A%0Ab');
    expect(decodePercentText('x%09%0Ay')).toBe('x%09%0Ay');
  });

  it('普通文本与孤立 % 原样返回', () => {
    expect(decodePercentText('合并两个文档，重复内容合并')).toBe('合并两个文档，重复内容合并');
    expect(decodePercentText('100% 还原')).toBe('100% 还原');
    expect(decodePercentText('printf("%s\\n")')).toBe('printf("%s\\n")');
  });

  it('编码串夹在正文中只还原编码部分', () => {
    const enc = '请合并 %E6%A8%A1%E5%9D%97.md 和 %E6%B8%85%E5%8D%95.md 两份文档';
    expect(decodePercentText(enc)).toBe('请合并 模块.md 和 清单.md 两份文档');
  });

  it('幂等：解码结果再解码不变', () => {
    const once = decodePercentText('%E6%A8%A1%E5%9D%97.md');
    expect(decodePercentText(once)).toBe(once);
  });
});
