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
    expect(eventLabel('Stop')).toBe('task_done');
    expect(eventLabel('done')).toBe('task_done');
    expect(eventLabel('agent-turn-complete')).toBe('task_done');
    expect(eventLabel('whatever')).toBe('task_done');
  });

  it('出错语义：error 不落入完成文案', () => {
    expect(eventLabel('error')).toBe('task_error');
  });

  it('等待确认语义：Notification / attention', () => {
    expect(eventLabel('Notification')).toBe('waiting_confirm');
    expect(eventLabel('attention')).toBe('waiting_confirm');
  });
});
