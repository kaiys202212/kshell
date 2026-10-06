// 桌面版主框架：自绘标题栏（首页 / 工作区页签 / 设置贴在窗口顶部）+ 内容区。
// 内容区采用「全部页签常挂载、非激活用 hidden」策略：切页签不卸载组件，
// 文件树展开态、SSH 状态、内嵌终端（xterm 缓冲与焦点）都不会丢。
// 终端事件总线在这里建立一次：terminal:data → 注册表分发到已挂载的 xterm，
// terminal:exit → 更新镜像并提示；Go 侧每会话保留 256KiB 环形缓冲兜住未挂载期间的输出。
// 全局快捷键：Ctrl+K 打开快速切换器，Ctrl+F 在工作区页签内派发 kshell:focus-search。
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  applyUpdate,
  archivedIDs,
  chatHistory,
  closeChat,
  closeTerminal,
  confirmArchive,
  getAppearance,
  listChats,
  listTerminals,
  onAppearanceChanged,
  onArchiveChanged,
  onArchiveSuggest,
  onChatExit,
  onChatMeta,
  onChatPermission,
  onChatUpdate,
  onProjectsChanged,
  onTerminalData,
  onTerminalExit,
  onTerminalMeta,
  onUpdateAvailable,
  writeTerminal,
} from './lib/api';
import type { AppearanceInfo } from './lib/appearance';
import { applyUiFontSize, clampUiFontSize } from './lib/appearance';
import type { ChatUpdate, UpdateInfo } from './lib/api';
import { encodeTerminalInput } from './lib/base64';
import { appendChatInput } from './lib/chatInputRegistry';
import { quotePathForShell } from './lib/dragPath';
import { OnFileDrop, OnFileDropOff } from '../wailsjs/runtime/runtime';
import { handleAnchorClick } from './lib/openHref';
import { applyChatUpdate, type TimelineItem } from './state/chatUpdate';
import { dispatchTerminalData } from './lib/terminalRegistry';
import { cn } from './lib/cn';
import { sameWorkspacePath } from './lib/workspacePath';
import Home from './pages/Home';
import Settings from './pages/Settings';
import WorkspaceTabView from './pages/WorkspaceTab';
import ArchiveSuggest from './components/ArchiveSuggest';
import UpdatePrompt from './components/UpdatePrompt';
import QuickSwitcher from './components/QuickSwitcher';
import TitleBar from './components/TitleBar';
import { Toaster } from './components/ui/toaster';
import { TooltipProvider } from './components/ui/tooltip';
import { bumpTerminalBusy, clearTerminalBusy } from './state/terminalBusy';
import { SETTINGS_TAB_ID, useAppStore } from './state/store';

function App() {
  const openTabs = useAppStore((s) => s.openTabs);
  const activeTabId = useAppStore((s) => s.activeTabId);
  const setActiveTab = useAppStore((s) => s.setActiveTab);
  const closeTab = useAppStore((s) => s.closeTab);
  const [switcherOpen, setSwitcherOpen] = useState(false);
  const archivePrompt = useAppStore((s) => s.archivePrompt);
  const [updatePrompt, setUpdatePrompt] = useState<UpdateInfo | null>(null);
  const [updateBusy, setUpdateBusy] = useState(false);
  const [updateError, setUpdateError] = useState('');
  const updateDismissedRef = useRef(false);

  useEffect(() => {
    listTerminals()
      .then((list) => useAppStore.getState().setTerminals(list))
      .catch(() => {});
  }, []);

  useEffect(() => {
    return onUpdateAvailable((info) => {
      if (updateDismissedRef.current) return;
      setUpdatePrompt(info);
    });
  }, []);

  useEffect(() => {
    // 聊天事件总线 + 镜像重建（整应用只订阅一次）。
    // 仅在某 chat 的「历史回放窗口」内缓冲其实时更新：chat:update 可能先于 chatHistory 到达，
    // 其高 Seq 会抢先抬高 chatSeq，导致随后回放的历史前缀被去重丢弃。
    // 窗口外的更新立即应用——尤其挂载后才新建的聊天，绝不能因为没有初始 listChats 条目而被永久缓冲。
    const pending = new Map<string, ChatUpdate[]>();
    const offU = onChatUpdate(({ id, update }) => {
      const buf = pending.get(id);
      if (buf) {
        buf.push(update);
        return;
      }
      useAppStore.getState().applyChat(id, update);
    });
    const offP = onChatPermission(({ id, request }) =>
      useAppStore.getState().setChatPermission(id, request),
    );
    const offX = onChatExit(({ id, exitCode, error }) => {
      const { markChatExited, setChatPermission, notify } = useAppStore.getState();
      markChatExited(id, exitCode, error);
      setChatPermission(id, null);
      notify(error || `会话已退出（退出码 ${exitCode}）`, error ? 'error' : 'info');
    });

    void listChats()
      .then(async (list) => {
        useAppStore.getState().setChats(list);
        for (const c of list) {
          const buf: ChatUpdate[] = [];
          pending.set(c.ID, buf); // 进入种子窗口
          const hist = await chatHistory(c.ID).catch(() => [] as ChatUpdate[]);
          useAppStore.getState().setChatItems(
            c.ID,
            hist.reduce((acc, u) => applyChatUpdate(acc, u), [] as TimelineItem[]),
          );
          pending.delete(c.ID); // 退出种子窗口
          for (const u of buf) useAppStore.getState().applyChat(c.ID, u);
        }
      })
      .catch(() => {});

    return () => {
      offU();
      offP();
      offX();
    };
  }, []);

  useEffect(() => {
    // 颜色模式：初始取一次（写 data-theme 并进 store），再订阅后续变化
    const apply = (info: AppearanceInfo) => {
      document.documentElement.dataset.theme = info.resolved;
      try { localStorage.setItem('kshell-appearance', info.resolved); } catch { /* 忽略持久化失败 */ }
      applyUiFontSize(info.fontSize);
      useAppStore.getState().setAppearance({
        ...info,
        fontSize: clampUiFontSize(info.fontSize),
      });
    };
    getAppearance().then(apply).catch(() => {});
    const off = onAppearanceChanged(apply);
    return off;
  }, []);

  useEffect(() => {
    // 用户一提交，Go 就改标题并打上 Prompted；这里立刻写进镜像，不必等下一轮扫描。
    const offTerm = onTerminalMeta((info) => useAppStore.getState().upsertTerminal(info));
    const offChat = onChatMeta((info) => useAppStore.getState().upsertChat(info));
    const offSuggest = onArchiveSuggest((p) => useAppStore.getState().setArchivePrompt(p));
    const refreshArchived = () => {
      void archivedIDs()
        .then((ids) => useAppStore.getState().setArchivedIDs(ids))
        .catch(() => {});
    };
    const offArch = onArchiveChanged(refreshArchived);
    refreshArchived();
    return () => {
      offTerm();
      offChat();
      offSuggest();
      offArch();
    };
  }, []);

  useEffect(() => {
    // 终端事件总线：整应用只订阅一次，避免每个终端组件各订阅一份
    const offData = onTerminalData(({ id, data }) => {
      if (id) bumpTerminalBusy(id);
      dispatchTerminalData(id, data);
    });
    const offExit = onTerminalExit(({ id, exitCode }) => {
      clearTerminalBusy(id);
      const { markTerminalExited: mark, notify } = useAppStore.getState();
      mark(id, exitCode);
      notify(exitCode === 0 ? '终端已退出' : `终端异常退出（退出码 ${exitCode}）`, exitCode === 0 ? 'info' : 'error');
    });
    return () => {
      offData();
      offExit();
    };
  }, []);

  // 关闭工作区页签时连带结束该工作区的内嵌终端与聊天：进程不能留在后台又没有任何入口
  const handleCloseTab = useCallback(
    (id: string) => {
      const { terminals, removeTerminal, chats, removeChat } = useAppStore.getState();
      const ownedTerms = terminals.filter((t) => sameWorkspacePath(t.Workspace, id));
      const ownedChats = chats.filter((c) => sameWorkspacePath(c.Workspace, id));
      closeTab(id);
      for (const t of ownedTerms) {
        removeTerminal(t.ID);
        closeTerminal(t.ID).catch(() => {});
      }
      for (const c of ownedChats) {
        removeChat(c.ID);
        closeChat(c.ID).catch(() => {});
      }
    },
    [closeTab],
  );

  useEffect(() => {
    // 项目表变更（新建/删除/还原）：payload 已带最新工作区列表——
    // 同步 store 并关闭已消失工作区的页签（连带结束其内嵌终端），避免页签指向被隐藏的项目。
    const off = onProjectsChanged(({ workspaces }) => {
      useAppStore.getState().setWorkspaces(workspaces);
      const { openTabs: tabs } = useAppStore.getState();
      for (const t of tabs) {
        if (!workspaces.some((w) => sameWorkspacePath(w.Path, t.id))) {
          handleCloseTab(t.id);
        }
      }
    });
    return off;
  }, [handleCloseTab]);

  useEffect(() => {
    // 外部文件拖入（资源管理器）：Wails 全窗口回调，按落点 data-drop-zone 路由到终端/聊天页签。
    // useDropTarget=false 关闭 Wails 自带遮罩；内部 HTML5 拖拽（无真实文件）不走这里。
    OnFileDrop((x, y, paths) => {
      if (!paths || paths.length === 0) return;
      const el = document.elementFromPoint(x, y)?.closest('[data-drop-zone]');
      const zone = el?.getAttribute('data-drop-zone') ?? '';
      const text = quotePathForShell(paths[0]);
      if (zone.startsWith('terminal:')) {
        const id = zone.slice('terminal:'.length);
        // 与 TerminalView/页签落点同口径：已退出的终端不接受拖入
        const term = useAppStore.getState().terminals.find((t) => t.ID === id);
        if (term?.Status !== 'exited') {
          void writeTerminal(id, encodeTerminalInput(text));
        }
      } else if (zone.startsWith('chat:')) {
        appendChatInput(zone.slice('chat:'.length), text);
      }
    }, false);
    return () => OnFileDropOff();
  }, []);

  useEffect(() => {
    // 单一全局 keydown：window 级监听不受输入框焦点影响（输入框聚焦时 Ctrl+K 仍触发），
    // preventDefault 压掉浏览器/WebView 的默认快捷语义
    const onKey = (e: KeyboardEvent) => {
      if (!e.ctrlKey) return;
      if (e.key === 'k' || e.key === 'K') {
        e.preventDefault();
        setSwitcherOpen((v) => !v); // toggle：已打开时再按关闭
      } else if (e.key === 'f' || e.key === 'F') {
        e.preventDefault();
        const { activeTabId: current } = useAppStore.getState();
        if (current !== null && current !== SETTINGS_TAB_ID) {
          window.dispatchEvent(new CustomEvent('kshell:focus-search'));
        }
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  useEffect(() => {
    // 捕获阶段拦截 <a>：阻止 WebView 整页导航，改走系统打开或工作区文件。
    const onClick = (e: MouseEvent) => {
      handleAnchorClick(e);
    };
    document.addEventListener('click', onClick, true);
    return () => document.removeEventListener('click', onClick, true);
  }, []);

  const isHome = activeTabId === null;
  const isSettings = activeTabId === SETTINGS_TAB_ID;

  return (
    <TooltipProvider>
      <div className="flex h-screen flex-col overflow-hidden">
        <TitleBar
          tabs={openTabs}
          activeTabId={activeTabId}
          onSelectTab={setActiveTab}
          onCloseTab={handleCloseTab}
        />
        {/* 内容区：全部页签常挂载，仅激活项可见（终端、文件树、SSH 状态得以保持） */}
        <div className="flex min-h-0 flex-1 flex-col">
          <div data-pane="home" className={cn('flex min-h-0 flex-1 flex-col', !isHome && 'hidden')}>
            <Home />
          </div>
          <div
            data-pane="settings"
            className={cn('flex min-h-0 flex-1 flex-col', !isSettings && 'hidden')}
          >
            <Settings />
          </div>
          {openTabs.map((t) => (
            <div
              key={t.id}
              data-pane={t.id}
              className={cn('flex min-h-0 flex-1 flex-col', t.id !== activeTabId && 'hidden')}
            >
              <WorkspaceTabView tab={t} visible={t.id === activeTabId} />
            </div>
          ))}
        </div>
      </div>
      <QuickSwitcher open={switcherOpen} onOpenChange={setSwitcherOpen} />
      <ArchiveSuggest
        open={!!archivePrompt}
        summary={archivePrompt?.summary ?? ''}
        onClose={() => useAppStore.getState().setArchivePrompt(null)}
        onConfirm={() => {
          const ref = useAppStore.getState().archivePrompt?.ref;
          useAppStore.getState().setArchivePrompt(null);
          if (ref) void confirmArchive(ref);
        }}
      />
      {updatePrompt ? (
        <UpdatePrompt
          info={updatePrompt}
          busy={updateBusy}
          error={updateError}
          onLater={() => {
            updateDismissedRef.current = true;
            setUpdatePrompt(null);
            setUpdateError('');
            setUpdateBusy(false);
          }}
          onUpgrade={() => {
            void (async () => {
              setUpdateBusy(true);
              setUpdateError('');
              try {
                await applyUpdate();
              } catch (e: unknown) {
                setUpdateError(e instanceof Error ? e.message : String(e));
                setUpdateBusy(false);
              }
            })();
          }}
        />
      ) : null}
      <Toaster />
    </TooltipProvider>
  );
}

export default App;
