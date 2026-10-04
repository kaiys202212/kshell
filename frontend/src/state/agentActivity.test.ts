// frontend/src/state/agentActivity.test.ts
import { describe, expect, it } from 'vitest';
import { resolveAgentActivity } from './agentActivity';

describe('resolveAgentActivity', () => {
  it('权限优先于 running', () => {
    expect(
      resolveAgentActivity({ status: 'running', hasPermission: true, completed: true }),
    ).toBe('awaiting');
  });

  it('running / starting → running', () => {
    expect(resolveAgentActivity({ status: 'running', hasPermission: false, completed: false })).toBe('running');
    expect(resolveAgentActivity({ status: 'starting', hasPermission: false, completed: false })).toBe('running');
  });

  it('completed 在 ready 时生效；running 时被压制', () => {
    expect(resolveAgentActivity({ status: 'ready', hasPermission: false, completed: true })).toBe('completed');
    expect(resolveAgentActivity({ status: 'running', hasPermission: false, completed: true })).toBe('running');
  });

  it('exited + completed → completed；exited 无标记 → idle', () => {
    expect(resolveAgentActivity({ status: 'exited', hasPermission: false, completed: true })).toBe('completed');
    expect(resolveAgentActivity({ status: 'exited', hasPermission: false, completed: false })).toBe('idle');
  });

  it('ready 无标记 → idle', () => {
    expect(resolveAgentActivity({ status: 'ready', hasPermission: false, completed: false })).toBe('idle');
  });
});
