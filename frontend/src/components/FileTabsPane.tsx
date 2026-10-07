// 文件区：VS Code 式预览页签 + 常挂载 Preview 实例。
import { useTranslation } from 'react-i18next';
import { cn } from '../lib/cn';
import {
  closeTab,
  fileTabLabel,
  parseCommitDiffTabPath,
  parseDiffTabPath,
  pinTab,
  type FileTabsState,
} from '../lib/fileTabs';
import { TAB_ACTIVE, TAB_BASE, TAB_UNDERLINE } from '../lib/ui';
import GitCommitDiffView from './GitCommitDiffView';
import GitDiffView from './GitDiffView';
import Preview from './Preview';

const subTabBase = `group ${TAB_BASE} h-7 max-w-44 text-xs`;

export default function FileTabsPane({
  wsPath,
  state,
  dirty,
  onChange,
  onDirty,
}: {
  wsPath: string;
  state: FileTabsState;
  dirty: Record<string, boolean>;
  onChange: (next: FileTabsState) => void;
  onDirty: (path: string, isDirty: boolean) => void;
}) {
  const { t: tr } = useTranslation();
  const closeOne = (path: string) => {
    if (dirty[path] && !window.confirm(tr('ui.files.close_dirty_confirm'))) return;
    onDirty(path, false);
    onChange(closeTab(state, path));
  };

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div
        className="flex shrink-0 items-stretch overflow-x-auto border-b border-border bg-muted/30"
        role="tablist"
        aria-label={tr('ui.files.tabs_aria')}
      >
        {state.tabs.map((t) => {
          const selected = state.activePath === t.path;
          const label = fileTabLabel(t.path);
          const isDirty = !!dirty[t.path];
          return (
            <div
              key={t.path}
              className={cn(subTabBase, selected && TAB_ACTIVE, t.preview && 'italic')}
              data-preview={t.preview ? 'true' : 'false'}
              data-dirty={isDirty ? 'true' : 'false'}
              onClick={() => onChange({ ...state, activePath: t.path })}
              onAuxClick={(e) => {
                if (e.button === 1) {
                  e.preventDefault();
                  closeOne(t.path);
                }
              }}
            >
              <button
                role="tab"
                aria-selected={selected}
                className="min-w-0 truncate text-xs"
                title={t.path}
              >
                {isDirty ? '● ' : ''}
                {label}
              </button>
              <button
                className={cn(
                  'ml-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-sm text-sm leading-none text-muted-foreground transition-opacity hover:bg-muted hover:text-foreground',
                  selected ? 'opacity-70 hover:opacity-100' : 'opacity-0 group-hover:opacity-100',
                )}
                aria-label={tr('ui.files.close_tab', { label })}
                onClick={(e) => {
                  e.stopPropagation();
                  closeOne(t.path);
                }}
              >
                ×
              </button>
              {selected && <span className={TAB_UNDERLINE} />}
            </div>
          );
        })}
      </div>
      <div className="min-h-0 flex-1 overflow-hidden p-3">
        {state.tabs.length === 0 && (
          <Preview wsPath={wsPath} path={null} />
        )}
        {state.tabs.map((t) => {
          const diff = parseDiffTabPath(t.path);
          const commitDiff = parseCommitDiffTabPath(t.path);
          return (
          <div
            key={t.path}
            className={cn('h-full', state.activePath !== t.path && 'hidden')}
            style={{ animation: 'kshell-fade-in var(--duration-fast) var(--ease-out)' }}
          >
            {commitDiff ? (
              <GitCommitDiffView
                wsPath={wsPath}
                repoRel={commitDiff.repoRel}
                hash={commitDiff.hash}
              />
            ) : diff ? (
              <GitDiffView wsPath={wsPath} repoRel={diff.repoRel} path={diff.path} side={diff.side} />
            ) : (
              <Preview
                wsPath={wsPath}
                path={t.path}
                onDirtyChange={(d) => onDirty(t.path, d)}
                onEdited={() => {
                  if (t.preview) onChange(pinTab(state, t.path));
                }}
              />
            )}
          </div>
          );
        })}
      </div>
    </div>
  );
}
