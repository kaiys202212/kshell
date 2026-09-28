// 终端窗口标题归一化测试：与 Go 侧 WindowManager.TerminalTitle 的行为逐条对齐。
import { describe, expect, it } from 'vitest';
import { terminalTitle } from './title';

describe('terminalTitle', () => {
  it('短标题加前缀，不截断', () => {
    expect(terminalTitle('修复上传白名单')).toBe('kshell · 修复上传白名单');
  });

  it('超 80 rune 截到 79 rune 并补省略号', () => {
    const title = '长'.repeat(100); // "kshell · " 占 9 rune，总长 109
    const result = terminalTitle(title);
    expect(Array.from(result)).toHaveLength(80);
    expect(result.endsWith('…')).toBe(true);
    expect(result.startsWith('kshell · ')).toBe(true);
  });

  it('恰好 80 rune 不截断、无省略号', () => {
    const title = 'x'.repeat(71); // 9 + 71 = 80
    const result = terminalTitle(title);
    expect(Array.from(result)).toHaveLength(80);
    expect(result.endsWith('…')).toBe(false);
  });

  it('按 rune 而非字节截断（中文与 emoji 同为一 rune）', () => {
    const title = '🚀'.repeat(100); // 每个 emoji 是 2 个 UTF-16 码元、1 个 rune
    const result = terminalTitle(title);
    expect(Array.from(result)).toHaveLength(80);
    // 截断必须落在 emoji 边界上：结尾是完整的 emoji + 省略号，不产生孤立代理对
    expect(result.endsWith('🚀…')).toBe(true);
  });
});
