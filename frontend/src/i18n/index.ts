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

// 外部语言码值域：与 Go 侧 config.ValidLanguage 的 languagePattern 同一模式
// （2-3 字母主语言段 + 可选 2-4 字母区域段），防止两侧值域漂移。
// 含点等非法字符的码会被 i18next 当路径分隔符，写坏资源库并让下一轮移除抛错。
const languagePattern = /^[A-Za-z]{2,3}(-[A-Za-z]{2,4})?$/;

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

/**
 * 同步初始化（测试 setup 与 initI18n 复用）：只注册内置资源，语言 en。
 * 调 init 后立即可用的前提是不 use backend / languageDetector——一旦挂了异步
 * 加载器，资源就绪会变成异步，setup 里的同步假设不再成立。
 */
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

/** 规范化新语言码写法：主语言段小写、区域段大写（BCP 47 惯例），保证按码查找可命中。 */
function normalizeCode(code: string): string {
  const [lang, region] = code.split('-');
  return region ? `${lang.toLowerCase()}-${region.toUpperCase()}` : lang.toLowerCase();
}

/**
 * 完整初始化：内置 + 外部语言包 + 配置语言。main.tsx render 前调用。
 * configured 传配置原值或 Go 侧 resolved 均可（仅 'system' 走 navigator 解析）；
 * main.tsx 侧统一传 Go 的 resolved，避免 navigator 与 OS 首选语言双解析分歧。
 */
export async function initI18n(configured: string, external: Record<string, string>): Promise<void> {
  initI18nBuiltin();
  // 重复调用时先清干净上一轮：移除外部新增语言的 bundle、内置语言整体还原，
  // 否则外部覆盖/新增 key 会跨调用粘连（深合并删不掉内置资源里没有的 key）。
  // 注意：下方 removes 与 adds 必须成对出现、其间不可插入提前返回，
  // 否则 i18next 的 options.ns 会被瞬时清空（removeNamespaces 副作用）导致后续翻译失效。
  for (const code of externalCodes) i18next.removeResourceBundle(code, 'translation');
  externalCodes = [];
  i18next.removeResourceBundle('en', 'translation');
  i18next.removeResourceBundle('zh-CN', 'translation');
  i18next.addResourceBundle('en', 'translation', en, true, false);
  i18next.addResourceBundle('zh-CN', 'translation', zhCN, true, false);
  options.length = 0;
  options.push({ code: 'en', name: 'English' }, { code: 'zh-CN', name: '简体中文' });
  for (const [file, content] of Object.entries(external)) {
    const raw = file.replace(/\.json$/i, '');
    if (!raw) {
      console.warn('[i18n] skip invalid language code: ', file);
      continue;
    }
    // 大小写归并与值域校验：与既有选项（含内置 en/zh-CN）按小写比对归并到规范写法，
    // 全新码先规范化再过值域正则
    const existing = options.find((o) => o.code.toLowerCase() === raw.toLowerCase());
    const code = existing ? existing.code : normalizeCode(raw);
    if (!existing && !languagePattern.test(code)) {
      console.warn('[i18n] skip invalid language code: ', file);
      continue;
    }
    let bundle: Bundle;
    try {
      bundle = JSON.parse(content) as Bundle;
    } catch {
      console.warn('[i18n] skip invalid JSON language pack: ', file);
      continue;
    }
    if (!bundle || typeof bundle !== 'object' || Array.isArray(bundle)) {
      console.warn('[i18n] skip non-object language pack: ', file);
      continue;
    }
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
  return [...options];
}
