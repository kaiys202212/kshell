// 会话列表（工作区页签左栏）：展示当前工作区的历史会话，支持恢复 / 聚焦。
// 数据流与首页一致：先渲染缓存（GetSessions），收到 "scan:done" 后重调刷新，
// 不再回头调 ScanSessions（它每次都会触发新一轮后台扫描，会形成事件循环）。
// 打开状态：恢复成功按窗口标题把 windowStatus 置 true；"window:closed" 后还原。
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
import { terminalTitle } from '../lib/title';
import { cn } from '../lib/cn';
import { useAppStore } from '../state/store';
import WorkspaceSearch from './WorkspaceSearch';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { EmptyState } from './ui/empty-state';
import { Skeleton } from './ui/skeleton';

export default function SessionList({ workspacePath }: { workspacePath: string }) {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [query, setQuery] = useState('');
  const [toolFilter, setToolFilter] = useState<string | null>(null); // null = 全部工具
  const windowStatus = useAppStore((s) => s.windowStatus);
  const setWindowStatus = useAppStore((s) => s.setWindowStatus);
  const scanState = useAppStore((s) => s.scanState);
  const setScanState = useAppStore((s) => s.setScanState);
  const notify = useAppStore((s) => s.notify);

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

  const handleResume = async (s: Session) => {
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
      <div className="flex flex-wrap items-center gap-1.5" aria-label="按工具筛选">
        <button
          className="rounded-full outline-none"
          aria-pressed={toolFilter === null}
          onClick={() => setToolFilter(null)}
        >
          <Badge variant={toolFilter === null ? 'default' : 'outline'}>全部</Badge>
        </button>
        {toolOptions.map((label) => (
          <button
            key={label}
            className="rounded-full outline-none"
            aria-pressed={toolFilter === label}
            onClick={() => setToolFilter(label)}
          >
            <Badge variant={toolFilter === label ? 'default' : 'outline'}>{label}</Badge>
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
            <Skeleton className="h-16 rounded-lg border border-border" />
            <Skeleton className="h-16 rounded-lg border border-border" />
            <Skeleton className="h-16 rounded-lg border border-border" />
            <Skeleton className="h-16 rounded-lg border border-border" />
          </div>
        ) : (
          <EmptyState title="该工作区暂无会话" />
        )
      ) : (
        <ul className="flex flex-col gap-2">
          {visible.map((s) => {
            const badge = badgeFor(s.ToolID);
            const key = terminalTitle(s.Title);
            const open = windowStatus[key] === true;
            return (
              <li
                key={s.ID}
                className={cn(
                  'rounded-lg border border-border bg-card p-2.5 transition-colors',
                  open && 'border-l-2 border-l-primary bg-primary/5',
                )}
              >
                <div className="flex min-w-0 items-center gap-1.5">
                  <span className="min-w-0 truncate text-sm font-medium" title={s.Path}>
                    {s.Title}
                  </span>
                  {open && <span className="shrink-0 text-xs text-primary">✓</span>}
                </div>
                <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
                  <Badge variant={badge.className === 'tool-badge--other' ? 'muted' : 'default'}>
                    {badge.label}
                  </Badge>
                  <span className="whitespace-nowrap">{formatRelativeTime(s.UpdatedAt)}</span>
                  <span className="whitespace-nowrap">{s.Messages} 条</span>
                  <Button
                    size="sm"
                    variant="secondary"
                    className="ml-auto shrink-0"
                    onClick={() => void handleResume(s)}
                  >
                    恢复
                  </Button>
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
