// 工作区页签：三栏布局（左右两栏宽度可拖动）。
//   左栏：会话列表
//   中栏：布局模式可配（store.layoutMode，全局持久化）：
//     tabs  — 单区多页签：左钉「会话预览」+ 可关 agent/chat；右钉「文件」「终端」（默认）
//     split — 左右分割：左半 agent（预览+会话页签），右半「文件」「终端」，中间可拖
//   右栏：文件树 | Git | SSH | Terminal（运行中的 shell/ssh）
// 终端页签一旦打开就常挂载（非激活用 hidden），xterm 缓冲与焦点不丢；
// 工作区页签本身也由 App 常挂载，因此只有关闭页签才会真正结束终端进程。
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  closeChat,
  closeTerminal,
  getTools,
  isSSHWorkspaceRef,
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
import { quotePathForShell } from '../lib/dragPath';
import { backendError, translateBackend } from '../lib/errors';
import { badgeFor } from '../lib/toolBadge';
import { usableForSessionMenu } from '../lib/toolUsable';
import { cn } from '../lib/cn';
import {
  loadCenterTabOrder,
  mergeCenterTabOrder,
  saveCenterTabOrder,
} from '../lib/centerTabOrder';
import { displayTitle } from '../lib/title';
import { TAB_ACTIVE, TAB_BASE } from '../lib/ui';
import { sameWorkspacePath } from '../lib/workspacePath';
import { LAYOUT_CENTER_DEFAULT, LAYOUT_CENTER_MAX, LAYOUT_CENTER_MIN } from '../state/store';
import CenterSessionTabs, { type CenterSessionTabItem } from '../components/CenterSessionTabs';
import FileTree from '../components/FileTree';
import FileTabsPane from '../components/FileTabsPane';
import GitPanel from '../components/GitPanel';
import type { GitDiffSpec } from '../components/GitPanel';
import PreviewToolPane from '../components/PreviewToolPane';
import ResizeHandle from '../components/ResizeHandle';
import SessionList from '../components/SessionList';
import SessionTranscript from '../components/SessionTranscript';
import ActiveTerminalsPanel from '../components/ActiveTerminalsPanel';
import SshPanel from '../components/SshPanel';
import TerminalView from '../components/TerminalView';
import ChatView from '../components/ChatView';
import LoginDialog from '../components/LoginDialog';
import NewSessionMenu from '../components/NewSessionMenu';
import { Button } from '../components/ui/button';
import { EmptyState } from '../components/ui/empty-state';
import { resolveAgentActivity } from '../state/agentActivity';
import { LAYOUT_DEFAULT, useAppStore } from '../state/store';
import type { WorkspaceTab } from '../state/store';
import {
  emptyFileTabs,
  openPinned,
  openPreview,
  browserTabPath,
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

type RightPane = 'files' | 'git' | 'ssh' | 'terminal';

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

export default function WorkspaceTabView({ tab, visible }: { tab: WorkspaceTab; visible: boolean }) {
  const { t: tr } = useTranslation();
  const [rightPane, setRightPane] = useState<RightPane>('files');
  // 中心区活跃页签按「半区」记录：左半（agent 类）与右半（文件/终端）各持一份，
  // 多页签模式用 lastSide 派生单一活跃页签——两种模式间切换无需迁移状态。
  const [leftTab, setLeftTab] = useState<string>(SESSION_TAB);
  const [rightTab, setRightTab] = useState<string>(FILES_TAB);
  const [lastSide, setLastSide] = useState<'left' | 'right'>('left');
  const [toolSubTab, setToolSubTab] = useState('');
  const [previewSession, setPreviewSession] = useState<Session | null>(null);
  const [fileTabs, setFileTabs] = useState(emptyFileTabs);
  const [fileDirty, setFileDirty] = useState<Record<string, boolean>>({});
  const [tools, setTools] = useState<ToolInfo[]>([]);
  const [busy, setBusy] = useState(false);
  const [showArchived, setShowArchived] = useState(false);
  // 新建会话后的延迟重扫定时器（卸载/再次新建时清掉，避免重复触发）
  const rescanTimers = useRef<number[]>([]);
  // 未登录降级提示条：Fallback 携带裸 wire key err.launch.not_logged_in 时出现，
  // 带「去登录」按钮打开登录弹窗（notify 系统不支持按钮，走内嵌提示条）
  const [loginPrompt, setLoginPrompt] = useState<{ toolID: string; toolName: string } | null>(null);
  const [loginOpen, setLoginOpen] = useState(false);

  const layout = useAppStore((s) => s.layout);
  const setLayout = useAppStore((s) => s.setLayout);
  const layoutMode = useAppStore((s) => s.layoutMode);
  const setLayoutMode = useAppStore((s) => s.setLayoutMode);
  const terminals = useAppStore((s) => s.terminals);
  const chats = useAppStore((s) => s.chats);
  const chatPermissions = useAppStore((s) => s.chatPermissions);
  const terminalBusy = useAppStore((s) => s.terminalBusy);
  const notify = useAppStore((s) => s.notify);

  // 多页签模式下的活跃页签：跟随最后点击的半区
  const centerTab = lastSide === 'left' ? leftTab : rightTab;
  const selectCenterTab = useCallback((next: string) => {
    // 文件/终端钉归右半，其余（预览钉、终端/聊天 id）归左半
    if (next === FILES_TAB || next === TERMINALS_TAB) {
      setRightTab(next);
      setLastSide('right');
    } else {
      setLeftTab(next);
      setLastSide('left');
    }
  }, []);
  const toggleLayout = useCallback(() => {
    setLayoutMode(layoutMode === 'split' ? 'tabs' : 'split');
  }, [layoutMode, setLayoutMode]);
  // 通知气泡的「切到对应页签」请求（见 store.requestFocusTerm）
  const focusTermKey = useAppStore((s) => s.focusTermKey);
  // 新建会话的工具选择：全局持久化（'' = 自动），跨页签/重启记住用户的选择
  const toolId = useAppStore((s) => s.newSessionTool);
  const setToolId = useAppStore((s) => s.setNewSessionTool);

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

  // 中心区会话页签顺序：localStorage 持久化；新建追加末尾，拖动后写回
  const liveSessionIDs = useMemo(
    () => [...terms.map((t) => t.ID), ...chatsForWs.map((c) => c.ID)],
    [terms, chatsForWs],
  );
  const [savedTabOrder, setSavedTabOrder] = useState(() => loadCenterTabOrder(tab.id));
  const sessionTabOrder = useMemo(
    () => mergeCenterTabOrder(savedTabOrder, liveSessionIDs),
    [savedTabOrder, liveSessionIDs],
  );
  useEffect(() => {
    saveCenterTabOrder(tab.id, sessionTabOrder);
  }, [tab.id, sessionTabOrder]);

  const centerSessionItems = useMemo<CenterSessionTabItem[]>(() => {
    const out: CenterSessionTabItem[] = [];
    for (const t of terms) {
      const badge = badgeFor(t.ToolID);
      const label = displayTitle(t.Title) || tr('ui.workspace.new_session');
      out.push({
        id: t.ID,
        kind: 'terminal',
        label,
        toolID: t.ToolID,
        activity: resolveAgentActivity({
          status: t.Status,
          hasPermission: false,
          kind: 'terminal',
          busy: !!terminalBusy[t.ID],
        }),
        titleAttr: t.ToolID ? tr('ui.workspace.tab_title_tool', { label, tool: badge.label }) : label,
        exited: t.Status === 'exited',
      });
    }
    for (const c of chatsForWs) {
      const label = displayTitle(c.Title) || tr('ui.workspace.new_session');
      out.push({
        id: c.ID,
        kind: 'chat',
        label,
        toolID: c.ToolID,
        activity: resolveAgentActivity({
          status: c.Status,
          hasPermission: !!chatPermissions[c.ID],
          kind: 'chat',
        }),
        titleAttr: label,
      });
    }
    return out;
  }, [terms, chatsForWs, terminalBusy, chatPermissions, tr]);

  // 会话列表选中态跟随左半（agent）活跃页签，右半切文件不影响高亮
  const selectedSessionID = useMemo(() => {
    if (leftTab === SESSION_TAB) {
      return previewSession?.ID ?? null;
    }
    const t = terms.find((x) => x.ID === leftTab);
    if (t?.SessionID) return t.SessionID;
    if (t?.Prompted) return `live:${t.ID}`;
    const c = chatsForWs.find((x) => x.ID === leftTab);
    if (c?.SessionID) return c.SessionID;
    if (c?.Prompted) return `live:${c.ID}`;
    return null;
  }, [leftTab, previewSession, terms, chatsForWs]);

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
    // 终端/聊天镜像不在此刷新：那是全局 store，App 层统一订阅 scan:done，
    // 每个页签各自订阅会对同一事件产生 N 倍重复 IPC。
    const refresh = () => {
      getTools()
        // 统一口径见 lib/toolUsable：确定未登录（Auth=out）的工具不在下拉出现，
        // 登录入口收敛到设置页工具分区。
        .then((list) => setTools(list.filter(usableForSessionMenu)))
        .catch(() => {});
    };
    refresh();
    const offScan = onScanDone(() => {
      refresh();
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
    // 左半活跃页签指向的终端/聊天已不存在时退回会话预览（右半只有钉住页签，无需回落）
    if (
      !PINNED_CENTER.has(leftTab) &&
      !terms.some((t) => t.ID === leftTab) &&
      !chatsForWs.some((c) => c.ID === leftTab)
    ) {
      setLeftTab(SESSION_TAB);
    }
  }, [terms, chatsForWs, leftTab]);

  useEffect(() => {
    // 消费通知气泡 / 右栏 Terminal 的页签聚焦请求：本工作区持有该 termKey
    // （终端/聊天 manager 的 key，Info.Key）就选中对应页签并按 seq 清除；
    // shell/ssh 落在预览区「终端」钉住页签的子页签。其他工作区视图不消费。
    if (!focusTermKey) return;
    const { termKey, seq } = focusTermKey;
    const t = terms.find((x) => x.Key === termKey);
    const c = t ? undefined : chatsForWs.find((x) => x.Key === termKey);
    const tool = !t && !c ? toolTerms.find((x) => x.Key === termKey) : undefined;
    if (!t && !c && !tool) return;
    if (tool) {
      selectCenterTab(TERMINALS_TAB);
      setToolSubTab(tool.ID);
    } else {
      selectCenterTab(t ? t.ID : c!.ID);
    }
    useAppStore.getState().clearFocusTerm(seq);
  }, [focusTermKey, terms, chatsForWs, toolTerms, selectCenterTab]);

  useEffect(() => {
    if (toolSubTab && !toolTerms.some((t) => t.ID === toolSubTab)) {
      setToolSubTab(toolTerms[0]?.ID ?? '');
    }
  }, [toolTerms, toolSubTab]);

  const closeSessionPreview = useCallback(() => {
    setPreviewSession(null);
  }, []);

  // 未登录是用户可自助修复的状态：用「文案 + 去登录」的专用提示条取代通用降级模板；
  // 其余 Fallback 原因仍走通用 notify。toolID 取本次请求参数（会话场景为会话所属工具）。
  const showFallback = useCallback(
    (fallback: string, toolID: string) => {
      if (fallback !== 'err.launch.not_logged_in') {
        notify(tr('ui.workspace.fallback', { reason: translateBackend(fallback) }), 'info');
        return;
      }
      setLoginPrompt({ toolID, toolName: tools.find((x) => x.ID === toolID)?.Name ?? toolID });
    },
    [notify, tr, tools],
  );

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
          if (res.Fallback) showFallback(res.Fallback, s.ToolID || res.Terminal?.ToolID || '');
          if (isSSHWorkspaceRef(tab.id)) useAppStore.getState().clearUnreachable(tab.id);
        })
        .catch((e: unknown) => {
          notify(tr('ui.workspace.open_session_failed', { err: backendError(e) }), 'error');
          if (isSSHWorkspaceRef(tab.id)) useAppStore.getState().markUnreachable(tab.id);
        });
    },
    [showFallback, selectCenterTab, closeSessionPreview, notify, tr, tab.id],
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
      if (res.Fallback) showFallback(res.Fallback, id);
      if (isSSHWorkspaceRef(tab.id)) useAppStore.getState().clearUnreachable(tab.id);
      // 新会话要过一会儿才落进工具自己的会话存储；按退避多扫几次，避免单次过早/撞车
      for (const tid of rescanTimers.current) window.clearTimeout(tid);
      rescanTimers.current = NEW_SESSION_RESCAN_DELAYS_MS.map((delay) =>
        window.setTimeout(() => {
          void scanSessions();
        }, delay),
      );
    } catch (e: unknown) {
      notify(tr('ui.workspace.new_session_failed', { err: backendError(e) }), 'error');
      if (isSSHWorkspaceRef(tab.id)) useAppStore.getState().markUnreachable(tab.id);
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

  // html 文件的浏览器页签（沙箱 iframe 渲染，固定不挤占预览位）。
  // 远程（SSH）工作区无本地路径，不提供浏览器预览入口。
  const isRemoteWs =
    useAppStore((s) => s.workspaces.find((w) => w.Path === tab.id)?.Kind) === 'ssh';
  const openBrowserTab = isRemoteWs ? undefined : (path: string) => {
    setFileTabs((s) => openPinned(s, browserTabPath(path), 'browser'));
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
    // 「查看全部」时连接可能绑定其他项目：终端 Info.Workspace 跟连接走，
    // 当前页签按 Workspace 过滤，不先切项目就会「双击没反应」。
    const bound = (c.Workspace || '').trim();
    const targetWs = bound || tab.id;
    const store = useAppStore.getState();
    if (!sameWorkspacePath(targetWs, tab.id)) {
      const known = store.workspaces.find((w) => sameWorkspacePath(w.Path, targetWs));
      const name =
        known?.Name ||
        (() => {
          const n = targetWs.replace(/[\\/]+$/, '');
          const i = Math.max(n.lastIndexOf('/'), n.lastIndexOf('\\'));
          return i >= 0 ? n.slice(i + 1) : n;
        })();
      store.openTab(known ?? { Path: targetWs, Name: name, LastUsed: '', SessionCount: 0, ToolCounts: {}, Source: 'manual' });
    }
    void openSSHTerminal(c.ID, 80, 24)
      .then((info) => {
        const st = useAppStore.getState();
        st.upsertTerminal(info);
        if (sameWorkspacePath(info.Workspace || targetWs, tab.id)) {
          selectCenterTab(TERMINALS_TAB);
          setToolSubTab(info.ID);
        } else if (info.Key) {
          // 目标工作区视图经 focusTermKey 切到预览区远程终端子页签
          st.requestFocusTerm(info.Key);
        }
      })
      .catch((e: unknown) => {
        notify(tr('ui.workspace.open_ssh_failed', { err: backendError(e) }), 'error');
      });
  };

  // 中心区内容包裹层：两种布局共用同一组子元素、只换容器，
  // 且全部用 hidden 切换显示（切页签/切布局都不重挂，xterm 缓冲不丢）
  const tabsMode = layoutMode === 'tabs';
  const fadeStyle = { animation: 'kshell-fade-in var(--duration-fast) var(--ease-out)' };
  const previewVisible = tabsMode ? centerTab === SESSION_TAB : leftTab === SESSION_TAB;
  const filesVisible = tabsMode ? centerTab === FILES_TAB : rightTab === FILES_TAB;
  const toolsVisible = tabsMode ? centerTab === TERMINALS_TAB : rightTab === TERMINALS_TAB;
  const agentVisible = (id: string) => visible && (tabsMode ? centerTab === id : leftTab === id);

  const sessionPreviewPane = (
    <div key="preview" className={cn('h-full', !previewVisible && 'hidden')} style={fadeStyle}>
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
  );
  const filesPane = (
    <div key="files" className={cn('h-full', !filesVisible && 'hidden')} style={fadeStyle}>
      <FileTabsPane
        wsPath={tab.id}
        state={fileTabs}
        dirty={fileDirty}
        onChange={setFileTabs}
        onDirty={(path, d) => setFileDirty((prev) => ({ ...prev, [path]: d }))}
        onOpenBrowser={openBrowserTab}
      />
    </div>
  );
  const toolsPane = (
    <div key="tools" className={cn('h-full', !toolsVisible && 'hidden')} style={fadeStyle}>
      <PreviewToolPane
        terms={toolTerms}
        active={visible && toolsVisible}
        subTab={toolSubTab}
        onSubTab={setToolSubTab}
        onCloseTerminal={handleCloseTerminal}
        onNewShell={handleNewShell}
      />
    </div>
  );
  const termPanes = terms.map((t) => (
    <div key={t.ID} className={cn('h-full', !agentVisible(t.ID) && 'hidden')} style={fadeStyle}>
      <TerminalView term={t} active={agentVisible(t.ID)} />
    </div>
  ));
  const chatPanes = chatsForWs.map((c) => (
    <div key={c.ID} className={cn('h-full', !agentVisible(c.ID) && 'hidden')} style={fadeStyle}>
      <ChatView chat={c} active={agentVisible(c.ID)} />
    </div>
  ));

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* 未登录降级提示条：文案带「去登录」，点击打开登录弹窗（弹窗内嵌官方登录终端） */}
      {loginPrompt && (
        <div className="flex shrink-0 items-center gap-2 border-b border-border bg-warning/10 px-3 py-1.5 text-xs text-warning">
          <span className="min-w-0 flex-1 truncate">
            {tr('ui.workspace.fallback_not_logged_in', { name: loginPrompt.toolName })}
          </span>
          <Button type="button" size="sm" variant="secondary" onClick={() => setLoginOpen(true)}>
            {tr('ui.auth.go_login')}
          </Button>
          <button
            type="button"
            aria-label={tr('ui.auth.close')}
            className="shrink-0 hover:opacity-80"
            onClick={() => setLoginPrompt(null)}
          >
            ✕
          </button>
        </div>
      )}
      <div className="flex min-h-0 flex-1">
      {/* 滚动隔离：左栏整体不滚动（overflow-hidden），固定新建会话/归档行，仅会话列表内部滚动 */}
      <aside
        className="flex shrink-0 flex-col gap-2 overflow-hidden border-r border-border bg-card p-2.5"
        style={{ width: layout.left }}
        aria-label={tr('ui.workspace.session_sidebar_aria')}
      >
        {/* 「新建会话」本身就是下拉菜单：点开列 agent，选中即启动（不再并排一个工具下拉框） */}
        <div className="flex shrink-0 items-center gap-2">
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
          <p className="shrink-0 text-xs text-muted-foreground">
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

      {tabsMode ? (
        <main className="flex min-w-0 flex-1 flex-col">
          <CenterSessionTabs
            variant="full"
            items={centerSessionItems}
            order={sessionTabOrder}
            centerTab={centerTab}
            previewTabId={SESSION_TAB}
            filesTabId={FILES_TAB}
            terminalsTabId={TERMINALS_TAB}
            toolTermCount={toolTerms.length}
            layoutMode={layoutMode}
            onLayoutToggle={toggleLayout}
            onSelect={selectCenterTab}
            onClose={(id) => {
              if (terms.some((t) => t.ID === id)) handleCloseTerminal(id);
              else handleCloseChat(id);
            }}
            onReorder={(next) => {
              setSavedTabOrder(next);
              saveCenterTabOrder(tab.id, next);
            }}
            onFileDropTerminal={(id, path, exited) => {
              if (!exited) void writeTerminal(id, encodeTerminalInput(quotePathForShell(path)));
            }}
            onFileDropChat={(id, path) => {
              appendChatInput(id, quotePathForShell(path));
            }}
          />

          <div className="min-h-0 flex-1 overflow-hidden">
            {/* 切页签时的淡入：动画挂在各内容包裹层上——hidden 切 display 会重放动画，
                因此无需 key 重挂（重挂会丢 xterm 缓冲，违背「终端常挂载」约定） */}
            {sessionPreviewPane}
            {filesPane}
            {toolsPane}
            {termPanes}
            {chatPanes}
          </div>
        </main>
      ) : (
        <main className="flex min-w-0 flex-1">
          {/* 左半：agent（会话预览 + 会话页签） */}
          <section
            className="flex min-w-0 flex-col"
            style={{ width: layout.center }}
            aria-label={tr('ui.workspace.left_pane_aria')}
          >
            <CenterSessionTabs
              variant="agent"
              items={centerSessionItems}
              order={sessionTabOrder}
              centerTab={leftTab}
              previewTabId={SESSION_TAB}
              filesTabId={FILES_TAB}
              terminalsTabId={TERMINALS_TAB}
              toolTermCount={toolTerms.length}
              layoutMode={layoutMode}
              onLayoutToggle={toggleLayout}
              onSelect={selectCenterTab}
              onClose={(id) => {
                if (terms.some((t) => t.ID === id)) handleCloseTerminal(id);
                else handleCloseChat(id);
              }}
              onReorder={(next) => {
                setSavedTabOrder(next);
                saveCenterTabOrder(tab.id, next);
              }}
              onFileDropTerminal={(id, path, exited) => {
                if (!exited) void writeTerminal(id, encodeTerminalInput(quotePathForShell(path)));
              }}
              onFileDropChat={(id, path) => {
                appendChatInput(id, quotePathForShell(path));
              }}
            />
            <div className="min-h-0 flex-1 overflow-hidden">
              {sessionPreviewPane}
              {termPanes}
              {chatPanes}
            </div>
          </section>

          <ResizeHandle
            side="left"
            width={layout.center}
            onResize={(w) => setLayout({ center: w })}
            defaultWidth={LAYOUT_CENTER_DEFAULT}
            min={LAYOUT_CENTER_MIN}
            max={LAYOUT_CENTER_MAX}
            label={tr('ui.workspace.split_resize')}
          />

          {/* 右半：文件 + 终端 */}
          <section
            className="flex min-w-0 flex-1 flex-col"
            aria-label={tr('ui.workspace.right_pane_aria')}
          >
            <CenterSessionTabs
              variant="tool"
              items={[]}
              order={[]}
              centerTab={rightTab}
              previewTabId={SESSION_TAB}
              filesTabId={FILES_TAB}
              terminalsTabId={TERMINALS_TAB}
              toolTermCount={toolTerms.length}
              onSelect={selectCenterTab}
              onClose={() => {}}
              onReorder={() => {}}
              onFileDropTerminal={() => {}}
              onFileDropChat={() => {}}
            />
            <div className="min-h-0 flex-1 overflow-hidden">
              {filesPane}
              {toolsPane}
            </div>
          </section>
        </main>
      )}

      <ResizeHandle
        side="right"
        width={layout.right}
        onResize={(w) => setLayout({ right: w })}
        defaultWidth={LAYOUT_DEFAULT.right}
        label={tr('ui.workspace.resize_files')}
      />

      {/* 滚动隔离：右栏整体不滚动（overflow-hidden），页签条固定，各面板内部滚动 */}
      <aside
        className="flex shrink-0 flex-col overflow-hidden border-l border-border bg-card p-2.5"
        style={{ width: layout.right }}
        aria-label={tr('ui.workspace.right_panel_aria')}
      >
        <div className="mb-2 flex shrink-0 gap-0.5 border-b border-border">
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
          <button
            className={cn(paneTabBase, rightPane === 'terminal' && paneTabActive)}
            aria-pressed={rightPane === 'terminal'}
            onClick={() => setRightPane('terminal')}
          >
            {tr('ui.workspace.active_terminals_tab')}
          </button>
        </div>
        {/* 四面板常挂载，仅用 hidden 切换显示 */}
        <div className={cn('min-h-0 flex-1', rightPane !== 'files' && 'hidden')}>
          <FileTree wsPath={tab.id} onOpenFile={openFile} onEditFile={editFile} />
        </div>
        <div className={cn('min-h-0 flex-1 overflow-y-auto', rightPane !== 'git' && 'hidden')}>
          <GitPanel wsPath={tab.id} visible={visible && rightPane === 'git'} onOpenDiff={openGitDiff} />
        </div>
        <div className={cn('min-h-0 flex-1 overflow-y-auto', rightPane !== 'ssh' && 'hidden')}>
          <SshPanel wsPath={tab.id} onOpenRemote={handleOpenRemote} />
        </div>
        <div className={cn('min-h-0 flex-1 overflow-y-auto', rightPane !== 'terminal' && 'hidden')}>
          <ActiveTerminalsPanel wsPath={tab.id} onNewShell={handleNewShell} />
        </div>
      </aside>
      </div>
      {loginOpen && loginPrompt && (
        <LoginDialog
          toolID={loginPrompt.toolID}
          toolName={loginPrompt.toolName}
          onClose={() => setLoginOpen(false)}
        />
      )}
    </div>
  );
}
