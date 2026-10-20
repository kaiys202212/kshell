import { describe, expect, it } from 'vitest';
import type { Session } from './api';
import {
  hubTsToMs,
  resolveHubSessionIdFromSessions,
  resolveSessionByProximity,
} from './hubSessionResolve';

function sess(partial: Partial<Session> & Pick<Session, 'ID'>): Session {
  return {
    ToolID: 'cursor',
    Workspace: 'd:\\ws',
    Title: 't',
    CreatedAt: '2026-10-21T00:00:00+08:00',
    UpdatedAt: '2026-10-21T00:43:00+08:00',
    Messages: 1,
    Path: '',
    ...partial,
  };
}

describe('hubTsToMs', () => {
  it('converts UnixNano to ms', () => {
    expect(hubTsToMs(1792514580379303600)).toBe(1792514580379);
  });
  it('returns null for tiny fixture ts', () => {
    expect(hubTsToMs(10)).toBeNull();
  });
});

describe('resolveSessionByProximity', () => {
  const msgMs = Date.parse('2026-10-21T00:43:00+08:00');
  const tsNano = msgMs * 1e6;

  it('hits unique nearby session', () => {
    const sessions = [
      sess({
        ID: 'near',
        UpdatedAt: '2026-10-21T00:44:00+08:00',
        Title: 'Activate Session Explain',
      }),
      sess({
        ID: 'far',
        UpdatedAt: '2026-10-20T12:00:00+08:00',
      }),
    ];
    expect(resolveSessionByProximity(sessions, 'cursor', 'd:\\ws', tsNano)).toBe('near');
  });

  it('returns null when top two are ambiguous', () => {
    const sessions = [
      sess({ ID: 'a', UpdatedAt: '2026-10-21T00:43:30+08:00' }),
      sess({ ID: 'b', UpdatedAt: '2026-10-21T00:44:00+08:00' }),
    ];
    expect(resolveSessionByProximity(sessions, 'cursor', 'd:\\ws', tsNano)).toBeNull();
  });

  it('ignores mismatched tool', () => {
    const sessions = [sess({ ID: 'x', ToolID: 'claude' })];
    expect(resolveSessionByProximity(sessions, 'cursor', 'd:\\ws', tsNano)).toBeNull();
  });
});

describe('resolveHubSessionIdFromSessions', () => {
  it('prefers sessionId / session termKey / exact title', () => {
    expect(resolveHubSessionIdFromSessions({ sessionId: 's1', ts: 1 }, [])).toBe('s1');
    expect(resolveHubSessionIdFromSessions({ termKey: 'session:s2', ts: 1 }, [])).toBe('s2');
    const sessions = [sess({ ID: 's3', Title: 'exact-title' })];
    expect(
      resolveHubSessionIdFromSessions(
        { workspace: 'd:\\ws', tool: 'cursor', sessionTitle: 'exact-title', ts: 1 },
        sessions,
      ),
    ).toBe('s3');
  });

  it('falls back to proximity when title mismatches', () => {
    const msgMs = Date.parse('2026-10-21T00:43:00+08:00');
    const sessions = [
      sess({
        ID: 'acp-1',
        Title: 'Activate Session Explain',
        UpdatedAt: '2026-10-21T00:44:57+08:00',
      }),
    ];
    expect(
      resolveHubSessionIdFromSessions(
        {
          tool: 'cursor',
          workspace: 'd:\\ws',
          sessionTitle: 'inactive-session-prompt',
          termKey: 'new:1',
          ts: msgMs * 1e6,
        },
        sessions,
      ),
    ).toBe('acp-1');
  });
});
