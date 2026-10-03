// 工具徽标映射：ToolID → 展示名、旧配色类（兼容调用点）与固定色相（C 视觉用色点）。
export interface ToolBadge {
  label: string;
  className: string;
  color: string;
}

// 未识别工具的中性色相（跟随 muted-foreground，深浅自动适配）
const OTHER_COLOR = 'var(--muted-foreground)';

const TOOL_BADGES: Record<string, ToolBadge> = {
  codebuddy: { label: 'CodeBuddy', className: 'tool-badge--codebuddy', color: '#8957e5' },
  codex: { label: 'Codex', className: 'tool-badge--codex', color: '#10a37f' },
  claude: { label: 'Claude', className: 'tool-badge--claude', color: '#d97757' },
  gemini: { label: 'Gemini', className: 'tool-badge--gemini', color: '#4285f4' },
  opencode: { label: 'OpenCode', className: 'tool-badge--opencode', color: '#14b8a6' },
};

export function badgeFor(toolID: string): ToolBadge {
  return (
    TOOL_BADGES[toolID.toLowerCase()] ?? {
      label: toolID || '未知',
      className: 'tool-badge--other',
      color: OTHER_COLOR,
    }
  );
}
