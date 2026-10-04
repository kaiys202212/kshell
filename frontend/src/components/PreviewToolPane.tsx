// 预览内容区：子页签「预览 | 终端… | +」。
// shell/ssh 终端常挂载（hidden 切换），与中心区 agent 终端同一套 TerminalView。
import { cn } from '../lib/cn';
import { TAB_ACTIVE, TAB_BASE, TAB_UNDERLINE } from '../lib/ui';
import type { TerminalInfo } from '../lib/api';
import Preview from './Preview';
import SessionTranscript from './SessionTranscript';
import TerminalView from './TerminalView';

export const PREVIEW_SUB = 'preview';
export const SESSION_PREVIEW_SUB = 'session-preview';

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
  wsPath,
  previewPath,
  terms,
  active,
  subTab,
  onSubTab,
  onCloseTerminal,
  onNewShell,
  sessionPreview = null,
  onCloseSessionPreview,
  onActivateSessionPreview,
}: {
  wsPath: string;
  previewPath: string | null;
  terms: TerminalInfo[];
  active: boolean;
  subTab: string;
  onSubTab: (id: string) => void;
  onCloseTerminal: (id: string) => void;
  onNewShell: () => void;
  sessionPreview?: { sessionID: string; title: string } | null;
  onCloseSessionPreview?: () => void;
  onActivateSessionPreview?: () => void;
}) {
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div
        className="flex shrink-0 items-stretch overflow-x-auto border-b border-border bg-muted/30"
        role="tablist"
        aria-label="预览区子页签"
      >
        <button
          role="tab"
          aria-selected={subTab === PREVIEW_SUB}
          className={cn(subTabBase, subTab === PREVIEW_SUB && subTabActive)}
          onClick={() => onSubTab(PREVIEW_SUB)}
        >
          预览
          {subTab === PREVIEW_SUB && <span className={TAB_UNDERLINE} />}
        </button>
        {sessionPreview && (
          <div
            className={cn(subTabBase, subTab === SESSION_PREVIEW_SUB && subTabActive)}
            onClick={() => onSubTab(SESSION_PREVIEW_SUB)}
          >
            <button
              role="tab"
              aria-selected={subTab === SESSION_PREVIEW_SUB}
              className="min-w-0 truncate text-xs"
              title={sessionPreview.title}
            >
              会话预览
            </button>
            <button
              className={cn(
                'ml-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-sm text-sm leading-none text-muted-foreground transition-opacity hover:bg-muted hover:text-foreground',
                subTab === SESSION_PREVIEW_SUB ? 'opacity-70 hover:opacity-100' : 'opacity-0 group-hover:opacity-100',
              )}
              aria-label="关闭会话预览"
              onClick={(e) => {
                e.stopPropagation();
                onCloseSessionPreview?.();
              }}
            >
              ×
            </button>
            {subTab === SESSION_PREVIEW_SUB && <span className={TAB_UNDERLINE} />}
          </div>
        )}
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
        <div
          className={cn('h-full overflow-hidden p-3', subTab !== PREVIEW_SUB && 'hidden')}
          style={{ animation: 'kshell-fade-in var(--duration-fast) var(--ease-out)' }}
        >
          <Preview wsPath={wsPath} path={previewPath} />
        </div>
        {sessionPreview && (
          <div
            className={cn('h-full', subTab !== SESSION_PREVIEW_SUB && 'hidden')}
            style={{ animation: 'kshell-fade-in var(--duration-fast) var(--ease-out)' }}
          >
            <SessionTranscript
              sessionID={sessionPreview.sessionID}
              title={sessionPreview.title}
              onActivate={() => onActivateSessionPreview?.()}
            />
          </div>
        )}
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
