// 桌面版主框架：自绘标题栏（首页 / 工作区页签 / 设置贴在窗口顶部）+ 内容区。
// 内容区采用「全部页签常挂载、非激活用 hidden」策略：切页签不卸载组件，
// 文件树展开态、SSH 状态、内嵌终端（xterm 缓冲与焦点）都不会丢。
// 终端事件总线在这里建立一次：terminal:data → 注册表分发到已挂载的 xterm，
// terminal:exit → 更新镜像并提示；Go 侧每会话保留 256KiB 环形缓冲兜住未挂载期间的输出。
// 全局快捷键：Ctrl+K 打开快速切换器，Ctrl+F 在工作区页签内派发 kshell:focus-search。
import { useCallback, useEffect, useState } from 'react';
import { closeTerminal, getBasket, listTerminals, onTerminalData, onTerminalExit } from './lib/api';
import { dispatchTerminalData } from './lib/terminalRegistry';
import { cn } from './lib/cn';
import Home from './pages/Home';
import Settings from './pages/Settings';
import WorkspaceTabView from './pages/WorkspaceTab';
import QuickSwitcher from './components/QuickSwitcher';
import TitleBar from './components/TitleBar';
import { Toaster } from './components/ui/toaster';
import { TooltipProvider } from './components/ui/tooltip';
import { SETTINGS_TAB_ID, useAppStore } from './state/store';

// 归一化工作区路径用于比较：会话记录里的 cwd 与工作区路径可能大小写/分隔符不一致
function sameWorkspace(a: string, b: string): boolean {
  return a.replace(/\\/g, '/').toLowerCase() === b.replace(/\\/g, '/').toLowerCase();
}

function App() {
  const openTabs = useAppStore((s) => s.openTabs);
  const activeTabId = useAppStore((s) => s.activeTabId);
  const setActiveTab = useAppStore((s) => s.setActiveTab);
  const closeTab = useAppStore((s) => s.closeTab);
  const [switcherOpen, setSwitcherOpen] = useState(false);

  useEffect(() => {
    // 只在挂载时重建一次篮子镜像；失败静默（未装配等场景篮子本就为空）
    getBasket()
      .then((paths) => useAppStore.getState().setBasket(paths))
      .catch(() => {});
  }, []);

  useEffect(() => {
    // 终端镜像重建：前端重载（开发态）或应用恢复时，Go 侧终端可能仍在跑
    listTerminals()
      .then((list) => useAppStore.getState().setTerminals(list))
      .catch(() => {});
  }, []);

  useEffect(() => {
    // 终端事件总线：整应用只订阅一次，避免每个终端组件各订阅一份
    const offData = onTerminalData(({ id, data }) => {
      dispatchTerminalData(id, data);
    });
    const offExit = onTerminalExit(({ id, exitCode }) => {
      const { markTerminalExited: mark, notify } = useAppStore.getState();
      mark(id, exitCode);
      notify(exitCode === 0 ? '终端已退出' : `终端异常退出（退出码 ${exitCode}）`, exitCode === 0 ? 'info' : 'error');
    });
    return () => {
      offData();
      offExit();
    };
  }, []);

  // 关闭工作区页签时连带结束该工作区的内嵌终端：进程不能留在后台又没有任何入口
  const handleCloseTab = useCallback(
    (id: string) => {
      const { terminals, removeTerminal } = useAppStore.getState();
      const owned = terminals.filter((t) => sameWorkspace(t.Workspace, id));
      closeTab(id);
      for (const t of owned) {
        removeTerminal(t.ID);
        closeTerminal(t.ID).catch(() => {});
      }
    },
    [closeTab],
  );

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
      <Toaster />
    </TooltipProvider>
  );
}

export default App;
