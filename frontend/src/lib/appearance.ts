// 颜色模式相关的前端工具：类型与 xterm 主题映射。
export type AppearanceMode = 'system' | 'light' | 'dark';
export type ResolvedTheme = 'light' | 'dark';

export interface AppearanceInfo {
  mode: AppearanceMode;
  resolved: ResolvedTheme;
}

// terminalTheme 按解析后的明暗返回 xterm 配色。
export function terminalTheme(theme: ResolvedTheme) {
  return theme === 'dark'
    ? { background: '#0d1117', foreground: '#e6edf3' }
    : { background: '#ffffff', foreground: '#1f2328' };
}
