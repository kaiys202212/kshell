import { useEffect, useState } from 'react';
import {
  formatAbsoluteTime,
  formatStat,
  layoutGitGraph,
  relativeTime,
  shortHash,
  type GraphCommit,
} from '../lib/gitGraph';
import { cn } from '../lib/cn';

const ROW_H = 22;
const COL_W = 14;
const COLORS = ['#3794ff', '#b180d7', '#d18616', '#3fa266', '#cc6633', '#808080'];

export type CommitStatView = {
  Files: number;
  Insertions: number;
  Deletions: number;
};

export function GitLogGraph({
  commits,
  selected,
  onSelect,
  loadStat,
}: {
  commits: GraphCommit[];
  selected: string;
  onSelect: (hash: string) => void;
  loadStat?: (hash: string) => Promise<CommitStatView>;
}) {
  const rows = layoutGitGraph(commits);
  const laneCount = Math.max(1, ...rows.map((r) => r.laneCount));
  const svgW = laneCount * COL_W + 8;
  const [hover, setHover] = useState<string>('');
  const [stat, setStat] = useState<CommitStatView | null>(null);

  useEffect(() => {
    if (!hover || !loadStat) {
      setStat(null);
      return;
    }
    let cancelled = false;
    void loadStat(hover).then((s) => {
      if (!cancelled) setStat(s);
    });
    return () => {
      cancelled = true;
    };
  }, [hover, loadStat]);

  const hovered = rows.find((r) => r.commit.hash === hover)?.commit;

  return (
    <div className="relative flex min-h-0 flex-1 overflow-auto">
      <svg width={svgW} height={rows.length * ROW_H + 6} className="sticky left-0 shrink-0">
        {rows.map((row, i) => {
          const y1 = i * ROW_H + ROW_H / 2;
          const y2 = y1 + ROW_H;
          return (
            <g key={row.commit.hash}>
              {row.edges.map((e, k) => {
                const x1 = 8 + e.from * COL_W;
                const x2 = 8 + e.to * COL_W;
                const color = COLORS[e.from % COLORS.length];
                const d =
                  x1 === x2
                    ? `M ${x1} ${y1} L ${x2} ${y2}`
                    : `M ${x1} ${y1} C ${x1} ${y1 + 10}, ${x2} ${y2 - 10}, ${x2} ${y2}`;
                return <path key={k} d={d} fill="none" stroke={color} strokeWidth={e.merge ? 1.6 : 1.2} />;
              })}
              <circle
                cx={8 + row.column * COL_W}
                cy={y1}
                r={3.5}
                fill={COLORS[row.column % COLORS.length]}
                stroke="var(--color-card, #1e1e1e)"
                strokeWidth={1}
              />
            </g>
          );
        })}
      </svg>
      <div className="min-w-0 flex-1">
        {rows.map((row) => (
          <button
            key={row.commit.hash}
            type="button"
            style={{ height: ROW_H }}
            className={cn(
              'flex w-full min-w-0 items-center gap-1 overflow-hidden px-1 text-left text-[11px] leading-[22px]',
              selected === row.commit.hash ? 'bg-primary/10' : 'hover:bg-muted/50',
            )}
            onClick={() => onSelect(row.commit.hash)}
            onMouseEnter={() => setHover(row.commit.hash)}
            onMouseLeave={() => setHover('')}
          >
            <span className="min-w-0 flex-1 truncate">{row.commit.subject || shortHash(row.commit.hash)}</span>
            {row.commit.decorations.slice(0, 2).map((d) => (
              <span key={d} className="max-w-20 shrink-0 truncate rounded bg-primary/15 px-1 text-[10px] text-primary">
                {d}
              </span>
            ))}
          </button>
        ))}
      </div>
      {hovered && (
        <div
          role="tooltip"
          className="pointer-events-none absolute right-1 top-1 z-20 w-[min(100%-8px,20rem)] rounded-md border border-border bg-card p-2 text-[11px] shadow-lg"
        >
          <div className="text-muted-foreground">
            {hovered.author}
            {' · '}
            {relativeTime(hovered.date)}（{formatAbsoluteTime(hovered.date)}）
          </div>
          <div className="mt-1 font-medium break-words">{hovered.subject}</div>
          {stat && formatStat({ files: stat.Files, insertions: stat.Insertions, deletions: stat.Deletions }) && (
            <div className="mt-1 text-muted-foreground">
              {formatStat({ files: stat.Files, insertions: stat.Insertions, deletions: stat.Deletions })}
            </div>
          )}
          <div className="mt-1 font-mono text-muted-foreground">{shortHash(hovered.hash)}</div>
        </div>
      )}
    </div>
  );
}
