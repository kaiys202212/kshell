import { describe, expect, it } from 'vitest';
import { terminalTheme } from './appearance';

describe('terminalTheme', () => {
  it('深色返回深底浅字', () => {
    expect(terminalTheme('dark')).toEqual({ background: '#0d1117', foreground: '#e6edf3' });
  });
  it('浅色返回白底深字', () => {
    expect(terminalTheme('light')).toEqual({ background: '#ffffff', foreground: '#1f2328' });
  });
});
