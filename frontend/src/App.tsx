// 桌面版主框架：顶部页签栏（首页固定 + 工作区页签，可多开可关闭），
// 下方按激活页签切换首页 / 工作区内容。
import Home from './pages/Home';
import WorkspaceTabView from './pages/WorkspaceTab';
import { useAppStore } from './state/store';

function App() {
  const openTabs = useAppStore((s) => s.openTabs);
  const activeTabId = useAppStore((s) => s.activeTabId);
  const setActiveTab = useAppStore((s) => s.setActiveTab);
  const closeTab = useAppStore((s) => s.closeTab);

  const activeTab = openTabs.find((t) => t.id === activeTabId) ?? null;

  return (
    <div className="app">
      <div className="tabbar">
        <button
          className={activeTab === null ? 'tab tab-active' : 'tab'}
          onClick={() => setActiveTab(null)}
        >
          首页
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
      {activeTab ? <WorkspaceTabView tab={activeTab} /> : <Home />}
    </div>
  );
}

export default App;
