import { describe, expect, it } from 'vitest';
import { composeTerminalPaste, isCopyKey, isPasteKey } from './clipboardPaste';

describe('composeTerminalPaste', () => {
  it('有路径时插入路径（含空格包引号）', () => {
    expect(composeTerminalPaste({ Text: 'x', Path: 'D:\\my shot.png' })).toBe('"D:\\my shot.png"');
  });

  it('无路径时插入文本', () => {
    expect(composeTerminalPaste({ Text: 'hello\nworld', Path: '' })).toBe('hello\nworld');
  });

  it('空快照得到空串', () => {
    expect(composeTerminalPaste({ Text: '', Path: '' })).toBe('');
    expect(composeTerminalPaste(null)).toBe('');
  });
});

describe('isPasteKey', () => {
  const base = { key: 'v', ctrlKey: false, metaKey: false, shiftKey: false, altKey: false };

  it('识别 Ctrl+V / Shift+Insert', () => {
    expect(isPasteKey({ ...base, ctrlKey: true })).toBe(true);
    expect(isPasteKey({ ...base, key: 'Insert', shiftKey: true })).toBe(true);
  });

  it('不把 Ctrl+C 或 Alt+V 当粘贴', () => {
    expect(isPasteKey({ ...base, key: 'c', ctrlKey: true })).toBe(false);
    expect(isPasteKey({ ...base, ctrlKey: true, altKey: true })).toBe(false);
  });
});

describe('isCopyKey', () => {
  const base = { key: 'c', ctrlKey: false, metaKey: false, shiftKey: false, altKey: false };

  it('识别 Ctrl+C / Ctrl+Insert', () => {
    expect(isCopyKey({ ...base, ctrlKey: true })).toBe(true);
    expect(isCopyKey({ ...base, key: 'Insert', ctrlKey: true })).toBe(true);
  });

  it('不把 Ctrl+V 或带 Shift/Alt 当复制', () => {
    expect(isCopyKey({ ...base, key: 'v', ctrlKey: true })).toBe(false);
    expect(isCopyKey({ ...base, ctrlKey: true, shiftKey: true })).toBe(false);
    expect(isCopyKey({ ...base, ctrlKey: true, altKey: true })).toBe(false);
  });
});
