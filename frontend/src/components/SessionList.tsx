// 会话列表（工作区页签左栏）：展示当前工作区的历史会话。
// 行点击与中心区页签/会话预览联动；未激活用小图标激活，已恢复只标状态。
// 数据流与首页一致：先渲染缓存（GetSessions），收到 "scan:done" 后重调刷新。
import { useEffect, useMemo, useRef, useState } from 'react';
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { useTranslation } from 'react-i18next';
import {
  batchArchiveSessions,
  batchRestoreSessions,
  getRemoteSessions,
  getSessions,
  isSSHWorkspaceRef,
  onScanDone,
  scanRemoteSessions,
} from '../lib/api';
import type { ChatInfo, Session, TerminalInfo } from '../lib/api';
import { backendError } from '../lib/errors';
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
import { Button } from './ui/button';
import { Dialog } from './ui/dialog';
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
  onArchive?(s: Session): void;
}

export default function SessionList({
  workspacePath,
  selectedSessionID = null,
  showArchived = false,
  onSelectRow,
  onActivate,
  onRestore,
  onArchive,
}: Props) {
  const { t } = useTranslation();
  const [sessions, setSessions] = useState<Session[]>([]);
  const [remoteReady, setRemoteReady] = useState(false);
  const [remoteFailed, setRemoteFailed] = useState(false);
  const [query, setQuery] = useState('');
  const [toolFilter, setToolFilter] = useState<string | null>(null);
  const [selectMode, setSelectMode] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [batchPending, setBatchPending] = useState(false);
  const selectAllRef = useRef<HTMLInputElement>(null);
  const scanState = useAppStore((s) => s.scanState);
  const setScanState = useAppStore((s) => s.setScanState);
  const notify = useAppStore((s) => s.notify);
  const markUnreachable = useAppStore((s) => s.markUnreachable);
  const clearUnreachable = useAppStore((s) => s.clearUnreachable);
  const ssh = isSSHWorkspaceRef(workspacePath);
  const listReady = ssh ? remoteReady : scanState === 'done';
  const terminals = useAppStore((s) => s.terminals);
  const chats = useAppStore((s) => s.chats);
  const chatPermissions = useAppStore((s) => s.chatPermissions);
  const terminalBusy = useAppStore((s) => s.terminalBusy);
  const archivedIDs = useAppStore((s) => s.archivedIDs);
  const archivedSet = useMemo(() => new Set(archivedIDs), [archivedIDs]);

  useEffect(() => {
    let cancelled = false;
    if (ssh) {
      setRemoteReady(false);
      setRemoteFailed(false);
    }
    const refresh = () => {
      if (ssh) {
        // 打开/切换到 ssh 工作区时走远端扫描；失败则回退读缓存。
        return scanRemoteSessions(workspacePath)
          .then((list) => {
            if (!cancelled) {
              setSessions(list);
              setRemoteFailed(false);
              setRemoteReady(true);
              clearUnreachable(workspacePath);
            }
          })
          .catch((scanErr: unknown) =>
            getRemoteSessions(workspacePath)
              .then((list) => {
                if (cancelled) return;
                setSessions(list);
                setRemoteReady(true);
                // Go 侧 getRemoteSessions 通常不 reject：空数组视为不可达，勿依赖 catch。
                if (list.length > 0) {
                  // 有缓存：展示列表并 toast；不 clearUnreachable（扫描未成功）
                  setRemoteFailed(false);
                  notify(
                    t('ui.session_list.remote_scan_failed', { err: backendError(scanErr) }),
                    'error',
                  );
                  return;
                }
                setRemoteFailed(true);
                markUnreachable(workspacePath);
                notify(
                  t('ui.session_list.remote_scan_failed', { err: backendError(scanErr) }),
                  'error',
                );
              })
              .catch((cacheErr: unknown) => {
                if (!cancelled) {
                  setSessions([]);
                  setRemoteFailed(true);
                  setRemoteReady(true);
                  markUnreachable(workspacePath);
                  notify(
                    t('ui.session_list.remote_scan_failed', {
                      err: backendError(cacheErr ?? scanErr),
                    }),
                    'error',
                  );
                }
              }),
          );
      }
      return getSessions()
        .then((list) => {
          if (!cancelled) setSessions(list);
        })
        .catch(() => {});
    };
    void refresh();
    const offScan = onScanDone(() => {
      setScanState('done');
      // 本地 scan:done 只刷新本地会话列表；远端列表由本 effect 的 workspacePath 驱动。
      if (!ssh) void refresh();
    });
    return () => {
      cancelled = true;
      offScan();
    };
  }, [workspacePath, ssh, setScanState, notify, markUnreachable, clearUnreachable, t]);

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

  // —— 批量多选：只收集磁盘会话 ID，live 行（Path 以 live: 开头）不可选 ——
  const diskIDs = useMemo(
    () => visible.filter((s) => !s.Path.startsWith('live:')).map((s) => s.ID),
    [visible],
  );
  const allSelected = diskIDs.length > 0 && diskIDs.every((id) => selected.has(id));
  // 实际会提交的集合：勾选中仍在当前可见磁盘列表里的部分。
  // 确认弹窗的计数也用它，避免「弹窗显示 3 个、实际只归档 1 个」的口径不一致。
  const confirmIds = useMemo(() => {
    if (selected.size === 0) return [];
    const disk = new Set(diskIDs);
    return [...selected].filter((id) => disk.has(id));
  }, [selected, diskIDs]);
  const exitSelectMode = () => {
    setSelectMode(false);
    setSelected(new Set());
  };
  const toggleAll = () => {
    setSelected(allSelected ? new Set() : new Set(diskIDs));
  };
  const toggleRow = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };
  const runBatch = async () => {
    // confirmIds 已与当前可见磁盘会话取交集：筛选条件变化后残留的不可见 id 不提交
    const ids = confirmIds;
    if (ids.length === 0 || batchPending) {
      setConfirmOpen(false);
      return;
    }
    setBatchPending(true);
    try {
      if (showArchived) {
        const changed = await batchRestoreSessions(ids);
        const { archivedIDs, setArchivedIDs } = useAppStore.getState();
        const drop = new Set(ids);
        setArchivedIDs(archivedIDs.filter((id) => !drop.has(id)));
        notify(t('ui.session_list.batch_restore_done', { count: changed }), 'success');
      } else {
        const changed = await batchArchiveSessions(ids);
        const { archivedIDs, setArchivedIDs } = useAppStore.getState();
        setArchivedIDs([...new Set([...archivedIDs, ...ids])]);
        notify(t('ui.session_list.batch_done', { count: changed }), 'success');
      }
      setSelected(new Set());
      setSelectMode(false);
    } catch (e) {
      notify(t('ui.session_list.batch_failed', { err: backendError(e) }), 'error');
    } finally {
      setConfirmOpen(false);
      setBatchPending(false);
    }
  };

  // 半选态：有勾选但未全选。分母与 allSelected 一致（仅磁盘会话），否则 live 行会让
  // checked 与 indeterminate 并存，视觉半选但点击却是全取消。
  useEffect(() => {
    if (selectAllRef.current) {
      selectAllRef.current.indeterminate = selected.size > 0 && !allSelected;
    }
  }, [selected, allSelected]);

  // 切换工作区/归档视图时退出多选，避免残留跨列表的勾选
  useEffect(() => {
    setSelectMode(false);
    setSelected(new Set());
  }, [workspacePath, showArchived]);

  // Esc 退出选择模式（确认弹窗打开时交给 Dialog 自己处理）
  useEffect(() => {
    if (!selectMode || confirmOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') exitSelectMode();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [selectMode, confirmOpen]);

  return (
    <TooltipProvider delayDuration={300}>
      {/* 滚动隔离：根容器撑满左栏剩余高度，搜索框与 chips 固定，仅列表区内部滚动 */}
      <div className="flex min-h-0 flex-1 flex-col gap-2">
      <div className="shrink-0">
        <WorkspaceSearch value={query} onChange={setQuery} />
      </div>
      <div className="flex shrink-0 flex-wrap items-center gap-1" aria-label={t('ui.session_list.filter_aria')}>
        <button
          className={chipClass(toolFilter === null)}
          aria-pressed={toolFilter === null}
          onClick={() => setToolFilter(null)}
        >
          {t('ui.session_list.all')}
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
        <button
          type="button"
          className={cn(chipClass(selectMode), 'disabled:pointer-events-none disabled:opacity-50')}
          aria-pressed={selectMode}
          aria-label={t('ui.session_list.select_mode_aria')}
          disabled={visible.length === 0}
          onClick={() => (selectMode ? exitSelectMode() : setSelectMode(true))}
        >
          {t('ui.session_list.select_mode')}
        </button>
      </div>
      {selectMode && (
        <div className="flex shrink-0 items-center gap-2 text-xs text-muted-foreground">
          <input
            ref={selectAllRef}
            type="checkbox"
            className="accent-primary"
            aria-label={t('ui.session_list.select_all')}
            checked={allSelected}
            onChange={toggleAll}
          />
          <span>
            {t('ui.session_list.selected_count', { selected: selected.size, total: visible.length })}
          </span>
        </div>
      )}
      {/* 空态/骨架/列表三分支统一包进滚动容器：固定头部（搜索、chips）不随之滚动 */}
      <div className="min-h-0 flex-1 overflow-y-auto">
      {visible.length === 0 ? (
        query.trim() ? (
          <EmptyState title={t('ui.session_list.no_match')} />
        ) : !listReady ? (
          <div className="flex flex-col gap-2">
            <Skeleton className="h-16 rounded border border-border" />
            <Skeleton className="h-16 rounded border border-border" />
            <Skeleton className="h-16 rounded border border-border" />
            <Skeleton className="h-16 rounded border border-border" />
          </div>
        ) : remoteFailed ? (
          <EmptyState title={t('ui.session_list.remote_unreachable')} />
        ) : (
          <EmptyState title={t('ui.session_list.empty')} />
        )
      ) : (
        <ul className="flex flex-col gap-2">
          {visible.map((s) => {
            const badge = badgeFor(s.ToolID);
            const liveID = s.Path.startsWith('live:') ? s.Path.slice('live:'.length) : '';
            const liveChat = liveID ? chats.find((c) => c.ID === liveID) : undefined;
            const liveTerm = liveID ? terminals.find((term) => term.ID === liveID) : undefined;
            const opened = liveChat
              ? { ID: liveChat.ID, Status: liveChat.Status, kind: 'chat' as const }
              : liveTerm
                ? { ID: liveTerm.ID, Status: liveTerm.Status, kind: 'terminal' as const }
                : findOpenedForSession(s.ID, chats, terminals);
            const restored = !!liveID || isRestored(s.ID, chats, terminals);
            const active = selectedSessionID === s.ID;
            const title = displayTitle(s.Title);
            return (
              <li
                key={s.ID}
                className={cn(
                  LIST_ROW,
                  'cursor-pointer p-2',
                  selectMode && 'flex items-start gap-2',
                  active && LIST_ROW_ACTIVE,
                )}
                onClick={() => {
                  // 选择模式下磁盘行点击 = 切换勾选；live 行仍走 onSelectRow
                  if (selectMode && !liveID) {
                    toggleRow(s.ID);
                    return;
                  }
                  onSelectRow?.(s);
                }}
              >
                {selectMode && (
                  <input
                    type="checkbox"
                    className="mt-0.5 shrink-0 accent-primary"
                    aria-label={title || t('ui.session_list.untitled')}
                    checked={selected.has(s.ID)}
                    disabled={!!liveID}
                    onClick={(e) => e.stopPropagation()}
                    onChange={() => toggleRow(s.ID)}
                  />
                )}
                <div className="flex min-w-0 flex-1 flex-col gap-1.5">
                  <div className="flex min-w-0 items-center gap-1.5">
                    {showArchived && (
                      <span
                        className="inline-flex h-3.5 w-3.5 shrink-0 text-muted-foreground"
                        role="img"
                        aria-label={t('ui.session_list.archived')}
                        title={t('ui.session_list.archived')}
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
                          {title || t('ui.session_list.untitled')}
                        </span>
                      </TooltipTrigger>
                      <TooltipContent className="max-w-xs sm:max-w-sm" side="bottom" align="start">
                        <p className="whitespace-pre-wrap break-words text-xs leading-5">
                          {title || t('ui.session_list.untitled')}
                        </p>
                        <p className="mt-1 text-[10px] opacity-70">
                          {badge.label} · {formatRelativeTime(s.UpdatedAt)} · {t('ui.session_list.messages', { count: s.Messages })}
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
                    {/* 磁盘扫描的 cursor 会话标注激活方式：ACPOnly 仅能 ACP 激活，
                        其余可终端 resume；live 行无该字段不标。 */}
                    {s.ToolID === 'cursor' && typeof s.ACPOnly === 'boolean' && (
                      <span
                        data-testid="session-mode-badge"
                        className="shrink-0 rounded-sm bg-muted px-1 py-px text-[10px] font-medium uppercase tracking-wide text-muted-foreground"
                        title={t(s.ACPOnly ? 'ui.session_list.mode_acp' : 'ui.session_list.mode_tui')}
                      >
                        {s.ACPOnly ? 'ACP' : 'TUI'}
                      </span>
                    )}
                    <span className={`whitespace-nowrap ${MONO}`}>{formatRelativeTime(s.UpdatedAt)}</span>
                    <span className={`whitespace-nowrap ${MONO}`}>{t('ui.session_list.messages', { count: s.Messages })}</span>
                    {showArchived ? (
                      <button
                        type="button"
                        className="ml-auto inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-sm text-muted-foreground hover:bg-muted hover:text-primary"
                        aria-label={t('ui.session_list.restore')}
                        title={t('ui.session_list.restore_title')}
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
                    ) : (
                      <div className="ml-auto flex shrink-0 items-center">
                        {!liveID && (
                          <button
                            type="button"
                            className="inline-flex h-5 w-5 items-center justify-center rounded-sm text-muted-foreground hover:bg-muted hover:text-primary"
                            aria-label={t('ui.session_list.archive')}
                            title={t('ui.session_list.archive')}
                            onClick={(e) => {
                              e.stopPropagation();
                              onArchive?.(s);
                            }}
                          >
                            <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" aria-hidden="true">
                              <path
                                d="M2 3.2h12l-1 2.2H3L2 3.2Zm1.2 3h9.6V13H3.2V6.2Zm2.3 2.2v1.2h5V8.4h-5Z"
                                fill="currentColor"
                              />
                            </svg>
                          </button>
                        )}
                        {restored ? (
                          <span
                            className="inline-flex h-5 w-5 items-center justify-center text-primary"
                            role="img"
                            aria-label={t('ui.session_list.restored')}
                            title={t('ui.session_list.restored')}
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
                            className="inline-flex h-5 w-5 items-center justify-center rounded-sm text-muted-foreground hover:bg-muted hover:text-primary"
                            aria-label={t('ui.session_list.activate')}
                            title={t('ui.session_list.activate')}
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
                    )}
                  </div>
                </div>
              </li>
            );
          })}
        </ul>
      )}
      {/* 选择模式下常驻（含列表为空时）：主按钮因 selected.size===0 自然禁用 */}
      {selectMode && (
        <div className="sticky bottom-0 flex items-center gap-2 border-t border-border bg-card py-2">
          <Button size="sm" disabled={selected.size === 0} onClick={() => setConfirmOpen(true)}>
            {t(showArchived ? 'ui.session_list.batch_restore' : 'ui.session_list.batch_archive')}
          </Button>
          <Button size="sm" variant="ghost" onClick={exitSelectMode}>
            {t('ui.session_list.batch_cancel')}
          </Button>
        </div>
      )}
      </div>
      </div>
      <Dialog
        open={confirmOpen}
        onOpenChange={(next) => {
          if (!next) setConfirmOpen(false);
        }}
      >
        <DialogPrimitive.Title className="mb-1 text-sm font-medium">
          {t(
            showArchived
              ? 'ui.session_list.batch_restore_confirm_title'
              : 'ui.session_list.batch_archive_confirm_title',
          )}
        </DialogPrimitive.Title>
        <p className="mb-3 text-xs leading-5 text-muted-foreground">
          {t(
            showArchived
              ? 'ui.session_list.batch_restore_confirm_body'
              : 'ui.session_list.batch_archive_confirm_body',
            { count: confirmIds.length },
          )}
        </p>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" disabled={batchPending} onClick={() => setConfirmOpen(false)}>
            {t('ui.session_list.batch_cancel')}
          </Button>
          <Button size="sm" disabled={batchPending || confirmIds.length === 0} onClick={() => void runBatch()}>
            {t('ui.session_list.batch_confirm_ok')}
          </Button>
        </div>
      </Dialog>
    </TooltipProvider>
  );
}
