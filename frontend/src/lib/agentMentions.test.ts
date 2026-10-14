import { describe, expect, it } from 'vitest';
import { buildMentionOptions, resolveMentionTarget, type MentionAgent } from './agentMentions';

const agents: MentionAgent[] = [
  { id: 'c1', toolId: 'claude', title: '重构认证', workspace: 'D:\\ws\\a' },
  { id: 'c2', toolId: 'claude', title: '写测试', workspace: 'D:\\ws\\b' },
  { id: 'c3', toolId: 'cursor', title: '调样式', workspace: 'D:\\ws\\a' },
];

describe('buildMentionOptions', () => {
  it('展示名重复时追加序号保证唯一', () => {
    const opts = buildMentionOptions(agents);
    expect(opts.map((o) => o.label)).toEqual(['Claude Code#1', 'Claude Code#2', 'Cursor#1']);
    expect(opts.map((o) => o.id)).toEqual(['c1', 'c2', 'c3']);
  });
});

describe('resolveMentionTarget', () => {
  it('命中唯一 @ 时路由到该 agent', () => {
    const opts = buildMentionOptions(agents);
    expect(resolveMentionTarget('帮我看下 @Cursor#1', opts, 'c1')).toBe('c3');
  });

  it('多个 @ 以最后一个为准', () => {
    const opts = buildMentionOptions(agents);
    expect(resolveMentionTarget('@Cursor#1 然后 @Claude Code#2', opts, 'c1')).toBe('c2');
  });

  it('未 @ 时回落到最后交互的 agent', () => {
    const opts = buildMentionOptions(agents);
    expect(resolveMentionTarget('随便看看', opts, 'c1')).toBe('c1');
  });

  it('未 @ 且无最后交互时返回 null', () => {
    const opts = buildMentionOptions(agents);
    expect(resolveMentionTarget('随便看看', opts, null)).toBeNull();
  });

  it('@ 的 agent 已不在列表时返回 null（不误投）', () => {
    const opts = buildMentionOptions(agents);
    expect(resolveMentionTarget('@不存在的东西', opts, 'c1')).toBeNull();
  });
});
