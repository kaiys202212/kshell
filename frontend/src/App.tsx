// 桌面版主框架：自绘标题栏（首页 / 工作区页签 / 设置贴在窗口顶部）+ 内容区。
// 内容区采用「全部页签常挂载、非激活用 hidden」策略：切页签不卸载组件，
// 文件树展开态、SSH 状态、内嵌终端（xterm 缓冲与焦点）都不会丢。
// 终端事件总线在这里建立一次：terminal:data → 注册表分发到已挂载的 xterm，
// terminal:exit → 更新镜像并提示；Go 侧每会话保留 256KiB 环形缓冲兜住未挂载期间的输出。
// 全局快捷键：键位表与分发逻辑收敛在 lib/keybindings，此处只注入动作
//（Ctrl+K 切换器 / Ctrl+F 聚焦搜索 / Ctrl+1..9、Ctrl+Tab 切页签 / Ctrl+W 关页签 /
// Ctrl+0、Ctrl+T 回首页 / F1 帮助面板）。
import { useCallback, useEffect, useRef, useState } from 'react';
import i18n from 'i18next';
import {
  applyUpdate,
  archivedIDs,
  chatHistory,
  closeChat,
  closeTerminal,
  confirmArchive,
  getAppearance,
  listChats,
  listHubMessages,
  listTerminals,
  onAppearanceChanged,
  onArchiveChanged,
  onArchiveSuggest,
  onChatExit,
  onChatCommands,
  onChatMeta,
  onChatPermission,
  onChatUpdate,
  onHubChanged,
  onNotifyAgent,
  onNotifyFocus,
  onProjectsChanged,
  onScanDone,
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
import { handleGlobalKey } from './lib/keybindings';
import { OnFileDrop, OnFileDropOff } from '../wailsjs/runtime/runtime';
import { handleAnchorClick } from './lib/openHref';
import { eventLabel } from './lib/agentNotify';
import { playNotifySound } from './lib/notifySound';
import { backendError, translateBackend } from './lib/errors';
import { applyChatUpdate, type TimelineItem } from './state/chatUpdate';
import { createChatThrottle } from './state/chatThrottle';
import { dispatchTerminalData } from './lib/terminalRegistry';
import { cn } from './lib/cn';
import { sameWorkspacePath } from './lib/workspacePath';
import Home from './pages/Home';
import Settings from './pages/Settings';
import WorkspaceTabView from './pages/WorkspaceTab';
import AgentSetupDialog from './components/AgentSetupDialog';
import ProjectSetupDialog from './components/ProjectSetupDialog';
import ArchiveSuggest from './components/ArchiveSuggest';
import UpdatePrompt from './components/UpdatePrompt';
import NotificationBubble, { handleNotifyFocus } from './components/NotificationBubble';
import QuickSwitcher from './components/QuickSwitcher';
import HelpOverlay from './components/HelpOverlay';
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
  // 帮助面板（F1）：仅此一处使用，局部 state 即可，不入全局 store
  const [helpOpen, setHelpOpen] = useState(false);
  const archivePrompt = useAppStore((s) => s.archivePrompt);
  const [updatePrompt, setUpdatePrompt] = useState<UpdateInfo | null>(null);
  const [updateBusy, setUpdateBusy] = useState(false);
  const [updateError, setUpdateError] = useState('');
  const updateDismissedRef = useRef(false);
  // chatThrottle.drop 的桥接 ref：节流器在聊天事件 effect 内创建，
  // 关页签回调（handleCloseTab）在此取用，对已移除的 chat 丢弃缓冲。
  const throttleDropRef = useRef<(id: string) => void>(() => {});

  useEffect(() => {
    listTerminals()
      .then((list) => useAppStore.getState().setTerminals(list))
      .catch(() => {});
  }, []);

  useEffect(() => {
    // scan:done 全局刷新终端/聊天镜像：Go 侧扫描会把新发现的会话回填到终端/聊天
    //（SessionID/真实标题），页签标题与「恢复/切换」判断依赖这份镜像。
    // 镜像写的是全局 store，整应用订阅一次即可——各工作区页签各自订阅会 N 倍 IPC。
    const offScan = onScanDone(() => {
      listTerminals()
        .then((list) => useAppStore.getState().setTerminals(list))
        .catch(() => {});
      listChats()
        .then((list) => useAppStore.getState().setChats(list))
        .catch(() => {});
    });
    return offScan;
  }, []);

  useEffect(() => {
    return onUpdateAvailable((info) => {
      if (updateDismissedRef.current) return;
      setUpdatePrompt(info);
    });
  }, []);

  useEffect(() => {
    // agent 通知事件总线（hook / OSC / ACP 聊天共用）：入气泡队列并按语义响提示音，
    // 整应用只订阅一次。出错同属需介入，与「等待确认」共用重音。
    return onNotifyAgent((p) => {
      useAppStore.getState().pushAgentNotice(p);
      const label = eventLabel(p.event);
      // 完成与用户主动取消都不必惊动：轻音；提前停止/出错/待确认才用重音。
      playNotifySound(label === 'task_done' || label === 'task_cancelled' ? 'light' : 'attention');
    });
  }, []);

  useEffect(() => {
    // 原生气泡点击 → Go Emit notify:focus → 与窗内气泡点击同一套聚焦/dismiss。
    return onNotifyFocus((termKey) => handleNotifyFocus(termKey));
  }, []);

  useEffect(() => {
    // AgentHub 历史变更（Go 落盘后 Emit hub:changed）：重拉持久历史回填 store。
    // Hub 面板只在首页可见，但订阅放这里保证切回首页时数据已是最新。
    return onHubChanged(() => {
      void listHubMessages()
        .then((list) => useAppStore.getState().setHubMessages(list))
        .catch(() => {});
    });
  }, []);

  useEffect(() => {
    // 聊天事件总线 + 镜像重建（整应用只订阅一次）。
    // 仅在某 chat 的「历史回放窗口」内缓冲其实时更新：chat:update 可能先于 chatHistory 到达，
    // 其高 Seq 会抢先抬高 chatSeq，导致随后回放的历史前缀被去重丢弃。
    // 窗口外的更新立即应用——尤其挂载后才新建的聊天，绝不能因为没有初始 listChats 条目而被永久缓冲。
    // pendingRebuild 是重建重拉窗口（onChatMeta Rebuilt 标记开启），与种子窗口同一竞态语义。
    const pending = new Map<string, ChatUpdate[]>();
    // 流式分片节流：agent 高频输出（实测 50-60 事件/秒 × 多会话）会让长文本
    // markdown 全量重渲染打满主线程、页面假死。文本分片微批合并 ~100ms flush，
    // tool/plan/turn_done/error 等结构化事件不节流。chat 关闭/退出时丢弃缓冲。
    const throttle = createChatThrottle((id, u) => useAppStore.getState().applyChat(id, u as ChatUpdate));
    throttleDropRef.current = throttle.drop;
    const pendingRebuild = new Map<string, ChatUpdate[]>();
    const offU = onChatUpdate(({ id, update }) => {
      const rebuildBuf = pendingRebuild.get(id);
      if (rebuildBuf) {
        rebuildBuf.push(update);
        return;
      }
      const buf = pending.get(id);
      if (buf) {
        // 历史回放种子窗口内有意绕过节流器：窗口极短（单次 history await），
        // 且这些更新会先入 buf、回放后统一 apply，行为与旧直通路径等价。
        buf.push(update);
        return;
      }
      throttle.push(id, update);
    });
    const offP = onChatPermission(({ id, request }) =>
      useAppStore.getState().setChatPermission(id, request),
    );
    const offX = onChatExit(({ id, exitCode, error }) => {
      throttle.drop(id); // 会话已退出：丢弃在途缓冲，避免迟到分片污染终态时间线
      const { markChatExited, setChatPermission, notify, clearChatQueue } = useAppStore.getState();
      clearChatQueue(id); // 本地排队消息随会话消亡：进程已退出，发出也偏离用户意图
      markChatExited(id, exitCode, error);
      setChatPermission(id, null);
      let reason: string;
      if (!error) {
        reason = i18n.t('ui.notify.session_exited', { 0: exitCode }) as string;
      } else {
        const translated = translateBackend(error);
        // 未命中 wire key 的裸错误（如 "exit status 1"）：包一层本地化前缀并指明
        // 日志位置，避免一条无上下文英文串；wire key 仍走原翻译路径。
        reason =
          translated === error
            ? (i18n.t('ui.notify.session_exited_err', { err: error }) as string)
            : translated;
      }
      notify(reason, error ? 'error' : 'info');
    });
    // 技能命令列表：agent 的 available_commands_update，存 store 供输入框补全
    const offC = onChatCommands(({ id, commands }) => {
      useAppStore.getState().setChatCommands(id, commands);
    });
    // 会话重建标记（Rebuilt，随 chat:meta 推送）：重建会话的时间线换了基准
    //（manager 侧 fresh 起步、Seq 从头计数），旧 Seq 门控会把重放与新事件当重复
    // 丢弃——先清时间线与 Seq 再重拉 ChatHistory 重建基准。重拉期间缓冲该 chat
    // 的实时更新（pendingRebuild），与挂载种子窗口同一竞态处理。
    // 镜像行的常规 upsert 仍由下方 meta 订阅负责（剥掉 Rebuilt 瞬时标记）。
    const offM = onChatMeta((info) => {
      if (!info.Rebuilt) return;
      const buf: ChatUpdate[] = [];
      pendingRebuild.set(info.ID, buf);
      useAppStore.getState().setChatItems(info.ID, []);
      void chatHistory(info.ID)
        .then((hist) => {
          useAppStore.getState().setChatItems(
            info.ID,
            hist.reduce((acc, u) => applyChatUpdate(acc, u), [] as TimelineItem[]),
          );
        })
        .catch(() => {})
        .finally(() => {
          pendingRebuild.delete(info.ID);
          for (const u of buf) useAppStore.getState().applyChat(info.ID, u);
        });
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
      offC();
      offM();
      throttle.dispose();
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
        showWhitespace: !!info.showWhitespace,
      });
    };
    getAppearance().then(apply).catch(() => {});
    const off = onAppearanceChanged(apply);
    return off;
  }, []);

  useEffect(() => {
    // 用户一提交，Go 就改标题并打上 Prompted；这里立刻写进镜像，不必等下一轮扫描。
    // Rebuilt 重建标记由聊天事件总线 effect 处理（重置 Seq 门控并重拉历史），
    // 这里统一剥掉标记：镜像行不携带瞬时状态，避免后续 upsert 把它当新信号。
    const offTerm = onTerminalMeta((info) => useAppStore.getState().upsertTerminal(info));
    const offChat = onChatMeta((info) => useAppStore.getState().upsertChat({ ...info, Rebuilt: false }));
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
      notify(
        exitCode === 0
          ? (i18n.t('ui.notify.terminal_exited') as string)
          : (i18n.t('ui.notify.terminal_exited_code', { 0: exitCode }) as string),
        exitCode === 0 ? 'info' : 'error',
      );
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
        throttleDropRef.current(c.ID); // 丢弃在途流式缓冲，避免 flush 在已移除的 chat 上重建孤儿状态
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
    // 单一全局 keydown：window 级监听不受输入框焦点影响；退让规则与匹配都收敛在
    // handleGlobalKey（defaultPrevented 退让、xterm 聚焦分级），此处只注入动作。
    // store 数据经 getState 现取现用，监听器只挂一次、不随页签变化重挂。
    const onKey = (e: KeyboardEvent) => {
      handleGlobalKey(e, {
        switchToTab: (n) => {
          const { openTabs } = useAppStore.getState();
          const tab = openTabs[n - 1]; // n 从 1 起，越界（无第 N 个页签）忽略
          if (tab) setActiveTab(tab.id);
        },
        nextTab: () => {
          const { openTabs, activeTabId: cur } = useAppStore.getState();
          if (openTabs.length === 0) return;
          // 首页（null）视为第 -1 项：next 落到第一个页签
          const idx = openTabs.findIndex((t) => t.id === cur);
          setActiveTab(openTabs[(idx + 1) % openTabs.length].id);
        },
        prevTab: () => {
          const { openTabs, activeTabId: cur } = useAppStore.getState();
          if (openTabs.length === 0) return;
          // 首页 findIndex 得 -1：(-1-1) 取模落到最后一个页签
          const idx = openTabs.findIndex((t) => t.id === cur);
          setActiveTab(openTabs[(idx - 1 + openTabs.length) % openTabs.length].id);
        },
        closeActiveTab: () => {
          const { activeTabId: cur } = useAppStore.getState();
          // 首页与设置页没有可关的工作区内容，忽略
          if (cur === null || cur === SETTINGS_TAB_ID) return;
          handleCloseTab(cur);
        },
        goHome: () => setActiveTab(null),
        toggleSwitcher: () => setSwitcherOpen((v) => !v),
        focusSearch: () => {
          const { activeTabId: current } = useAppStore.getState();
          if (current !== null && current !== SETTINGS_TAB_ID) {
            // cancelable：Markdown 预览可见时优先消费（打开其检索工具条）
            window.dispatchEvent(new CustomEvent('kshell:focus-search', { cancelable: true }));
          } else if (current === null) {
            // 首页：聚焦工作区过滤框
            window.dispatchEvent(new CustomEvent('kshell:focus-home-filter'));
          }
        },
        toggleHelp: () => setHelpOpen((v) => !v),
      });
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [setActiveTab, handleCloseTab]);

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
      <AgentSetupDialog />
      <ProjectSetupDialog />
      <QuickSwitcher open={switcherOpen} onOpenChange={setSwitcherOpen} />
      <HelpOverlay open={helpOpen} onClose={() => setHelpOpen(false)} />
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
                // 正常路径进程会退出重启；若重启被拦截（Promise 已 resolve 但进程还在），
                // busy 必须复位，否则对话框永远 spinner、无法重试或点「稍后」。
                setUpdateBusy(false);
              } catch (e: unknown) {
                setUpdateError(backendError(e));
                setUpdateBusy(false);
              }
            })();
          }}
        />
      ) : null}
      <Toaster />
      <NotificationBubble />
    </TooltipProvider>
  );
}

export default App;
