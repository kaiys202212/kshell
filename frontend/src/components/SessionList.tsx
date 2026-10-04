// 会话列表（工作区页签左栏）：展示当前工作区的历史会话。
// 行点击与中心区页签/会话预览联动；未激活用小图标激活，已恢复只标状态。
// 数据流与首页一致：先渲染缓存（GetSessions），收到 "scan:done" 后重调刷新。
import { useEffect, useMemo, useState } from 'react';
import { getSessions, onScanDone } from '../lib/api';
import type { ChatInfo, Session, TerminalInfo } from '../lib/api';
import { formatRelativeTime } from '../lib/format';
import { badgeFor } from '../lib/toolBadge';
import { displayTitle } from '../lib/title';
import { normalizeWorkspacePath } from '../lib/workspacePath';
import { cn } from '../lib/cn';
import { LIST_ROW, LIST_ROW_ACTIVE, MONO } from '../lib/ui';
import { resolveAgentActivity } from '../state/agentActivity';
import { useAppStore } from '../state/store';
import AgentActivityIcon from './AgentActivityIcon';
import WorkspaceSearch from './WorkspaceSearch';
import { EmptyState } from './ui/empty-state';
import { Skeleton } from './ui/skeleton';
import { ToolDot } from './ui/tool-dot';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip';

function findOpenedForSession(
  sessionID: string,
  chats: ChatInfo[],
  terminals: TerminalInfo[],
): { ID: string; Status: string; kind: 'chat' | 'terminal' } | null {
  const chatHit = chats.filter((c) => c.SessionID === sessionID);
  const termHit = terminals.filter((t) => t.SessionID === sessionID);
  const prefer = (list: { ID: string; Status: string }[]) =>
    list.find((x) => x.Status !== 'exited') ?? list[0];
  const c = prefer(chatHit);
  if (c) return { ID: c.ID, Status: c.Status, kind: 'chat' };
  const t = prefer(termHit);
  if (t) return { ID: t.ID, Status: t.Status, kind: 'terminal' };
  return null;
}

function isRestored(sessionID: string, chats: ChatInfo[], terminals: TerminalInfo[]): boolean {
  return [...chats, ...terminals].some((t) => t.SessionID === sessionID && t.Status !== 'exited');
}

// 用户已经发送、但磁盘扫描还没收录的会话。Path 用 live:<终端或聊天 id>，点行直接切到已打开的页签。
function liveSessions(
  workspacePath: string,
  diskIDs: Set<string>,
  chats: ChatInfo[],
  terminals: TerminalInfo[],
): Session[] {
  const target = normalizeWorkspacePath(workspacePath);
  const now = new Date().toISOString();
  const out: Session[] = [];
  const push = (id: string, sessionID: string, toolID: string, workspace: string, title: string) => {
    if (sessionID && diskIDs.has(sessionID)) return;
    out.push({
      ID: sessionID || `live:${id}`,
      ToolID: toolID,
      Workspace: workspace,
      Title: title,
      CreatedAt: now,
      UpdatedAt: now,
      Messages: 1,
      Path: `live:${id}`,
    });
  };
  for (const t of terminals) {
    if (!t.Prompted || (t.Kind !== 'new' && t.Kind !== 'session')) continue;
    if (normalizeWorkspacePath(t.Workspace) !== target) continue;
    push(t.ID, t.SessionID, t.ToolID, t.Workspace, t.Title);
  }
  for (const c of chats) {
    if (!c.Prompted || (c.Kind !== 'new' && c.Kind !== 'session')) continue;
    if (normalizeWorkspacePath(c.Workspace) !== target) continue;
    push(c.ID, c.SessionID, c.ToolID, c.Workspace, c.Title);
  }
  return out;
}

const tsOf = (v: string) => {
  const n = Date.parse(v);
  return Number.isFinite(n) ? n : 0;
};

const chipClass = (active: boolean) =>
  cn(
    'rounded-sm px-1.5 py-0.5 text-xs transition-colors',
    active
      ? 'bg-primary/10 font-medium text-primary underline decoration-primary underline-offset-4'
      : 'text-muted-foreground hover:bg-muted hover:text-foreground',
  );

interface Props {
  workspacePath: string;
  selectedSessionID?: string | null;
  /** 勾选后只看已归档会话 */
  showArchived?: boolean;
  onSelectRow?(s: Session): void;
  onActivate?(s: Session): void;
  onRestore?(s: Session): void;
}

export default function SessionList({
  workspacePath,
  selectedSessionID = null,
  showArchived = false,
  onSelectRow,
  onActivate,
  onRestore,
}: Props) {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [query, setQuery] = useState('');
  const [toolFilter, setToolFilter] = useState<string | null>(null);
  const scanState = useAppStore((s) => s.scanState);
  const setScanState = useAppStore((s) => s.setScanState);
  const terminals = useAppStore((s) => s.terminals);
  const chats = useAppStore((s) => s.chats);
  const chatPermissions = useAppStore((s) => s.chatPermissions);
  const terminalBusy = useAppStore((s) => s.terminalBusy);
  const archivedIDs = useAppStore((s) => s.archivedIDs);
  const archivedSet = useMemo(() => new Set(archivedIDs), [archivedIDs]);

  useEffect(() => {
    let cancelled = false;
    const refresh = () =>
      getSessions()
        .then((list) => {
          if (!cancelled) setSessions(list);
        })
        .catch(() => {});
    refresh();
    const offScan = onScanDone(() => {
      setScanState('done');
      void refresh();
    });
    return () => {
      cancelled = true;
      offScan();
    };
  }, [setScanState]);

  const target = normalizeWorkspacePath(workspacePath);
  const pooled = useMemo(() => {
    const diskIDs = new Set(sessions.map((s) => s.ID));
    const live = showArchived ? [] : liveSessions(workspacePath, diskIDs, chats, terminals);
    return [...live, ...sessions].filter((s) => {
      if (normalizeWorkspacePath(s.Workspace) !== target) return false;
      const archived = archivedSet.has(s.ID);
      if (showArchived) return archived && !s.Path.startsWith('live:');
      return !archived;
    });
  }, [sessions, showArchived, workspacePath, chats, terminals, target, archivedSet]);
  const toolOptions = useMemo(() => {
    const set = new Set<string>();
    for (const s of pooled) set.add(badgeFor(s.ToolID).label);
    return [...set];
  }, [pooled]);
  const visible = useMemo(() => {
    const q = query.trim().toLowerCase();
    return pooled
      .filter((s) => toolFilter === null || badgeFor(s.ToolID).label === toolFilter)
      .filter((s) => {
        if (!q) return true;
        return (
          s.Title.toLowerCase().includes(q) ||
          s.ToolID.toLowerCase().includes(q) ||
          badgeFor(s.ToolID).label.toLowerCase().includes(q)
        );
      })
      .sort(
        (a, b) =>
          tsOf(b.UpdatedAt) - tsOf(a.UpdatedAt) ||
          tsOf(b.CreatedAt) - tsOf(a.CreatedAt) ||
          a.Path.localeCompare(b.Path),
      );
  }, [pooled, query, toolFilter]);

  return (
    <TooltipProvider delayDuration={300}>
      <div className="flex flex-col gap-2">
      <WorkspaceSearch value={query} onChange={setQuery} />
      <div className="flex flex-wrap items-center gap-1" aria-label="按工具筛选">
        <button
          className={chipClass(toolFilter === null)}
          aria-pressed={toolFilter === null}
          onClick={() => setToolFilter(null)}
        >
          全部
        </button>
        {toolOptions.map((label) => (
          <button
            key={label}
            className={chipClass(toolFilter === label)}
            aria-pressed={toolFilter === label}
            onClick={() => setToolFilter(label)}
          >
            {label}
          </button>
        ))}
      </div>
      {visible.length === 0 ? (
        query.trim() ? (
          <EmptyState title="没有匹配的会话" />
        ) : scanState !== 'done' ? (
          <div className="flex flex-col gap-2">
            <Skeleton className="h-16 rounded border border-border" />
            <Skeleton className="h-16 rounded border border-border" />
            <Skeleton className="h-16 rounded border border-border" />
            <Skeleton className="h-16 rounded border border-border" />
          </div>
        ) : (
          <EmptyState title="该工作区暂无会话" />
        )
      ) : (
        <ul className="flex flex-col gap-2">
          {visible.map((s) => {
            const badge = badgeFor(s.ToolID);
            const liveID = s.Path.startsWith('live:') ? s.Path.slice('live:'.length) : '';
            const liveChat = liveID ? chats.find((c) => c.ID === liveID) : undefined;
            const liveTerm = liveID ? terminals.find((t) => t.ID === liveID) : undefined;
            const opened = liveChat
              ? { ID: liveChat.ID, Status: liveChat.Status, kind: 'chat' as const }
              : liveTerm
                ? { ID: liveTerm.ID, Status: liveTerm.Status, kind: 'terminal' as const }
                : findOpenedForSession(s.ID, chats, terminals);
            const restored = !!liveID || isRestored(s.ID, chats, terminals);
            const selected = selectedSessionID === s.ID;
            const title = displayTitle(s.Title);
            return (
              <li
                key={s.ID}
                className={cn(LIST_ROW, 'cursor-pointer p-2', selected && LIST_ROW_ACTIVE)}
                onClick={() => onSelectRow?.(s)}
              >
                <div className="flex min-w-0 flex-col gap-1.5">
                  <div className="flex min-w-0 items-center gap-1.5">
                    {showArchived && (
                      <span
                        className="inline-flex h-3.5 w-3.5 shrink-0 text-muted-foreground"
                        role="img"
                        aria-label="已归档"
                        title="已归档"
                      >
                        <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" aria-hidden="true">
                          <path
                            d="M2 3.2h12l-1 2.2H3L2 3.2Zm1.2 3h9.6V13H3.2V6.2Zm2.3 2.2v1.2h5V8.4h-5Z"
                            fill="currentColor"
                          />
                        </svg>
                      </span>
                    )}
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <span
                          data-testid="session-title"
                          className={cn(
                            'min-w-0 truncate text-sm font-medium',
                            (!title || showArchived) && 'text-muted-foreground',
                            !title && 'italic',
                          )}
                        >
                          {title || '(无标题)'}
                        </span>
                      </TooltipTrigger>
                      <TooltipContent className="max-w-xs sm:max-w-sm" side="bottom" align="start">
                        <p className="whitespace-pre-wrap break-words text-xs leading-5">
                          {title || '(无标题)'}
                        </p>
                        <p className="mt-1 text-[10px] opacity-70">
                          {badge.label} · {formatRelativeTime(s.UpdatedAt)} · {s.Messages} 条
                        </p>
                      </TooltipContent>
                    </Tooltip>
                    {opened && (
                      <AgentActivityIcon
                        activity={resolveAgentActivity({
                          status: opened.Status,
                          hasPermission: opened.kind === 'chat' && !!chatPermissions[opened.ID],
                          kind: opened.kind,
                          busy: opened.kind === 'terminal' ? !!terminalBusy[opened.ID] : undefined,
                        })}
                      />
                    )}
                  </div>
                  <div className="flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
                    <ToolDot toolID={s.ToolID} className="shrink-0" />
                    <span className={`whitespace-nowrap ${MONO}`}>{formatRelativeTime(s.UpdatedAt)}</span>
                    <span className={`whitespace-nowrap ${MONO}`}>{s.Messages} 条</span>
                    {showArchived ? (
                      <button
                        type="button"
                        className="ml-auto inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-sm text-muted-foreground hover:bg-muted hover:text-primary"
                        aria-label="还原"
                        title="还原到开发列表"
                        onClick={(e) => {
                          e.stopPropagation();
                          onRestore?.(s);
                        }}
                      >
                        <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" aria-hidden="true">
                          <path
                            d="M8 2.2a5.8 5.8 0 1 0 4.9 8.8l-1.1-.7A4.4 4.4 0 1 1 8 3.6V2.2Zm4.2.4v3.2H9l1.1-1.1A5.7 5.7 0 0 0 8 3.6"
                            fill="currentColor"
                          />
                        </svg>
                      </button>
                    ) : restored ? (
                      <span
                        className="ml-auto inline-flex h-5 w-5 shrink-0 items-center justify-center text-primary"
                        role="img"
                        aria-label="已恢复"
                        title="已恢复"
                      >
                        <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" aria-hidden="true">
                          <path
                            d="M8 1.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13Zm3.1 4.4-3.7 4.3-1.8-1.7-.9.9 2.8 2.6 4.6-5.4-.9-.7Z"
                            fill="currentColor"
                          />
                        </svg>
                      </span>
                    ) : (
                      <button
                        type="button"
                        className="ml-auto inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-sm text-muted-foreground hover:bg-muted hover:text-primary"
                        aria-label="激活"
                        title="激活"
                        onClick={(e) => {
                          e.stopPropagation();
                          onActivate?.(s);
                        }}
                      >
                        <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" aria-hidden="true">
                          <path d="M4.5 2.8v10.4L13.2 8 4.5 2.8Z" fill="currentColor" />
                        </svg>
                      </button>
                    )}
                  </div>
                </div>
              </li>
            );
          })}
        </ul>
      )}
      </div>
    </TooltipProvider>
  );
}
