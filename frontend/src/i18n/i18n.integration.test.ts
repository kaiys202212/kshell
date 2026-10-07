// Task 14 专项集成：语言切换、system 解析、外部语言包覆盖/新增/清除、非法包跳过。
// 断言直连全局 i18next 实例，覆盖 initI18n 的端到端行为（而非仅纯函数）。
import { afterEach, describe, expect, it, vi } from 'vitest';
import i18next from 'i18next';
import en from '../locales/en.json';
import zhCN from '../locales/zh-CN.json';
import { getLanguageOptions, initI18n } from './index';

// 每个用例后还原被 stub 的全局，并回到英文默认，避免全局 i18next 状态串到下一用例
afterEach(async () => {
  vi.unstubAllGlobals();
  await initI18n('en', {});
});

describe('i18n 集成', () => {
  it('默认 en：内置英文文案', async () => {
    await initI18n('en', {});
    expect(i18next.t('err.session_not_found')).toBe(en.err.session_not_found);
  });

  it('切 zh-CN：内置中文文案', async () => {
    await initI18n('zh-CN', {});
    expect(i18next.t('err.session_not_found')).toBe(zhCN.err.session_not_found);
  });

  it('system：jsdom navigator(en-US) 解析为 en', async () => {
    await initI18n('system', {});
    expect(i18next.t('err.session_not_found')).toBe(en.err.session_not_found);
  });

  it('system：navigator.language=zh-CN 时解析为 zh-CN', async () => {
    vi.stubGlobal('navigator', { language: 'zh-CN' });
    await initI18n('system', {});
    expect(i18next.t('err.session_not_found')).toBe(zhCN.err.session_not_found);
  });

  it('外部包覆盖同语言同 key', async () => {
    await initI18n('zh-CN', {
      'zh-CN.json': JSON.stringify({ err: { session_not_found: '外部覆盖' } }),
    });
    expect(i18next.t('err.session_not_found')).toBe('外部覆盖');
  });

  it('外部新增语言：注册为选项且 configured 可切换到该语言', async () => {
    await initI18n('ja', {
      'ja.json': JSON.stringify({
        err: { session_not_found: 'セッションが見つかりません' },
        $name: '日本語',
      }),
    });
    const opt = getLanguageOptions().find((o) => o.code === 'ja');
    expect(opt?.name).toBe('日本語');
    expect(i18next.t('err.session_not_found')).toBe('セッションが見つかりません');
  });

  it('非法 JSON 外部包被跳过且不抛错', async () => {
    await expect(initI18n('en', { 'xx.json': 'not json' })).resolves.not.toThrow();
    expect(getLanguageOptions().map((o) => o.code)).not.toContain('xx');
    expect(i18next.getResourceBundle('xx', 'translation')).toBeUndefined();
  });

  it('反复 initI18n 无残留：新一轮清除上一轮注册的 ja key 与选项', async () => {
    await initI18n('ja', {
      'ja.json': JSON.stringify({ err: { session_not_found: 'セッション' }, $name: '日本語' }),
    });
    expect(getLanguageOptions().map((o) => o.code)).toContain('ja');

    await initI18n('en', {});
    expect(getLanguageOptions().map((o) => o.code)).not.toContain('ja');
    expect(i18next.getResourceBundle('ja', 'translation')).toBeUndefined();
  });
});
