// Resolve durable session ID from an AgentHub message (for inactive quick-reply).
import type { HubMessage, Session } from './api';
import { sameWorkspacePath } from './workspacePath';

/** Hub Ts is UnixNano; tiny fixture values skip proximity matching. */
export function hubTsToMs(ts: number): number | null {
  if (!Number.isFinite(ts) || ts <= 0) return null;
  if (ts > 1e14) return Math.floor(ts / 1e6); // nano -> ms
  if (ts > 1e11) return ts; // already ms
  return null;
}

const PROXIMITY_WINDOW_MS = 45 * 60 * 1000;
const PROXIMITY_AMBIG_MS = 5 * 60 * 1000;

/**
 * Same tool+workspace: pick the unique nearest UpdatedAt within the window.
 * Returns null if outside window or top-two are too close (ambiguous).
 */
export function resolveSessionByProximity(
  sessions: Session[],
  tool: string | undefined,
  workspace: string | undefined,
  ts: number | undefined,
): string | null {
  const msgMs = hubTsToMs(ts ?? 0);
  if (msgMs == null || !tool || !workspace) return null;
  const ranked = sessions
    .filter((s) => s.ToolID === tool && sameWorkspacePath(s.Workspace, workspace))
    .map((s) => {
      const updated = Date.parse(s.UpdatedAt);
      const dist = Number.isFinite(updated) ? Math.abs(updated - msgMs) : Number.POSITIVE_INFINITY;
      return { id: s.ID, dist };
    })
    .filter((c) => c.dist <= PROXIMITY_WINDOW_MS)
    .sort((a, b) => a.dist - b.dist);
  if (ranked.length === 0) return null;
  if (ranked.length >= 2 && Math.abs(ranked[0].dist - ranked[1].dist) < PROXIMITY_AMBIG_MS) {
    return null;
  }
  return ranked[0].id;
}

/**
 * sessionId -> termKey session: -> exact title -> UpdatedAt proximity.
 */
export function resolveHubSessionIdFromSessions(
  m: Pick<HubMessage, 'sessionId' | 'termKey' | 'workspace' | 'tool' | 'sessionTitle' | 'ts'>,
  sessions: Session[],
): string | null {
  if (m.sessionId) return m.sessionId;
  if (m.termKey?.startsWith('session:')) return m.termKey.slice('session:'.length);
  if (m.workspace && m.tool && m.sessionTitle) {
    const hit = sessions.find(
      (s) =>
        s.ToolID === m.tool &&
        sameWorkspacePath(s.Workspace, m.workspace!) &&
        s.Title === m.sessionTitle,
    );
    if (hit?.ID) return hit.ID;
  }
  return resolveSessionByProximity(sessions, m.tool, m.workspace, m.ts);
}
