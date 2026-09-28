// 桌面版主框架：顶部页签栏（首页固定 + 设置固定 + 工作区页签，可多开可关闭），
// 下方按激活页签切换首页 / 设置 / 工作区内容。
// 挂载时调一次 GetBasket 重建篮子镜像：Go 侧篮子在应用生命周期内持续存在，
// 前端刷新/重开后必须拉取一次，否则镜像与 Go 状态漂移。
import { useEffect } from 'react';
import { getBasket } from './lib/api';
import Home from './pages/Home';
import Settings from './pages/Settings';
import WorkspaceTabView from './pages/WorkspaceTab';
import { Toaster } from './components/ui/toaster';
import { SETTINGS_TAB_ID, useAppStore } from './state/store';

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
    <>
      <div className="app">
        <div className="tabbar">
          <button
            className={activeTabId === null ? 'tab tab-active' : 'tab'}
            onClick={() => setActiveTab(null)}
          >
            首页
          </button>
          <button
            className={activeTabId === SETTINGS_TAB_ID ? 'tab tab-active' : 'tab'}
            onClick={() => setActiveTab(SETTINGS_TAB_ID)}
          >
            设置
          </button>
          {openTabs.map((t) => (
            <div
              key={t.id}
              className={activeTab?.id === t.id ? 'tab tab-active' : 'tab'}
            >
              <button className="tab-title" onClick={() => setActiveTab(t.id)}>
                {t.name}
              </button>
              <button
                className="tab-close"
                aria-label={`关闭 ${t.name}`}
                onClick={() => closeTab(t.id)}
              >
                ×
              </button>
            </div>
          ))}
        </div>
        {activeTabId === SETTINGS_TAB_ID ? (
          <Settings />
        ) : activeTab ? (
          <WorkspaceTabView tab={activeTab} />
        ) : (
          <Home />
        )}
      </div>
      <Toaster />
    </>
  );
}

export default App;
