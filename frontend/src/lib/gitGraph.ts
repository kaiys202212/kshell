// 非组件 lib 直接用 i18next 单例（不能走 useTranslation），文案在 ui.git.* / ui.* 下。
import i18next from 'i18next';

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
    const next = lanes.map((h) => (h === c.hash ? null : h));
    if (parents.length > 0 && next.indexOf(parents[0]) < 0) {
      next[col] = parents[0];
    }
    for (let p = 1; p < parents.length; p++) {
      if (next.indexOf(parents[p]) >= 0) continue;
      let pc = firstNull(next);
      if (pc < 0) {
        pc = next.length;
        next.push(parents[p]);
      } else {
        next[pc] = parents[p];
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

export function formatStat(st: { files: number; insertions: number; deletions: number }): string {
  if (st.files <= 0 && st.insertions <= 0 && st.deletions <= 0) return '';
  return i18next.t('ui.git.status_summary', {
    0: st.files,
    1: st.insertions,
    2: st.deletions,
  }) as string;
}

export function formatAbsoluteTime(iso: string, locale: string = i18next.language): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  try {
    return new Intl.DateTimeFormat(locale || undefined, {
      year: 'numeric',
      month: 'long',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    }).format(d);
  } catch {
    // 非法 locale（外部语言码值域允许 3 字母区域，如 en-ABC）会让 Intl 抛 RangeError；
    // 该函数在 render 期被 GitLogGraph 调用且无 ErrorBoundary，必须回退到稳定格式避免白屏。
    return d.toISOString().slice(0, 16).replace('T', ' ');
  }
}
