// RequirementList：首页左列的需求清单卡片。条目可新增/编辑/删除、可拖拽到项目卡片
// 派发（dragstart 写入自定义 MIME + text/plain，项目卡片 onDrop 解析后建 ACP 会话）。
// 数据流：挂载拉取 listRequirements，写操作后 Go 侧 emit "requirements:changed"，
// 本组件订阅该事件重拉（单一数据源，不做本地乐观插入）。
import { useCallback, useEffect, useState } from 'react';
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { useTranslation } from 'react-i18next';
import {
  addRequirement,
  deleteRequirement,
  listRequirements,
  onRequirementsChanged,
  readClipboardPaste,
  updateRequirement,
  type Requirement,
} from '../lib/api';
import { backendError } from '../lib/errors';
import { quotePathForShell } from '../lib/dragPath';
import { baseName } from '../lib/hubAttribution';
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
    <Dialog open={open} onOpenChange={onOpenChange} className="w-96">
      <DialogPrimitive.Title className="mb-3 text-sm font-medium">
        {editing ? t('ui.requirements.edit') : t('ui.requirements.add')}
      </DialogPrimitive.Title>
      <label className="mb-2 flex flex-col gap-1 text-xs text-muted-foreground">
        {t('ui.requirements.text_label')}
        <textarea
          data-testid="req-text"
          className="min-h-16 w-full resize-none rounded border border-border bg-card p-2 text-sm text-foreground"
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

export default function RequirementList({ className }: { className?: string }) {
  const { t } = useTranslation();
  const [items, setItems] = useState<Requirement[]>([]);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<Requirement | null>(null);

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
        <Button size="sm" variant="secondary" onClick={openAdd}>
          + {t('ui.requirements.add')}
        </Button>
      </div>
      {items.length === 0 ? (
        <EmptyState className="min-h-0 flex-1 py-6" title={t('ui.requirements.empty')} />
      ) : (
        <ul className="m-0 flex min-h-0 flex-1 list-none flex-col gap-2 overflow-y-auto p-0">
          {items.map((r) => (
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
                  {r.tags.map((tag) => (
                    <Badge key={tag} variant="outline">
                      {tag}
                    </Badge>
                  ))}
                  {r.dispatchedTo && <Badge variant="outline">{baseName(r.dispatchedTo)}</Badge>}
                </span>
              </span>
              {/* 编辑/删除按钮：hover 显现（兄弟节点定位，避免嵌套进文本区） */}
              <span className="absolute right-1.5 top-1.5 hidden gap-0.5 group-hover:flex">
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
              </span>
            </li>
          ))}
        </ul>
      )}
      <RequirementDialog open={dialogOpen} editing={editing} onOpenChange={setDialogOpen} />
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
