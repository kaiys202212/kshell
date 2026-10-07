// translateBackend：wire 格式解析、未知 key 原样返回、参数插值
// backendError：unknown 取串后走 translateBackend
import { describe, it, expect } from 'vitest';
import { backendError, translateBackend } from './errors';
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
  it('预设/更新等带连字符的 key 也能翻译', () => {
    expect(translateBackend('preset.minimax-token-plan.name')).toBe(
      en.preset['minimax-token-plan'].name,
    );
    expect(translateBackend('update.reason.up_to_date')).toBe(en.update.reason.up_to_date);
  });
});

describe('backendError', () => {
  it('Error 取 message 并翻译注册 key', () => {
    expect(backendError(new Error('err.session_not_found'))).toBe(
      en.err.session_not_found as string,
    );
  });
  it('字符串原样（非 key）返回', () => {
    expect(backendError('plain failure')).toBe('plain failure');
  });
  it('空串两种来源均返回空串', () => {
    expect(backendError('')).toBe('');
    expect(backendError(new Error(''))).toBe('');
  });
});
