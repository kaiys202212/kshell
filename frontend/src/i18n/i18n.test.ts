// i18n 基础设施：内置资源 key 一致、语言解析、外部包深合并、语言选项
import { describe, it, expect } from 'vitest';
import i18next from 'i18next';
import en from '../locales/en.json';
import zhCN from '../locales/zh-CN.json';
import { resolveLanguage, mergeExternalBundle, getLanguageOptions, initI18n } from './index';

function flatten(obj: Record<string, unknown>, prefix = ''): string[] {
  return Object.entries(obj).flatMap(([k, v]) => {
    const key = prefix ? `${prefix}.${k}` : k;
    if (v && typeof v === 'object') return flatten(v as Record<string, unknown>, key);
    return [key];
  });
}

describe('内置资源', () => {
  it('en 与 zh-CN 的 key 集完全一致', () => {
    expect(flatten(zhCN).sort()).toEqual(flatten(en).sort());
  });
});

describe('resolveLanguage', () => {
  it('非 system 原样返回', () => {
    expect(resolveLanguage('zh-CN')).toBe('zh-CN');
    expect(resolveLanguage('en')).toBe('en');
  });
  it('system 按 navigator.language 解析（jsdom=en-US → en）', () => {
    expect(resolveLanguage('system')).toBe('en');
  });
});

describe('mergeExternalBundle', () => {
  it('外部同语言覆盖同 key（深合并），调用方负责剥 $name', () => {
    const merged = mergeExternalBundle({ a: { b: 'ext' } }, { a: { b: 'base', c: 'keep' } });
    expect(merged).toEqual({ a: { b: 'ext', c: 'keep' } });
  });
});

describe('getLanguageOptions', () => {
  it('默认含 en/zh-CN', () => {
    expect(getLanguageOptions().map((o) => o.code)).toEqual(expect.arrayContaining(['en', 'zh-CN']));
  });
});

describe('initI18n', () => {
  it('外部覆盖在下一轮调用被还原为内置文案', async () => {
    await initI18n('en', { 'en.json': JSON.stringify({ err: { session_not_found: 'OVERRIDDEN' } }) });
    expect(i18next.t('err.session_not_found')).toBe('OVERRIDDEN');
    await initI18n('en', {});
    expect(i18next.t('err.session_not_found')).toBe(en.err.session_not_found);
  });

  it('外部新增 key 与外部新语言在下一轮调用被清除', async () => {
    await initI18n('en', {
      'en.json': JSON.stringify({ err: { external_only: 'EXTERNAL_ONLY' } }),
      de: JSON.stringify({ $name: 'Deutsch', err: { session_not_found: 'Sitzung fehlt' } }),
    });
    expect(i18next.exists('err.external_only')).toBe(true);
    expect(getLanguageOptions().map((o) => o.code)).toContain('de');
    expect(i18next.t('err.session_not_found', { lng: 'de' })).toBe('Sitzung fehlt');

    await initI18n('en', {});
    expect(i18next.exists('err.external_only')).toBe(false);
    expect(getLanguageOptions().map((o) => o.code)).not.toContain('de');
    expect(i18next.getResourceBundle('de', 'translation')).toBeUndefined();
  });

  it('非法 JSON / 非对象包跳过，不进语言选项', async () => {
    await initI18n('en', { 'xx.json': 'not json', 'yy.json': '[1,2]' });
    expect(getLanguageOptions().map((o) => o.code)).not.toContain('xx');
    expect(getLanguageOptions().map((o) => o.code)).not.toContain('yy');
    await initI18n('en', {});
  });
});
