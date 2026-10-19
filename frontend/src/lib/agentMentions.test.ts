import { describe, expect, it } from 'vitest';
import { buildMentionOptions, displayMentionTitles, resolveMentionTarget, type MentionAgent } from './agentMentions';

const agents: MentionAgent[] = [
  { id: 'c1', toolId: 'claude', title: '重构认证', workspace: 'D:\\ws\\a' },
  { id: 'c2', toolId: 'claude', title: '写测试', workspace: 'D:\\ws\\b' },
  { id: 'c3', toolId: 'cursor', title: '调样式', workspace: 'D:\\ws\\a' },
];

describe('buildMentionOptions', () => {
  it('路由标签用稳定 id；写入标签用会话标题', () => {
    const opts = buildMentionOptions(agents);
    expect(opts.map((o) => o.label)).toEqual(['Claude Code#c1', 'Claude Code#c2', 'Cursor#c3']);
    expect(opts.map((o) => o.insert)).toEqual(['Claude Code#重构认证', 'Claude Code#写测试', 'Cursor#调样式']);
    expect(opts.map((o) => o.id)).toEqual(['c1', 'c2', 'c3']);
  });

  it('同工具同标题时写入标签退回 id，标题相同项目不同则带上项目名', () => {
    const dup: MentionAgent[] = [
      { id: 'c1', toolId: 'claude', title: '同名', workspace: 'D:\\ws\\a' },
      { id: 'c2', toolId: 'claude', title: '同名', workspace: 'D:\\ws\\b' },
    ];
    const opts = buildMentionOptions(dup);
    expect(opts.map((o) => o.insert)).toEqual(['Claude Code#同名 · a', 'Claude Code#同名 · b']);
    const sameProject: MentionAgent[] = [
      { id: 'c1', toolId: 'cursor', title: '同名', workspace: 'D:\\ws\\a' },
      { id: 'c2', toolId: 'cursor', title: '同名', workspace: 'D:\\ws\\a' },
    ];
    expect(buildMentionOptions(sameProject).map((o) => o.insert)).toEqual(['Cursor#c1', 'Cursor#c2']);
  });

  it('没有标题时写入标签用项目基名', () => {
    const opts = buildMentionOptions([{ id: 'cdm8_1_2', toolId: 'cursor', title: '', workspace: 'D:\\ws\\kshell' }]);
    expect(opts[0].insert).toBe('Cursor#kshell');
    expect(opts[0].label).toBe('Cursor#cdm8_1_2');
  });

  it('标签与列表顺序无关（打乱输入后 id↔label 映射不变）', () => {
    const shuffled = [agents[2], agents[0], agents[1]];
    const map = (list: MentionAgent[]) =>
      Object.fromEntries(buildMentionOptions(list).map((o) => [o.id, o.label]));

    expect(map(shuffled)).toEqual(map(agents));
  });

  it('detail 为「项目基名 · 会话标题」，workspace 缺失时只留标题', () => {
    const opts = buildMentionOptions(agents);
    expect(opts[0].detail).toBe('a · 重构认证');
    expect(opts[2].detail).toBe('a · 调样式');
    const noWs = buildMentionOptions([{ id: 'c9', toolId: 'codex', title: '只有标题', workspace: '' }]);
    expect(noWs[0].detail).toBe('只有标题');
    const neither = buildMentionOptions([{ id: 'c8', toolId: 'codex', title: '', workspace: '' }]);
    expect(neither[0].detail).toBeUndefined();
  });
});

describe('displayMentionTitles', () => {
  it('把 @提及#会话ID 展示为 #会话标题', () => {
    expect(displayMentionTitles('@Claude Code#c1 帮我看下', agents)).toBe('@Claude Code#重构认证 帮我看下');
  });

  it('多处提及与多次出现都替换；未知 id 保留原文', () => {
    const text = '@Cursor#c3 看 A\n@Cursor#c3 看 B @Claude Code#c999 未知';
    expect(displayMentionTitles(text, agents)).toBe('@Cursor#调样式 看 A\n@Cursor#调样式 看 B @Claude Code#c999 未知');
  });

  it('标题为空的会话保留原文', () => {
    const noTitle: MentionAgent[] = [{ id: 'c1', toolId: 'claude', title: '', workspace: '' }];
    expect(displayMentionTitles('@Claude Code#c1 hi', noTitle)).toBe('@Claude Code#c1 hi');
  });

  it('id 前缀重叠时长标签优先，不被短 id 截断误替换', () => {
    const overlap: MentionAgent[] = [
      { id: 'c1x', toolId: 'claude', title: '长会话', workspace: '' },
      { id: 'c1', toolId: 'claude', title: '短会话', workspace: '' },
    ];
    expect(displayMentionTitles('@Claude Code#c1x hi', overlap)).toBe('@Claude Code#长会话 hi');
    expect(displayMentionTitles('@Claude Code#c1 hi', overlap)).toBe('@Claude Code#短会话 hi');
  });
});

describe('resolveMentionTarget', () => {
  it('命中唯一 @ 时路由到该 agent（id 与标题两种写法）', () => {
    const opts = buildMentionOptions(agents);
    expect(resolveMentionTarget('帮我看下 @Cursor#c3', opts, 'c1')).toBe('c3');
    expect(resolveMentionTarget('帮我看下 @Cursor#调样式', opts, 'c1')).toBe('c3');
  });

  it('多个 @ 以最后一个为准', () => {
    const opts = buildMentionOptions(agents);
    expect(resolveMentionTarget('@Cursor#c3 然后 @Claude Code#c2', opts, 'c1')).toBe('c2');
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

  it('文本含邮箱（@ 不在词首）不算无效提及，回落 lastAgentId', () => {
    const opts = buildMentionOptions(agents);
    expect(resolveMentionTarget('a@b.com 帮我看看', opts, 'c1')).toBe('c1');
  });

  it('邮箱与真提及混排时仍以真提及为准', () => {
    const opts = buildMentionOptions(agents);
    expect(resolveMentionTarget('a@b.com 然后 @Cursor#c3', opts, 'c1')).toBe('c3');
  });

  it('无 lastAgentId 且仅邮箱时返回 null', () => {
    const opts = buildMentionOptions(agents);
    expect(resolveMentionTarget('a@b.com', opts, null)).toBeNull();
  });
});
