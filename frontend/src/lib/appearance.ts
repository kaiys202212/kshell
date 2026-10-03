// 颜色模式相关的前端工具：类型与 xterm 主题映射。
export type AppearanceMode = 'system' | 'light' | 'dark';
export type ResolvedTheme = 'light' | 'dark';

export interface AppearanceInfo {
  mode: AppearanceMode;
  resolved: ResolvedTheme;
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
