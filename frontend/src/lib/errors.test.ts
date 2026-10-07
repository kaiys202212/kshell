// translateBackend：wire 格式解析、未知 key 原样返回、参数插值
import { describe, it, expect } from 'vitest';
import { translateBackend } from './errors';
import en from '../locales/en.json';

describe('translateBackend', () => {
  it('无参数 key 直接翻译', () => {
    expect(translateBackend('err.session_not_found')).toBe(en.err.session_not_found as string);
  });
  it('带参数按位置插值', () => {
    const out = translateBackend('err.files.too_big_preview|10|20');
    expect(out).toContain('10');
    expect(out).toContain('20');
    expect(out).not.toContain('{{0}}');
  });
  it('未知 key 原样返回', () => {
    expect(translateBackend('random non key')).toBe('random non key');
    expect(translateBackend('err.not_registered|x')).toBe('err.not_registered|x');
  });
  it('空串原样返回', () => {
    expect(translateBackend('')).toBe('');
  });
});
