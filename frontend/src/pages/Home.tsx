// 首页：工作区列表（名称、会话数、git 标记），按会话数降序；点击打开工作区页签。
// 数据流：先渲染缓存（GetWorkspaces），再触发后台扫描，等 "scan:done" 后重调
// GetWorkspaces 刷新（统一走一条取数路径，事件 payload 只当触发信号用）。
import { useEffect } from 'react';
import { getWorkspaces, onScanDone, scanSessions } from '../lib/api';
import type { Workspace } from '../lib/api';
import { useAppStore } from '../state/store';

export default function Home() {
  const workspaces = useAppStore((s) => s.workspaces);
  const setWorkspaces = useAppStore((s) => s.setWorkspaces);
  const setScanState = useAppStore((s) => s.setScanState);
  const openTab = useAppStore((s) => s.openTab);

  useEffect(() => {
    let cancelled = false;
    const refresh = () =>
      getWorkspaces()
        .then((list) => {
          if (!cancelled) setWorkspaces(list);
        })
        .catch(() => {
          // 绑定调用异常时静默，等 scan:done 再触发下一轮刷新
        });
    setScanState('scanning');
    refresh();
    scanSessions(); // 触发后台扫描，完成与否都推 "scan:done"
    const off = onScanDone(() => {
      setScanState('done');
      void refresh();
    });
    return () => {
      cancelled = true;
      off();
    };
  }, [setWorkspaces, setScanState]);

  const sorted = [...workspaces].sort(
    (a, b) => b.SessionCount - a.SessionCount || a.Name.localeCompare(b.Name),
  );

  return (
    <div className="home">
      <h1 className="home-title">工作区</h1>
      {sorted.length === 0 ? (
        <p className="home-empty">未发现工作区，正在扫描……</p>
      ) : (
        <ul className="ws-list">
          {sorted.map((ws: Workspace) => (
            <li key={ws.Path}>
              <button
                className="ws-item"
                onClick={() => openTab(ws)}
                title={ws.Path}
              >
                <span className="ws-name">{ws.Name}</span>
                {ws.Source === 'git' && <span className="ws-badge">git</span>}
                <span className="ws-count">{ws.SessionCount} 个会话</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
