// i18n 单一入口：内置资源同步注册，外部 ~/.kshell/locales/*.json 启动时合并。
// 默认语言 en；system 由 navigator.language 解析（WebView2 与 OS 用户区域一致）。
import i18next from 'i18next';
import { initReactI18next } from 'react-i18next';
import en from '../locales/en.json';
import zhCN from '../locales/zh-CN.json';

export interface LanguageOption {
  code: string;
  name?: string; // 外部包 $name 提供时显示
}

type Bundle = Record<string, unknown>;

const options: LanguageOption[] = [
  { code: 'en', name: 'English' },
  { code: 'zh-CN', name: '简体中文' },
];

// 上一轮 initI18n 注入过的外部语言码，用于下一轮重置
let externalCodes: string[] = [];

/** 深合并：source 覆盖 target 同 key，对象递归，其余整体替换。 */
export function mergeExternalBundle(source: Bundle, target: Bundle): Bundle {
  const out: Bundle = { ...target };
  for (const [k, v] of Object.entries(source)) {
    const t = out[k];
    if (v && typeof v === 'object' && !Array.isArray(v) && t && typeof t === 'object' && !Array.isArray(t)) {
      out[k] = mergeExternalBundle(v as Bundle, t as Bundle);
    } else {
      out[k] = v;
    }
  }
  return out;
}

export function resolveLanguage(configured: string): string {
  if (configured !== 'system') return configured;
  const nav = typeof navigator !== 'undefined' ? navigator.language || 'en' : 'en';
  return nav.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en';
}

/** 同步初始化（测试 setup 与 initI18n 复用）：只注册内置资源，语言 en。 */
export function initI18nBuiltin(lng = 'en'): void {
  if (i18next.isInitialized) return;
  void i18next.use(initReactI18next).init({
    lng,
    fallbackLng: 'en',
    resources: { en: { translation: en }, 'zh-CN': { translation: zhCN } },
    interpolation: { escapeValue: false },
    returnNull: false,
  });
}

/** 完整初始化：内置 + 外部语言包 + 配置语言。main.tsx render 前调用。 */
export async function initI18n(configured: string, external: Record<string, string>): Promise<void> {
  initI18nBuiltin();
  // 重复调用时先清干净上一轮：移除外部新增语言的 bundle、内置语言整体还原，
  // 否则外部覆盖/新增 key 会跨调用粘连（深合并删不掉内置资源里没有的 key）
  for (const code of externalCodes) i18next.removeResourceBundle(code, 'translation');
  externalCodes = [];
  i18next.removeResourceBundle('en', 'translation');
  i18next.removeResourceBundle('zh-CN', 'translation');
  i18next.addResourceBundle('en', 'translation', en, true, false);
  i18next.addResourceBundle('zh-CN', 'translation', zhCN, true, false);
  options.length = 0;
  options.push({ code: 'en', name: 'English' }, { code: 'zh-CN', name: '简体中文' });
  for (const [file, content] of Object.entries(external)) {
    const code = file.replace(/\.json$/i, '');
    if (!code) continue;
    let bundle: Bundle;
    try {
      bundle = JSON.parse(content) as Bundle;
    } catch {
      continue; // 非法 JSON 跳过
    }
    if (!bundle || typeof bundle !== 'object' || Array.isArray(bundle)) continue; // 非对象包跳过
    const { $name, ...strings } = bundle as Bundle & { $name?: string };
    const base = (i18next.getResourceBundle(code, 'translation') as Bundle | undefined) ?? {};
    i18next.addResourceBundle(code, 'translation', mergeExternalBundle(strings, base), true, true);
    if (!externalCodes.includes(code)) externalCodes.push(code);
    if (!options.some((o) => o.code === code)) {
      options.push({ code, name: typeof $name === 'string' ? $name : undefined });
    }
  }
  await i18next.changeLanguage(resolveLanguage(configured));
}

export function getLanguageOptions(): LanguageOption[] {
  return options;
}
