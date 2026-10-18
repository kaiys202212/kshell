// RequirementList：首页左列的需求清单卡片。条目可新增/编辑/删除、可拖拽到项目卡片
// 派发（dragstart 写入自定义 MIME + text/plain，项目卡片 onDrop 解析后建 ACP 会话）。
// 状态机：未开始(pending) → 派发后 active → 手动标记 done → 手动归档 archived；
// active 显「标记完成」、done 显「归档」，archived 条目收进归档检索视图（只读）。
// 数据流：挂载拉取 listRequirements（全量含 archived），写操作后 Go 侧 emit
// "requirements:changed"，本组件订阅该事件重拉（单一数据源，不做本地乐观插入）。
import { useCallback, useEffect, useMemo, useState } from 'react';
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { useTranslation } from 'react-i18next';
import {
  addRequirement,
  archiveRequirement,
  deleteRequirement,
  listRequirements,
  onRequirementsChanged,
  readClipboardPaste,
  setRequirementStatus,
  updateRequirement,
  type Requirement,
  type Workspace,
} from '../lib/api';
import { backendError } from '../lib/errors';
import { quotePathForShell } from '../lib/dragPath';
import { baseName } from '../lib/hubAttribution';
import { formatRelativeTime } from '../lib/format';
import { reopenRequirementSession, dispatchRequirement } from '../lib/dispatchRequirement';
import { loadDefaultCreate } from '../lib/agenthubDefaults';
import { sameWorkspacePath } from '../lib/workspacePath';
import { focusNoticeTarget } from './NotificationBubble';
import { cn } from '../lib/cn';
import { useAppStore } from '../state/store';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Dialog } from './ui/dialog';
import { EmptyState } from './ui/empty-state';

// 拖拽 payload 的自定义 MIME 类型（Home 卡片 onDrop 按此识别需求条目）
export const REQUIREMENT_MIME = 'application/x-kshell-requirement';

// 优先级色点配色：high 红、medium 主色、low muted
function dotClass(priority: Requirement['priority']): string {
  if (priority === 'high') return 'bg-destructive';
  if (priority === 'low') return 'bg-muted-foreground';
  return 'bg-primary';
}

// 会话运行状态点配色（token 体系内取色）：running 绿、starting 主色、
// ready 灰、exited/不存在 红（退出或重启失效同视「已结束」）
function sessionDotClass(status: string | undefined): string {
  if (status === 'running') return 'bg-success';
  if (status === 'starting') return 'bg-primary';
  if (status === 'ready') return 'bg-muted-foreground';
  return 'bg-destructive';
}

// 状态徽章配色（token 体系内取色）：pending 默认描边、active 主色、done 绿、archived 灰
function statusBadgeVariant(status: Requirement['status']): 'default' | 'muted' | 'outline' {
  if (status === 'active') return 'default';
  if (status === 'done') return 'default';
  if (status === 'archived') return 'muted';
  return 'outline';
}

// 归档提示里的「会话名」：优先派发目标项目基名（chatId hex 无语义，仅兜底）；
// 两者都无时返回空串，由调用方退化为不含 name 的通用提示
function archiveSessionName(r: Requirement): string {
  if (r.dispatchedTo) return baseName(r.dispatchedTo);
  return r.chatId ?? '';
}

// 跳转按钮图标（箭头出框）
function JumpGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.6">
      <path strokeLinecap="round" strokeLinejoin="round" d="M14 5h5v5M19 5l-8 8M18 13v5a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5" />
    </svg>
  );
}

// 重新打开按钮图标（循环箭头）
function ReopenGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.6">
      <path strokeLinecap="round" strokeLinejoin="round" d="M20 12a8 8 0 1 1-2.3-5.6M20 3v4h-4" />
    </svg>
  );
}

// 编辑/新增对话框：受控 open，editing 非 null 表示编辑该条目。
function RequirementDialog({
  open,
  editing,
  onOpenChange,
}: {
  open: boolean;
  editing: Requirement | null;
  onOpenChange: (v: boolean) => void;
}) {
  const { t } = useTranslation();
  const workspaces = useAppStore((s) => s.workspaces);
  const [project, setProject] = useState('');
  const [text, setText] = useState('');
  const [priority, setPriority] = useState<Requirement['priority']>('medium');
  const [tagsInput, setTagsInput] = useState('');
  const [busy, setBusy] = useState(false);

  // 打开时按编辑对象回填；新增则默认当前选中项目（编辑场景 project 是后端必填字段）
  useEffect(() => {
    if (!open) return;
    setProject(editing?.project ?? useAppStore.getState().selectedProject ?? '');
    setText(editing?.text ?? '');
    setPriority(editing?.priority ?? 'medium');
    setTagsInput((editing?.tags ?? []).join(', '));
    setBusy(false);
  }, [open, editing]);

  const save = () => {
    const trimmed = text.trim();
    if (!trimmed || busy) return;
    // 第一道防线：project 必填（后端还有 err.requirements.project_required 兜底）
    if (!project) {
      useAppStore.getState().notify(t('err.requirements.project_required'), 'error');
      return;
    }
    // 逗号分隔转数组：去空白、丢空段、去重（重复标签会让 Badge key 冲突）
    const tags = [...new Set(
      tagsInput
        .split(/[,,]/)
        .map((s) => s.trim())
        .filter(Boolean),
    )];
    setBusy(true);
    const call = editing
      ? updateRequirement(editing.id, project, trimmed, priority, tags)
      : addRequirement(project, trimmed, priority, tags);
    void call
      // 成功后靠 requirements:changed 事件回流刷新，这里只关弹窗
      .then(() => onOpenChange(false))
      .catch((e: unknown) => {
        useAppStore.getState().notify(t('ui.requirements.save_failed', { err: backendError(e) }), 'error');
        setBusy(false);
      });
  };

  // 粘贴附件：clipboardData 带文件（资源管理器复制 / 截图）时拦截默认粘贴，
  // 经原生剪贴板拿落盘路径（图片已自动落盘）转成地址追加到需求文本；
  // 纯文本走浏览器默认粘贴。读取失败静默（粘贴是辅助操作）。
  const handlePaste = async (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    if (!e.clipboardData.types.includes('Files')) return;
    e.preventDefault();
    try {
      const paste = await readClipboardPaste();
      if (!paste.Path) return;
      const add = quotePathForShell(paste.Path);
      setText((cur) => (cur ? `${cur} ${add}` : add));
    } catch {
      // 静默
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange} className="w-[min(92vw,34rem)]">
      <DialogPrimitive.Title className="mb-3 text-sm font-medium">
        {editing ? t('ui.requirements.edit') : t('ui.requirements.add')}
      </DialogPrimitive.Title>
      <label className="mb-2 flex flex-col gap-1 text-xs text-muted-foreground">
        {t('ui.requirements.field_project')}
        <select
          data-testid="req-project"
          className="rounded border border-border bg-card px-2 py-1 text-sm text-foreground"
          value={project}
          onChange={(e) => setProject(e.target.value)}
        >
          {/* 显式空选项：无预选项目时让用户看到「请选择项目」，理解保存为何禁用 */}
          <option value="" disabled>
            {t('ui.requirements.select_project')}
          </option>
          {workspaces.map((w) => (
            <option key={w.Path} value={w.Path}>
              {w.Name}
            </option>
          ))}
        </select>
      </label>
      <label className="mb-2 flex flex-col gap-1 text-xs text-muted-foreground">
        {t('ui.requirements.text_label')}
        <textarea
          data-testid="req-text"
          className="min-h-32 w-full resize-none rounded border border-border bg-card p-2 text-sm text-foreground"
          placeholder={t('ui.requirements.text_placeholder')}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onPaste={(e) => void handlePaste(e)}
        />
      </label>
      <label className="mb-2 flex flex-col gap-1 text-xs text-muted-foreground">
        {t('ui.requirements.priority_label')}
        <select
          data-testid="req-priority"
          className="rounded border border-border bg-card px-2 py-1 text-sm text-foreground"
          value={priority}
          onChange={(e) => setPriority(e.target.value as Requirement['priority'])}
        >
          <option value="high">{t('ui.requirements.priority_high')}</option>
          <option value="medium">{t('ui.requirements.priority_medium')}</option>
          <option value="low">{t('ui.requirements.priority_low')}</option>
        </select>
      </label>
      <label className="mb-3 flex flex-col gap-1 text-xs text-muted-foreground">
        {t('ui.requirements.tags_label')}
        <input
          data-testid="req-tags"
          type="text"
          className="h-8 rounded border border-border bg-card px-2 text-sm text-foreground"
          placeholder={t('ui.requirements.tags_placeholder')}
          value={tagsInput}
          onChange={(e) => setTagsInput(e.target.value)}
        />
      </label>
      <div className="flex justify-end gap-2">
        <Button size="sm" variant="outline" onClick={() => onOpenChange(false)}>
          {t('ui.requirements.cancel')}
        </Button>
        <Button size="sm" disabled={!text.trim() || !project || busy} onClick={save}>
          {t('ui.requirements.save')}
        </Button>
      </div>
    </Dialog>
  );
}

// projectDisplayName 项目徽标显示名：优先工作区列表里的显示名（Name），
// 匹配不到（工作区已删/列表未加载）退化为路径末段。
function projectDisplayName(project: string, workspaces: Workspace[]): string {
  const ws = workspaces.find((w) => sameWorkspacePath(w.Path, project));
  return ws?.Name || baseName(project);
}

// 归档检索视图：全量归档条目的只读列表，支持项目 / 关键字 / tag 过滤。
// 数据由父组件传入（items 已是全量，这里只做过滤与排序，不重复请求）。
function ArchiveDialog({
  open,
  onOpenChange,
  items,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  items: Requirement[];
}) {
  const { t } = useTranslation();
  const [project, setProject] = useState('');
  const [keyword, setKeyword] = useState('');
  const [tag, setTag] = useState('');

  // 打开时重置过滤，避免上次的选择残留
  useEffect(() => {
    if (open) {
      setProject('');
      setKeyword('');
      setTag('');
    }
  }, [open]);

  const archived = useMemo(
    () => items.filter((r) => r.status === 'archived'),
    [items],
  );
  // 项目下拉选项：dispatchedTo 基名去重
  const projects = useMemo(
    () =>
      [...new Set(
        archived
          .map((r) => (r.dispatchedTo ? baseName(r.dispatchedTo) : undefined))
          .filter((p): p is string => !!p),
      )],
    [archived],
  );
  // tag chips：归档条目全部 tag 去重（tags 可能为存量 null，防御性兜底）
  const tags = useMemo(() => [...new Set(archived.flatMap((r) => r.tags ?? []))], [archived]);

  // 过滤 + archivedAt 降序（0 视为最旧沉底）；关键字匹配大小写不敏感
  // （tag 过滤是点 chip 精确选中，与关键字独立，无需大小写处理）
  const shown = useMemo(
    () =>
      archived
        .filter((r) => {
          const kw = keyword.toLowerCase();
          return (!project || (r.dispatchedTo && baseName(r.dispatchedTo) === project))
            && (!kw || r.text.toLowerCase().includes(kw))
            && (!tag || (r.tags ?? []).includes(tag));
        })
        .sort((a, b) => (b.archivedAt ?? 0) - (a.archivedAt ?? 0)),
    [archived, project, keyword, tag],
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange} className="w-[min(92vw,42rem)]">
      <DialogPrimitive.Title className="mb-3 text-sm font-medium">
        {t('ui.requirements.archived_title')}
      </DialogPrimitive.Title>
      {archived.length === 0 ? (
        <p className="py-6 text-center text-sm text-muted-foreground">
          {t('ui.requirements.archived_empty')}
        </p>
      ) : (
        <>
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <select
              aria-label={t('ui.requirements.filter_project')}
              className="h-8 rounded border border-border bg-card px-2 text-sm text-foreground"
              value={project}
              onChange={(e) => setProject(e.target.value)}
            >
              <option value="">{t('ui.requirements.filter_all')}</option>
              {projects.map((p) => (
                <option key={p} value={p}>{p}</option>
              ))}
            </select>
            <input
              type="text"
              aria-label={t('ui.requirements.filter_keyword')}
              placeholder={t('ui.requirements.filter_keyword')}
              className="h-8 w-40 rounded border border-border bg-card px-2 text-sm text-foreground"
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
            />
            {tags.map((tg) => (
              <button
                key={tg}
                type="button"
                className={cn(
                  'rounded-sm border px-1.5 py-0.5 text-xs',
                  tag === tg
                    ? 'border-primary bg-primary/10 text-primary'
                    : 'border-border text-muted-foreground hover:text-foreground',
                )}
                onClick={() => setTag((cur) => (cur === tg ? '' : tg))}
              >
                {tg}
              </button>
            ))}
          </div>
          <ul className="m-0 flex max-h-[60vh] list-none flex-col gap-2 overflow-y-auto p-0">
            {shown.map((r) => (
              <li key={r.id} className="flex min-w-0 items-start gap-2 rounded border border-border px-3 py-2">
                <span className="min-w-0 flex-1">
                  <span className="line-clamp-2 block break-words text-[13.5px] leading-relaxed">{r.text}</span>
                  <span className="mt-1 flex min-w-0 flex-wrap items-center gap-1 text-xs text-muted-foreground">
                    <Badge variant="muted">{t('ui.requirements.status_archived')}</Badge>
                    {(r.tags ?? []).map((tg) => (
                      <Badge key={tg} variant="outline">{tg}</Badge>
                    ))}
                    {/* 派发目标徽章：与 project 同一工作区时隐藏（避免双同名徽章），archived 视图同理 */}
                    {r.dispatchedTo && !(r.project && sameWorkspacePath(r.project, r.dispatchedTo)) && (
                      <Badge variant="outline">{baseName(r.dispatchedTo)}</Badge>
                    )}
                    <span>{r.archivedAt ? formatRelativeTime(new Date(r.archivedAt / 1e6).toISOString()) : ''}</span>
                  </span>
                </span>
              </li>
            ))}
          </ul>
        </>
      )}
    </Dialog>
  );
}

export default function RequirementList({ className }: { className?: string }) {
  const { t } = useTranslation();
  const [items, setItems] = useState<Requirement[]>([]);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<Requirement | null>(null);
  const [archiveOpen, setArchiveOpen] = useState(false);
  // 活跃会话镜像：会话行用实时标题/状态覆盖落盘快照（App.tsx 全局订阅维护）
  const chats = useAppStore((s) => s.chats);
  // 选中项目：过滤需求列表（不新增 props，直接读全局 store）
  const selectedProject = useAppStore((s) => s.selectedProject);
  // 工作区列表：项目徽标显示名来源
  const workspaces = useAppStore((s) => s.workspaces);

  const refresh = useCallback(() => {
    listRequirements()
      .then(setItems)
      .catch(() => {
        // 拉取失败静默：需求列表是辅助视图，不打断首页
      });
  }, []);

  useEffect(() => {
    refresh();
    const off = onRequirementsChanged(refresh);
    return off;
  }, [refresh]);

  // 打开归档视图时补一次拉取：归档入口可能错过 requirements:changed 事件（边缘丢事件场景）
  useEffect(() => {
    if (archiveOpen) refresh();
  }, [archiveOpen, refresh]);

  const openAdd = () => {
    setEditing(null);
    setDialogOpen(true);
  };

  const openEdit = (r: Requirement) => {
    setEditing(r);
    setDialogOpen(true);
  };

  const remove = (r: Requirement) => {
    void deleteRequirement(r.id).catch((e: unknown) => {
      useAppStore.getState().notify(t('ui.requirements.delete_failed', { err: backendError(e) }), 'error');
    });
  };

  // 标记完成：active → done，成功靠 requirements:changed 回流刷新
  const markDone = (r: Requirement) => {
    void setRequirementStatus(r.id, 'done').catch((e: unknown) => {
      useAppStore.getState().notify(t('ui.requirements.update_failed', { err: backendError(e) }), 'error');
    });
  };

  // 归档：done → archived。released 表示关联会话是否空闲被自动关闭；
  // 失败（非 done 等）提示 update_failed，靠 changed 事件回流刷新。
  // name 取不到（未派发过）时退化为不含会话名的通用成功提示。
  const archive = (r: Requirement) => {
    void archiveRequirement(r.id)
      .then(({ released }) => {
        const name = archiveSessionName(r);
        if (!name) {
          useAppStore.getState().notify(t('ui.requirements.archived_plain'), 'success');
        } else if (released) {
          useAppStore.getState().notify(t('ui.requirements.archived_released', { name }), 'success');
        } else {
          useAppStore.getState().notify(t('ui.requirements.archived_busy_kept', { name }), 'info');
        }
      })
      .catch((e: unknown) => {
        useAppStore.getState().notify(t('ui.requirements.update_failed', { err: backendError(e) }), 'error');
      });
  };

  // 重新打开已失效会话：在原工作区建新会话并更新绑定；失败提示。
  // 成功后靠 markRequirementDispatched 触发的 requirements:changed 回流刷新。
  const reopen = (r: Requirement) => {
    void reopenRequirementSession(r).catch((e: unknown) => {
      useAppStore.getState().notify(t('ui.requirements.reopen_failed', { err: backendError(e) }), 'error');
    });
  };

  // 快捷开始在途集合：派发是长操作，期间禁用按钮避免连点；dispatchRequirement
  // 内部的模块级守卫兜底双保险。组件本地记录用于渲染态。
  const [startingIds, setStartingIds] = useState<ReadonlySet<string>>(new Set());

  // 快捷开始：未开始需求一键派发（默认 agent + 需求自身项目），等价拖到
  // AgentHub 的默认路径。成功/在途/失败提示与拖拽落点（AgentHubPanel）保持一致；
  // 成功后 markRequirementDispatched 触发 requirements:changed 回流刷新列表。
  const quickStart = (r: Requirement) => {
    const wsPath = r.project;
    if (!wsPath) return;
    setStartingIds((prev) => new Set(prev).add(r.id));
    void dispatchRequirement(r.id, r.text, wsPath, loadDefaultCreate() ?? undefined)
      .then((res) => {
        if (res === 'busy') {
          useAppStore.getState().notify(t('ui.requirements.dispatch_busy'), 'error');
          return;
        }
        useAppStore.getState().notify(t('ui.requirements.dispatch_ok', { name: baseName(wsPath) }), 'info');
      })
      .catch((e: unknown) => {
        useAppStore
          .getState()
          .notify(t('ui.requirements.dispatch_failed', { err: backendError(e) }), 'error');
      })
      .finally(() => {
        setStartingIds((prev) => {
          const next = new Set(prev);
          next.delete(r.id);
          return next;
        });
      });
  };

  // 活跃列表只显示非 archived 条目（listRequirements 全量返回）
  const activeItems = useMemo(() => items.filter((r) => r.status !== 'archived'), [items]);
  // 选中项目时只显示属于该项目的条目（路径归一化比较）；未选中显示全部。
  // 无 project 的存量条目在选中态下被过滤掉（用户编辑补选后重新可见）。
  const shownItems = useMemo(
    () =>
      selectedProject
        ? activeItems.filter((r) => !!r.project && sameWorkspacePath(r.project, selectedProject))
        : activeItems,
    [activeItems, selectedProject],
  );

  const handleDragStart = (e: React.DragEvent<HTMLElement>, r: Requirement) => {
    // 终态（done/archived）禁拖：派发守卫会拒绝 markRequirementDispatched，
    // 但会话已建、prompt 已发，留下无主孤儿会话
    if (r.status === 'done' || r.status === 'archived') return;
    // 无 project 禁拖：派发目标是需求自身 project（D1 决策），缺失则无处投递
    if (!r.project) return;
    e.dataTransfer.setData(REQUIREMENT_MIME, JSON.stringify({ id: r.id, text: r.text, project: r.project }));
    e.dataTransfer.setData('text/plain', r.text);
    e.dataTransfer.effectAllowed = 'copy';
  };

  return (
    <section
      aria-label={t('ui.requirements.title')}
      className={cn('flex min-h-0 flex-col rounded border border-border bg-card p-3 text-sm', className)}
    >
      <div className="mb-2 flex shrink-0 items-center justify-between gap-2">
        <h2 className="text-sm font-medium">{t('ui.requirements.title')}</h2>
        <div className="flex items-center gap-1.5">
          <Button size="sm" variant="secondary" onClick={() => setArchiveOpen(true)}>
            {t('ui.requirements.archive_open')}
          </Button>
          <Button size="sm" variant="secondary" onClick={openAdd}>
            + {t('ui.requirements.add')}
          </Button>
        </div>
      </div>
      {shownItems.length === 0 ? (
        <EmptyState
          className="min-h-0 flex-1 py-6"
          title={t(activeItems.length === 0 ? 'ui.requirements.empty' : 'ui.requirements.empty_filtered')}
        />
      ) : (
        <ul className="m-0 flex min-h-0 flex-1 list-none flex-col gap-2 overflow-y-auto p-0">
          {shownItems.map((r) => {
            const status = r.status ?? 'pending';
            // 终态与无 project 条目均不可拖：拖拽派发目标是需求自身 project
            const draggable = status !== 'done' && status !== 'archived' && !!r.project;
            // 会话行数据：活跃会话镜像命中时用实时标题/状态，否则回退落盘快照
            // （|| 而非 ??：空串标题也视为缺失，回退快照）
            const chat = r.chatId ? chats.find((c) => c.ID === r.chatId) : undefined;
            const sessionName = chat?.Title || r.sessionTitle || r.chatId;
            return (
              <li
                key={r.id}
                draggable={draggable}
                onDragStart={(e) => handleDragStart(e, r)}
                // 无 project 禁拖的可见解释：读屏与悬停用户都能感知原因
                title={!r.project ? t('ui.requirements.no_project_drag_hint') : undefined}
                className="group relative flex min-h-14 min-w-0 items-start gap-2 rounded border border-border px-3 py-2.5"
              >
                <span
                  data-testid="req-dot"
                  aria-hidden="true"
                  className={cn('mt-2 h-2 w-2 shrink-0 rounded-full', dotClass(r.priority))}
                />
                <span className="min-w-0 flex-1">
                  <span className="line-clamp-3 block break-words text-[13.5px] leading-relaxed">{r.text}</span>
                  {/* 紧凑单行：session 片段 + 状态/tags/工作区徽章 + 行尾 hover 按钮组（含跳转/重开） */}
                  <span className="mt-1 flex min-w-0 flex-wrap items-center gap-1">
                    {/* 会话片段：已派发条目展示绑定会话的运行状态点与名称（实时标题优先） */}
                    {(r.dispatchedTo || r.chatId) && (
                      <span className="flex min-w-0 max-w-[45%] items-center gap-1.5 text-xs text-muted-foreground">
                        {/* 状态点带 role=img + aria 文案：读屏用户也能感知运行状态 */}
                        <span
                          data-testid="req-session-dot"
                          role="img"
                          aria-label={t(chat ? 'ui.requirements.session_running_aria' : 'ui.requirements.session_ended')}
                          className={cn('h-1.5 w-1.5 shrink-0 rounded-full', sessionDotClass(chat?.Status))}
                        />
                        <span className="min-w-0 truncate" title={sessionName}>{sessionName}</span>
                      </span>
                    )}
                    <Badge
                      variant={statusBadgeVariant(status)}
                      className={cn(status === 'done' && 'bg-success/15 text-success')}
                    >
                      {t(`ui.requirements.status_${status}`)}
                    </Badge>
                    {/* 项目徽标：项目名取工作区显示名，匹配不到退化为路径末段 */}
                    {r.project && (
                      <Badge variant="outline" title={r.project}>
                        {projectDisplayName(r.project, workspaces)}
                      </Badge>
                    )}
                    {/* tag 徽章：与项目显示名重复的隐藏（项目徽章已表达，避免同屏双 "kshell"） */}
                    {(r.tags ?? [])
                      .filter((tag) => !r.project || tag.toLowerCase() !== projectDisplayName(r.project, workspaces).toLowerCase())
                      .map((tag) => (
                        <Badge key={tag} variant="outline">
                          {tag}
                        </Badge>
                      ))}
                    {/* 派发目标徽章：与 project 同一工作区时隐藏（项目徽章已表达，避免同屏双 "kshell"） */}
                    {r.dispatchedTo && !(r.project && sameWorkspacePath(r.project, r.dispatchedTo)) && (
                      <Badge variant="outline" title={r.dispatchedTo}>
                        {baseName(r.dispatchedTo)}
                      </Badge>
                    )}
                    {/* 行尾按钮组分两段：常显区（快捷开始/重开，不用 hover 就能点）+
                        hover 区（跳转与需求操作）。同一行避免卡片纵向膨胀。
                        pending：快捷开始常显 + hover 编辑删除；active：hover 标记完成，
                        已绑定且会话失效时重开常显；done：hover 归档；archived 不进活跃列表。 */}
                    <span className="ml-auto flex shrink-0 items-center">
                      {status === 'pending' && (
                        <button
                          type="button"
                          aria-label={t('ui.requirements.quick_start')}
                          title={!r.project ? t('ui.requirements.no_project_drag_hint') : t('ui.requirements.quick_start')}
                          // 无 project 无处派发（与拖拽同一限制）；在途期间禁用防连点
                          disabled={!r.project || startingIds.has(r.id)}
                          className="rounded p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
                          onClick={() => quickStart(r)}
                        >
                          <PlayGlyph />
                        </button>
                      )}
                      {!chat && r.chatId && status !== 'done' && (
                        // 重开仅限 pending/active：done 是终态，MarkDispatched 守卫会拒绝。
                        // 常显（不藏 hover）：会话失效是用户需要主动处理的异常态。
                        <button
                          type="button"
                          aria-label={t('ui.requirements.session_reopen')}
                          title={t('ui.requirements.session_reopen')}
                          className="rounded p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                          onClick={() => reopen(r)}
                        >
                          <ReopenGlyph />
                        </button>
                      )}
                      <span className="ml-auto hidden shrink-0 gap-0.5 group-hover:flex">
                        {chat && chat.Key && (
                          <button
                            type="button"
                            aria-label={t('ui.requirements.session_jump')}
                            title={t('ui.requirements.session_jump')}
                            className="rounded p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                            onClick={() => focusNoticeTarget(chat.Key!)}
                          >
                            <JumpGlyph />
                          </button>
                        )}
                      {status === 'pending' && (
                        <>
                          <button
                            type="button"
                            aria-label={t('ui.requirements.edit')}
                            title={t('ui.requirements.edit')}
                            className="rounded p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                            onClick={() => openEdit(r)}
                          >
                            <PencilGlyph />
                          </button>
                          <button
                            type="button"
                            aria-label={t('ui.requirements.delete')}
                            title={t('ui.requirements.delete')}
                            className="rounded p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-destructive"
                            onClick={() => remove(r)}
                          >
                            <TrashGlyph />
                          </button>
                        </>
                      )}
                      {status === 'active' && (
                        <button
                          type="button"
                          aria-label={t('ui.requirements.mark_done')}
                          title={t('ui.requirements.mark_done')}
                          className="rounded p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                          onClick={() => markDone(r)}
                        >
                          <CheckGlyph />
                        </button>
                      )}
                      {status === 'done' && (
                        <button
                          type="button"
                          aria-label={t('ui.requirements.archive_btn')}
                          title={t('ui.requirements.archive_btn')}
                          className="rounded p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                          onClick={() => archive(r)}
                        >
                          <ArchiveGlyph />
                        </button>
                      )}
                      </span>
                    </span>
                  </span>
                </span>
              </li>
            );
          })}
        </ul>
      )}
      <RequirementDialog open={dialogOpen} editing={editing} onOpenChange={setDialogOpen} />
      <ArchiveDialog open={archiveOpen} onOpenChange={setArchiveOpen} items={items} />
    </section>
  );
}

// 快捷开始按钮图标（播放三角）
function PlayGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.6">
      <path strokeLinecap="round" strokeLinejoin="round" d="M7 5.5v13l11-6.5-11-6.5Z" />
    </svg>
  );
}

// 编辑按钮图标（铅笔）
function PencilGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.6">
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        d="m16.5 4.5 3 3M4 20l1-4.5L16.5 4a2.1 2.1 0 0 1 3 0l.5.5a2.1 2.1 0 0 1 0 3L8.5 19 4 20Z"
      />
    </svg>
  );
}

// 删除按钮图标（垃圾桶），与 Home 卡片删除按钮同款
function TrashGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.6">
      <path strokeLinecap="round" strokeLinejoin="round" d="M4 7h16M9 7V5h6v2M6 7l1 13h10l1-13" />
    </svg>
  );
}

// 标记完成按钮图标（对勾）
function CheckGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.6">
      <path strokeLinecap="round" strokeLinejoin="round" d="m5 13 4 4L19 7" />
    </svg>
  );
}

// 归档按钮图标（收纳盒）
function ArchiveGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.6">
      <path strokeLinecap="round" strokeLinejoin="round" d="M4 8h16M5 8v11h14V8M9 12h6" />
    </svg>
  );
}
