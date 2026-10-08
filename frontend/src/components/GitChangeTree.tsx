import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { GitDiffSide, GitSCMEntry } from '../lib/api';
import { buildChangeTree, collectPaths, type ChangeNode } from '../lib/gitChangeTree';
import { cn } from '../lib/cn';
import { Button } from './ui/button';
import { ChevronIcon, FileIcon, FolderIcon } from './treeIcons';
import { PANE_HEADER } from '../lib/ui';

export function GitChangeTree({
  title,
  entries,
  side,
  onOpen,
  onStage,
  onUnstage,
  onDiscard,
}: {
  title: string;
  entries: GitSCMEntry[];
  side: GitDiffSide;
  onOpen: (e: GitSCMEntry, side: GitDiffSide, preview: boolean) => void;
  onStage: ((paths: string[]) => void) | null;
  onUnstage: ((paths: string[]) => void) | null;
  onDiscard: ((paths: string[], label: string) => void) | null;
}) {
  const { t } = useTranslation();
  const tree = useMemo(() => buildChangeTree(entries), [entries]);
  return (
    <div className="min-h-0">
      <div className={cn(PANE_HEADER, 'flex items-center justify-between pr-1')}>
        <span>
          {title} ({entries.length})
        </span>
        {entries.length > 0 && (
          <div className="flex">
            {onStage && (
              <Button size="sm" variant="ghost" aria-label={t('ui.git.stage_all_aria', { title })} onClick={() => onStage(collectPaths(tree))}>
                +
              </Button>
            )}
            {onUnstage && (
              <Button size="sm" variant="ghost" aria-label={t('ui.git.unstage_all_aria', { title })} onClick={() => onUnstage(collectPaths(tree))}>
                −
              </Button>
            )}
          </div>
        )}
      </div>
      {tree.children.map((n) => (
        <TreeRow
          key={n.path}
          node={n}
          depth={0}
          side={side}
          onOpen={onOpen}
          onStage={onStage}
          onUnstage={onUnstage}
          onDiscard={onDiscard}
        />
      ))}
    </div>
  );
}

function TreeRow({
  node,
  depth,
  side,
  onOpen,
  onStage,
  onUnstage,
  onDiscard,
}: {
  node: ChangeNode;
  depth: number;
  side: GitDiffSide;
  onOpen: (e: GitSCMEntry, side: GitDiffSide, preview: boolean) => void;
  onStage: ((paths: string[]) => void) | null;
  onUnstage: ((paths: string[]) => void) | null;
  onDiscard: ((paths: string[], label: string) => void) | null;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(true);
  const paths = collectPaths(node);
  const glyph = statusGlyph(node.entry, side);
  return (
    <div>
      <div
        className="group flex h-5 items-center gap-0.5 rounded py-0 leading-5 hover:bg-muted/50"
        style={{ paddingLeft: 4 + depth * 12 }}
      >
        {node.dir ? (
          <button
            type="button"
            className="flex h-4 w-4 shrink-0 items-center justify-center text-muted-foreground"
            aria-label={t('ui.git.expand_aria', { path: node.path })}
            onClick={() => setOpen((v) => !v)}
          >
            <ChevronIcon open={open} />
          </button>
        ) : (
          <span className="h-4 w-4 shrink-0" />
        )}
        {node.dir ? <FolderIcon open={open} /> : <FileIcon />}
        {node.dir || !node.entry ? (
          <span className={cn('min-w-0 flex-1 truncate font-mono text-xs', node.dir && 'font-medium')}>{node.name}</span>
        ) : (
          <button
            type="button"
            className="min-w-0 flex-1 truncate text-left font-mono text-xs"
            onClick={() => onOpen(node.entry!, side, true)}
            onDoubleClick={() => onOpen(node.entry!, side, false)}
          >
            {node.name}
          </button>
        )}
        {glyph && <span className="w-3 shrink-0 text-[10px] text-muted-foreground">{glyph}</span>}
        {onStage && (
          <Button size="sm" variant="ghost" className="h-5 min-h-0 px-1 opacity-0 group-hover:opacity-100" aria-label={t('ui.git.stage_aria', { path: node.path })} onClick={() => onStage(paths)}>
            +
          </Button>
        )}
        {onUnstage && (
          <Button size="sm" variant="ghost" className="h-5 min-h-0 px-1 opacity-0 group-hover:opacity-100" aria-label={t('ui.git.unstage_aria', { path: node.path })} onClick={() => onUnstage(paths)}>
            −
          </Button>
        )}
        {onDiscard && (
          <Button
            size="sm"
            variant="ghost"
            className="h-5 min-h-0 px-1 opacity-0 group-hover:opacity-100"
            aria-label={t('ui.git.discard_aria', { path: node.path })}
            onClick={() => onDiscard(paths, node.path)}
          >
            {t('ui.git.discard')}
          </Button>
        )}
      </div>
      {node.dir && open &&
        node.children.map((c) => (
          <TreeRow
            key={c.path}
            node={c}
            depth={depth + 1}
            side={side}
            onOpen={onOpen}
            onStage={onStage}
            onUnstage={onUnstage}
            onDiscard={onDiscard}
          />
        ))}
    </div>
  );
}

function statusGlyph(e: GitSCMEntry | undefined, side: GitDiffSide): string {
  if (!e) return '';
  if (e.Untracked) return 'U';
  if (e.Conflicted) return '!';
  const ch = side === 'staged' ? e.X : e.Y;
  return (ch && ch !== ' ' ? ch : 'M').trim();
}
