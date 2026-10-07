// tt()：按 key 取 en 资源文案，缺 key 直接抛错防假阳性断言
import { describe, it, expect } from 'vitest';
import { tt } from './i18n';
import en from '../locales/en.json';

describe('tt', () => {
  it('命中 key 返回 en 文案', () => {
    expect(tt('err.session_not_found')).toBe(en.err.session_not_found);
  });

  it('缺少 key 抛错而非原样返回', () => {
    expect(() => tt('err.not_registered_key')).toThrowError(/缺少 key err\.not_registered_key/);
    expect(() => tt('err.session_not_found.deep.dive')).toThrow();
  });
});
