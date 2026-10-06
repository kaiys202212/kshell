export type GraphCommit = {
  hash: string;
  parents: string[];
  subject: string;
  author: string;
  date: string;
  decorations: string[];
};

export type GraphEdge = {
  from: number;
  to: number;
  merge: boolean;
};

export type GraphRow = {
  commit: GraphCommit;
  column: number;
  laneCount: number;
  edges: GraphEdge[];
};

/** git log 新→旧，分配 VS Code Graph 风格的 lane。 */
export function layoutGitGraph(commits: GraphCommit[]): GraphRow[] {
  const rows: GraphRow[] = [];
  let lanes: (string | null)[] = [];

  for (const c of commits) {
    let col = lanes.indexOf(c.hash);
    if (col < 0) {
      col = firstNull(lanes);
      if (col < 0) {
        col = lanes.length;
        lanes.push(c.hash);
      } else {
        lanes[col] = c.hash;
      }
    }

    const parents = c.parents ?? [];
    const next = [...lanes];
    if (parents.length === 0) {
      next[col] = null;
    } else {
      next[col] = parents[0];
      for (let p = 1; p < parents.length; p++) {
        let pc = next.indexOf(parents[p]);
        if (pc < 0) {
          pc = firstNull(next);
          if (pc < 0) {
            pc = next.length;
            next.push(parents[p]);
          } else {
            next[pc] = parents[p];
          }
        }
      }
    }

    const edges: GraphEdge[] = [];
    parents.forEach((p, i) => {
      const to = next.indexOf(p);
      if (to >= 0) edges.push({ from: col, to, merge: i > 0 });
    });
    lanes.forEach((h, L) => {
      if (L === col || !h) return;
      const to = next.indexOf(h);
      if (to >= 0) edges.push({ from: L, to, merge: false });
    });

    rows.push({
      commit: c,
      column: col,
      laneCount: Math.max(lanes.length, next.length, 1),
      edges,
    });
    lanes = next;
  }
  return rows;
}

function firstNull(lanes: (string | null)[]): number {
  return lanes.findIndex((x) => x === null);
}

export function shortHash(hash: string): string {
  return hash.slice(0, 7);
}

export function relativeTime(iso: string, now = Date.now()): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  const sec = Math.round((now - t) / 1000);
  if (sec < 60) return '刚刚';
  if (sec < 3600) return `${Math.floor(sec / 60)} 分钟前`;
  if (sec < 86400) return `${Math.floor(sec / 3600)} 小时前`;
  if (sec < 86400 * 30) return `${Math.floor(sec / 86400)} 天前`;
  return iso.slice(0, 10);
}
