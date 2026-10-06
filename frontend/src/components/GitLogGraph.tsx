import { layoutGitGraph, relativeTime, shortHash, type GraphCommit } from '../lib/gitGraph';
import { cn } from '../lib/cn';

const ROW_H = 22;
const COL_W = 14;
const COLORS = ['#3794ff', '#b180d7', '#d18616', '#3fa266', '#cc6633', '#808080'];

export function GitLogGraph({
  commits,
  selected,
  onSelect,
}: {
  commits: GraphCommit[];
  selected: string;
  onSelect: (hash: string) => void;
}) {
  const rows = layoutGitGraph(commits);
  const laneCount = Math.max(1, ...rows.map((r) => r.laneCount));
  const svgW = laneCount * COL_W + 8;

  return (
    <div className="flex min-h-0 flex-1 overflow-auto">
      <svg width={svgW} height={Math.max(rows.length * ROW_H, ROW_H)} className="sticky left-0 shrink-0">
        {rows.map((row, i) => {
          const y1 = i * ROW_H + ROW_H / 2;
          const y2 = y1 + ROW_H;
          return (
            <g key={row.commit.hash}>
              {row.edges.map((e, k) => {
                const x1 = 8 + e.from * COL_W;
                const x2 = 8 + e.to * COL_W;
                const color = COLORS[Math.min(e.from, e.to) % COLORS.length];
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
              'flex w-full items-center gap-2 overflow-hidden px-1 text-left text-[11px] leading-[22px]',
              selected === row.commit.hash ? 'bg-primary/10' : 'hover:bg-muted/50',
            )}
            onClick={() => onSelect(row.commit.hash)}
          >
            <span className="min-w-0 flex-1 truncate">{row.commit.subject}</span>
            <span className="hidden shrink-0 text-muted-foreground sm:inline">{row.commit.author}</span>
            <span className="shrink-0 text-muted-foreground">{relativeTime(row.commit.date)}</span>
            <span className="shrink-0 font-mono text-muted-foreground">{shortHash(row.commit.hash)}</span>
            {row.commit.decorations.slice(0, 3).map((d) => (
              <span key={d} className="shrink-0 rounded bg-primary/15 px-1 text-[10px] text-primary">
                {d}
              </span>
            ))}
          </button>
        ))}
      </div>
    </div>
  );
}
