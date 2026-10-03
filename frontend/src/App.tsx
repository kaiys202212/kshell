// 桌面版主框架：自绘标题栏（首页 / 工作区页签 / 设置贴在窗口顶部）+ 内容区。
// 内容区采用「全部页签常挂载、非激活用 hidden」策略：切页签不卸载组件，
// 文件树展开态、SSH 状态、内嵌终端（xterm 缓冲与焦点）都不会丢。
// 终端事件总线在这里建立一次：terminal:data → 注册表分发到已挂载的 xterm，
// terminal:exit → 更新镜像并提示；Go 侧每会话保留 256KiB 环形缓冲兜住未挂载期间的输出。
// 全局快捷键：Ctrl+K 打开快速切换器，Ctrl+F 在工作区页签内派发 kshell:focus-search。
import { useCallback, useEffect, useState } from 'react';
import {
  closeTerminal,
  getAppearance,
  listTerminals,
  onAppearanceChanged,
  onProjectsChanged,
  onTerminalData,
  onTerminalExit,
} from './lib/api';
import type { AppearanceInfo } from './lib/appearance';
import { dispatchTerminalData } from './lib/terminalRegistry';
import { cn } from './lib/cn';
import { sameWorkspacePath } from './lib/workspacePath';
import Home from './pages/Home';
import Settings from './pages/Settings';
import WorkspaceTabView from './pages/WorkspaceTab';
import QuickSwitcher from './components/QuickSwitcher';
import TitleBar from './components/TitleBar';
import { Toaster } from './components/ui/toaster';
import { TooltipProvider } from './components/ui/tooltip';
import { SETTINGS_TAB_ID, useAppStore } from './state/store';

function App() {
  const openTabs = useAppStore((s) => s.openTabs);
  const activeTabId = useAppStore((s) => s.activeTabId);
  const setActiveTab = useAppStore((s) => s.setActiveTab);
  const closeTab = useAppStore((s) => s.closeTab);
  const [switcherOpen, setSwitcherOpen] = useState(false);

  useEffect(() => {
    // 终端镜像重建：前端重载（开发态）或应用恢复时，Go 侧终端可能仍在跑
    listTerminals()
      .then((list) => useAppStore.getState().setTerminals(list))
      .catch(() => {});
  }, []);

  useEffect(() => {
    // 颜色模式：初始取一次（写 data-theme 并进 store），再订阅后续变化
    const apply = (info: AppearanceInfo) => {
      document.documentElement.dataset.theme = info.resolved;
      useAppStore.getState().setAppearance(info);
    };
    getAppearance().then(apply).catch(() => {});
    const off = onAppearanceChanged(apply);
    return off;
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
      const owned = terminals.filter((t) => sameWorkspacePath(t.Workspace, id));
      closeTab(id);
      for (const t of owned) {
        removeTerminal(t.ID);
        closeTerminal(t.ID).catch(() => {});
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
