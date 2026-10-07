// 中心「终端」页签内容：shell/ssh 多页签 + +。常挂载 TerminalView。
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { cn } from '../lib/cn';
import { TAB_ACTIVE, TAB_BASE, TAB_UNDERLINE } from '../lib/ui';
import type { TerminalInfo } from '../lib/api';
import TerminalView from './TerminalView';
import { EmptyState } from './ui/empty-state';

const subTabBase = `group ${TAB_BASE} h-7 max-w-44 text-xs`;
const subTabActive = TAB_ACTIVE;

/** 本地 shell 按出现顺序编号；SSH 用连接名。 */
export function toolTermLabel(terms: TerminalInfo[], target: TerminalInfo, t: TFunction): string {
  if (target.Kind === 'ssh') return target.Title || 'SSH';
  const shells = terms.filter((x) => x.Kind === 'shell');
  const idx = shells.findIndex((x) => x.ID === target.ID);
  return idx <= 0 ? t('ui.terminal.label') : t('ui.terminal.label_n', { n: idx + 1 });
}

export default function PreviewToolPane({
  terms,
  active,
  subTab,
  onSubTab,
  onCloseTerminal,
  onNewShell,
}: {
  terms: TerminalInfo[];
  active: boolean;
  subTab: string;
  onSubTab: (id: string) => void;
  onCloseTerminal: (id: string) => void;
  onNewShell: () => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div
        className="flex shrink-0 items-stretch overflow-x-auto border-b border-border bg-muted/30"
        role="tablist"
        aria-label={t('ui.terminal.tabs_aria')}
      >
        {terms.map((term) => {
          const label = toolTermLabel(terms, term, t);
          const selected = subTab === term.ID;
          return (
            <div
              key={term.ID}
              className={cn(subTabBase, selected && subTabActive)}
              onClick={() => onSubTab(term.ID)}
              onAuxClick={(e) => {
                if (e.button === 1) {
                  e.preventDefault();
                  onCloseTerminal(term.ID);
                }
              }}
            >
              {term.Status === 'exited' && (
                <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-muted-foreground" />
              )}
              <button role="tab" aria-selected={selected} className="min-w-0 truncate text-xs" title={label}>
                {label}
              </button>
              <button
                className={cn(
                  'ml-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-sm text-sm leading-none text-muted-foreground transition-opacity hover:bg-muted hover:text-foreground',
                  selected ? 'opacity-70 hover:opacity-100' : 'opacity-0 group-hover:opacity-100',
                )}
                aria-label={t('ui.terminal.close_tab', { label })}
                onClick={(e) => {
                  e.stopPropagation();
                  onCloseTerminal(term.ID);
                }}
              >
                ×
              </button>
              {selected && <span className={TAB_UNDERLINE} />}
            </div>
          );
        })}
        <button
          type="button"
          className={cn(subTabBase, 'px-2 text-muted-foreground')}
          aria-label={t('ui.terminal.new')}
          title={t('ui.terminal.new_local')}
          onClick={onNewShell}
        >
          +
        </button>
      </div>

      <div className="min-h-0 flex-1 overflow-hidden">
        {terms.length === 0 && <EmptyState title={t('ui.terminal.empty_hint')} />}
        {terms.map((term) => (
          <div
            key={term.ID}
            className={cn('h-full', subTab !== term.ID && 'hidden')}
            style={{ animation: 'kshell-fade-in var(--duration-fast) var(--ease-out)' }}
          >
            <TerminalView term={term} active={active && subTab === term.ID} />
          </div>
        ))}
      </div>
    </div>
  );
}
