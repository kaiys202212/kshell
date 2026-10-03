// 快速切换器（Ctrl+K 命令面板，Radix Dialog）：
// 两组条目——「已打开的页签」（确认时 setActiveTab 仅激活）与「工作区」（确认时 openTab）；
// 顶部输入框自动聚焦，按名称/路径 includes 过滤（大小写不敏感）；
// ↑↓ 移动高亮、Enter 确认，数据全空时给「先去首页扫描」的引导空态。
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { useEffect, useMemo, useState } from 'react';
import type { KeyboardEvent } from 'react';
import type { Workspace } from '../lib/api';
import { cn } from '../lib/cn';
import { PANE_HEADER } from '../lib/ui';
import { useAppStore } from '../state/store';
import type { WorkspaceTab } from '../state/store';
import { EmptyState } from './ui/empty-state';
import { Dialog } from './ui/dialog';
import { Input } from './ui/input';

// 扁平条目：两组列表共用一套 ↑↓/Enter 序号导航
interface Entry {
  key: string;
  label: string;
  hint: string;
  activate(): void;
}

interface Props {
  open: boolean;
  onOpenChange(open: boolean): void;
}

export default function QuickSwitcher({ open, onOpenChange }: Props) {
  const openTabs = useAppStore((s) => s.openTabs);
  const workspaces = useAppStore((s) => s.workspaces);
  const setActiveTab = useAppStore((s) => s.setActiveTab);
  const openTab = useAppStore((s) => s.openTab);
  const [query, setQuery] = useState('');
  const [highlight, setHighlight] = useState(0);

  // 每次打开重置过滤词与高亮（数据在打开期间实时读取 store）
  useEffect(() => {
    if (open) {
      setQuery('');
      setHighlight(0);
    }
  }, [open]);

  const q = query.trim().toLowerCase();
  const tabs = useMemo(
    () =>
      openTabs.filter(
        (t: WorkspaceTab) => t.name.toLowerCase().includes(q) || t.id.toLowerCase().includes(q),
      ),
    [openTabs, q],
  );
  const spaces = useMemo(
    () =>
      workspaces.filter(
        (w: Workspace) => w.Name.toLowerCase().includes(q) || w.Path.toLowerCase().includes(q),
      ),
    [workspaces, q],
  );

  // 条目序与渲染序一致：先已开页签后工作区
  const entries: Entry[] = [
    ...tabs.map((t) => ({
      key: `tab:${t.id}`,
      label: t.name,
      hint: '页签',
      activate: () => setActiveTab(t.id),
    })),
    ...spaces.map((w) => ({
      key: `ws:${w.Path}`,
      label: w.Name,
      hint: '工作区',
      activate: () => openTab(w),
    })),
  ];
  // 过滤词变化可能让高亮越界，渲染与激活前统一收敛
  const clamped = entries.length === 0 ? 0 : Math.min(highlight, entries.length - 1);

  const confirm = (item: Entry | undefined) => {
    if (!item) return;
    item.activate();
    onOpenChange(false);
  };

  const onInputKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setHighlight((h) => Math.min(h + 1, entries.length - 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setHighlight((h) => Math.max(h - 1, 0));
    } else if (e.key === 'Enter') {
      e.preventDefault();
      confirm(entries[clamped]);
    }
  };

  const renderItem = (item: Entry, index: number) => (
    <li key={item.key}>
      <button
        className={cn(
          'flex w-full items-center gap-1.5 rounded px-2 py-1 text-left text-xs transition-colors',
          index === clamped ? 'bg-muted' : 'hover:bg-muted/60',
        )}
        onClick={() => confirm(item)}
      >
        <span className="min-w-0 truncate">{item.label}</span>
        <span className="ml-auto shrink-0 text-xs text-muted-foreground">{item.hint}</span>
      </button>
    </li>
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange} className="top-20 w-[480px] max-w-[90vw] p-3 outline-none">
      <DialogPrimitive.Title className="sr-only">快速切换</DialogPrimitive.Title>
      <Input
        autoFocus
        className="w-full"
        aria-label="搜索页签或工作区"
        placeholder="输入以过滤页签 / 工作区…"
        value={query}
        onChange={(e) => {
          setQuery(e.target.value);
          setHighlight(0);
        }}
        onKeyDown={onInputKeyDown}
      />
      {entries.length === 0 ? (
        <EmptyState
          className="py-6"
          title={
            openTabs.length === 0 && workspaces.length === 0
              ? '暂无工作区，请先在首页完成扫描'
              : '没有匹配项'
          }
        />
      ) : (
        <div className="mt-2 max-h-80 overflow-y-auto">
          {tabs.length > 0 && (
            <section className="mb-2">
              <p className={`px-1 py-1 ${PANE_HEADER}`}>已打开的页签</p>
              <ul className="m-0 flex list-none flex-col gap-0.5 p-0">
                {tabs.map((t, i) => renderItem(entries[i], i))}
              </ul>
            </section>
          )}
          {spaces.length > 0 && (
            <section>
              <p className={`px-1 py-1 ${PANE_HEADER}`}>工作区</p>
              <ul className="m-0 flex list-none flex-col gap-0.5 p-0">
                {spaces.map((w, i) => renderItem(entries[tabs.length + i], tabs.length + i))}
              </ul>
            </section>
          )}
        </div>
      )}
    </Dialog>
  );
}
