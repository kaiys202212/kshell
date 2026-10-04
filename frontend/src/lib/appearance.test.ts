import { describe, expect, it, vi } from 'vitest';
import { applyUiFontSize, clampUiFontSize, terminalTheme } from './appearance';

describe('clampUiFontSize', () => {
  it('缺省与越界钳到 10–20，默认 13', () => {
    expect(clampUiFontSize(Number.NaN)).toBe(13);
    expect(clampUiFontSize(0)).toBe(13);
    expect(clampUiFontSize(-2)).toBe(13);
    expect(clampUiFontSize(7)).toBe(10);
    expect(clampUiFontSize(16)).toBe(16);
    expect(clampUiFontSize(25)).toBe(20);
  });
});

describe('applyUiFontSize', () => {
  it('写入 CSS 变量、html rem 基准与 localStorage', () => {
    const setItem = vi.spyOn(Storage.prototype, 'setItem');
    applyUiFontSize(16);
    expect(document.documentElement.style.getPropertyValue('--app-font-size')).toBe('16px');
    expect(document.documentElement.style.fontSize).toBe(`${(16 * 16) / 13}px`);
    expect(setItem).toHaveBeenCalledWith('kshell-font-size', '16');
    setItem.mockRestore();
  });
});

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
