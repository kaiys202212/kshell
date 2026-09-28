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
import { useAppStore } from '../state/store';
import WorkspaceSearch from './WorkspaceSearch';

export default function SessionList({ workspacePath }: { workspacePath: string }) {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [query, setQuery] = useState('');
  const windowStatus = useAppStore((s) => s.windowStatus);
  const setWindowStatus = useAppStore((s) => s.setWindowStatus);
  const scanState = useAppStore((s) => s.scanState);

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
    const offScan = onScanDone(() => void refresh());
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
  }, []);

  // 工作区过滤（路径大小写不敏感，对齐 Go 侧 NormalizePath）+ 关键词过滤 + 时间降序
  const target = workspacePath.toLowerCase();
  const visible = useMemo(() => {
    const q = query.trim().toLowerCase();
    return sessions
      .filter((s) => s.Workspace.toLowerCase() === target)
      .filter((s) => {
        if (!q) return true;
        return (
          s.Title.toLowerCase().includes(q) ||
          s.ToolID.toLowerCase().includes(q) ||
          badgeFor(s.ToolID).label.toLowerCase().includes(q)
        );
      })
      .sort((a, b) => +new Date(b.UpdatedAt) - +new Date(a.UpdatedAt));
  }, [sessions, query, target]);

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
    } catch {
      // 启动失败（未就绪等）不弹窗打断，行状态保持原样
    }
  };

  return (
    <div className="session-list">
      <WorkspaceSearch value={query} onChange={setQuery} />
      {visible.length === 0 ? (
        <p className="session-empty">
          {query.trim()
            ? '没有匹配的会话'
            : scanState === 'done'
              ? '该工作区暂无会话'
              : '暂无会话，正在扫描……'}
        </p>
      ) : (
        <ul className="session-items">
          {visible.map((s) => {
            const badge = badgeFor(s.ToolID);
            const key = terminalTitle(s.Title);
            const open = windowStatus[key] === true;
            return (
              <li
                key={s.ID}
                className={open ? 'session-item session-item--open' : 'session-item'}
              >
                <div className="session-row">
                  <span className="session-title" title={s.Path}>
                    {s.Title}
                  </span>
                  {open && <span className="session-open-mark">✓</span>}
                </div>
                <div className="session-meta">
                  <span className={`tool-badge ${badge.className}`}>{badge.label}</span>
                  <span className="session-time">{formatRelativeTime(s.UpdatedAt)}</span>
                  <span className="session-count">{s.Messages} 条</span>
                  <button
                    className="session-resume"
                    onClick={() => void handleResume(s)}
                  >
                    恢复
                  </button>
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
