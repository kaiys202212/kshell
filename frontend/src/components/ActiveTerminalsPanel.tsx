// 右栏 Terminal 页签：列出运行中的预览区 shell/ssh，支持跨项目查看与点击定位。
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { TerminalInfo } from '../lib/api';
import { displayTitle } from '../lib/title';
import { sameWorkspacePath } from '../lib/workspacePath';
import { cn } from '../lib/cn';
import { LIST_ROW_ACTIVE } from '../lib/ui';
import { useAppStore } from '../state/store';
import { Button } from './ui/button';
import { EmptyState } from './ui/empty-state';

function isPreviewTerm(t: TerminalInfo): boolean {
  return (t.Kind === 'shell' || t.Kind === 'ssh') && t.Status === 'running';
}

function wsBaseName(ws: string): string {
  const n = ws.replace(/[\\/]+$/, '');
  const i = Math.max(n.lastIndexOf('/'), n.lastIndexOf('\\'));
  return i >= 0 ? n.slice(i + 1) : n;
}

export default function ActiveTerminalsPanel({ wsPath }: { wsPath: string }) {
  const { t } = useTranslation();
  const [showAll, setShowAll] = useState(false);
  const terminals = useAppStore((s) => s.terminals);

  const rows = useMemo(() => {
    return terminals.filter((x) => {
      if (!isPreviewTerm(x)) return false;
      if (showAll) return true;
      return sameWorkspacePath(x.Workspace, wsPath);
    });
  }, [terminals, showAll, wsPath]);

  const focusRow = (term: TerminalInfo) => {
    const { openTabs, setActiveTab, requestFocusTerm } = useAppStore.getState();
    const tab = openTabs.find((x) => sameWorkspacePath(x.id, term.Workspace));
    if (tab) setActiveTab(tab.id);
    if (term.Key) requestFocusTerm(term.Key);
  };

  return (
    <div className="flex flex-col gap-2.5 text-sm">
      <div className="flex items-center justify-end">
        <Button size="sm" variant="secondary" onClick={() => setShowAll((v) => !v)}>
          {showAll
            ? t('ui.workspace.active_terminals_view_current')
            : t('ui.workspace.active_terminals_view_all')}
        </Button>
      </div>

      {rows.length === 0 ? (
        <EmptyState title={t('ui.workspace.active_terminals_empty')} />
      ) : (
        <ul
          aria-label={t('ui.workspace.active_terminals_list_aria')}
          className="m-0 flex list-none flex-col gap-1.5 p-0"
        >
          {rows.map((term) => {
            const label = displayTitle(term.Title) || t('ui.workspace.new_session');
            const kindKey =
              term.Kind === 'ssh'
                ? 'ui.workspace.active_terminals_kind_ssh'
                : 'ui.workspace.active_terminals_kind_shell';
            return (
              <li key={term.ID}>
                <button
                  type="button"
                  className={cn(
                    'flex w-full min-w-0 flex-col gap-1 rounded border border-border bg-card px-2.5 py-1.5 text-left transition-colors hover:bg-muted/50',
                    sameWorkspacePath(term.Workspace, wsPath) && !showAll && LIST_ROW_ACTIVE,
                  )}
                  onClick={() => focusRow(term)}
                >
                  <div className="flex min-w-0 items-center gap-1.5">
                    <span className="min-w-0 truncate text-sm font-medium">{label}</span>
                    <span className="ml-auto shrink-0 rounded-sm border border-border px-1.5 py-px font-mono text-[10px] text-muted-foreground">
                      {t(kindKey)}
                    </span>
                  </div>
                  {showAll && term.Workspace && (
                    <span
                      className="truncate font-mono text-[10px] text-muted-foreground"
                      title={term.Workspace}
                    >
                      {t('ui.workspace.active_terminals_workspace', {
                        name: wsBaseName(term.Workspace),
                      })}
                    </span>
                  )}
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
