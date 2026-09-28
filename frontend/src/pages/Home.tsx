// 首页：工作区卡片网格（名称、git 徽标、会话数、最后活动时间、工具分布），
// 按最后活动时间倒序 —— 从未用过（Go 零值时间）的 git 扫描工作区排最后，再按名称。
// 点击整卡打开工作区页签；键盘可达（整卡是 button）。
// 数据流：先渲染缓存（GetWorkspaces），再触发后台扫描，等 "scan:done" 后重调
// GetWorkspaces 刷新（统一走一条取数路径，事件 payload 只当触发信号用）。
import { useEffect } from 'react';
import { getWorkspaces, onScanDone, scanSessions } from '../lib/api';
import type { Workspace } from '../lib/api';
import { formatRelativeTime } from '../lib/format';
import { badgeFor } from '../lib/toolBadge';
import { useAppStore } from '../state/store';
import { Badge } from '../components/ui/badge';
import { Button } from '../components/ui/button';
import { EmptyState } from '../components/ui/empty-state';
import { Skeleton } from '../components/ui/skeleton';

// 卡片工具分布最多展示的徽标数，其余折叠成 "+N"
const TOOL_BADGES_MAX = 3;
// 网格列宽：窄窗口自动降列
const GRID = 'grid gap-3 grid-cols-[repeat(auto-fill,minmax(240px,1fr))]';

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

// LastUsed 的毫秒时间戳；Go 零值时间（0001-01-01T00:00:00Z，时间戳为负）与非法值
// （旧缓存缺字段）都视为「未使用过」：排序排最后，卡片上不显示相对时间。
function lastUsedTime(ws: Workspace): number | null {
  if (!ws.LastUsed) return null;
  const t = Date.parse(ws.LastUsed);
  if (Number.isNaN(t) || t <= 0) return null;
  return t;
}

// 工具分布：按会话数降序取前 TOOL_BADGES_MAX 个（并列按工具 ID 保证顺序稳定），其余折叠
function toolDistribution(counts: Record<string, number>) {
  const entries = Object.entries(counts ?? {}).sort(
    (a, b) => b[1] - a[1] || a[0].localeCompare(b[0]),
  );
  return { shown: entries.slice(0, TOOL_BADGES_MAX), rest: entries.length - TOOL_BADGES_MAX };
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

  // 最后活动时间倒序；未使用过（零值/空值）的排最后，再按名称
  const sorted = [...workspaces].sort((a, b) => {
    const ta = lastUsedTime(a);
    const tb = lastUsedTime(b);
    if (ta === null && tb === null) return a.Name.localeCompare(b.Name);
    if (ta === null) return 1;
    if (tb === null) return -1;
    return tb - ta || a.Name.localeCompare(b.Name);
  });

  const totalSessions = workspaces.reduce((n, ws) => n + ws.SessionCount, 0);

  // 重新扫描：复用 onScanDone 订阅刷新；扫描进行中按钮禁用防重复触发
  const handleRescan = () => {
    if (scanState === 'scanning') return;
    setScanState('scanning');
    scanSessions();
  };

  return (
    <div className="flex-1 overflow-y-auto p-6">
      <div className="mb-3 flex items-center justify-between gap-2">
        <h1 className="text-lg font-semibold">
          工作区
          <span className="ml-2 text-xs font-normal text-muted-foreground">
            {totalSessions} 个会话
          </span>
        </h1>
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
      <p className="mb-4 flex items-center gap-1.5 text-xs text-muted-foreground">
        <kbd className="rounded-sm border border-border bg-secondary px-1.5 py-0.5 font-mono text-[10px] text-secondary-foreground">
          Ctrl K
        </kbd>
        切换
        <kbd className="rounded-sm border border-border bg-secondary px-1.5 py-0.5 font-mono text-[10px] text-secondary-foreground">
          Ctrl F
        </kbd>
        搜索
      </p>
      {sorted.length === 0 ? (
        // 空态按 scanState 收敛：扫描未完成（idle/scanning）用骨架屏占位，
        // done 后仍未发现才是「确实没有」
        scanState !== 'done' ? (
          <div className={GRID}>
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-24 rounded border border-border" />
            ))}
          </div>
        ) : (
          <EmptyState icon={<FolderGlyph />} title="未发现工作区" />
        )
      ) : (
        <ul className={GRID}>
          {sorted.map((ws: Workspace) => {
            const lastUsed = lastUsedTime(ws);
            const { shown, rest } = toolDistribution(ws.ToolCounts);
            return (
              <li key={ws.Path} className="min-w-0">
                <button
                  className="flex h-full w-full min-w-0 flex-col gap-1.5 rounded border border-border bg-card px-3 py-2.5 text-left transition-colors hover:bg-muted"
                  onClick={() => openTab(ws)}
                  title={ws.Path}
                >
                  {/* 第一行：名称（单行截断）+ git 徽标 */}
                  <span className="flex min-w-0 items-center gap-1.5">
                    <span className="min-w-0 truncate text-sm font-medium">{ws.Name}</span>
                    {ws.Source === 'git' && <Badge variant="outline">git</Badge>}
                  </span>
                  {/* 第二行：会话数 + 最后活动时间 */}
                  <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-muted-foreground">
                    <span className="whitespace-nowrap">{ws.SessionCount} 个会话</span>
                    <span className="whitespace-nowrap">
                      {lastUsed === null
                        ? '未使用过'
                        : `最后活动 ${formatRelativeTime(ws.LastUsed)}`}
                    </span>
                  </span>
                  {/* 第三行：工具分布（最多 3 个 + "+N"） */}
                  {shown.length > 0 && (
                    <span className="flex min-w-0 flex-wrap items-center gap-1">
                      {shown.map(([toolID]) => (
                        <Badge key={toolID} variant="muted">
                          {badgeFor(toolID).label}
                        </Badge>
                      ))}
                      {rest > 0 && (
                        <span className="text-xs text-muted-foreground">+{rest}</span>
                      )}
                    </span>
                  )}
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
