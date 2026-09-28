// 工具徽标映射：ToolID → 展示名与配色；未识别的工具给中性色。
export interface ToolBadge {
  label: string;
  className: string;
}

const TOOL_BADGES: Record<string, ToolBadge> = {
  codebuddy: { label: 'CodeBuddy', className: 'tool-badge--codebuddy' },
  codex: { label: 'Codex', className: 'tool-badge--codex' },
  claude: { label: 'Claude', className: 'tool-badge--claude' },
  gemini: { label: 'Gemini', className: 'tool-badge--gemini' },
};

export function badgeFor(toolID: string): ToolBadge {
  return (
    TOOL_BADGES[toolID.toLowerCase()] ?? {
      label: toolID || '未知',
      className: 'tool-badge--other',
    }
  );
}
