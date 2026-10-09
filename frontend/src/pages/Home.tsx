// 首页：工作区/项目卡片网格（名称、git 徽标、会话数、最后活动时间、工具分布），
// 按最后活动时间倒序 —— 从未用过（Go 零值时间）的 git 扫描工作区排最后，再按名称。
// 点击整卡打开工作区页签；键盘可达（整卡是 button）。
// 项目表操作：顶部「新建项目」先出本地/远程对话框；卡片 hover 出删除按钮（逻辑删除）；
// 「回收站」弹层列出已删除项目可逐项还原。删除/还原后工作区列表由事件驱动刷新。
// 数据流：先渲染缓存（GetWorkspaces），再触发后台扫描，等 "scan:done" 后重调
// GetWorkspaces 刷新（统一走一条取数路径，事件 payload 只当触发信号用）。
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import RemoteDirPicker from '../components/RemoteDirPicker';
import {
  addSSHProject,
  createProject,
  getDeletedProjects,
  getWorkspaces,
  hideProject,
  listConnections,
  onProjectsChanged,
  onScanDone,
  restoreProject,
  scanSessions,
  type DeletedProject,
  type SshConnection,
  type Workspace,
} from '../lib/api';
import { backendError } from '../lib/errors';
import { formatRelativeTime } from '../lib/format';
import { badgeFor } from '../lib/toolBadge';
import { MONO } from '../lib/ui';
import { SETTINGS_TAB_ID, useAppStore } from '../state/store';
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

type CreateStep = 'closed' | 'choose' | 'remote-conn' | 'remote-browse';

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

function isSSH(ws: Workspace): boolean {
  return ws.Kind === 'ssh';
}

function cardSubtitle(ws: Workspace, t: (key: string, opts?: Record<string, string>) => string): string {
  if (isSSH(ws)) {
    const conn = ws.ConnName || ws.ConnID || '';
    const path = ws.RemotePath || ws.Path;
    return t('ui.home.remote_subtitle', { conn, path });
  }
  return ws.Path;
}

export default function Home() {
  const { t } = useTranslation();
  const workspaces = useAppStore((s) => s.workspaces);
  const setWorkspaces = useAppStore((s) => s.setWorkspaces);
  const scanState = useAppStore((s) => s.scanState);
  const setScanState = useAppStore((s) => s.setScanState);
  const openTab = useAppStore((s) => s.openTab);
  const setActiveTab = useAppStore((s) => s.setActiveTab);
  const notify = useAppStore((s) => s.notify);

  const [deleted, setDeleted] = useState<DeletedProject[]>([]);
  const [binOpen, setBinOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [createStep, setCreateStep] = useState<CreateStep>('closed');
  const [createKind, setCreateKind] = useState<'local' | 'remote'>('local');
  const [connections, setConnections] = useState<SshConnection[]>([]);
  const [selectedConnID, setSelectedConnID] = useState('');

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

  const closeCreate = () => {
    setCreateStep('closed');
    setCreateKind('local');
    setSelectedConnID('');
    setConnections([]);
  };

  const handleCreateLocal = async () => {
    if (creating) return;
    setCreating(true);
    closeCreate();
    try {
      const dir = await createProject();
      if (!dir) return; // 用户取消：静默
      notify(t('ui.home.added', { name: baseName(dir) }), 'success');
      refresh();
      refreshDeleted();
    } catch (err) {
      notify(t('ui.home.create_failed', { err: backendError(err) }), 'error');
    } finally {
      setCreating(false);
    }
  };

  const openCreateDialog = () => {
    if (creating) return;
    setCreateKind('local');
    setCreateStep('choose');
  };

  const handleCreateContinue = async () => {
    if (createKind === 'local') {
      await handleCreateLocal();
      return;
    }
    try {
      const list = await listConnections('');
      setConnections(list);
      if (list.length === 0) {
        setCreateStep('remote-conn');
        setSelectedConnID('');
        return;
      }
      setSelectedConnID(list[0].ID);
      setCreateStep('remote-conn');
    } catch (err) {
      notify(t('ui.home.create_failed', { err: backendError(err) }), 'error');
      closeCreate();
    }
  };

  const handleRemoteBrowse = () => {
    if (!selectedConnID) return;
    setCreateStep('remote-browse');
  };

  const handleRemoteConfirm = async (remotePath: string) => {
    if (creating || !selectedConnID) return;
    setCreating(true);
    try {
      await addSSHProject(selectedConnID, remotePath);
      closeCreate();
      notify(t('ui.home.added', { name: baseName(remotePath) }), 'success');
      refresh();
      refreshDeleted();
    } catch (err) {
      notify(t('ui.home.create_failed', { err: backendError(err) }), 'error');
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (ws: Workspace) => {
    try {
      await hideProject(ws.Path);
      notify(t('ui.home.deleted', { name: ws.Name }));
      refresh();
      refreshDeleted();
    } catch (err) {
      notify(t('ui.home.delete_failed', { err: backendError(err) }), 'error');
    }
  };

  const handleRestore = async (path: string, name: string) => {
    try {
      await restoreProject(path);
      notify(t('ui.home.restored', { name }), 'success');
      refresh();
      refreshDeleted();
    } catch (err) {
      notify(t('ui.home.restore_failed', { err: backendError(err) }), 'error');
    }
  };

  const createOpen = createStep !== 'closed';

  return (
    <div className="flex-1 overflow-y-auto p-6">
      <div className="mb-3 flex items-center justify-between gap-2">
        <h1 className="text-lg font-semibold">
          {t('ui.home.title')}
          <span className="ml-2 text-xs font-normal text-muted-foreground">
            {t('ui.home.session_count', { count: totalSessions })}
          </span>
        </h1>
        <div className="flex items-center gap-2">
          <Button size="sm" disabled={creating} onClick={openCreateDialog}>
            {creating ? t('ui.home.creating') : t('ui.home.create_project')}
          </Button>
          <Dialog
            open={createOpen}
            onOpenChange={(open) => {
              if (!open) closeCreate();
            }}
            className="w-[min(92vw,28rem)] p-3 outline-none"
          >
            <DialogPrimitive.Title className="mb-2 text-sm font-medium">
              {createStep === 'remote-browse'
                ? t('ui.home.remote_browse_title')
                : t('ui.home.create_dialog_title')}
            </DialogPrimitive.Title>
            {createStep === 'choose' && (
              <div className="flex flex-col gap-3">
                <fieldset className="m-0 border-0 p-0">
                  <legend className="mb-1.5 text-xs text-muted-foreground">{t('ui.home.create_location')}</legend>
                  <div className="flex gap-2">
                    <Button
                      size="sm"
                      variant={createKind === 'local' ? 'default' : 'secondary'}
                      onClick={() => setCreateKind('local')}
                    >
                      {t('ui.home.create_local')}
                    </Button>
                    <Button
                      size="sm"
                      variant={createKind === 'remote' ? 'default' : 'secondary'}
                      onClick={() => setCreateKind('remote')}
                    >
                      {t('ui.home.create_remote')}
                    </Button>
                  </div>
                </fieldset>
                <div className="flex justify-end gap-2">
                  <Button size="sm" variant="secondary" onClick={closeCreate}>
                    {t('ui.home.create_cancel')}
                  </Button>
                  <Button size="sm" onClick={() => void handleCreateContinue()}>
                    {t('ui.home.create_continue')}
                  </Button>
                </div>
              </div>
            )}
            {createStep === 'remote-conn' && connections.length === 0 && (
              <div className="flex flex-col gap-3">
                <EmptyState
                  className="py-4"
                  title={t('ui.home.create_no_connections')}
                  hint={t('ui.home.create_no_connections_hint')}
                />
                <div className="flex justify-end gap-2">
                  <Button size="sm" variant="secondary" onClick={closeCreate}>
                    {t('ui.home.create_cancel')}
                  </Button>
                  <Button
                    size="sm"
                    onClick={() => {
                      closeCreate();
                      setActiveTab(SETTINGS_TAB_ID);
                    }}
                  >
                    {t('ui.home.create_go_settings')}
                  </Button>
                </div>
              </div>
            )}
            {createStep === 'remote-conn' && connections.length > 0 && (
              <div className="flex flex-col gap-3">
                <label className="flex flex-col gap-1 text-xs">
                  <span className="text-muted-foreground">{t('ui.home.create_pick_connection')}</span>
                  <select
                    className="h-8 rounded-[3px] border border-input bg-card px-2 text-[13px]"
                    value={selectedConnID}
                    onChange={(e) => setSelectedConnID(e.target.value)}
                  >
                    {connections.map((c) => (
                      <option key={c.ID} value={c.ID}>
                        {c.Name || c.Host}
                      </option>
                    ))}
                  </select>
                </label>
                <div className="flex justify-end gap-2">
                  <Button size="sm" variant="secondary" onClick={closeCreate}>
                    {t('ui.home.create_cancel')}
                  </Button>
                  <Button size="sm" disabled={!selectedConnID} onClick={handleRemoteBrowse}>
                    {t('ui.home.create_continue')}
                  </Button>
                </div>
              </div>
            )}
            {createStep === 'remote-browse' && selectedConnID && (
              <RemoteDirPicker
                connID={selectedConnID}
                onCancel={closeCreate}
                onConfirm={(path) => void handleRemoteConfirm(path)}
              />
            )}
          </Dialog>
          {/* 回收站：0 项时禁用（没有可还原的内容，点开只有空态） */}
          <Button
            size="sm"
            variant="secondary"
            disabled={deleted.length === 0}
            onClick={() => setBinOpen(true)}
          >
            {t('ui.home.recycle_bin')}{deleted.length > 0 ? ` (${deleted.length})` : ''}
          </Button>
          <Dialog
            open={binOpen}
            onOpenChange={setBinOpen}
            className="top-20 w-[560px] max-w-[90vw] p-3 outline-none"
          >
            <DialogPrimitive.Title className="mb-2 text-sm font-medium">
              {t('ui.home.recycle_bin')}
            </DialogPrimitive.Title>
            {deleted.length === 0 ? (
              <EmptyState
                className="py-6"
                title={t('ui.home.bin_empty_title')}
                hint={t('ui.home.bin_empty_hint')}
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
                    {!d.exists && <Badge variant="outline">{t('ui.home.dir_missing')}</Badge>}
                    <span className="shrink-0 text-xs text-muted-foreground">
                      {formatRelativeTime(d.at)}
                    </span>
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() => void handleRestore(d.path, d.name)}
                    >
                      {t('ui.home.restore')}
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
            {scanState === 'scanning' ? t('ui.home.scanning') : t('ui.home.rescan')}
          </Button>
        </div>
      </div>
      {/* 快捷键提示：kbd 风格小块（全局 Ctrl+K / Ctrl+F，见 App.tsx） */}
      <p className="mb-4 flex items-center gap-1.5 text-xs text-muted-foreground">
        <kbd className="rounded-sm border border-border bg-secondary px-1.5 py-0.5 font-mono text-[10px] text-secondary-foreground">
          Ctrl K
        </kbd>
        {t('ui.home.shortcut_switch')}
        <kbd className="rounded-sm border border-border bg-secondary px-1.5 py-0.5 font-mono text-[10px] text-secondary-foreground">
          Ctrl F
        </kbd>
        {t('ui.home.shortcut_search')}
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
          <EmptyState icon={<FolderGlyph />} title={t('ui.home.empty')} />
        )
      ) : (
        <ul className={GRID}>
          {sorted.map((ws: Workspace, i: number) => {
            const lastUsed = lastUsedTime(ws);
            const { shown, rest } = toolDistribution(ws.ToolCounts);
            // 左缘色条取占比最高的工具色；无工具上下文（如纯 git 扫描工作区）用主色
            const barColor = shown[0] ? badgeFor(shown[0][0]).color : 'var(--primary)';
            const subtitle = cardSubtitle(ws, t);
            return (
              <li
                key={ws.Path}
                className="group relative min-w-0"
                style={{
                  animation: 'kshell-rise-in var(--duration-base) var(--ease-out) both',
                  animationDelay: `${Math.min(i, 10) * 30}ms`,
                }}
              >
                <button
                  className="relative flex h-full w-full min-w-0 flex-col gap-1 rounded border border-border bg-card px-2.5 py-2 text-left transition-[background-color,border-color,box-shadow,transform] duration-[var(--duration-fast)] ease-[var(--ease-out)] hover:-translate-y-px hover:border-primary/40 hover:bg-muted hover:shadow-[var(--shadow-card)]"
                  onClick={() => openTab(ws)}
                  title={subtitle}
                >
                  <span
                    aria-hidden="true"
                    className="absolute bottom-3 left-0 top-3 w-[3px] rounded-full"
                    style={{ background: barColor }}
                  />
                  {/* 第一行：名称（单行截断）+ git / 远程 / 手动徽标 */}
                  <span className="flex min-w-0 items-center gap-1.5">
                    <span data-testid="ws-name" className="min-w-0 truncate text-[12.5px] font-medium">{ws.Name}</span>
                    {isSSH(ws) && <Badge variant="outline">{t('ui.home.remote_badge')}</Badge>}
                    {ws.Source === 'git' && <Badge variant="outline">git</Badge>}
                    {ws.Source === 'manual' && !isSSH(ws) && (
                      <Badge variant="outline">{t('ui.home.source_manual')}</Badge>
                    )}
                  </span>
                  {/* 副标题：本地完整路径 / 远程连接名+远端路径（完整路径见卡片 button title） */}
                  <span className={`min-w-0 truncate text-[10px] text-muted-foreground ${MONO}`}>
                    {subtitle}
                  </span>
                  {/* 会话数 + 最后活动时间 */}
                  <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-muted-foreground">
                    <span className={`whitespace-nowrap ${MONO}`}>{t('ui.home.session_count', { count: ws.SessionCount })}</span>
                    <span className={`whitespace-nowrap ${MONO}`}>
                      {lastUsed === null
                        ? t('ui.home.never_used')
                        : t('ui.home.last_active', { rel: formatRelativeTime(ws.LastUsed) })}
                    </span>
                  </span>
                  {/* 工具分布（最多 3 个 + "+N"） */}
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
                  aria-label={t('ui.home.delete_project', { name: ws.Name })}
                  title={t('ui.home.delete_project_title')}
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
