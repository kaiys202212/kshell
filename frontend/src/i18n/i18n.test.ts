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

/** 取字符串叶子的占位符集合（排序后），如 {{0}}、{{name}} */
function placeholders(s: string): string[] {
  return (s.match(/\{\{[^}]*\}\}/g) ?? []).sort();
}

function stringLeaves(obj: Record<string, unknown>, prefix = ''): Map<string, string> {
  const out = new Map<string, string>();
  for (const [k, v] of Object.entries(obj)) {
    const key = prefix ? `${prefix}.${k}` : k;
    if (v && typeof v === 'object') {
      for (const [ik, iv] of stringLeaves(v as Record<string, unknown>, key)) out.set(ik, iv);
    } else if (typeof v === 'string') {
      out.set(key, v);
    }
  }
  return out;
}

describe('内置资源', () => {
  it('en 与 zh-CN 的 key 集完全一致', () => {
    expect(flatten(zhCN).sort()).toEqual(flatten(en).sort());
  });

  it('同 key 叶子的占位符集合一致（防 {{0}} 只在一侧存在）', () => {
    const enLeaves = stringLeaves(en);
    const zhLeaves = stringLeaves(zhCN);
    for (const [key, enVal] of enLeaves) {
      const zhVal = zhLeaves.get(key);
      expect(zhVal, `zh-CN 缺少 key ${key}`).toBeTypeOf('string');
      expect(placeholders(zhVal as string), `key ${key} 占位符不一致`).toEqual(placeholders(enVal));
    }
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

  it('返回拷贝，调用方改动不污染内部状态', () => {
    const opts = getLanguageOptions();
    opts.length = 0;
    opts.push({ code: 'xx' });
    expect(getLanguageOptions().map((o) => o.code)).toEqual(expect.arrayContaining(['en', 'zh-CN']));
    expect(getLanguageOptions().map((o) => o.code)).not.toContain('xx');
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

  it('非法语言码（含点）被跳过，不进选项、不污染 store、重复 init 不抛错', async () => {
    await initI18n('en', { 'foo.bar.json': JSON.stringify({ err: { session_not_found: 'BAD' } }) });
    expect(getLanguageOptions().map((o) => o.code)).not.toContain('foo.bar');
    expect(i18next.getResourceBundle('foo.bar', 'translation')).toBeUndefined();
    expect(i18next.getResourceBundle('foo', 'translation')).toBeUndefined();
    // i18next addResourceBundle 对含点的 lng 走路径重载，会把 store 写坏；修复后
    // removeResourceBundle 不应再因垃圾结构抛 TypeError
    await expect(initI18n('en', {})).resolves.not.toThrow();
    expect(i18next.t('err.session_not_found')).toBe(en.err.session_not_found);
  });

  it('大小写不规范的内置语言码归并到规范 code（zh-cn.json → zh-CN，不注册第二门语言）', async () => {
    await initI18n('en', { 'zh-cn.json': JSON.stringify({ err: { session_not_found: '小写归并' } }) });
    const codes = getLanguageOptions().map((o) => o.code);
    expect(codes).toContain('zh-CN');
    expect(codes.filter((c) => c.toLowerCase() === 'zh-cn')).toHaveLength(1);
    await i18next.changeLanguage('zh-CN');
    expect(i18next.t('err.session_not_found')).toBe('小写归并');
    await initI18n('en', {});
    expect(i18next.t('err.session_not_found', { lng: 'zh-CN' })).toBe(zhCN.err.session_not_found);
  });

  it('外部合法语言码大小写不规范时也归并（DE.json 与 de.json 合成一门语言）', async () => {
    await initI18n('en', {
      'DE.json': JSON.stringify({ $name: 'Deutsch', err: { session_not_found: 'DE' } }),
      'de.json': JSON.stringify({ err: { session_not_found: 'de' } }),
    });
    const codes = getLanguageOptions().map((o) => o.code);
    expect(codes.filter((c) => c.toLowerCase() === 'de')).toHaveLength(1);
    expect(i18next.t('err.session_not_found', { lng: 'de' })).toBe('de');
    await initI18n('en', {});
    expect(i18next.getResourceBundle('de', 'translation')).toBeUndefined();
  });

  // 整码校验回归：旧实现只取前两段，会把三段码静默改写成合法两段码而放行
  it('超值域的三段码 zh-hans-CN.json 被跳过，不静默改写成 zh-HANS', async () => {
    await initI18n('en', { 'zh-hans-CN.json': JSON.stringify({ err: { session_not_found: 'BAD' } }) });
    const codes = getLanguageOptions().map((o) => o.code);
    expect(codes).not.toContain('zh-HANS');
    expect(codes).not.toContain('zh-HANS-CN');
    expect(i18next.getResourceBundle('zh-HANS', 'translation')).toBeUndefined();
    await initI18n('en', {});
  });

  it('正常外部码 de.json 仍注册并可用', async () => {
    await initI18n('en', { 'de.json': JSON.stringify({ err: { session_not_found: 'Sitzung fehlt' } }) });
    expect(getLanguageOptions().map((o) => o.code)).toContain('de');
    await i18next.changeLanguage('de');
    expect(i18next.t('err.session_not_found')).toBe('Sitzung fehlt');
    await initI18n('en', {});
    expect(i18next.getResourceBundle('de', 'translation')).toBeUndefined();
  });
});
