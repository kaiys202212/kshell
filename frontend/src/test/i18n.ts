// 测试专用：按 key 解析 en 资源文案，测试断言不硬编码语言字面量
import en from '../locales/en.json';

export function tt(key: string): string {
  const v = key
    .split('.')
    .reduce<unknown>(
      (o, k) => (o && typeof o === 'object' ? (o as Record<string, unknown>)[k] : undefined),
      en,
    );
  // 缺 key 直接抛错：原样返回会让断言退化成「期望值 == key 本身」的假阳性
  if (typeof v !== 'string') throw new Error(`tt: 缺少 key ${key}`);
  return v;
}
