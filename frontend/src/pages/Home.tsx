// 首页：工作区/项目卡片网格（名称、git 徽标、会话数、最后活动时间、工具分布），
// 按最后活动时间倒序 —— 从未用过（Go 零值时间）的 git 扫描工作区排最后，再按名称。
// 点击整卡打开工作区页签；键盘可达（整卡是 button）。
// 项目表操作：顶部「新建项目」调原生目录选择器；卡片 hover 出删除按钮（逻辑删除）；
// 「回收站」弹层列出已删除项目可逐项还原。删除/还原后工作区列表由事件驱动刷新。
// 数据流：先渲染缓存（GetWorkspaces），再触发后台扫描，等 "scan:done" 后重调
// GetWorkspaces 刷新（统一走一条取数路径，事件 payload 只当触发信号用）。
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { useCallback, useEffect, useState } from 'react';
import {
  createProject,
  getDeletedProjects,
  getWorkspaces,
  hideProject,
  onProjectsChanged,
  onScanDone,
  restoreProject,
  scanSessions,
} from '../lib/api';
import type { DeletedProject, Workspace } from '../lib/api';
import { formatRelativeTime } from '../lib/format';
import { MONO } from '../lib/ui';
import { useAppStore } from '../state/store';
import { Badge } from '../components/ui/badge';
import { Button } from '../components/ui/button';
import { Dialog } from '../components/ui/dialog';
import { EmptyState } from '../components/ui/empty-state';
import { Skeleton } from '../components/ui/skeleton';
import { ToolDot } from '../components/ui/tool-dot';

// 卡片工具分布最多展示的徽标数，其余折叠成 "+N"
const TOOL_BADGES_MAX = 3;
// 网格列宽：窄窗口自动降列
const GRID = 'grid gap-2.5 grid-cols-[repeat(auto-fill,minmax(220px,1fr))]';

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

// 删除按钮图标（垃圾桶）：比文字更省空间，配合 aria-label 保证可访问性
function TrashGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.6">
      <path strokeLinecap="round" strokeLinejoin="round" d="M4 7h16M9 7V5h6v2M6 7l1 13h10l1-13" />
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

// 取路径末级目录名作为项目显示名（与 Go 侧 WorkspaceName 同口径）
function baseName(p: string): string {
  const parts = p.replace(/\\/g, '/').replace(/\/+$/, '').split('/');
  return parts[parts.length - 1] || p;
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

export default function Home() {
  const workspaces = useAppStore((s) => s.workspaces);
  const setWorkspaces = useAppStore((s) => s.setWorkspaces);
  const scanState = useAppStore((s) => s.scanState);
  const setScanState = useAppStore((s) => s.setScanState);
  const openTab = useAppStore((s) => s.openTab);
  const notify = useAppStore((s) => s.notify);

  const [deleted, setDeleted] = useState<DeletedProject[]>([]);
  const [binOpen, setBinOpen] = useState(false);
  const [creating, setCreating] = useState(false);

  const refresh = useCallback(() => {
    getWorkspaces()
      .then((list) => setWorkspaces(list))
      .catch(() => {
        // 绑定调用异常时静默，等 scan:done 或 projects:changed 再触发下一轮刷新
      });
  }, [setWorkspaces]);

  const refreshDeleted = useCallback(() => {
    getDeletedProjects()
      .then((list) => setDeleted(list))
      .catch(() => {});
  }, []);

  useEffect(() => {
    let cancelled = false;
    const load = () => {
      getWorkspaces()
        .then((list) => {
          if (!cancelled) setWorkspaces(list);
        })
        .catch(() => {});
      getDeletedProjects()
        .then((list) => {
          if (!cancelled) setDeleted(list);
        })
        .catch(() => {});
    };
    setScanState('scanning');
    load();
    scanSessions(); // 触发后台扫描，完成与否都推 "scan:done"
    const offScan = onScanDone(() => {
      setScanState('done');
      void load();
    });
    // 项目表变更（新建/删除/还原）：工作区与回收站都重拉
    const offProjects = onProjectsChanged(() => {
      if (cancelled) return;
      refresh();
      refreshDeleted();
    });
    return () => {
      cancelled = true;
      offScan();
      offProjects();
    };
  }, [setWorkspaces, setScanState, refresh, refreshDeleted]);

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

  const handleCreate = async () => {
    if (creating) return;
    setCreating(true);
    try {
      const dir = await createProject();
      if (!dir) return; // 用户取消：静默
      notify(`已添加项目「${baseName(dir)}」`, 'success');
      refresh();
      refreshDeleted();
    } catch (err) {
      notify(`新建项目失败：${errorText(err)}`, 'error');
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (ws: Workspace) => {
    try {
      await hideProject(ws.Path);
      notify(`已删除「${ws.Name}」，可在回收站还原`);
      refresh();
      refreshDeleted();
    } catch (err) {
      notify(`删除项目失败：${errorText(err)}`, 'error');
    }
  };

  const handleRestore = async (path: string, name: string) => {
    try {
      await restoreProject(path);
      notify(`已还原「${name}」`, 'success');
      refresh();
      refreshDeleted();
    } catch (err) {
      notify(`还原项目失败：${errorText(err)}`, 'error');
    }
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
        <div className="flex items-center gap-2">
          <Button size="sm" disabled={creating} onClick={() => void handleCreate()}>
            {creating ? '添加中…' : '新建项目'}
          </Button>
          {/* 回收站：0 项时禁用（没有可还原的内容，点开只有空态） */}
          <Button
            size="sm"
            variant="secondary"
            disabled={deleted.length === 0}
            onClick={() => setBinOpen(true)}
          >
            回收站{deleted.length > 0 ? ` (${deleted.length})` : ''}
          </Button>
          <Dialog
            open={binOpen}
            onOpenChange={setBinOpen}
            className="top-20 w-[560px] max-w-[90vw] p-3 outline-none"
          >
            <DialogPrimitive.Title className="mb-2 text-sm font-medium">
              回收站
            </DialogPrimitive.Title>
            {deleted.length === 0 ? (
              <EmptyState
                className="py-6"
                title="回收站是空的"
                hint="删除的项目会先放到这里，可随时还原"
              />
            ) : (
              <ul className="m-0 flex max-h-80 list-none flex-col gap-1 overflow-y-auto p-0">
                {deleted.map((d) => (
                  <li
                    key={d.path}
                    className="flex items-center gap-2 rounded border border-border px-2 py-1.5"
                  >
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-xs font-medium">{d.name}</span>
                      <span className={`block truncate ${MONO}`} title={d.path}>
                        {d.path}
                      </span>
                    </span>
                    {!d.exists && <Badge variant="outline">目录已不存在</Badge>}
                    <span className="shrink-0 text-xs text-muted-foreground">
                      {formatRelativeTime(d.at)}
                    </span>
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() => void handleRestore(d.path, d.name)}
                    >
                      还原
                    </Button>
                  </li>
                ))}
              </ul>
            )}
          </Dialog>
          <Button
            size="sm"
            variant="secondary"
            disabled={scanState === 'scanning'}
            onClick={handleRescan}
          >
            {scanState === 'scanning' ? '扫描中…' : '重新扫描'}
          </Button>
        </div>
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
              <li key={ws.Path} className="group relative min-w-0">
                <button
                  className="flex h-full w-full min-w-0 flex-col gap-1 rounded border border-border bg-card px-2.5 py-2 text-left transition-colors hover:bg-muted"
                  onClick={() => openTab(ws)}
                  title={ws.Path}
                >
                  {/* 第一行：名称（单行截断）+ git 徽标 */}
                  <span className="flex min-w-0 items-center gap-1.5">
                    <span data-testid="ws-name" className="min-w-0 truncate text-[12.5px] font-medium">{ws.Name}</span>
                    {ws.Source === 'git' && <Badge variant="outline">git</Badge>}
                    {ws.Source === 'manual' && <Badge variant="outline">项目</Badge>}
                  </span>
                  {/* 第二行：会话数 + 最后活动时间 */}
                  <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-muted-foreground">
                    <span className={`whitespace-nowrap ${MONO}`}>{ws.SessionCount} 个会话</span>
                    <span className={`whitespace-nowrap ${MONO}`}>
                      {lastUsed === null
                        ? '未使用过'
                        : `最后活动 ${formatRelativeTime(ws.LastUsed)}`}
                    </span>
                  </span>
                  {/* 第三行：工具分布（最多 3 个 + "+N"） */}
                  {shown.length > 0 && (
                    <span className="flex min-w-0 flex-wrap items-center gap-1">
                      {shown.map(([toolID]) => (
                        <ToolDot key={toolID} toolID={toolID} />
                      ))}
                      {rest > 0 && (
                        <span className="text-xs text-muted-foreground">+{rest}</span>
                      )}
                    </span>
                  )}
                </button>
                {/* 删除按钮：绝对定位的兄弟节点（卡片本身是 button，不能嵌套），hover 显现 */}
                <button
                  type="button"
                  aria-label={`删除项目 ${ws.Name}`}
                  title="删除项目（可在回收站还原）"
                  className="absolute right-1 top-1 hidden rounded p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-destructive group-hover:block"
                  onClick={() => void handleDelete(ws)}
                >
                  <TrashGlyph />
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
