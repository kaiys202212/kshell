// 桌面版主框架：顶部页签栏（首页固定 + 设置固定 + 工作区页签，可多开可关闭），
// 下方按激活页签切换首页 / 设置 / 工作区内容。
// 挂载时调一次 GetBasket 重建篮子镜像：Go 侧篮子在应用生命周期内持续存在，
// 前端刷新/重开后必须拉取一次，否则镜像与 Go 状态漂移。
import { useEffect } from 'react';
import { getBasket } from './lib/api';
import { cn } from './lib/cn';
import Home from './pages/Home';
import Settings from './pages/Settings';
import WorkspaceTabView from './pages/WorkspaceTab';
import { Toaster } from './components/ui/toaster';
import { TooltipProvider } from './components/ui/tooltip';
import { SETTINGS_TAB_ID, useAppStore } from './state/store';

// 页签胶囊基础态：未激活 muted 文字 + hover 反馈；激活态用 accent 填充
const tabBase =
  'flex h-8 shrink-0 items-center rounded-md px-3 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground';
const tabActive = 'bg-accent text-accent-foreground font-medium hover:bg-accent';

function App() {
  const openTabs = useAppStore((s) => s.openTabs);
  const activeTabId = useAppStore((s) => s.activeTabId);
  const setActiveTab = useAppStore((s) => s.setActiveTab);
  const closeTab = useAppStore((s) => s.closeTab);

  const activeTab = openTabs.find((t) => t.id === activeTabId) ?? null;

  useEffect(() => {
    // 只在挂载时重建一次镜像；失败静默（未装配等场景篮子本就为空）
    getBasket()
      .then((paths) => useAppStore.getState().setBasket(paths))
      .catch(() => {});
  }, []);

  return (
    <TooltipProvider>
      <div className="flex h-screen flex-col">
        <header className="flex h-11 shrink-0 items-center gap-1 overflow-x-auto border-b border-border bg-card px-2">
          <span className="mr-2 shrink-0 text-sm font-semibold tracking-wide">
            <span className="text-primary">k</span>shell
          </span>
          <button
            className={cn(tabBase, activeTabId === null && tabActive)}
            onClick={() => setActiveTab(null)}
          >
            首页
          </button>
          <button
            className={cn(tabBase, activeTabId === SETTINGS_TAB_ID && tabActive)}
            onClick={() => setActiveTab(SETTINGS_TAB_ID)}
          >
            设置
          </button>
          {openTabs.length > 0 && (
            <div className="mx-1 h-5 w-px shrink-0 bg-border" aria-hidden="true" />
          )}
          {openTabs.map((t) => {
            const active = activeTab?.id === t.id;
            return (
              <div
                key={t.id}
                className={cn('group flex shrink-0 items-center rounded-md pr-1', tabBase, active && tabActive)}
              >
                <button className="text-sm" onClick={() => setActiveTab(t.id)}>
                  {t.name}
                </button>
                <button
                  className={cn(
                    'ml-0.5 flex h-5 w-5 items-center justify-center rounded text-base leading-none text-muted-foreground transition-opacity hover:bg-background hover:text-foreground',
                    active ? 'opacity-70 hover:opacity-100' : 'opacity-0 group-hover:opacity-100',
                  )}
                  aria-label={`关闭 ${t.name}`}
                  onClick={() => closeTab(t.id)}
                >
                  ×
                </button>
              </div>
            );
          })}
        </header>
        {activeTabId === SETTINGS_TAB_ID ? (
          <Settings />
        ) : activeTab ? (
          <WorkspaceTabView tab={activeTab} />
        ) : (
          <Home />
        )}
      </div>
      <Toaster />
    </TooltipProvider>
  );
}

export default App;
