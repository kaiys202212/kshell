import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import {
  formatAbsoluteTime,
  formatStat,
  layoutGitGraph,
  shortHash,
  type GraphCommit,
} from '../lib/gitGraph';
import { formatRelativeTime } from '../lib/format';
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
  onOpenCommit,
  loadStat,
}: {
  commits: GraphCommit[];
  selected: string;
  onSelect: (hash: string) => void;
  /** 单击预览 / 双击固定：打开该提交的完整 diff 页签 */
  onOpenCommit?: (hash: string, preview: boolean) => void;
  loadStat?: (hash: string) => Promise<CommitStatView>;
}) {
  const rows = layoutGitGraph(commits);
  const laneCount = Math.max(1, ...rows.map((r) => r.laneCount));
  const svgW = laneCount * COL_W + 8;
  const [hover, setHover] = useState('');
  const [edgeTip, setEdgeTip] = useState<{ label: string; top: number; left: number } | null>(null);
  const [tipPos, setTipPos] = useState<{ top: number; left: number } | null>(null);
  const [stat, setStat] = useState<CommitStatView | null>(null);
  const rootRef = useRef<HTMLDivElement>(null);

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

  const showTip = (hash: string, el: HTMLElement) => {
    setHover(hash);
    const row = el.getBoundingClientRect();
    const root = rootRef.current?.getBoundingClientRect();
    // 详情贴在整块列表最左侧外侧，垂直对齐该行
    const left = root?.left ?? row.left;
    setTipPos({ top: row.top + row.height / 2, left });
  };

  const hideTip = () => {
    setHover('');
    setTipPos(null);
  };

  return (
    <div ref={rootRef} className="relative flex min-h-0 flex-1 overflow-auto">
      <svg width={svgW} height={Math.max(rows.length * ROW_H, ROW_H)} className="sticky left-0 shrink-0 overflow-visible">
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
                return (
                  <g key={k}>
                    {/* 透明加宽命中区，细线本身难悬停 */}
                    <path
                      d={d}
                      fill="none"
                      stroke="transparent"
                      strokeWidth={10}
                      className={e.label ? 'cursor-default' : undefined}
                      onMouseEnter={(ev) => {
                        if (!e.label) return;
                        setEdgeTip({ label: e.label, top: ev.clientY, left: ev.clientX });
                      }}
                      onMouseMove={(ev) => {
                        if (!e.label) return;
                        setEdgeTip({ label: e.label, top: ev.clientY, left: ev.clientX });
                      }}
                      onMouseLeave={() => setEdgeTip(null)}
                    />
                    <path
                      d={d}
                      fill="none"
                      stroke={color}
                      strokeWidth={e.merge ? 1.6 : 1.2}
                      className="pointer-events-none"
                    />
                  </g>
                );
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
        {rows.map((row) => {
          const isSel = selected === row.commit.hash;
          const isHover = hover === row.commit.hash;
          return (
            <button
              key={row.commit.hash}
              type="button"
              style={{ height: ROW_H }}
              className={cn(
                'flex w-full min-w-0 items-center gap-1 overflow-hidden border-l-2 px-1 text-left text-[11px] leading-[22px]',
                isSel
                  ? 'border-l-primary bg-primary/15 text-foreground'
                  : 'border-l-transparent',
                isHover && !isSel && 'bg-muted',
                isHover && isSel && 'bg-primary/25',
                !isHover && !isSel && 'hover:bg-muted/60',
              )}
              onClick={() => {
                onSelect(row.commit.hash);
                onOpenCommit?.(row.commit.hash, true);
              }}
              onDoubleClick={() => onOpenCommit?.(row.commit.hash, false)}
              onMouseEnter={(e) => showTip(row.commit.hash, e.currentTarget)}
              onMouseLeave={hideTip}
            >
              <span className="min-w-0 flex-1 truncate">{row.commit.subject || shortHash(row.commit.hash)}</span>
              {row.commit.decorations.slice(0, 2).map((d) => (
                <span key={d} className="max-w-20 shrink-0 truncate rounded bg-primary/15 px-1 text-[10px] text-primary">
                  {d}
                </span>
              ))}
            </button>
          );
        })}
      </div>
      {hovered &&
        tipPos &&
        createPortal(
          <div
            role="tooltip"
            className="pointer-events-none z-[200] w-64 max-w-[min(20rem,40vw)] -translate-x-full -translate-y-1/2 rounded-md border border-border bg-card p-2 text-[11px] shadow-lg"
            style={{ position: 'fixed', top: tipPos.top, left: tipPos.left - 8 }}
          >
            <div className="text-muted-foreground">
              {hovered.author}
              {' · '}
              {formatRelativeTime(hovered.date)} ({formatAbsoluteTime(hovered.date)})
            </div>
            <div className="mt-1 font-medium break-words">{hovered.subject}</div>
            {stat && formatStat({ files: stat.Files, insertions: stat.Insertions, deletions: stat.Deletions }) && (
              <div className="mt-1 text-muted-foreground">
                {formatStat({ files: stat.Files, insertions: stat.Insertions, deletions: stat.Deletions })}
              </div>
            )}
            <div className="mt-1 font-mono text-muted-foreground">{shortHash(hovered.hash)}</div>
          </div>,
          document.body,
        )}
      {edgeTip &&
        createPortal(
          <div
            role="tooltip"
            className="pointer-events-none z-[201] -translate-y-full rounded-md border border-border bg-card px-2 py-1 text-[11px] shadow-lg"
            style={{ position: 'fixed', top: edgeTip.top - 8, left: edgeTip.left + 12 }}
          >
            {edgeTip.label}
          </div>,
          document.body,
        )}
    </div>
  );
}
