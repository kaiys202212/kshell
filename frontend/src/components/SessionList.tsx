// 会话列表（工作区页签左栏）：展示当前工作区的历史会话，支持恢复 / 聚焦。
// 数据流与首页一致：先渲染缓存（GetSessions），收到 "scan:done" 后重调刷新，
// 不再回头调 ScanSessions（它每次都会触发新一轮后台扫描，会形成事件循环）。
// 打开状态：恢复成功按窗口标题把 windowStatus 置 true；"window:closed" 后还原。
// 标题匹配策略：Go 侧弹窗标题 = "kshell · " + 会话标题（超长按 80 rune 截断），
// 事件 payload 可能是被截断的标题，还原时对已知 key 做前缀匹配兜底。
import { useEffect, useMemo, useState } from 'react';
import {
  focusSession,
  getSessions,
  onScanDone,
  onWindowClosed,
  resumeSession,
} from '../lib/api';
import type { Session } from '../lib/api';
import { useAppStore } from '../state/store';
import WorkspaceSearch from './WorkspaceSearch';

// 工具徽标：ToolID → 展示名与配色；未识别的工具给中性色
const TOOL_BADGES: Record<string, { label: string; className: string }> = {
  codebuddy: { label: 'CodeBuddy', className: 'tool-badge--codebuddy' },
  codex: { label: 'Codex', className: 'tool-badge--codex' },
  claude: { label: 'Claude', className: 'tool-badge--claude' },
  gemini: { label: 'Gemini', className: 'tool-badge--gemini' },
};

function badgeFor(toolID: string) {
  return (
    TOOL_BADGES[toolID.toLowerCase()] ?? {
      label: toolID || '未知',
      className: 'tool-badge--other',
    }
  );
}

// 弹窗完整标题：与 Go 侧 WindowManager.TerminalTitle 的前缀保持一致
const fullTitle = (sessionTitle: string) => `kshell · ${sessionTitle}`;

// 相对时间：刚刚 / N 分钟前 / N 小时前 / N 天前，更久直接给日期
function formatRelativeTime(iso: string): string {
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return '';
  const min = Math.floor((Date.now() - t) / 60_000);
  if (min < 1) return '刚刚';
  if (min < 60) return `${min} 分钟前`;
  const hours = Math.floor(min / 60);
  if (hours < 24) return `${hours} 小时前`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${days} 天前`;
  return new Date(t).toLocaleDateString();
}

export default function SessionList({ workspacePath }: { workspacePath: string }) {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [query, setQuery] = useState('');
  const windowStatus = useAppStore((s) => s.windowStatus);
  const setWindowStatus = useAppStore((s) => s.setWindowStatus);

  useEffect(() => {
    let cancelled = false;
    const refresh = () =>
      getSessions().then((list) => {
        if (!cancelled) setSessions(list);
      });
    refresh();
    const offScan = onScanDone(() => void refresh());
    // 窗口关闭：payload 为完整窗口标题（可能被截断），对已知 key 前缀匹配还原
    const offClosed = onWindowClosed((title) => {
      const { windowStatus: status, setWindowStatus: set } = useAppStore.getState();
      for (const key of Object.keys(status)) {
        if (key === title || key.startsWith(title)) {
          set(key, false);
        }
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
    const key = fullTitle(s.Title);
    if (windowStatus[key] === true) {
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
          {query.trim() ? '没有匹配的会话' : '暂无会话，正在扫描……'}
        </p>
      ) : (
        <ul className="session-items">
          {visible.map((s) => {
            const badge = badgeFor(s.ToolID);
            const open = windowStatus[fullTitle(s.Title)] === true;
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
