// 后端 wire 文案翻译：`<key>` 或 `<key>|<arg1>|<arg2>`；非注册 key 原样返回，
// 保证 Go 侧未转换的动态串（如底层库英文错误）不被破坏。
import i18n from 'i18next';

// i18next 运行时支持 t(key, ['a','b']) 位置插值，但 TS 类型签名不认数组实参，这里放宽签名
const tPos = i18n.t.bind(i18n) as unknown as (key: string, args?: unknown) => string;

export function translateBackend(raw: string): string {
  if (!raw) return raw;
  const idx = raw.indexOf('|');
  const key = idx === -1 ? raw : raw.slice(0, idx);
  if (!i18n.exists(key)) return raw;
  if (idx === -1) return i18n.t(key) as string;
  const args = raw.slice(idx + 1).split('|');
  return tPos(key, args);
}
