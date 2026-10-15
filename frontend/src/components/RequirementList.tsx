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
} from '../lib/api';
import { backendError } from '../lib/errors';
import { quotePathForShell } from '../lib/dragPath';
import { baseName } from '../lib/hubAttribution';
import { formatRelativeTime } from '../lib/format';
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

// 状态徽章配色（token 体系内取色）：pending 默认描边、active 主色、done 绿、archived 灰
function statusBadgeVariant(status: Requirement['status']): 'default' | 'muted' | 'outline' {
  if (status === 'active') return 'default';
  if (status === 'done') return 'default';
  if (status === 'archived') return 'muted';
  return 'outline';
}

// 归档提示里的「会话名」：优先 chatId，否则用派发目标路径基名
function archiveSessionName(r: Requirement): string {
  if (r.chatId) return r.chatId;
  return r.dispatchedTo ? baseName(r.dispatchedTo) : '';
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
  const [text, setText] = useState('');
  const [priority, setPriority] = useState<Requirement['priority']>('medium');
  const [tagsInput, setTagsInput] = useState('');
  const [busy, setBusy] = useState(false);

  // 打开时按编辑对象回填；新增则清空
  useEffect(() => {
    if (!open) return;
    setText(editing?.text ?? '');
    setPriority(editing?.priority ?? 'medium');
    setTagsInput((editing?.tags ?? []).join(', '));
    setBusy(false);
  }, [open, editing]);

  const save = () => {
    const trimmed = text.trim();
    if (!trimmed || busy) return;
    // 逗号分隔转数组：去空白、丢空段、去重（重复标签会让 Badge key 冲突）
    const tags = [...new Set(
      tagsInput
        .split(/[,,]/)
        .map((s) => s.trim())
        .filter(Boolean),
    )];
    setBusy(true);
    const call = editing
      ? updateRequirement(editing.id, trimmed, priority, tags)
      : addRequirement(trimmed, priority, tags);
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
        <Button size="sm" disabled={!text.trim() || busy} onClick={save}>
          {t('ui.requirements.save')}
        </Button>
      </div>
    </Dialog>
  );
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
  // tag chips：归档条目全部 tag 去重
  const tags = useMemo(() => [...new Set(archived.flatMap((r) => r.tags))], [archived]);

  // 过滤 + archivedAt 降序（0 视为最旧沉底）
  const shown = useMemo(
    () =>
      archived
        .filter((r) => (!project || (r.dispatchedTo && baseName(r.dispatchedTo) === project))
          && (!keyword || r.text.includes(keyword))
          && (!tag || r.tags.includes(tag)))
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
          {t('ui.requirements.empty')}
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
                    {r.tags.map((tg) => (
                      <Badge key={tg} variant="outline">{tg}</Badge>
                    ))}
                    {r.dispatchedTo && <Badge variant="outline">{baseName(r.dispatchedTo)}</Badge>}
                    <span>{r.archivedAt ? formatRelativeTime(new Date(r.archivedAt * 1000).toISOString()) : ''}</span>
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
  const archive = (r: Requirement) => {
    void archiveRequirement(r.id)
      .then(({ released }) => {
        const name = archiveSessionName(r);
        if (released) {
          useAppStore.getState().notify(t('ui.requirements.archived_released', { name }), 'success');
        } else {
          useAppStore.getState().notify(t('ui.requirements.archived_busy_kept', { name }), 'info');
        }
      })
      .catch((e: unknown) => {
        useAppStore.getState().notify(t('ui.requirements.update_failed', { err: backendError(e) }), 'error');
      });
  };

  // 活跃列表只显示非 archived 条目（listRequirements 全量返回）
  const activeItems = useMemo(() => items.filter((r) => r.status !== 'archived'), [items]);

  const handleDragStart = (e: React.DragEvent<HTMLElement>, r: Requirement) => {
    e.dataTransfer.setData(REQUIREMENT_MIME, JSON.stringify({ id: r.id, text: r.text }));
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
      {activeItems.length === 0 ? (
        <EmptyState className="min-h-0 flex-1 py-6" title={t('ui.requirements.empty')} />
      ) : (
        <ul className="m-0 flex min-h-0 flex-1 list-none flex-col gap-2 overflow-y-auto p-0">
          {activeItems.map((r) => {
            const status = r.status ?? 'pending';
            return (
              <li
                key={r.id}
                draggable
                onDragStart={(e) => handleDragStart(e, r)}
                className="group relative flex min-h-14 min-w-0 items-start gap-2 rounded border border-border px-3 py-2.5"
              >
                <span
                  data-testid="req-dot"
                  aria-hidden="true"
                  className={cn('mt-2 h-2 w-2 shrink-0 rounded-full', dotClass(r.priority))}
                />
                <span className="min-w-0 flex-1">
                  <span className="line-clamp-3 block break-words text-[13.5px] leading-relaxed">{r.text}</span>
                  <span className="mt-1 flex min-w-0 flex-wrap items-center gap-1">
                    <Badge
                      variant={statusBadgeVariant(status)}
                      className={cn(status === 'done' && 'bg-success/15 text-success')}
                    >
                      {t(`ui.requirements.status_${status}`)}
                    </Badge>
                    {r.tags.map((tag) => (
                      <Badge key={tag} variant="outline">
                        {tag}
                      </Badge>
                    ))}
                    {r.dispatchedTo && <Badge variant="outline">{baseName(r.dispatchedTo)}</Badge>}
                  </span>
                </span>
                {/* 操作按钮：hover 显现（兄弟节点定位，避免嵌套进文本区）。
                    pending：编辑+删除；active：标记完成；done：归档；archived 不进活跃列表。 */}
                <span className="absolute right-1.5 top-1.5 hidden gap-0.5 group-hover:flex">
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
