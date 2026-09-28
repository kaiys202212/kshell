// 工具徽标映射测试。
import { describe, expect, it } from 'vitest';
import { badgeFor } from './toolBadge';

describe('badgeFor', () => {
  it.each([
    ['codebuddy', 'CodeBuddy', 'tool-badge--codebuddy'],
    ['codex', 'Codex', 'tool-badge--codex'],
    ['claude', 'Claude', 'tool-badge--claude'],
    ['gemini', 'Gemini', 'tool-badge--gemini'],
  ])('已知工具 %s 映射展示名与配色', (toolID, label, className) => {
    expect(badgeFor(toolID)).toEqual({ label, className });
  });

  it('大小写不敏感', () => {
    expect(badgeFor('Claude').label).toBe('Claude');
  });

  it('未知工具给中性徽标，展示原始 ToolID', () => {
    expect(badgeFor('aider')).toEqual({
      label: 'aider',
      className: 'tool-badge--other',
    });
  });

  it('空 ToolID 显示「未知」', () => {
    expect(badgeFor('').label).toBe('未知');
  });
});
