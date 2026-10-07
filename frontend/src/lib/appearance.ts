// 颜色模式相关的前端工具：类型与 xterm 主题映射。
export type AppearanceMode = 'system' | 'light' | 'dark';
export type ResolvedTheme = 'light' | 'dark';

export interface AppearanceInfo {
  mode: AppearanceMode;
  resolved: ResolvedTheme;
  fontSize: number;
  showWhitespace?: boolean;
}

export const DEFAULT_UI_FONT_SIZE = 13;
export const MIN_UI_FONT_SIZE = 10;
export const MAX_UI_FONT_SIZE = 20;

export function clampUiFontSize(n: number): number {
  if (!Number.isFinite(n) || n <= 0) return DEFAULT_UI_FONT_SIZE;
  const rounded = Math.round(n);
  if (rounded < MIN_UI_FONT_SIZE) return MIN_UI_FONT_SIZE;
  if (rounded > MAX_UI_FONT_SIZE) return MAX_UI_FONT_SIZE;
  return rounded;
}

// applyUiFontSize 写 CSS 变量与 html rem 基准，并记住本地值避免启动闪默认字号。
export function applyUiFontSize(px: number): void {
  const n = clampUiFontSize(px);
  const root = document.documentElement;
  root.style.setProperty('--app-font-size', `${n}px`);
  root.style.fontSize = `${(16 * n) / DEFAULT_UI_FONT_SIZE}px`;
  try {
    localStorage.setItem('kshell-font-size', String(n));
  } catch {
    /* 忽略持久化失败 */
  }
}

// 读取根元素上的 CSS 变量；非浏览器环境或读不到时返回 null。
function cssVar(name: string): string | null {
  try {
    return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || null;
  } catch {
    return null;
  }
}

// terminalTheme 按解析后的明暗返回 xterm 配色：优先读 style.css token（改 token 自动同步），读不到用回退常量。
export function terminalTheme(
  theme: ResolvedTheme,
  resolve: (name: string) => string | null = cssVar,
) {
  const bg = resolve('--card');
  const fg = resolve('--foreground');
  if (bg && fg) return { background: bg, foreground: fg };
  return theme === 'dark'
    ? { background: '#131b18', foreground: '#dce8e4' }
    : { background: '#ffffff', foreground: '#1c2a27' };
}
