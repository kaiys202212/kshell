// 相对时间格式化：刚刚 / N 分钟前 / N 小时前 / N 天前，更久直接给本地日期。
// 非组件 lib 直接用 i18next 单例（不能走 useTranslation），文案在 time.* 下。
// N 走 count 复数（en 有 _one/_other 区分，n=1 得 "1 minute ago"）；just_now 无数字。
import i18next from 'i18next';

export function formatRelativeTime(iso: string): string {
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return '';
  const min = Math.floor((Date.now() - t) / 60_000);
  if (min < 1) return i18next.t('time.just_now') as string;
  if (min < 60) return i18next.t('time.minutes_ago', { count: min }) as string;
  const hours = Math.floor(min / 60);
  if (hours < 24) return i18next.t('time.hours_ago', { count: hours }) as string;
  const days = Math.floor(hours / 24);
  if (days < 30) return i18next.t('time.days_ago', { count: days }) as string;
  return new Date(t).toLocaleDateString();
}

// 取多行文本的尾部 N 行（远端命令输出只展示最后 N 行，N 由调用方传）。
// 行数不足时原样返回。
export function tailLines(text: string, maxLines: number): string {
  const lines = text.split('\n');
  if (lines.length <= maxLines) return text;
  return lines.slice(-maxLines).join('\n');
}

// Go time.Duration 的 JSON 纳秒值格式化为人类可读时长：
// 毫秒级整数展示、秒级两位小数；低于 1ms 或非法值兜底 0ms（SSH 命令不会快到微秒级有意义）。
export function formatDuration(ns: number): string {
  if (!Number.isFinite(ns) || ns <= 0) return '0ms';
  const ms = ns / 1e6;
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}
