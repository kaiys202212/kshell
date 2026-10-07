// 工具徽标映射测试。
import { describe, expect, it } from 'vitest';
import { tt } from '../test/i18n';
import { badgeFor } from './toolBadge';

describe('badgeFor', () => {
  it.each([
    ['codebuddy', 'CodeBuddy', 'tool-badge--codebuddy', 'var(--tool-codebuddy)'],
    ['codex', 'Codex', 'tool-badge--codex', 'var(--tool-codex)'],
    ['claude', 'Claude', 'tool-badge--claude', 'var(--tool-claude)'],
    ['gemini', 'Gemini', 'tool-badge--gemini', 'var(--tool-gemini)'],
    ['opencode', 'OpenCode', 'tool-badge--opencode', 'var(--tool-opencode)'],
  ])('已知工具 %s 映射展示名/配色/色相', (toolID, label, className, color) => {
    expect(badgeFor(toolID)).toEqual({ label, className, color });
  });

  it('大小写不敏感', () => {
    expect(badgeFor('Claude').label).toBe('Claude');
  });

  it('未知工具给中性色相与中性徽标，展示原始 ToolID', () => {
    expect(badgeFor('aider')).toEqual({
      label: 'aider',
      className: 'tool-badge--other',
      color: 'var(--muted-foreground)',
    });
  });

  it('空 ToolID 显示「未知」', () => {
    expect(badgeFor('').label).toBe(tt('ui.tool.unknown'));
  });
});
