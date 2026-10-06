// 中心「终端」页签内容：shell/ssh 多页签 + +。常挂载 TerminalView。
import { cn } from '../lib/cn';
import { TAB_ACTIVE, TAB_BASE, TAB_UNDERLINE } from '../lib/ui';
import type { TerminalInfo } from '../lib/api';
import TerminalView from './TerminalView';
import { EmptyState } from './ui/empty-state';

const subTabBase = `group ${TAB_BASE} h-7 max-w-44 text-xs`;
const subTabActive = TAB_ACTIVE;

/** 本地 shell 按出现顺序编号；SSH 用连接名。 */
export function toolTermLabel(terms: TerminalInfo[], t: TerminalInfo): string {
  if (t.Kind === 'ssh') return t.Title || 'SSH';
  const shells = terms.filter((x) => x.Kind === 'shell');
  const idx = shells.findIndex((x) => x.ID === t.ID);
  return idx <= 0 ? '终端' : `终端 ${idx + 1}`;
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
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div
        className="flex shrink-0 items-stretch overflow-x-auto border-b border-border bg-muted/30"
        role="tablist"
        aria-label="终端子页签"
      >
        {terms.map((t) => {
          const label = toolTermLabel(terms, t);
          const selected = subTab === t.ID;
          return (
            <div
              key={t.ID}
              className={cn(subTabBase, selected && subTabActive)}
              onClick={() => onSubTab(t.ID)}
              onAuxClick={(e) => {
                if (e.button === 1) {
                  e.preventDefault();
                  onCloseTerminal(t.ID);
                }
              }}
            >
              {t.Status === 'exited' && (
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
                aria-label={`关闭 ${label}`}
                onClick={(e) => {
                  e.stopPropagation();
                  onCloseTerminal(t.ID);
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
          aria-label="新建终端"
          title="新建本地终端"
          onClick={onNewShell}
        >
          +
        </button>
      </div>

      <div className="min-h-0 flex-1 overflow-hidden">
        {terms.length === 0 && <EmptyState title="点 + 新建本地终端，或从右侧 SSH 打开远程会话" />}
        {terms.map((t) => (
          <div
            key={t.ID}
            className={cn('h-full', subTab !== t.ID && 'hidden')}
            style={{ animation: 'kshell-fade-in var(--duration-fast) var(--ease-out)' }}
          >
            <TerminalView term={t} active={active && subTab === t.ID} />
          </div>
        ))}
      </div>
    </div>
  );
}
