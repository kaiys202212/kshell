// 工具徽标映射测试。
import { describe, expect, it } from 'vitest';
import { badgeFor } from './toolBadge';

describe('badgeFor', () => {
  it.each([
    ['codebuddy', 'CodeBuddy', 'tool-badge--codebuddy', '#8957e5'],
    ['codex', 'Codex', 'tool-badge--codex', '#10a37f'],
    ['claude', 'Claude', 'tool-badge--claude', '#d97757'],
    ['gemini', 'Gemini', 'tool-badge--gemini', '#4285f4'],
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
    expect(badgeFor('').label).toBe('未知');
  });
});
