// 工作区页签：三栏布局（左右两栏宽度可拖动）。
//   左栏：「新建会话」下拉菜单 + 会话列表（点行联动页签/会话预览，图标激活）
//   中栏：左侧 agent 页签；最右钉「预览」（预览区内再开「文件预览|终端」子页签）
//   右栏：文件 | SSH 子页签（点文件自动切到中栏的预览页签）
// 终端页签一旦打开就常挂载（非激活用 hidden），xterm 缓冲与焦点不丢；
// 工作区页签本身也由 App 常挂载，因此只有关闭页签才会真正结束终端进程。
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  closeChat,
  closeTerminal,
  getTools,
  listChats,
  listTerminals,
  onScanDone,
  openSession,
  openShellTerminal,
  openSSHTerminal,
  openWorkspace,
  openWorkspaceACP,
  scanSessions,
  writeTerminal,
} from '../lib/api';
import type { Session, SshConnection, TerminalInfo, ToolInfo } from '../lib/api';
import { encodeTerminalInput } from '../lib/base64';
import { appendChatInput } from '../lib/chatInputRegistry';
import { DRAG_MIME, quotePathForShell } from '../lib/dragPath';
import { badgeFor } from '../lib/toolBadge';
import { cn } from '../lib/cn';
import { displayTitle } from '../lib/title';
import { TAB_ACTIVE, TAB_BASE, TAB_UNDERLINE } from '../lib/ui';
import { sameWorkspacePath } from '../lib/workspacePath';
import FileTree from '../components/FileTree';
import PreviewToolPane, { PREVIEW_SUB, SESSION_PREVIEW_SUB } from '../components/PreviewToolPane';
import ResizeHandle from '../components/ResizeHandle';
import SessionList from '../components/SessionList';
import SshPanel from '../components/SshPanel';
import TerminalView from '../components/TerminalView';
import AgentActivityIcon from '../components/AgentActivityIcon';
import ChatView from '../components/ChatView';
import NewSessionMenu from '../components/NewSessionMenu';
import { ToolDot } from '../components/ui/tool-dot';
import { resolveAgentActivity } from '../state/agentActivity';
import { LAYOUT_DEFAULT, useAppStore } from '../state/store';
import type { WorkspaceTab } from '../state/store';

function isAgentTerm(t: TerminalInfo): boolean {
  return t.Kind === 'session' || t.Kind === 'new';
}

function isToolTerm(t: TerminalInfo): boolean {
  return t.Kind === 'shell' || t.Kind === 'ssh';
}

type RightPane = 'files' | 'ssh';

// 中心区固定页签「预览」的保留 id（终端 id 形如 t1，不会冲突）
const PREVIEW_TAB = 'preview';

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
  const [rightPane, setRightPane] = useState<RightPane>('files');
  const [previewPath, setPreviewPath] = useState<string | null>(null);
  const [centerTab, setCenterTab] = useState<string>(PREVIEW_TAB);
  const [toolSubTab, setToolSubTab] = useState<string>(PREVIEW_SUB);
  const [previewSession, setPreviewSession] = useState<Session | null>(null);
  const [tools, setTools] = useState<ToolInfo[]>([]);
  const [busy, setBusy] = useState(false);
  // 新建会话后的延迟重扫定时器（卸载/再次新建时清掉，避免重复触发）
  const rescanTimers = useRef<number[]>([]);

  const layout = useAppStore((s) => s.layout);
  const setLayout = useAppStore((s) => s.setLayout);
  const terminals = useAppStore((s) => s.terminals);
  const chats = useAppStore((s) => s.chats);
  const chatPermissions = useAppStore((s) => s.chatPermissions);
  const terminalBusy = useAppStore((s) => s.terminalBusy);
  const notify = useAppStore((s) => s.notify);
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
    if (centerTab === PREVIEW_TAB) {
      if (toolSubTab === SESSION_PREVIEW_SUB && previewSession) return previewSession.ID;
      return null;
    }
    const t = terms.find((x) => x.ID === centerTab);
    if (t?.SessionID) return t.SessionID;
    const c = chatsForWs.find((x) => x.ID === centerTab);
    if (c?.SessionID) return c.SessionID;
    return null;
  }, [centerTab, toolSubTab, previewSession, terms, chatsForWs]);

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
    return onScanDone(() => {
      refresh();
      refreshMirrors();
    });
  }, []);

  useEffect(() => {
    // 当前中心区页签指向的终端/聊天已不存在（被关闭/退出后清理）时退回预览
    if (
      centerTab !== PREVIEW_TAB &&
      !terms.some((t) => t.ID === centerTab) &&
      !chatsForWs.some((c) => c.ID === centerTab)
    ) {
      selectCenterTab(PREVIEW_TAB);
    }
  }, [terms, chatsForWs, centerTab, selectCenterTab]);

  useEffect(() => {
    // 预览区子页签指向的 shell/ssh 已关闭时退回「文件预览」
    if (toolSubTab !== PREVIEW_SUB && toolSubTab !== SESSION_PREVIEW_SUB && !toolTerms.some((t) => t.ID === toolSubTab)) {
      setToolSubTab(PREVIEW_SUB);
    }
    if (toolSubTab === SESSION_PREVIEW_SUB && !previewSession) {
      setToolSubTab(PREVIEW_SUB);
    }
  }, [toolTerms, toolSubTab, previewSession]);

  const closeSessionPreview = useCallback(() => {
    setPreviewSession(null);
    setToolSubTab((cur) => (cur === SESSION_PREVIEW_SUB ? PREVIEW_SUB : cur));
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
          if (res.Fallback) notify(`已回退到终端模式：${res.Fallback}`, 'info');
        })
        .catch((e: unknown) => {
          notify(`打开会话失败：${e instanceof Error ? e.message : String(e)}`, 'error');
        });
    },
    [notify, selectCenterTab, closeSessionPreview],
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
      selectCenterTab(PREVIEW_TAB);
      setToolSubTab(SESSION_PREVIEW_SUB);
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
  const startSession = async (id: string, forceACP = false) => {
    if (busy) return;
    setBusy(true);
    try {
      const res = forceACP
        ? await openWorkspaceACP(tab.id, id)
        : await openWorkspace(tab.id, id);
      if (res.Kind === 'chat' && res.Chat) {
        useAppStore.getState().upsertChat(res.Chat);
        selectCenterTab(res.Chat.ID);
      } else if (res.Terminal) {
        useAppStore.getState().upsertTerminal(res.Terminal);
        selectCenterTab(res.Terminal.ID);
      }
      if (res.Fallback) notify(`已回退到终端模式：${res.Fallback}`, 'info');
      // 新会话要过一会儿才落进工具自己的会话存储；按退避多扫几次，避免单次过早/撞车
      for (const tid of rescanTimers.current) window.clearTimeout(tid);
      rescanTimers.current = NEW_SESSION_RESCAN_DELAYS_MS.map((delay) =>
        window.setTimeout(() => {
          void scanSessions();
        }, delay),
      );
    } catch (e: unknown) {
      notify(`新建会话失败：${e instanceof Error ? e.message : String(e)}`, 'error');
    } finally {
      setBusy(false);
    }
  };

  const handleCloseTerminal = (id: string) => {
    useAppStore.getState().removeTerminal(id);
    closeTerminal(id).catch(() => {});
    if (centerTab === id) selectCenterTab(PREVIEW_TAB);
    if (toolSubTab === id) setToolSubTab(PREVIEW_SUB);
  };

  const handleCloseChat = (id: string) => {
    useAppStore.getState().removeChat(id);
    closeChat(id).catch(() => {});
    if (centerTab === id) selectCenterTab(PREVIEW_TAB);
  };

  const openFile = (path: string) => {
    setPreviewPath(path);
    selectCenterTab(PREVIEW_TAB);
    setToolSubTab(PREVIEW_SUB);
  };

  const handleNewShell = () => {
    void openShellTerminal(tab.id, 80, 24)
      .then((info) => {
        useAppStore.getState().upsertTerminal(info);
        selectCenterTab(PREVIEW_TAB);
        setToolSubTab(info.ID);
      })
      .catch((e: unknown) => {
        notify(`打开终端失败：${e instanceof Error ? e.message : String(e)}`, 'error');
      });
  };

  const handleOpenRemote = (c: SshConnection) => {
    void openSSHTerminal(c.ID, 80, 24)
      .then((info) => {
        useAppStore.getState().upsertTerminal(info);
        selectCenterTab(PREVIEW_TAB);
        setToolSubTab(info.ID);
      })
      .catch((e: unknown) => {
        notify(`打开 SSH 失败：${e instanceof Error ? e.message : String(e)}`, 'error');
      });
  };

  return (
    <div className="flex min-h-0 flex-1">
      <aside
        className="flex shrink-0 flex-col gap-2 overflow-y-auto border-r border-border bg-card p-2.5"
        style={{ width: layout.left }}
        aria-label="会话列表栏"
      >
        {/* 「新建会话」本身就是下拉菜单：点开列 agent，选中即启动（不再并排一个工具下拉框） */}
        <NewSessionMenu
          tools={tools}
          value={toolId}
          onChange={setToolId}
          onSelect={(id) => void startSession(id)}
          onSelectACP={(id) => void startSession(id, true)}
          disabled={busy}
        />
        {tools.length === 0 && (
          <p className="text-xs text-muted-foreground">
            未检测到可用的 agent：请先安装 Claude Code / Codex / OpenCode 等 CLI，再点首页「重新扫描」。
          </p>
        )}
        <SessionList
          workspacePath={tab.id}
          selectedSessionID={selectedSessionID}
          onSelectRow={handleSelectSessionRow}
          onActivate={openChatOrTerminal}
        />
      </aside>

      <ResizeHandle
        side="left"
        width={layout.left}
        onResize={(w) => setLayout({ left: w })}
        defaultWidth={LAYOUT_DEFAULT.left}
        label="调整会话列表宽度"
      />

      <main className="flex min-w-0 flex-1 flex-col">
        {/* 中心区页签条：左侧 agent；最右钉「预览」（工具区入口，样式弱化区分） */}
        <div
          className="flex shrink-0 items-stretch border-b border-border"
          role="tablist"
          aria-label="中心区页签"
        >
          <div className="flex min-w-0 flex-1 items-stretch overflow-x-auto">
          {terms.map((t) => {
            const badge = badgeFor(t.ToolID);
            const active = centerTab === t.ID;
            // 渲染层再洗一次：Cursor 等历史缓存标题可能仍带 <timestamp>Sunday...
            const label = displayTitle(t.Title) || t.Title;
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
                  title={`${label}${t.ToolID ? `（${badge.label}）` : ''}`}
                >
                  {label}
                </button>
                {/* 新建会话的标题已含「· 工具名」，徽标只留色点避免出现两个工具名 */}
                {t.ToolID && (
                  <ToolDot toolID={t.ToolID} className="shrink-0" showLabel={t.Kind !== 'new'} />
                )}
                <button
                  className={cn(
                    'ml-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-sm text-sm leading-none text-muted-foreground transition-opacity hover:bg-muted hover:text-foreground',
                    active ? 'opacity-70 hover:opacity-100' : 'opacity-0 group-hover:opacity-100',
                  )}
                  aria-label={`关闭终端 ${label}`}
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
            const label = displayTitle(c.Title) || c.Title;
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
                {/* 聊天标题通常已含工具名，徽标只留色点避免重复 */}
                {c.ToolID && <ToolDot toolID={c.ToolID} className="shrink-0" showLabel={false} />}
                <button
                  className={cn(
                    'ml-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-sm text-sm leading-none text-muted-foreground transition-opacity hover:bg-muted hover:text-foreground',
                    active ? 'opacity-70 hover:opacity-100' : 'opacity-0 group-hover:opacity-100',
                  )}
                  aria-label={`关闭会话 ${label}`}
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
            aria-selected={centerTab === PREVIEW_TAB}
            aria-label="预览与命令行"
            className={cn(
              centerTabBase,
              'ml-auto shrink-0 border-l border-border bg-muted/40 text-muted-foreground',
              centerTab === PREVIEW_TAB && cn(centerTabActive, 'bg-muted/70'),
            )}
            onClick={() => selectCenterTab(PREVIEW_TAB)}
          >
            预览
            {toolTerms.length > 0 && (
              <span className="rounded-sm bg-muted px-1 font-mono text-[10px] text-muted-foreground">
                {toolTerms.length}
              </span>
            )}
            {centerTab === PREVIEW_TAB && <span className={TAB_UNDERLINE} />}
          </button>
        </div>

        <div className="min-h-0 flex-1 overflow-hidden">
          {/* 切页签时的淡入：动画挂在各内容包裹层上——hidden 切 display 会重放动画，
              因此无需 key 重挂（重挂会丢 xterm 缓冲，违背「终端常挂载」约定） */}
          <div
            className={cn('h-full', centerTab !== PREVIEW_TAB && 'hidden')}
            style={{ animation: 'kshell-fade-in var(--duration-fast) var(--ease-out)' }}
          >
            <PreviewToolPane
              wsPath={tab.id}
              previewPath={previewPath}
              terms={toolTerms}
              active={visible && centerTab === PREVIEW_TAB}
              subTab={toolSubTab}
              onSubTab={setToolSubTab}
              onCloseTerminal={handleCloseTerminal}
              onNewShell={handleNewShell}
              sessionPreview={
                previewSession
                  ? { sessionID: previewSession.ID, title: displayTitle(previewSession.Title) || previewSession.Title }
                  : null
              }
              onCloseSessionPreview={closeSessionPreview}
              onActivateSessionPreview={() => {
                if (previewSession) openChatOrTerminal(previewSession);
              }}
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
        label="调整文件面板宽度"
      />

      <aside
        className="flex shrink-0 flex-col overflow-y-auto border-l border-border bg-card p-2.5"
        style={{ width: layout.right }}
        aria-label="文件与 SSH 面板"
      >
        <div className="mb-2 flex gap-0.5 border-b border-border">
          <button
            className={cn(paneTabBase, rightPane === 'files' && paneTabActive)}
            aria-pressed={rightPane === 'files'}
            onClick={() => setRightPane('files')}
          >
            文件
          </button>
          <button
            className={cn(paneTabBase, rightPane === 'ssh' && paneTabActive)}
            aria-pressed={rightPane === 'ssh'}
            onClick={() => setRightPane('ssh')}
          >
            SSH
          </button>
        </div>
        {/* 双面板常挂载，仅用 hidden 切换显示：切「文件|SSH」页签不再卸载重载，
            文件树展开态与 SSH 连接列表/命令历史得以保留 */}
        <div className={cn('min-h-0 flex-1', rightPane !== 'files' && 'hidden')}>
          <FileTree wsPath={tab.id} onOpenFile={openFile} />
        </div>
        <div className={cn('min-h-0 flex-1', rightPane !== 'ssh' && 'hidden')}>
          <SshPanel wsPath={tab.id} onOpenRemote={handleOpenRemote} />
        </div>
      </aside>
    </div>
  );
}
