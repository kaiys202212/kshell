// FindSessionDialog：#find 触发的会话查找弹窗。
// open 时拉取 discovery 全量已发现会话（getSessions），客户端按
// 项目基名 / 会话标题 / 工具展示名三处不分大小写 includes 过滤，
// 按 UpdatedAt 倒序展示（上限 50 条）。键盘 ↑↓ 移动高亮、Enter 选中、
// Esc 关闭（Esc 由 Dialog 的 Radix 行为提供）。
import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import * as DialogPrimitive from '@radix-ui/react-dialog';
import type { KeyboardEvent } from 'react';
import type { Session } from '../lib/api';
import { getSessions } from '../lib/api';
import { toolDisplayName } from '../lib/agentNotify';
import { baseName } from '../lib/hubAttribution';
import { formatRelativeTime } from '../lib/format';
import { cn } from '../lib/cn';
import { Dialog } from './ui/dialog';
import { Input } from './ui/input';

interface Props {
  open: boolean;
  initialQuery: string;
  onClose: () => void;
  onPick: (session: Session) => void;
}

// 倒序排序 + 截断：最近更新优先，最多展示 50 条
const MAX_ROWS = 50;

export default function FindSessionDialog({ open, initialQuery, onClose, onPick }: Props) {
  const { t } = useTranslation();
  const [query, setQuery] = useState(initialQuery);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [highlight, setHighlight] = useState(0);

  // open 变 true 时拉全量并重置过滤词/高亮；拉取失败回落空态（空态文案兜底）
  useEffect(() => {
    if (!open) return;
    setQuery(initialQuery);
    setHighlight(0);
    let cancelled = false;
    void getSessions()
      .then((list) => {
        if (!cancelled) setSessions(list ?? []);
      })
      .catch(() => {
        if (!cancelled) setSessions([]);
      });
    return () => {
      cancelled = true;
    };
  }, [open, initialQuery]);

  // 客户端过滤：三处 includes 不分大小写，按 UpdatedAt 倒序、上限 50 条
  const rows = useMemo(() => {
    const q = query.trim().toLowerCase();
    const matched = sessions.filter((s) => {
      if (!q) return true;
      const wsBase = baseName(s.Workspace).toLowerCase();
      const title = (s.Title ?? '').toLowerCase();
      const tool = toolDisplayName(s.ToolID).toLowerCase();
      return wsBase.includes(q) || title.includes(q) || tool.includes(q);
    });
    return matched
      .sort((a, b) => new Date(b.UpdatedAt).getTime() - new Date(a.UpdatedAt).getTime())
      .slice(0, MAX_ROWS);
  }, [sessions, query]);

  // 过滤词变化可能让高亮越界：渲染与激活前统一收敛（同 QuickSwitcher 模型）
  const clamped = rows.length === 0 ? 0 : Math.min(highlight, rows.length - 1);

  const onInputKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setHighlight((h) => Math.min(h + 1, rows.length - 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setHighlight((h) => Math.max(h - 1, 0));
    } else if (e.key === 'Enter') {
      e.preventDefault();
      const row = rows[clamped];
      if (row) onPick(row);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => (v ? undefined : onClose())} className="top-20 w-[560px] max-w-[90vw] p-3 outline-none">
      <DialogPrimitive.Title className="mb-2 text-sm font-medium">{t('ui.hub.find_title')}</DialogPrimitive.Title>
      <Input
        autoFocus
        className="w-full"
        aria-label={t('ui.hub.find_title')}
        placeholder={t('ui.hub.find_placeholder')}
        value={query}
        onChange={(e) => {
          setQuery(e.target.value);
          setHighlight(0);
        }}
        onKeyDown={onInputKeyDown}
      />
      {rows.length === 0 ? (
        <div className="py-6 text-center text-xs text-muted-foreground">{t('ui.hub.find_empty')}</div>
      ) : (
        <ul className="m-0 mt-2 flex max-h-80 list-none flex-col gap-0.5 overflow-y-auto p-0" role="listbox" aria-label={t('ui.hub.find_title')}>
          {rows.map((s, i) => (
            <li
              key={`${s.ID}:${i}`}
              role="option"
              aria-selected={i === clamped}
              className={cn('cursor-pointer rounded px-2 py-1.5 text-xs', i === clamped ? 'bg-muted' : 'hover:bg-muted/60')}
              // 与 QuickSwitcher 的条目按钮同用 onClick；Radix Dialog 不因输入框 blur 关闭
              onClick={() => onPick(s)}
            >
              <div className="flex items-center gap-2">
                <span className="shrink-0 font-medium">{toolDisplayName(s.ToolID)}</span>
                <span className="min-w-0 truncate">{baseName(s.Workspace) || s.Workspace}</span>
                <span className="min-w-0 flex-1 truncate text-muted-foreground">{s.Title}</span>
                <span className="shrink-0 text-muted-foreground">{formatRelativeTime(s.UpdatedAt)}</span>
              </div>
            </li>
          ))}
        </ul>
      )}
    </Dialog>
  );
}
