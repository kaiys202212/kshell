// 会话列表（工作区页签左栏）：展示当前工作区的历史会话，支持恢复（中心区内嵌终端）与
// 「在外部终端打开」（原有弹窗路径）。
// 数据流与首页一致：先渲染缓存（GetSessions），收到 "scan:done" 后重调刷新，
// 不再回头调 ScanSessions（它每次都会触发新一轮后台扫描，会形成事件循环）。
// 打开状态：外部终端恢复成功按窗口标题把 windowStatus 置 true；"window:closed" 后还原。
// 标题匹配策略：键与事件 payload 都是 terminalTitle 归一化后的完整标题，
// 用严格相等匹配（不用前缀兜底——标题互为前缀的会话会误伤）。
import { useEffect, useMemo, useState } from 'react';
import {
  focusSession,
  getSessions,
  onScanDone,
  onWindowClosed,
  resumeSession,
} from '../lib/api';
import type { Session } from '../lib/api';
import { formatRelativeTime } from '../lib/format';
import { badgeFor } from '../lib/toolBadge';
import { displayTitle, terminalTitle } from '../lib/title';
import { cn } from '../lib/cn';
import { useAppStore } from '../state/store';
import WorkspaceSearch from './WorkspaceSearch';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { EmptyState } from './ui/empty-state';
import { Skeleton } from './ui/skeleton';

// 工具筛选项：扁平下划线式（选中态主色底 + 主色文字 + 下划线），统一 rounded-sm 不用胶囊
const chipClass = (active: boolean) =>
  cn(
    'rounded-sm px-1.5 py-0.5 text-xs transition-colors',
    active
      ? 'bg-primary/10 font-medium text-primary underline decoration-primary underline-offset-4'
      : 'text-muted-foreground hover:bg-muted hover:text-foreground',
  );

// 「在外部终端打开」图标（手写内联 SVG，不引图标库）：方框 + 右上角外跳箭头
function ExternalGlyph() {
  return (
    <svg
      viewBox="0 0 24 24"
      className="h-3.5 w-3.5"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      aria-hidden="true"
    >
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        d="M13.5 4.5h6v6M19.5 4.5 11 13M17 14.5V18a1.5 1.5 0 0 1-1.5 1.5h-9A1.5 1.5 0 0 1 5 18V9a1.5 1.5 0 0 1 1.5-1.5H10"
      />
    </svg>
  );
}

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
  const windowStatus = useAppStore((s) => s.windowStatus);
  const setWindowStatus = useAppStore((s) => s.setWindowStatus);
  const scanState = useAppStore((s) => s.scanState);
  const setScanState = useAppStore((s) => s.setScanState);
  const notify = useAppStore((s) => s.notify);
  const terminals = useAppStore((s) => s.terminals);

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
    // 窗口关闭：payload 为完整窗口标题（terminalTitle 形态），严格相等匹配还原
    const offClosed = onWindowClosed((title) => {
      const { windowStatus: status, setWindowStatus: set } = useAppStore.getState();
      if (title in status) {
        set(title, false);
      }
    });
    return () => {
      cancelled = true;
      offScan();
      offClosed();
    };
  }, [setScanState]);

  // 工作区过滤（路径大小写不敏感，对齐 Go 侧 NormalizePath）+ 关键词过滤 + 时间降序
  const target = workspacePath.toLowerCase();
  // 工具 chip 选项：当前工作区会话按工具展示名去重（保持出现顺序）
  const toolOptions = useMemo(() => {
    const set = new Set<string>();
    for (const s of sessions) {
      if (s.Workspace.toLowerCase() === target) set.add(badgeFor(s.ToolID).label);
    }
    return [...set];
  }, [sessions, target]);
  const visible = useMemo(() => {
    const q = query.trim().toLowerCase();
    return sessions
      .filter((s) => s.Workspace.toLowerCase() === target)
      .filter((s) => toolFilter === null || badgeFor(s.ToolID).label === toolFilter)
      .filter((s) => {
        if (!q) return true;
        return (
          s.Title.toLowerCase().includes(q) ||
          s.ToolID.toLowerCase().includes(q) ||
          badgeFor(s.ToolID).label.toLowerCase().includes(q)
        );
      })
      .sort((a, b) => +new Date(b.UpdatedAt) - +new Date(a.UpdatedAt));
  }, [sessions, query, toolFilter, target]);

  // 「在外部终端打开」：恢复弹窗 / 已打开则聚焦（与内嵌终端入口互不影响）
  const handleResumeExternal = async (s: Session) => {
    const key = terminalTitle(s.Title);
    // 读实时状态而非闭包值，避免连续点击时用到过期的 windowStatus
    if (useAppStore.getState().windowStatus[key] === true) {
      // 已打开过：转聚焦；返回 false 说明窗口实际已关，还原状态
      const ok = await focusSession(s.ID);
      if (!ok) setWindowStatus(key, false);
      return;
    }
    try {
      await resumeSession(s.ID);
      setWindowStatus(key, true);
      notify('已在外部终端打开', 'success');
    } catch (e: unknown) {
      // 启动失败（未就绪等）不弹窗打断：行状态保持原样，轻量提示告知原因
      notify(`恢复会话失败：${e instanceof Error ? e.message : String(e)}`, 'error');
    }
  };

  return (
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
            const key = terminalTitle(s.Title);
            const externalOpen = windowStatus[key] === true;
            // 该会话是否已有运行中的内嵌终端（决定主按钮「恢复」还是「切换」）
            const embedded = terminals.some((t) => t.SessionID === s.ID && t.Status === 'running');
            const running = externalOpen || embedded;
            // 渲染层兜底清洗：历史 / 未重扫的缓存里可能仍带着 XML 包装标签
            const title = displayTitle(s.Title);
            return (
              <li
                key={s.ID}
                className={cn(
                  'rounded border border-border bg-card p-2.5 transition-colors',
                  running && 'border-l-2 border-l-primary bg-primary/5',
                )}
              >
                <div className="flex min-w-0 flex-col gap-1.5">
                  {/* 第一行：标题（单行截断，title 给完整原文）+ 运行中标记 */}
                  <div className="flex min-w-0 items-center gap-1.5">
                    <span
                      className={cn(
                        'min-w-0 truncate text-sm font-medium',
                        !title && 'italic text-muted-foreground',
                      )}
                      title={s.Title}
                    >
                      {title || '(无标题)'}
                    </span>
                    {running && (
                      <span className="shrink-0 text-xs text-primary" title="运行中">
                        ✓
                      </span>
                    )}
                  </div>
                  {/* 第二行：工具徽标 + 相对时间 + 条数 + 右侧操作区 */}
                  <div className="flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
                    <Badge
                      variant={badge.className === 'tool-badge--other' ? 'muted' : 'default'}
                    >
                      {badge.label}
                    </Badge>
                    <span className="whitespace-nowrap">{formatRelativeTime(s.UpdatedAt)}</span>
                    <span className="whitespace-nowrap">{s.Messages} 条</span>
                    <Button
                      size="sm"
                      variant={embedded ? 'default' : 'secondary'}
                      className="ml-auto shrink-0"
                      onClick={() => onOpenTerminal?.(s)}
                    >
                      {embedded ? '切换' : '恢复'}
                    </Button>
                    <Button
                      size="icon"
                      variant="ghost"
                      className="shrink-0"
                      aria-label="在外部终端打开"
                      title="在外部终端打开"
                      onClick={() => void handleResumeExternal(s)}
                    >
                      <ExternalGlyph />
                    </Button>
                  </div>
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
