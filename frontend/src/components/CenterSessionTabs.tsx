// 中心区页签条：左钉「会话预览」+ 可关会话页签（溢出进「更多」菜单、可拖排序）
// + 右钉「文件 | 终端」。会话过多时不出现横向滚动条。
// variant 支持中心区分割布局：agent 条隐藏右钉、tool 条只渲染右钉。
import { useLayoutEffect, useMemo, useRef, useState, type DragEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { Columns2, MoreHorizontal, PanelTop } from 'lucide-react';
import {
  CENTER_TAB_MIME,
  MORE_BTN_WIDTH,
  computeOverflowHidden,
  reorderCenterTabs,
} from '../lib/centerTabOrder';
import { cn } from '../lib/cn';
import { DRAG_MIME } from '../lib/dragPath';
import { TAB_ACTIVE, TAB_BASE, TAB_UNDERLINE } from '../lib/ui';
import type { LayoutMode } from '../state/store';
import type { AgentActivity } from '../state/agentActivity';
import AgentActivityIcon from './AgentActivityIcon';
import { ToolDot } from './ui/tool-dot';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from './ui/dropdown-menu';

const centerTabBase = `group ${TAB_BASE} h-7 max-w-56 text-xs`;
const centerTabActive = TAB_ACTIVE;

export type CenterSessionTabItem = {
  id: string;
  kind: 'terminal' | 'chat';
  label: string;
  toolID: string;
  activity: AgentActivity;
  titleAttr: string;
  /** 终端是否已退出（文件拖入时跳过写入） */
  exited?: boolean;
};

export interface CenterSessionTabsProps {
  items: CenterSessionTabItem[];
  order: string[];
  centerTab: string;
  previewTabId: string;
  filesTabId: string;
  terminalsTabId: string;
  toolTermCount: number;
  /** full=完整条（默认）；agent=只留预览钉+会话页签；tool=只留右钉「文件|终端」 */
  variant?: 'full' | 'agent' | 'tool';
  /** 传入时在条尾渲染布局模式切换按钮 */
  layoutMode?: LayoutMode;
  onLayoutToggle?(): void;
  onSelect(id: string): void;
  onClose(id: string): void;
  onReorder(next: string[]): void;
  onFileDropTerminal(id: string, path: string, exited: boolean): void;
  onFileDropChat(id: string, path: string): void;
}

export default function CenterSessionTabs({
  items,
  order,
  centerTab,
  previewTabId,
  filesTabId,
  terminalsTabId,
  toolTermCount,
  variant = 'full',
  layoutMode,
  onLayoutToggle,
  onSelect,
  onClose,
  onReorder,
  onFileDropTerminal,
  onFileDropChat,
}: CenterSessionTabsProps) {
  const { t: tr } = useTranslation();
  const byId = useMemo(() => {
    const m = new Map<string, CenterSessionTabItem>();
    for (const it of items) m.set(it.id, it);
    return m;
  }, [items]);
  const ordered = useMemo(
    () => order.map((id) => byId.get(id)).filter((x): x is CenterSessionTabItem => !!x),
    [order, byId],
  );

  const stripRef = useRef<HTMLDivElement>(null);
  // 单探针测标题文字宽，避免克隆整页签造成 DOM 文案/图标重复（测试与 a11y 会误命中）
  const probeRef = useRef<HTMLSpanElement>(null);
  const [hiddenIds, setHiddenIds] = useState<string[]>([]);
  const [draggingId, setDraggingId] = useState<string | null>(null);

  useLayoutEffect(() => {
    const strip = stripRef.current;
    const probe = probeRef.current;
    if (!strip || !probe) return;

    const measure = () => {
      const ids = ordered.map((it) => it.id);
      // 页签 chrome：活动图标 + 工具点 + 关闭钮 + 内边距/间隙（与 centerTabBase 大致对齐）
      const chrome = 14 + 12 + 16 + 20;
      const maxTab = 224; // max-w-56
      const widths = ordered.map((it) => {
        probe.textContent = it.label;
        const textW = probe.offsetWidth;
        return Math.min(maxTab, chrome + textW);
      });
      probe.textContent = '';
      if (widths.some((w) => w > chrome)) {
        const active = ordered.some((it) => it.id === centerTab) ? centerTab : null;
        setHiddenIds(computeOverflowHidden(ids, widths, active, strip.clientWidth, MORE_BTN_WIDTH));
      } else {
        // jsdom 下 offsetWidth 常为 0：不隐藏，保证测试与首屏可见
        setHiddenIds([]);
      }
    };

    measure();
    // jsdom 无 ResizeObserver：只做一次测量，窗口缩放场景由真实环境覆盖
    if (typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(measure);
    ro.observe(strip);
    return () => ro.disconnect();
  }, [ordered, centerTab]);

  const hiddenSet = useMemo(() => new Set(hiddenIds), [hiddenIds]);
  const showMore = hiddenIds.length > 0;

  const onTabDragStart = (e: DragEvent, id: string) => {
    e.dataTransfer.setData(CENTER_TAB_MIME, id);
    e.dataTransfer.effectAllowed = 'move';
    setDraggingId(id);
  };

  const onTabDragEnd = () => setDraggingId(null);

  const onTabDragOver = (e: DragEvent) => {
    if ([...e.dataTransfer.types].includes(CENTER_TAB_MIME)) {
      e.preventDefault();
      e.dataTransfer.dropEffect = 'move';
    } else if ([...e.dataTransfer.types].includes(DRAG_MIME)) {
      e.preventDefault();
    }
  };

  const onTabDrop = (e: DragEvent, targetId: string, item: CenterSessionTabItem) => {
    const fromId = e.dataTransfer.getData(CENTER_TAB_MIME);
    if (fromId) {
      e.preventDefault();
      e.stopPropagation();
      if (fromId !== targetId) onReorder(reorderCenterTabs(order, fromId, targetId));
      setDraggingId(null);
      return;
    }
    const path = e.dataTransfer.getData(DRAG_MIME);
    if (!path) return;
    e.preventDefault();
    e.stopPropagation();
    onSelect(targetId);
    if (item.kind === 'terminal') {
      onFileDropTerminal(targetId, path, !!item.exited);
    } else {
      onFileDropChat(targetId, path);
    }
  };

  return (
    <div className="relative flex shrink-0 items-stretch border-b border-border">
      <span
        ref={probeRef}
        className="pointer-events-none absolute -left-[9999px] max-w-56 truncate text-xs opacity-0"
        aria-hidden="true"
      />

      <div
        className="flex min-w-0 flex-1 items-stretch"
        role="tablist"
        aria-label={tr('ui.workspace.center_tabs_aria')}
      >
      {variant !== 'tool' && (
        <button
          role="tab"
          aria-selected={centerTab === previewTabId}
          className={cn(centerTabBase, 'shrink-0', centerTab === previewTabId && centerTabActive)}
          onClick={() => onSelect(previewTabId)}
        >
          {tr('ui.workspace.session_preview')}
          {centerTab === previewTabId && <span className={TAB_UNDERLINE} />}
        </button>
      )}

      {/* 仅会话页签区参与溢出测量；预览钉与右钉不计入 available */}
      {variant !== 'tool' && (
      <div ref={stripRef} className="flex min-w-0 flex-1 items-stretch overflow-hidden">
        {ordered.map((it) => {
          if (hiddenSet.has(it.id)) return null;
          const active = centerTab === it.id;
          return (
            <div
              key={it.id}
              className={cn(
                centerTabBase,
                'shrink-0',
                active && centerTabActive,
                draggingId === it.id && 'opacity-50',
              )}
              draggable
              onDragStart={(e) => onTabDragStart(e, it.id)}
              onDragEnd={onTabDragEnd}
              onClick={() => onSelect(it.id)}
              onAuxClick={(e) => {
                if (e.button === 1) {
                  e.preventDefault();
                  onClose(it.id);
                }
              }}
              onDragOver={onTabDragOver}
              onDrop={(e) => onTabDrop(e, it.id, it)}
            >
              <AgentActivityIcon activity={it.activity} />
              <button
                role="tab"
                aria-selected={active}
                className="min-w-0 truncate text-xs"
                title={it.titleAttr}
              >
                {it.label}
              </button>
              {it.toolID && <ToolDot toolID={it.toolID} className="shrink-0" showLabel={false} />}
              <button
                className={cn(
                  'ml-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-sm text-sm leading-none text-muted-foreground transition-opacity hover:bg-muted hover:text-foreground',
                  active ? 'opacity-70 hover:opacity-100' : 'opacity-0 group-hover:opacity-100',
                )}
                aria-label={
                  it.kind === 'terminal'
                    ? tr('ui.workspace.close_terminal', { label: it.label })
                    : tr('ui.workspace.close_chat', { label: it.label })
                }
                onClick={(e) => {
                  e.stopPropagation();
                  onClose(it.id);
                }}
              >
                ×
              </button>
              {active && <span className={TAB_UNDERLINE} />}
            </div>
          );
        })}

        {showMore && (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <button
                type="button"
                className={cn(centerTabBase, 'shrink-0 px-1.5')}
                aria-label={tr('ui.workspace.more_sessions_aria')}
                title={tr('ui.workspace.more_sessions_aria')}
              >
                <MoreHorizontal className="h-3.5 w-3.5" aria-hidden />
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="max-h-72 min-w-48 overflow-y-auto">
              {ordered.map((it) => {
                const active = centerTab === it.id;
                return (
                  <DropdownMenuItem
                    key={it.id}
                    className={cn('gap-2', active && 'bg-accent')}
                    onSelect={() => onSelect(it.id)}
                  >
                    <AgentActivityIcon activity={it.activity} />
                    <span className="min-w-0 flex-1 truncate">{it.label}</span>
                    {active && (
                      <span className="shrink-0 text-[10px] text-muted-foreground">
                        {tr('ui.workspace.more_sessions_active')}
                      </span>
                    )}
                  </DropdownMenuItem>
                );
              })}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>
      )}

      {variant !== 'agent' && (
      <>
      <button
        role="tab"
        aria-selected={centerTab === filesTabId}
        className={cn(
          centerTabBase,
          'shrink-0 border-l border-border bg-muted/40 text-muted-foreground',
          centerTab === filesTabId && cn(centerTabActive, 'bg-muted/70'),
        )}
        onClick={() => onSelect(filesTabId)}
      >
        {tr('ui.workspace.files_tab')}
        {centerTab === filesTabId && <span className={TAB_UNDERLINE} />}
      </button>
      <button
        role="tab"
        aria-selected={centerTab === terminalsTabId}
        aria-label={tr('ui.workspace.terminals_aria')}
        className={cn(
          centerTabBase,
          'shrink-0 border-l border-border bg-muted/40 text-muted-foreground',
          centerTab === terminalsTabId && cn(centerTabActive, 'bg-muted/70'),
        )}
        onClick={() => onSelect(terminalsTabId)}
      >
        {tr('ui.workspace.terminals_tab')}
        {toolTermCount > 0 && (
          <span className="rounded-sm bg-muted px-1 font-mono text-[10px] text-muted-foreground">
            {toolTermCount}
          </span>
        )}
        {centerTab === terminalsTabId && <span className={TAB_UNDERLINE} />}
      </button>
      </>
      )}

      {/* 布局切换按钮在 tablist 之外（ARIA 要求 tablist 只含 tab），视觉上仍居条尾 */}
      </div>

      {onLayoutToggle && (
        <button
          type="button"
          className={cn(centerTabBase, 'shrink-0 border-l border-border px-1.5')}
          aria-label={tr(
            layoutMode === 'split' ? 'ui.workspace.layout_toggle_tabs' : 'ui.workspace.layout_toggle_split',
          )}
          title={tr(
            layoutMode === 'split' ? 'ui.workspace.layout_toggle_tabs' : 'ui.workspace.layout_toggle_split',
          )}
          onClick={onLayoutToggle}
        >
          {layoutMode === 'split' ? (
            <PanelTop className="h-3.5 w-3.5" aria-hidden />
          ) : (
            <Columns2 className="h-3.5 w-3.5" aria-hidden />
          )}
        </button>
      )}
    </div>
  );
}
