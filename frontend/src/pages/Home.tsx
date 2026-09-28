// 首页：工作区列表（名称、会话数、git 标记），按会话数降序；点击打开工作区页签。
// 数据流：先渲染缓存（GetWorkspaces），再触发后台扫描，等 "scan:done" 后重调
// GetWorkspaces 刷新（统一走一条取数路径，事件 payload 只当触发信号用）。
import { useEffect } from 'react';
import { getWorkspaces, onScanDone, scanSessions } from '../lib/api';
import type { Workspace } from '../lib/api';
import { useAppStore } from '../state/store';
import { Badge } from '../components/ui/badge';
import { Button } from '../components/ui/button';
import { EmptyState } from '../components/ui/empty-state';
import { Skeleton } from '../components/ui/skeleton';

// 空态图标：手写内联 SVG（不引图标库）
function FolderGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="h-8 w-8" fill="none" stroke="currentColor" strokeWidth="1.5">
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        d="M3 7.5A2.5 2.5 0 0 1 5.5 5h3.6a2 2 0 0 1 1.4.6L12 7h6.5A2.5 2.5 0 0 1 21 9.5v7A2.5 2.5 0 0 1 18.5 19h-13A2.5 2.5 0 0 1 3 16.5v-9Z"
      />
    </svg>
  );
}

export default function Home() {
  const workspaces = useAppStore((s) => s.workspaces);
  const setWorkspaces = useAppStore((s) => s.setWorkspaces);
  const scanState = useAppStore((s) => s.scanState);
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

  // 重新扫描：复用 onScanDone 订阅刷新；扫描进行中按钮禁用防重复触发
  const handleRescan = () => {
    if (scanState === 'scanning') return;
    setScanState('scanning');
    scanSessions();
  };

  return (
    <div className="flex-1 overflow-y-auto p-6">
      <div className="max-w-2xl">
        <div className="mb-4 flex items-center justify-between gap-2">
          <h1 className="text-lg font-semibold">工作区</h1>
          <Button
            size="sm"
            variant="secondary"
            disabled={scanState === 'scanning'}
            onClick={handleRescan}
          >
            {scanState === 'scanning' ? '扫描中…' : '重新扫描'}
          </Button>
        </div>
        {/* 快捷键提示：kbd 风格小块（全局 Ctrl+K / Ctrl+F，见 App.tsx） */}
        <p className="-mt-2 mb-4 flex items-center gap-1.5 text-xs text-muted-foreground">
          <kbd className="rounded border border-border bg-secondary px-1.5 py-0.5 font-mono text-[10px] text-secondary-foreground">
            Ctrl K
          </kbd>
          切换
          <kbd className="rounded border border-border bg-secondary px-1.5 py-0.5 font-mono text-[10px] text-secondary-foreground">
            Ctrl F
          </kbd>
          搜索
        </p>
        {sorted.length === 0 ? (
          // 空态按 scanState 收敛：扫描未完成（idle/scanning）用骨架屏占位，
          // done 后仍未发现才是「确实没有」
          scanState !== 'done' ? (
            <div className="flex flex-col gap-2">
              <Skeleton className="h-16 rounded-lg border border-border" />
              <Skeleton className="h-16 rounded-lg border border-border" />
              <Skeleton className="h-16 rounded-lg border border-border" />
            </div>
          ) : (
            <EmptyState icon={<FolderGlyph />} title="未发现工作区" />
          )
        ) : (
          <ul className="flex flex-col gap-2">
            {sorted.map((ws: Workspace) => (
              <li key={ws.Path}>
                <button
                  className="flex w-full items-center gap-2 rounded-lg border border-border bg-card px-3 py-2.5 text-left transition-colors hover:border-ring/50 hover:shadow-sm"
                  onClick={() => openTab(ws)}
                  title={ws.Path}
                >
                  <span className="min-w-0 truncate text-sm font-medium">{ws.Name}</span>
                  {ws.Source === 'git' && <Badge variant="outline">git</Badge>}
                  <span className="ml-auto whitespace-nowrap text-xs text-muted-foreground">
                    {ws.SessionCount} 个会话
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
