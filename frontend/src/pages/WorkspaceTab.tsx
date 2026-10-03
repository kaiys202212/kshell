// 工作区页签：三栏布局（左右两栏宽度可拖动）。
//   左栏：「新建会话」下拉菜单（选 agent 即启动）+ 会话列表（「恢复」开中心区内嵌终端）
//   中栏：中心区页签（预览 / 每个内嵌终端一个页签）
//   右栏：文件 | SSH 子页签（点文件自动切到中栏的预览页签）
// 终端页签一旦打开就常挂载（非激活用 hidden），xterm 缓冲与焦点不丢；
// 工作区页签本身也由 App 常挂载，因此只有关闭页签才会真正结束终端进程。
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  closeChat,
  closeTerminal,
  getTools,
  listTerminals,
  onScanDone,
  openSession,
  openWorkspace,
  scanSessions,
} from '../lib/api';
import type { Session, TerminalInfo, ToolInfo } from '../lib/api';
import { badgeFor } from '../lib/toolBadge';
import { cn } from '../lib/cn';
import { TAB_ACTIVE, TAB_BASE, TAB_UNDERLINE } from '../lib/ui';
import { sameWorkspacePath } from '../lib/workspacePath';
import FileTree from '../components/FileTree';
import Preview from '../components/Preview';
import ResizeHandle from '../components/ResizeHandle';
import SessionList from '../components/SessionList';
import SshPanel from '../components/SshPanel';
import TerminalView from '../components/TerminalView';
import ChatView from '../components/ChatView';
import NewSessionMenu from '../components/NewSessionMenu';
import { ToolDot } from '../components/ui/tool-dot';
import { LAYOUT_DEFAULT, useAppStore } from '../state/store';
import type { WorkspaceTab } from '../state/store';

type RightPane = 'files' | 'ssh';

// 中心区固定页签「预览」的保留 id（终端 id 形如 t1，不会冲突）
const PREVIEW_TAB = 'preview';

// 新建会话后隔多久触发一次后台重扫（毫秒）。
// 工具自己的会话记录是它启动后才落盘的（opencode 先起 TUI 再写 SQLite），立刻重扫会查不到；
// 3s 够这些 CLI 完成启动与建记录，重扫本身在后台异步执行、不阻塞交互。
const NEW_SESSION_RESCAN_DELAY = 3000;

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
  const [tools, setTools] = useState<ToolInfo[]>([]);
  const [busy, setBusy] = useState(false);
  // 新建会话后的延迟重扫定时器（卸载/再次新建时清掉，避免重复触发）
  const rescanTimer = useRef<number | null>(null);

  const layout = useAppStore((s) => s.layout);
  const setLayout = useAppStore((s) => s.setLayout);
  const terminals = useAppStore((s) => s.terminals);
  const chats = useAppStore((s) => s.chats);
  const notify = useAppStore((s) => s.notify);
  // 新建会话的工具选择：全局持久化（'' = 自动），跨页签/重启记住用户的选择
  const toolId = useAppStore((s) => s.newSessionTool);
  const setToolId = useAppStore((s) => s.setNewSessionTool);


  // 本工作区的内嵌终端（按创建顺序）
  const terms = useMemo<TerminalInfo[]>(
    () => terminals.filter((t) => sameWorkspacePath(t.Workspace, tab.id)),
    [terminals, tab.id],
  );

  // 本工作区的聊天会话（按创建顺序）
  const chatsForWs = useMemo(
    () => chats.filter((c) => sameWorkspacePath(c.Workspace, tab.id)),
    [chats, tab.id],
  );

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
    return onScanDone(refresh);
  }, []);

  useEffect(() => {
    // 当前中心区页签指向的终端/聊天已不存在（被关闭/退出后清理）时退回预览
    if (
      centerTab !== PREVIEW_TAB &&
      !terms.some((t) => t.ID === centerTab) &&
      !chatsForWs.some((c) => c.ID === centerTab)
    ) {
      setCenterTab(PREVIEW_TAB);
    }
  }, [terms, chatsForWs, centerTab]);

  // 恢复历史会话：优先走 ACP 聊天，Go 侧按可用性决定聊天或回退终端
  const openChatOrTerminal = useCallback(
    (s: Session) => {
      void openSession(s.ID)
        .then((res) => {
          if (res.Kind === 'chat' && res.Chat) {
            useAppStore.getState().upsertChat(res.Chat);
            setCenterTab(res.Chat.ID);
          } else if (res.Terminal) {
            useAppStore.getState().upsertTerminal(res.Terminal);
            setCenterTab(res.Terminal.ID);
          }
          if (res.Fallback) notify(`已回退到终端模式：${res.Fallback}`, 'info');
        })
        .catch((e: unknown) => {
          notify(`打开会话失败：${e instanceof Error ? e.message : String(e)}`, 'error');
        });
    },
    [notify],
  );

  useEffect(
    () => () => {
      if (rescanTimer.current !== null) window.clearTimeout(rescanTimer.current);
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
        setCenterTab(res.Chat.ID);
      } else if (res.Terminal) {
        useAppStore.getState().upsertTerminal(res.Terminal);
        setCenterTab(res.Terminal.ID);
      }
      if (res.Fallback) notify(`已回退到终端模式：${res.Fallback}`, 'info');
      // 新会话要过一会儿才落进工具自己的会话存储，延迟重扫一次让会话列表把它带出来
      if (rescanTimer.current !== null) window.clearTimeout(rescanTimer.current);
      rescanTimer.current = window.setTimeout(() => {
        rescanTimer.current = null;
        void scanSessions();
      }, NEW_SESSION_RESCAN_DELAY);
    } catch (e: unknown) {
      notify(`新建会话失败：${e instanceof Error ? e.message : String(e)}`, 'error');
    } finally {
      setBusy(false);
    }
  };

  const handleCloseTerminal = (id: string) => {
    useAppStore.getState().removeTerminal(id);
    closeTerminal(id).catch(() => {});
    if (centerTab === id) setCenterTab(PREVIEW_TAB);
  };

  const handleCloseChat = (id: string) => {
    useAppStore.getState().removeChat(id);
    closeChat(id).catch(() => {});
    if (centerTab === id) setCenterTab(PREVIEW_TAB);
  };

  const openFile = (path: string) => {
    setPreviewPath(path);
    setCenterTab(PREVIEW_TAB);
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
          disabled={busy}
        />
        {tools.length === 0 && (
          <p className="text-xs text-muted-foreground">
            未检测到可用的 agent：请先安装 Claude Code / Codex / OpenCode 等 CLI，再点首页「重新扫描」。
          </p>
        )}
        <SessionList workspacePath={tab.id} onOpenTerminal={openChatOrTerminal} />
      </aside>

      <ResizeHandle
        side="left"
        width={layout.left}
        onResize={(w) => setLayout({ left: w })}
        defaultWidth={LAYOUT_DEFAULT.left}
        label="调整会话列表宽度"
      />

      <main className="flex min-w-0 flex-1 flex-col">
        {/* 中心区页签条：预览固定，其后是本工作区的内嵌终端 */}
        <div
          className="flex shrink-0 items-stretch overflow-x-auto border-b border-border"
          role="tablist"
          aria-label="中心区页签"
        >
          <button
            role="tab"
            aria-selected={centerTab === PREVIEW_TAB}
            className={cn(centerTabBase, centerTab === PREVIEW_TAB && centerTabActive)}
            onClick={() => setCenterTab(PREVIEW_TAB)}
          >
            预览
            {centerTab === PREVIEW_TAB && (
              <span className={TAB_UNDERLINE} />
            )}
          </button>
          {terms.map((t) => {
            const badge = badgeFor(t.ToolID);
            const active = centerTab === t.ID;
            return (
              <div
                key={t.ID}
                className={cn(centerTabBase, active && centerTabActive)}
                // 整条页签可点（标题右侧的工具徽标/留白此前点不动，只有标题按钮响应）
                onClick={() => setCenterTab(t.ID)}
                onAuxClick={(e) => {
                  if (e.button === 1) {
                    e.preventDefault();
                    handleCloseTerminal(t.ID);
                  }
                }}
              >
                {t.Status === 'exited' && (
                  <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-muted-foreground" />
                )}
                <button
                  role="tab"
                  aria-selected={active}
                  className="min-w-0 truncate text-xs"
                  title={`${t.Title}${t.ToolID ? `（${badge.label}）` : ''}`}
                >
                  {t.Title}
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
                  aria-label={`关闭终端 ${t.Title}`}
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
            return (
              <div
                key={c.ID}
                className={cn(centerTabBase, active && centerTabActive)}
                // 整条页签可点（标题右侧的徽标/留白也响应）
                onClick={() => setCenterTab(c.ID)}
                onAuxClick={(e) => {
                  if (e.button === 1) {
                    e.preventDefault();
                    handleCloseChat(c.ID);
                  }
                }}
              >
                {c.Status === 'running' && (
                  <span className="h-1.5 w-1.5 shrink-0 animate-pulse rounded-full bg-success" title="运行中" />
                )}
                <button
                  role="tab"
                  aria-selected={active}
                  className="min-w-0 truncate text-xs"
                  title={c.Title}
                >
                  {c.Title}
                </button>
                {/* 聊天标题通常已含工具名，徽标只留色点避免重复 */}
                {c.ToolID && <ToolDot toolID={c.ToolID} className="shrink-0" showLabel={false} />}
                <button
                  className={cn(
                    'ml-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-sm text-sm leading-none text-muted-foreground transition-opacity hover:bg-muted hover:text-foreground',
                    active ? 'opacity-70 hover:opacity-100' : 'opacity-0 group-hover:opacity-100',
                  )}
                  aria-label={`关闭会话 ${c.Title}`}
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

        <div className="min-h-0 flex-1 overflow-hidden">
          {/* 切页签时的淡入：动画挂在各内容包裹层上——hidden 切 display 会重放动画，
              因此无需 key 重挂（重挂会丢 xterm 缓冲，违背「终端常挂载」约定） */}
          {/* 预览页签：外层负责滚动与内边距，终端页签各自撑满（xterm 自己管滚动） */}
          <div
            className={cn('h-full overflow-y-auto p-3', centerTab !== PREVIEW_TAB && 'hidden')}
            style={{ animation: 'kshell-fade-in var(--duration-fast) var(--ease-out)' }}
          >
            <Preview wsPath={tab.id} path={previewPath} />
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
          <SshPanel wsPath={tab.id} />
        </div>
      </aside>
    </div>
  );
}
