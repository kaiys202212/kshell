// 测试专用：按 key 解析 en 资源文案，测试断言不硬编码语言字面量
import en from '../locales/en.json';

export function tt(key: string): string {
  const v = key
    .split('.')
    .reduce<unknown>(
      (o, k) => (o && typeof o === 'object' ? (o as Record<string, unknown>)[k] : undefined),
      en,
    );
  return typeof v === 'string' ? v : key;
}
