import { describe, expect, it } from 'vitest';
import { terminalTheme } from './appearance';

describe('terminalTheme', () => {
  // jsdom 的 getComputedStyle 对 CSS 变量返回空串，这里天然走回退常量路径。
  it('深色回退常量：深底浅字（与 style.css token 同值）', () => {
    expect(terminalTheme('dark')).toEqual({ background: '#131b18', foreground: '#dce8e4' });
  });
  it('浅色回退常量：白底深字（与 style.css token 同值）', () => {
    expect(terminalTheme('light')).toEqual({ background: '#ffffff', foreground: '#1c2a27' });
  });
  it('能读到 CSS 变量时 token 优先', () => {
    const resolve = (name: string) => (name === '--card' ? '#abc' : '#def');
    expect(terminalTheme('dark', resolve)).toEqual({ background: '#abc', foreground: '#def' });
  });
  it('token 缺失其一（如 --foreground 读不到）时回退常量', () => {
    const resolve = (name: string) => (name === '--card' ? '#abc' : null);
    expect(terminalTheme('light', resolve)).toEqual({ background: '#ffffff', foreground: '#1c2a27' });
  });
});
