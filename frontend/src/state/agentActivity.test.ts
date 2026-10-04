import { describe, expect, it } from 'vitest';
import { resolveAgentActivity } from './agentActivity';

describe('resolveAgentActivity', () => {
  it('权限优先于 running / waiting', () => {
    expect(
      resolveAgentActivity({
        status: 'running',
        hasPermission: true,
        kind: 'chat',
      }),
    ).toBe('awaiting');
    expect(
      resolveAgentActivity({
        status: 'ready',
        hasPermission: true,
        kind: 'chat',
      }),
    ).toBe('awaiting');
  });

  it('聊天 running/starting → running；ready → waiting', () => {
    expect(
      resolveAgentActivity({ status: 'running', hasPermission: false, kind: 'chat' }),
    ).toBe('running');
    expect(
      resolveAgentActivity({ status: 'starting', hasPermission: false, kind: 'chat' }),
    ).toBe('running');
    expect(
      resolveAgentActivity({ status: 'ready', hasPermission: false, kind: 'chat' }),
    ).toBe('waiting');
  });

  it('聊天 exited → idle', () => {
    expect(
      resolveAgentActivity({ status: 'exited', hasPermission: false, kind: 'chat' }),
    ).toBe('idle');
  });

  it('终端 starting 或 running+busy → running；running 非 busy → waiting', () => {
    expect(
      resolveAgentActivity({
        status: 'starting',
        hasPermission: false,
        kind: 'terminal',
        busy: false,
      }),
    ).toBe('running');
    expect(
      resolveAgentActivity({
        status: 'running',
        hasPermission: false,
        kind: 'terminal',
        busy: true,
      }),
    ).toBe('running');
    expect(
      resolveAgentActivity({
        status: 'running',
        hasPermission: false,
        kind: 'terminal',
        busy: false,
      }),
    ).toBe('waiting');
  });

  it('终端 exited → idle', () => {
    expect(
      resolveAgentActivity({
        status: 'exited',
        hasPermission: false,
        kind: 'terminal',
        busy: true,
      }),
    ).toBe('idle');
  });
});
