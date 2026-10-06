// 工具显示名与事件语义映射：气泡标题文案的来源（agenthook payload → 中文文案）。
import { describe, expect, it } from 'vitest';
import { eventLabel, toolDisplayName } from './agentNotify';

describe('toolDisplayName', () => {
  it('已知工具映射成展示名', () => {
    expect(toolDisplayName('claude')).toBe('Claude Code');
    expect(toolDisplayName('codebuddy')).toBe('CodeBuddy');
    expect(toolDisplayName('codex')).toBe('Codex');
    expect(toolDisplayName('opencode')).toBe('OpenCode');
    expect(toolDisplayName('gemini')).toBe('Gemini CLI');
    expect(toolDisplayName('cursor')).toBe('Cursor');
  });

  it('未知工具显示原文，空工具回退 Agent', () => {
    expect(toolDisplayName('mystery')).toBe('mystery');
    expect(toolDisplayName('')).toBe('Agent');
  });
});

describe('eventLabel', () => {
  it('完成语义：Stop / done / agent-turn-complete / 未知事件都算任务完成', () => {
    expect(eventLabel('Stop')).toBe('任务完成');
    expect(eventLabel('done')).toBe('任务完成');
    expect(eventLabel('agent-turn-complete')).toBe('任务完成');
    expect(eventLabel('whatever')).toBe('任务完成');
  });

  it('等待确认语义：Notification / attention', () => {
    expect(eventLabel('Notification')).toBe('等待确认');
    expect(eventLabel('attention')).toBe('等待确认');
  });
});
