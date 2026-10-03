// 会话列表（工作区页签左栏）：展示当前工作区的历史会话，统一在中心区内嵌终端里恢复。
// 曾经每行还有一个「在外部终端打开」按钮（走 window_win 的 cmd /c start 弹系统控制台窗口），
// 已移除：那是独立黑窗的来源，且与内嵌终端入口语义重复。
// 数据流与首页一致：先渲染缓存（GetSessions），收到 "scan:done" 后重调刷新，
// 不再回头调 ScanSessions（它每次都会触发新一轮后台扫描，会形成事件循环）。
import { useEffect, useMemo, useState } from 'react';
import { getSessions, onScanDone } from '../lib/api';
import type { Session } from '../lib/api';
import { formatRelativeTime } from '../lib/format';
import { badgeFor } from '../lib/toolBadge';
import { displayTitle } from '../lib/title';
import { normalizeWorkspacePath } from '../lib/workspacePath';
import { cn } from '../lib/cn';
import { LIST_ROW, LIST_ROW_ACTIVE, MONO } from '../lib/ui';
import { useAppStore } from '../state/store';
import WorkspaceSearch from './WorkspaceSearch';
import { Button } from './ui/button';
import { EmptyState } from './ui/empty-state';
import { Skeleton } from './ui/skeleton';
import { ToolDot } from './ui/tool-dot';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip';

// 时间戳数值化：解析失败按 0 兜底，避免 NaN 让比较器失效导致排序错乱
const tsOf = (v: string) => {
  const n = Date.parse(v);
  return Number.isFinite(n) ? n : 0;
};

// 工具筛选项：扁平下划线式（选中态主色底 + 主色文字 + 下划线），统一 rounded-sm 不用胶囊
const chipClass = (active: boolean) =>
  cn(
    'rounded-sm px-1.5 py-0.5 text-xs transition-colors',
    active
      ? 'bg-primary/10 font-medium text-primary underline decoration-primary underline-offset-4'
      : 'text-muted-foreground hover:bg-muted hover:text-foreground',
  );

interface Props {
  workspacePath: string;
  // 中心区内嵌终端入口（开/切换终端页签），由 WorkspaceTab 注入。
  // 可选：未注入时主按钮退化为无操作，不至于崩（WorkspaceTab 接线前也能单独渲染）。
  onOpenTerminal?(s: Session): void;
}

export default function SessionList({ workspacePath, onOpenTerminal }: Props) {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [query, setQuery] = useState('');
  const [toolFilter, setToolFilter] = useState<string | null>(null); // null = 全部工具
  const scanState = useAppStore((s) => s.scanState);
  const setScanState = useAppStore((s) => s.setScanState);
  const terminals = useAppStore((s) => s.terminals);
  const chats = useAppStore((s) => s.chats);

  useEffect(() => {
    let cancelled = false;
    const refresh = () =>
      getSessions()
        .then((list) => {
          if (!cancelled) setSessions(list);
        })
        .catch(() => {
          // 绑定调用异常时保持现状，等 scan:done 再触发下一轮刷新
        });
    refresh();
    // scan:done 同时把 scanState 置 done（幂等）：会话列表可能挂在扫描完成后
    // 才首次订阅事件的路径上，仅靠首页置 done 存在死角
    const offScan = onScanDone(() => {
      setScanState('done');
      void refresh();
    });
    return () => {
      cancelled = true;
      offScan();
    };
  }, [setScanState]);

  // 工作区过滤（分隔符/大小写不敏感，见 lib/workspacePath）+ 关键词过滤 + 时间降序
  const target = normalizeWorkspacePath(workspacePath);
  // 工具 chip 选项：当前工作区会话按工具展示名去重（保持出现顺序）
  const toolOptions = useMemo(() => {
    const set = new Set<string>();
    for (const s of sessions) {
      if (normalizeWorkspacePath(s.Workspace) === target) set.add(badgeFor(s.ToolID).label);
    }
    return [...set];
  }, [sessions, target]);
  const visible = useMemo(() => {
    const q = query.trim().toLowerCase();
    return sessions
      .filter((s) => normalizeWorkspacePath(s.Workspace) === target)
      .filter((s) => toolFilter === null || badgeFor(s.ToolID).label === toolFilter)
      .filter((s) => {
        if (!q) return true;
        return (
          s.Title.toLowerCase().includes(q) ||
          s.ToolID.toLowerCase().includes(q) ||
          badgeFor(s.ToolID).label.toLowerCase().includes(q)
        );
      })
      .sort(
        (a, b) =>
          tsOf(b.UpdatedAt) - tsOf(a.UpdatedAt) ||
          tsOf(b.CreatedAt) - tsOf(a.CreatedAt) ||
          a.Path.localeCompare(b.Path),
      );
  }, [sessions, query, toolFilter, target]);

  return (
    // Provider 挂在组件根部：App 层已有一层，这里自带延迟参数并让单测可独立渲染
    <TooltipProvider delayDuration={300}>
      <div className="flex flex-col gap-2">
      <WorkspaceSearch value={query} onChange={setQuery} />
      {/* 工具 chip 行：「全部」+ 当前工作区去重后的工具，与文字搜索 AND 叠加 */}
      <div className="flex flex-wrap items-center gap-1" aria-label="按工具筛选">
        <button
          className={chipClass(toolFilter === null)}
          aria-pressed={toolFilter === null}
          onClick={() => setToolFilter(null)}
        >
          全部
        </button>
        {toolOptions.map((label) => (
          <button
            key={label}
            className={chipClass(toolFilter === label)}
            aria-pressed={toolFilter === label}
            onClick={() => setToolFilter(label)}
          >
            {label}
          </button>
        ))}
      </div>
      {visible.length === 0 ? (
        // 空态收敛：有关键词 → 无匹配；扫描未完成 → 骨架屏占位；
        // done 后仍为空才是「确实没有」
        query.trim() ? (
          <EmptyState title="没有匹配的会话" />
        ) : scanState !== 'done' ? (
          <div className="flex flex-col gap-2">
            <Skeleton className="h-16 rounded border border-border" />
            <Skeleton className="h-16 rounded border border-border" />
            <Skeleton className="h-16 rounded border border-border" />
            <Skeleton className="h-16 rounded border border-border" />
          </div>
        ) : (
          <EmptyState title="该工作区暂无会话" />
        )
      ) : (
        <ul className="flex flex-col gap-2">
          {visible.map((s) => {
            const badge = badgeFor(s.ToolID);
            // 该会话是否已在中心区打开且未退出（内嵌终端或 ACP 聊天都算）：
            // 决定主按钮「恢复」还是「切换」，也决定行高亮
            const running =
              [...terminals, ...chats].some((t) => t.SessionID === s.ID && t.Status !== 'exited');
            // 渲染层兜底清洗：历史 / 未重扫的缓存里可能仍带着 XML 包装标签
            const title = displayTitle(s.Title);
            return (
              <li
                key={s.ID}
                className={cn(LIST_ROW, 'p-2', running && LIST_ROW_ACTIVE)}
              >
                <div className="flex min-w-0 flex-col gap-1.5">
                  {/* 第一行：标题（单行截断，悬停浮出完整标题卡片）+ 运行中标记 */}
                  <div className="flex min-w-0 items-center gap-1.5">
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <span
                          data-testid="session-title"
                          className={cn(
                            'min-w-0 cursor-default truncate text-sm font-medium',
                            !title && 'italic text-muted-foreground',
                          )}
                        >
                          {title || '(无标题)'}
                        </span>
                      </TooltipTrigger>
                      {/* 浮动卡片：完整标题（自动换行）+ 工具/时间/条数元信息 */}
                      <TooltipContent className="max-w-xs sm:max-w-sm" side="bottom" align="start">
                        <p className="whitespace-pre-wrap break-words text-xs leading-5">
                          {title || '(无标题)'}
                        </p>
                        <p className="mt-1 text-[10px] opacity-70">
                          {badge.label} · {formatRelativeTime(s.UpdatedAt)} · {s.Messages} 条
                        </p>
                      </TooltipContent>
                    </Tooltip>
                    {running && (
                      <span className="shrink-0 text-xs text-success" title="运行中">
                        ✓
                      </span>
                    )}
                  </div>
                  {/* 第二行：工具色点 + 相对时间 + 条数 + 右侧操作区 */}
                  <div className="flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
                    <ToolDot toolID={s.ToolID} className="shrink-0" />
                    <span className={`whitespace-nowrap ${MONO}`}>{formatRelativeTime(s.UpdatedAt)}</span>
                    <span className={`whitespace-nowrap ${MONO}`}>{s.Messages} 条</span>
                    <Button
                      size="sm"
                      variant={running ? 'default' : 'secondary'}
                      className="ml-auto shrink-0"
                      onClick={() => onOpenTerminal?.(s)}
                    >
                      {running ? '切换' : '恢复'}
                    </Button>
                  </div>
                </div>
              </li>
            );
          })}
        </ul>
      )}
      </div>
    </TooltipProvider>
  );
}
