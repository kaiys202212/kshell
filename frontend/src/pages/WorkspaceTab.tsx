// 工作区页签：三栏布局（左右两栏宽度可拖动）。
//   左栏：会话列表
//   中栏：左钉「会话预览」+ 可关 agent/chat；右钉「文件」「终端」
//   右栏：文件树 | Git | SSH
// 终端页签一旦打开就常挂载（非激活用 hidden），xterm 缓冲与焦点不丢；
// 工作区页签本身也由 App 常挂载，因此只有关闭页签才会真正结束终端进程。
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  closeChat,
  closeTerminal,
  getTools,
  listChats,
  listTerminals,
  onScanDone,
  onToolsUpdated,
  openSession,
  openShellTerminal,
  openSSHTerminal,
  openWorkspace,
  archiveSession,
  restoreSession,
  scanSessions,
  writeTerminal,
} from '../lib/api';
import type { Session, SshConnection, TerminalInfo, ToolInfo } from '../lib/api';
import { encodeTerminalInput } from '../lib/base64';
import { appendChatInput } from '../lib/chatInputRegistry';
import { DRAG_MIME, quotePathForShell } from '../lib/dragPath';
import { backendError, translateBackend } from '../lib/errors';
import { badgeFor } from '../lib/toolBadge';
import { cn } from '../lib/cn';
import { displayTitle } from '../lib/title';
import { TAB_ACTIVE, TAB_BASE, TAB_UNDERLINE } from '../lib/ui';
import { sameWorkspacePath } from '../lib/workspacePath';
import FileTree from '../components/FileTree';
import FileTabsPane from '../components/FileTabsPane';
import GitPanel from '../components/GitPanel';
import type { GitDiffSpec } from '../components/GitPanel';
import PreviewToolPane from '../components/PreviewToolPane';
import ResizeHandle from '../components/ResizeHandle';
import SessionList from '../components/SessionList';
import SessionTranscript from '../components/SessionTranscript';
import SshPanel from '../components/SshPanel';
import TerminalView from '../components/TerminalView';
import AgentActivityIcon from '../components/AgentActivityIcon';
import ChatView from '../components/ChatView';
import NewSessionMenu from '../components/NewSessionMenu';
import { EmptyState } from '../components/ui/empty-state';
import { ToolDot } from '../components/ui/tool-dot';
import { resolveAgentActivity } from '../state/agentActivity';
import { LAYOUT_DEFAULT, useAppStore } from '../state/store';
import type { WorkspaceTab } from '../state/store';
import {
  emptyFileTabs,
  openPinned,
  openPreview,
  commitDiffTabPath,
  diffTabPath,
} from '../lib/fileTabs';
import { OPEN_FILE_EVENT } from '../lib/openHref';

function isAgentTerm(t: TerminalInfo): boolean {
  return t.Kind === 'session' || t.Kind === 'new';
}

function isToolTerm(t: TerminalInfo): boolean {
  return t.Kind === 'shell' || t.Kind === 'ssh';
}

type RightPane = 'files' | 'git' | 'ssh';

// 中心区钉住页签（与终端 id 如 t1 不冲突）
const SESSION_TAB = 'session-preview';
const FILES_TAB = 'files';
const TERMINALS_TAB = 'terminals';
const PINNED_CENTER = new Set([SESSION_TAB, FILES_TAB, TERMINALS_TAB]);

// 新建会话后触发后台重扫的延迟序列（毫秒，相对新建时刻）。
// 工具自己的会话记录是它启动后才落盘的（opencode 先起 TUI 再写 SQLite；Claude 常要等首条消息），
// 单次 3s 经常查不到，会表现为「再新建下一个时上一个才进列表」。多档退避覆盖落盘窗口。
const NEW_SESSION_RESCAN_DELAYS_MS = [3000, 8000, 15000];

// 右栏「文件 | SSH」子页签：扁平下划线式
const paneTabBase = `${TAB_BASE} h-7 text-xs`;
const paneTabActive = TAB_ACTIVE;

// 中心区页签：与标题栏同语言（下划线激活），终端页签带工具徽标与关闭键
const centerTabBase = `group ${TAB_BASE} h-7 max-w-56 text-xs`;
const centerTabActive = TAB_ACTIVE;

export default function WorkspaceTabView({ tab, visible }: { tab: WorkspaceTab; visible: boolean }) {
  const { t: tr } = useTranslation();
  const [rightPane, setRightPane] = useState<RightPane>('files');
  const [centerTab, setCenterTab] = useState<string>(SESSION_TAB);
  const [toolSubTab, setToolSubTab] = useState('');
  const [previewSession, setPreviewSession] = useState<Session | null>(null);
  const [fileTabs, setFileTabs] = useState(emptyFileTabs);
  const [fileDirty, setFileDirty] = useState<Record<string, boolean>>({});
  const [tools, setTools] = useState<ToolInfo[]>([]);
  const [busy, setBusy] = useState(false);
  const [showArchived, setShowArchived] = useState(false);
  // 新建会话后的延迟重扫定时器（卸载/再次新建时清掉，避免重复触发）
  const rescanTimers = useRef<number[]>([]);

  const layout = useAppStore((s) => s.layout);
  const setLayout = useAppStore((s) => s.setLayout);
  const terminals = useAppStore((s) => s.terminals);
  const chats = useAppStore((s) => s.chats);
  const chatPermissions = useAppStore((s) => s.chatPermissions);
  const terminalBusy = useAppStore((s) => s.terminalBusy);
  const notify = useAppStore((s) => s.notify);
  // 通知气泡的「切到对应页签」请求（见 store.requestFocusTerm）
  const focusTermKey = useAppStore((s) => s.focusTermKey);
  // 新建会话的工具选择：全局持久化（'' = 自动），跨页签/重启记住用户的选择
  const toolId = useAppStore((s) => s.newSessionTool);
  const setToolId = useAppStore((s) => s.setNewSessionTool);

  const selectCenterTab = useCallback((next: string) => {
    setCenterTab(next);
  }, []);

  // 本工作区全部内嵌终端（按创建顺序）
  const allTerms = useMemo<TerminalInfo[]>(
    () => terminals.filter((t) => sameWorkspacePath(t.Workspace, tab.id)),
    [terminals, tab.id],
  );
  // agent 终端进左侧中心区页签；shell/ssh 进预览区子页签
  const terms = useMemo(() => allTerms.filter(isAgentTerm), [allTerms]);
  const toolTerms = useMemo(() => allTerms.filter(isToolTerm), [allTerms]);

  // 本工作区的聊天会话（按创建顺序）
  const chatsForWs = useMemo(
    () => chats.filter((c) => sameWorkspacePath(c.Workspace, tab.id)),
    [chats, tab.id],
  );

  const selectedSessionID = useMemo(() => {
    if (centerTab === SESSION_TAB) {
      return previewSession?.ID ?? null;
    }
    const t = terms.find((x) => x.ID === centerTab);
    if (t?.SessionID) return t.SessionID;
    if (t?.Prompted) return `live:${t.ID}`;
    const c = chatsForWs.find((x) => x.ID === centerTab);
    if (c?.SessionID) return c.SessionID;
    if (c?.Prompted) return `live:${c.ID}`;
    return null;
  }, [centerTab, previewSession, terms, chatsForWs]);

  useEffect(() => {
    // 挂载时补一次镜像：页签是常挂载的，但终端可能在别的工作区页签里被创建
    listTerminals()
      .then((list) => useAppStore.getState().setTerminals(list))
      .catch(() => {});
  }, []);

  useEffect(() => {
    // 工具列表只在挂载时取一次（用于「选择 agent」下拉），而扫描是异步的：
    // 必须订阅 scan:done 再取一次，否则挂载早于扫描完成时下拉会一直禁用，
    // 表现为「没有选择 agent 的选项」。
    const refresh = () => {
      getTools()
        .then((list) => setTools(list.filter((t) => t.Installed && t.BinPath !== '')))
        .catch(() => {});
    };
    refresh();
    // 扫描完成后重取终端/聊天镜像：Go 侧会把新发现的会话回填到新建终端/聊天
    // （SessionID/真实标题），页签标题与「恢复/切换」判断都依赖这份镜像
    const refreshMirrors = () => {
      listTerminals()
        .then((list) => useAppStore.getState().setTerminals(list))
        .catch(() => {});
      listChats()
        .then((list) => useAppStore.getState().setChats(list))
        .catch(() => {});
    };
    const offScan = onScanDone(() => {
      refresh();
      refreshMirrors();
    });
    const offTools = onToolsUpdated(() => {
      refresh();
    });
    return () => {
      offScan();
      offTools();
    };
  }, []);

  useEffect(() => {
    // 当前中心区页签指向的终端/聊天已不存在时退回会话预览
    if (
      !PINNED_CENTER.has(centerTab) &&
      !terms.some((t) => t.ID === centerTab) &&
      !chatsForWs.some((c) => c.ID === centerTab)
    ) {
      selectCenterTab(SESSION_TAB);
    }
  }, [terms, chatsForWs, centerTab, selectCenterTab]);

  useEffect(() => {
    // 消费通知气泡的页签聚焦请求：本工作区持有该 termKey（终端/聊天 manager 的
    // key，Info.Key）就选中对应中心区页签并按 seq 清除；其他工作区视图不消费。
    // 挂载时也会执行一次，覆盖「点击时工作区页签尚未打开」的情况。
    if (!focusTermKey) return;
    const { termKey, seq } = focusTermKey;
    const t = terms.find((x) => x.Key === termKey);
    const c = t ? undefined : chatsForWs.find((x) => x.Key === termKey);
    if (!t && !c) return;
    selectCenterTab(t ? t.ID : c!.ID);
    useAppStore.getState().clearFocusTerm(seq);
  }, [focusTermKey, terms, chatsForWs, selectCenterTab]);

  useEffect(() => {
    if (toolSubTab && !toolTerms.some((t) => t.ID === toolSubTab)) {
      setToolSubTab(toolTerms[0]?.ID ?? '');
    }
  }, [toolTerms, toolSubTab]);

  const closeSessionPreview = useCallback(() => {
    setPreviewSession(null);
  }, []);

  // 恢复历史会话：优先走 ACP 聊天，Go 侧按可用性决定聊天或回退终端
  const openChatOrTerminal = useCallback(
    (s: Session) => {
      void openSession(s.ID)
        .then((res) => {
          closeSessionPreview();
          if (res.Kind === 'chat' && res.Chat) {
            useAppStore.getState().upsertChat(res.Chat);
            selectCenterTab(res.Chat.ID);
          } else if (res.Terminal) {
            useAppStore.getState().upsertTerminal(res.Terminal);
            selectCenterTab(res.Terminal.ID);
          }
          if (res.Fallback) notify(tr('ui.workspace.fallback', { reason: translateBackend(res.Fallback) }), 'info');
        })
        .catch((e: unknown) => {
          notify(tr('ui.workspace.open_session_failed', { err: backendError(e) }), 'error');
        });
    },
    [notify, selectCenterTab, closeSessionPreview, tr],
  );

  const handleSelectSessionRow = useCallback(
    (s: Session) => {
      const chatHit = chatsForWs.filter((c) => c.SessionID === s.ID);
      const termHit = terms.filter((t) => t.SessionID === s.ID);
      const live =
        chatHit.find((x) => x.Status !== 'exited') ??
        termHit.find((x) => x.Status !== 'exited') ??
        null;
      if (live) {
        selectCenterTab(live.ID);
        return;
      }
      setPreviewSession(s);
      selectCenterTab(SESSION_TAB);
    },
    [chatsForWs, terms, selectCenterTab],
  );

  useEffect(
    () => () => {
      for (const id of rescanTimers.current) window.clearTimeout(id);
      rescanTimers.current = [];
    },
    [],
  );

  // 菜单里选中 agent 后启动会话（空 id = 交给 Go 侧按该工作区最常用的工具选）
  const startSession = async (id: string) => {
    if (busy) return;
    setBusy(true);
    try {
      const res = await openWorkspace(tab.id, id);
      if (res.Kind === 'chat' && res.Chat) {
        useAppStore.getState().upsertChat(res.Chat);
        selectCenterTab(res.Chat.ID);
      } else if (res.Terminal) {
        useAppStore.getState().upsertTerminal(res.Terminal);
        selectCenterTab(res.Terminal.ID);
      }
      if (res.Fallback) notify(tr('ui.workspace.fallback', { reason: translateBackend(res.Fallback) }), 'info');
      // 新会话要过一会儿才落进工具自己的会话存储；按退避多扫几次，避免单次过早/撞车
      for (const tid of rescanTimers.current) window.clearTimeout(tid);
      rescanTimers.current = NEW_SESSION_RESCAN_DELAYS_MS.map((delay) =>
        window.setTimeout(() => {
          void scanSessions();
        }, delay),
      );
    } catch (e: unknown) {
      notify(tr('ui.workspace.new_session_failed', { err: backendError(e) }), 'error');
    } finally {
      setBusy(false);
    }
  };

  const handleCloseTerminal = (id: string) => {
    useAppStore.getState().removeTerminal(id);
    closeTerminal(id).catch(() => {});
    if (centerTab === id) selectCenterTab(SESSION_TAB);
    if (toolSubTab === id) setToolSubTab(toolTerms.find((t) => t.ID !== id)?.ID ?? '');
  };

  const handleCloseChat = (id: string) => {
    useAppStore.getState().removeChat(id);
    closeChat(id).catch(() => {});
    if (centerTab === id) selectCenterTab(SESSION_TAB);
  };

  const openFile = (path: string) => {
    setFileTabs((s) => openPreview(s, path));
    selectCenterTab(FILES_TAB);
  };

  useEffect(() => {
    const onOpen = (e: Event) => {
      const d = (e as CustomEvent<{ workspace?: string; path?: string }>).detail;
      if (!d?.path || !d.workspace) return;
      if (!sameWorkspacePath(d.workspace, tab.id)) return;
      openFile(d.path);
    };
    window.addEventListener(OPEN_FILE_EVENT, onOpen);
    return () => window.removeEventListener(OPEN_FILE_EVENT, onOpen);
  }, [tab.id]);

  const editFile = (path: string) => {
    setFileTabs((s) => openPinned(s, path));
    selectCenterTab(FILES_TAB);
  };

  const openGitDiff = (spec: GitDiffSpec) => {
    const key =
      spec.kind === 'commit'
        ? commitDiffTabPath(spec.repoRel, spec.hash)
        : diffTabPath(spec.side, spec.repoRel, spec.path);
    setFileTabs((s) => (spec.preview ? openPreview(s, key, 'diff') : openPinned(s, key, 'diff')));
    selectCenterTab(FILES_TAB);
  };

  const handleNewShell = () => {
    void openShellTerminal(tab.id, 80, 24)
      .then((info) => {
        useAppStore.getState().upsertTerminal(info);
        selectCenterTab(TERMINALS_TAB);
        setToolSubTab(info.ID);
      })
      .catch((e: unknown) => {
        notify(tr('ui.workspace.open_terminal_failed', { err: backendError(e) }), 'error');
      });
  };

  const handleOpenRemote = (c: SshConnection) => {
    void openSSHTerminal(c.ID, 80, 24)
      .then((info) => {
        useAppStore.getState().upsertTerminal(info);
        selectCenterTab(TERMINALS_TAB);
        setToolSubTab(info.ID);
      })
      .catch((e: unknown) => {
        notify(tr('ui.workspace.open_ssh_failed', { err: backendError(e) }), 'error');
      });
  };

  return (
    <div className="flex min-h-0 flex-1">
      <aside
        className="flex shrink-0 flex-col gap-2 overflow-y-auto border-r border-border bg-card p-2.5"
        style={{ width: layout.left }}
        aria-label={tr('ui.workspace.session_sidebar_aria')}
      >
        {/* 「新建会话」本身就是下拉菜单：点开列 agent，选中即启动（不再并排一个工具下拉框） */}
        <div className="flex items-center gap-2">
          <div className="min-w-0 flex-1">
            <NewSessionMenu
              tools={tools}
              value={toolId}
              onChange={setToolId}
              onSelect={(id) => void startSession(id)}
              disabled={busy}
            />
          </div>
          <label className="flex shrink-0 items-center gap-1 text-xs text-muted-foreground">
            <input
              type="checkbox"
              className="accent-primary"
              checked={showArchived}
              aria-label={tr('ui.workspace.show_archived_aria')}
              onChange={(e) => setShowArchived(e.target.checked)}
            />
            {tr('ui.workspace.archived')}
          </label>
        </div>
        {tools.length === 0 && (
          <p className="text-xs text-muted-foreground">
            {tr('ui.workspace.no_agent_hint')}
          </p>
        )}
        <SessionList
          workspacePath={tab.id}
          selectedSessionID={selectedSessionID}
          showArchived={showArchived}
          onSelectRow={(s) => {
            if (s.Path.startsWith('live:')) {
              selectCenterTab(s.Path.slice('live:'.length));
              return;
            }
            handleSelectSessionRow(s);
          }}
          onActivate={openChatOrTerminal}
          onArchive={(s) => {
            void archiveSession(s.ID).then(() => {
              const { archivedIDs, setArchivedIDs } = useAppStore.getState();
              if (archivedIDs.includes(s.ID)) return;
              setArchivedIDs([...archivedIDs, s.ID]);
            });
          }}
          onRestore={(s) => {
            void restoreSession(s.ID).then(() => {
              // 名单以 archive:changed 为准；这里先从本地名单拿掉，避免等事件期间还留在归档视图
              const { archivedIDs, setArchivedIDs } = useAppStore.getState();
              setArchivedIDs(archivedIDs.filter((id) => id !== s.ID));
            });
          }}
        />
      </aside>

      <ResizeHandle
        side="left"
        width={layout.left}
        onResize={(w) => setLayout({ left: w })}
        defaultWidth={LAYOUT_DEFAULT.left}
        label={tr('ui.workspace.resize_sessions')}
      />

      <main className="flex min-w-0 flex-1 flex-col">
        {/* 中心区页签条：左钉会话预览 + 可关会话；右钉文件 | 终端 */}
        <div
          className="flex shrink-0 items-stretch border-b border-border"
          role="tablist"
          aria-label={tr('ui.workspace.center_tabs_aria')}
        >
          <div className="flex min-w-0 flex-1 items-stretch overflow-x-auto">
          <button
            role="tab"
            aria-selected={centerTab === SESSION_TAB}
            className={cn(centerTabBase, centerTab === SESSION_TAB && centerTabActive)}
            onClick={() => selectCenterTab(SESSION_TAB)}
          >
            {tr('ui.workspace.session_preview')}
            {centerTab === SESSION_TAB && <span className={TAB_UNDERLINE} />}
          </button>
          {terms.map((t) => {
            const badge = badgeFor(t.ToolID);
            const active = centerTab === t.ID;
            // 渲染层再洗一次：Cursor 包装标签 / OSC 查色残片等；洗净后为空则用占位，勿回退原文
            const label = displayTitle(t.Title) || tr('ui.workspace.new_session');
            const activity = resolveAgentActivity({
              status: t.Status,
              hasPermission: false,
              kind: 'terminal',
              busy: !!terminalBusy[t.ID],
            });
            return (
              <div
                key={t.ID}
                className={cn(centerTabBase, active && centerTabActive)}
                // 整条页签可点（标题右侧的工具徽标/留白此前点不动，只有标题按钮响应）
                onClick={() => selectCenterTab(t.ID)}
                onAuxClick={(e) => {
                  if (e.button === 1) {
                    e.preventDefault();
                    handleCloseTerminal(t.ID);
                  }
                }}
                // 拖文件到未激活页签标题：先切页签再插入，落点反馈与结果一致
                onDragOver={(e) => {
                  if (e.dataTransfer.types.includes(DRAG_MIME)) e.preventDefault();
                }}
                onDrop={(e) => {
                  const p = e.dataTransfer.getData(DRAG_MIME);
                  if (!p) return;
                  e.preventDefault();
                  e.stopPropagation();
                  selectCenterTab(t.ID); // 先切页签，插入后用户立即看到
                  if (t.Status !== 'exited') void writeTerminal(t.ID, encodeTerminalInput(quotePathForShell(p)));
                }}
              >
                <AgentActivityIcon activity={activity} />
                <button
                  role="tab"
                  aria-selected={active}
                  className="min-w-0 truncate text-xs"
                  title={t.ToolID ? tr('ui.workspace.tab_title_tool', { label, tool: badge.label }) : label}
                >
                  {label}
                </button>
                {/* 中栏 agent 页签徽标只留图标，工具名见悬停 title */}
                {t.ToolID && <ToolDot toolID={t.ToolID} className="shrink-0" showLabel={false} />}
                <button
                  className={cn(
                    'ml-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-sm text-sm leading-none text-muted-foreground transition-opacity hover:bg-muted hover:text-foreground',
                    active ? 'opacity-70 hover:opacity-100' : 'opacity-0 group-hover:opacity-100',
                  )}
                  aria-label={tr('ui.workspace.close_terminal', { label })}
                  onClick={(e) => {
                    e.stopPropagation(); // 只关，不顺带切到/切走该页签
                    handleCloseTerminal(t.ID);
                  }}
                >
                  ×
                </button>
                {active && <span className={TAB_UNDERLINE} />}
              </div>
            );
          })}
          {chatsForWs.map((c) => {
            const active = centerTab === c.ID;
            const label = displayTitle(c.Title) || tr('ui.workspace.new_session');
            const activity = resolveAgentActivity({
              status: c.Status,
              hasPermission: !!chatPermissions[c.ID],
              kind: 'chat',
            });
            return (
              <div
                key={c.ID}
                className={cn(centerTabBase, active && centerTabActive)}
                // 整条页签可点（标题右侧的徽标/留白也响应）
                onClick={() => selectCenterTab(c.ID)}
                onAuxClick={(e) => {
                  if (e.button === 1) {
                    e.preventDefault();
                    handleCloseChat(c.ID);
                  }
                }}
                // 同终端页签：拖文件到聊天页签标题先切换，再投递到输入框草稿
                onDragOver={(e) => {
                  if (e.dataTransfer.types.includes(DRAG_MIME)) e.preventDefault();
                }}
                onDrop={(e) => {
                  const p = e.dataTransfer.getData(DRAG_MIME);
                  if (!p) return;
                  e.preventDefault();
                  e.stopPropagation();
                  selectCenterTab(c.ID);
                  appendChatInput(c.ID, quotePathForShell(p));
                }}
              >
                <AgentActivityIcon activity={activity} />
                <button
                  role="tab"
                  aria-selected={active}
                  className="min-w-0 truncate text-xs"
                  title={label}
                >
                  {label}
                </button>
                {/* 聊天标题通常已含工具名，徽标只留图标避免重复 */}
                {c.ToolID && <ToolDot toolID={c.ToolID} className="shrink-0" showLabel={false} />}
                <button
                  className={cn(
                    'ml-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-sm text-sm leading-none text-muted-foreground transition-opacity hover:bg-muted hover:text-foreground',
                    active ? 'opacity-70 hover:opacity-100' : 'opacity-0 group-hover:opacity-100',
                  )}
                  aria-label={tr('ui.workspace.close_chat', { label })}
                  onClick={(e) => {
                    e.stopPropagation(); // 只关，不顺带切到/切走该页签
                    handleCloseChat(c.ID);
                  }}
                >
                  ×
                </button>
                {active && <span className={TAB_UNDERLINE} />}
              </div>
            );
          })}
          </div>
          <button
            role="tab"
            aria-selected={centerTab === FILES_TAB}
            className={cn(
              centerTabBase,
              'ml-auto shrink-0 border-l border-border bg-muted/40 text-muted-foreground',
              centerTab === FILES_TAB && cn(centerTabActive, 'bg-muted/70'),
            )}
            onClick={() => selectCenterTab(FILES_TAB)}
          >
            {tr('ui.workspace.files_tab')}
            {centerTab === FILES_TAB && <span className={TAB_UNDERLINE} />}
          </button>
          <button
            role="tab"
            aria-selected={centerTab === TERMINALS_TAB}
            aria-label={tr('ui.workspace.terminals_aria')}
            className={cn(
              centerTabBase,
              'shrink-0 border-l border-border bg-muted/40 text-muted-foreground',
              centerTab === TERMINALS_TAB && cn(centerTabActive, 'bg-muted/70'),
            )}
            onClick={() => selectCenterTab(TERMINALS_TAB)}
          >
            {tr('ui.workspace.terminals_tab')}
            {toolTerms.length > 0 && (
              <span className="rounded-sm bg-muted px-1 font-mono text-[10px] text-muted-foreground">
                {toolTerms.length}
              </span>
            )}
            {centerTab === TERMINALS_TAB && <span className={TAB_UNDERLINE} />}
          </button>
        </div>

        <div className="min-h-0 flex-1 overflow-hidden">
          {/* 切页签时的淡入：动画挂在各内容包裹层上——hidden 切 display 会重放动画，
              因此无需 key 重挂（重挂会丢 xterm 缓冲，违背「终端常挂载」约定） */}
          <div
            className={cn('h-full', centerTab !== SESSION_TAB && 'hidden')}
            style={{ animation: 'kshell-fade-in var(--duration-fast) var(--ease-out)' }}
          >
            {previewSession ? (
              <SessionTranscript
                sessionID={previewSession.ID}
                workspaceRoot={previewSession.Workspace}
                title={displayTitle(previewSession.Title) || tr('ui.workspace.new_session')}
                onActivate={() => openChatOrTerminal(previewSession)}
              />
            ) : (
              <EmptyState title={tr('ui.workspace.pick_session')} />
            )}
          </div>
          <div
            className={cn('h-full', centerTab !== FILES_TAB && 'hidden')}
            style={{ animation: 'kshell-fade-in var(--duration-fast) var(--ease-out)' }}
          >
            <FileTabsPane
              wsPath={tab.id}
              state={fileTabs}
              dirty={fileDirty}
              onChange={setFileTabs}
              onDirty={(path, d) => setFileDirty((prev) => ({ ...prev, [path]: d }))}
            />
          </div>
          <div
            className={cn('h-full', centerTab !== TERMINALS_TAB && 'hidden')}
            style={{ animation: 'kshell-fade-in var(--duration-fast) var(--ease-out)' }}
          >
            <PreviewToolPane
              terms={toolTerms}
              active={visible && centerTab === TERMINALS_TAB}
              subTab={toolSubTab}
              onSubTab={setToolSubTab}
              onCloseTerminal={handleCloseTerminal}
              onNewShell={handleNewShell}
            />
          </div>
          {terms.map((t) => (
            <div
              key={t.ID}
              className={cn('h-full', centerTab !== t.ID && 'hidden')}
              style={{ animation: 'kshell-fade-in var(--duration-fast) var(--ease-out)' }}
            >
              <TerminalView term={t} active={visible && centerTab === t.ID} />
            </div>
          ))}
          {chatsForWs.map((c) => (
            <div
              key={c.ID}
              className={cn('h-full', centerTab !== c.ID && 'hidden')}
              style={{ animation: 'kshell-fade-in var(--duration-fast) var(--ease-out)' }}
            >
              <ChatView chat={c} active={visible && centerTab === c.ID} />
            </div>
          ))}
        </div>
      </main>

      <ResizeHandle
        side="right"
        width={layout.right}
        onResize={(w) => setLayout({ right: w })}
        defaultWidth={LAYOUT_DEFAULT.right}
        label={tr('ui.workspace.resize_files')}
      />

      <aside
        className="flex shrink-0 flex-col overflow-y-auto border-l border-border bg-card p-2.5"
        style={{ width: layout.right }}
        aria-label={tr('ui.workspace.right_panel_aria')}
      >
        <div className="mb-2 flex gap-0.5 border-b border-border">
          <button
            className={cn(paneTabBase, rightPane === 'files' && paneTabActive)}
            aria-pressed={rightPane === 'files'}
            onClick={() => setRightPane('files')}
          >
            {tr('ui.workspace.files_tab')}
          </button>
          <button
            className={cn(paneTabBase, rightPane === 'git' && paneTabActive)}
            aria-pressed={rightPane === 'git'}
            onClick={() => setRightPane('git')}
          >
            Git
          </button>
          <button
            className={cn(paneTabBase, rightPane === 'ssh' && paneTabActive)}
            aria-pressed={rightPane === 'ssh'}
            onClick={() => setRightPane('ssh')}
          >
            SSH
          </button>
        </div>
        {/* 三面板常挂载，仅用 hidden 切换显示 */}
        <div className={cn('min-h-0 flex-1', rightPane !== 'files' && 'hidden')}>
          <FileTree wsPath={tab.id} onOpenFile={openFile} onEditFile={editFile} />
        </div>
        <div className={cn('min-h-0 flex-1 overflow-y-auto', rightPane !== 'git' && 'hidden')}>
          <GitPanel wsPath={tab.id} visible={visible && rightPane === 'git'} onOpenDiff={openGitDiff} />
        </div>
        <div className={cn('min-h-0 flex-1', rightPane !== 'ssh' && 'hidden')}>
          <SshPanel wsPath={tab.id} onOpenRemote={handleOpenRemote} />
        </div>
      </aside>
    </div>
  );
}
