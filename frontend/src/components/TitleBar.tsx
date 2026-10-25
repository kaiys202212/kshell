// 自绘标题栏（无边框窗口）：左侧 logo、中间页签（首页 + 工作区页签 + 设置）、右侧窗口控件。
// 拖拽靠 CSS --wails-draggable:drag（见 style.css 的 .kshell-drag），可点元素必须 no-drag。
// 窗口控件走 Wails runtime：最小化 / 最大化切换 / 关闭；最大化图标靠
// WindowIsMaximised + resize 事件（防抖）校正——Wails v2 不推窗口状态事件，
// 用户用 Win+↑ 或贴边改变状态时只能靠尺寸变化间接发现。
import { useCallback, useEffect, useMemo, useState, type MouseEvent as ReactMouseEvent } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Quit,
  WindowIsMaximised,
  WindowMinimise,
  WindowToggleMaximise,
} from '../../wailsjs/runtime/runtime';
import { cn } from '../lib/cn';
import { TAB_ACTIVE, TAB_BASE, TAB_UNDERLINE, TAB_UNDERLINE_PREVIEW } from '../lib/ui';
import { sameWorkspacePath } from '../lib/workspacePath';
import { resolveAgentActivity } from '../state/agentActivity';
import { SETTINGS_TAB_ID, useAppStore, type WorkspaceTab } from '../state/store';
import AgentActivityIcon from './AgentActivityIcon';

// 页签基础态：扁平下划线激活态，不做胶囊填充；group 供 hover 预览条显隐用
// 工作区页签 max-w-48（~192px）加宽，避免长项目名几乎看不见
const tabBase = `kshell-no-drag group ${TAB_BASE} h-7 max-w-48`;
const tabActive = TAB_ACTIVE;

// 窗口控件按钮：Windows 习惯的热区，关闭键 hover 变危险色
const controlBase =
  'kshell-no-drag flex h-8 w-10 items-center justify-center text-muted-foreground transition-colors hover:bg-muted hover:text-foreground';

function MinGlyph() {
  return (
    <svg viewBox="0 0 12 12" className="h-3 w-3" aria-hidden="true">
      <path d="M2 6h8" stroke="currentColor" strokeWidth="1.2" />
    </svg>
  );
}

function MaxGlyph() {
  return (
    <svg viewBox="0 0 12 12" className="h-3 w-3" aria-hidden="true">
      <rect x="2.5" y="2.5" width="7" height="7" fill="none" stroke="currentColor" strokeWidth="1.2" />
    </svg>
  );
}

function RestoreGlyph() {
  return (
    <svg viewBox="0 0 12 12" className="h-3 w-3" aria-hidden="true">
      <rect x="2" y="3.5" width="6" height="6" fill="none" stroke="currentColor" strokeWidth="1.2" />
      <path d="M4.5 3.5V2.5h5v5h-1" fill="none" stroke="currentColor" strokeWidth="1.2" />
    </svg>
  );
}

function CloseGlyph() {
  return (
    <svg viewBox="0 0 12 12" className="h-3 w-3" aria-hidden="true">
      <path d="M3 3l6 6M9 3l-6 6" stroke="currentColor" strokeWidth="1.2" />
    </svg>
  );
}

/** 该工作区是否有会话处于「执行中」（与中心区旋转图标同口径） */
export function workspaceHasRunning(
  wsPath: string,
  chats: { ID: string; Workspace: string; Status: string }[],
  terminals: { ID: string; Workspace: string; Status: string; Kind: string }[],
  chatPermissions: Record<string, unknown>,
  terminalBusy: Record<string, boolean>,
): boolean {
  for (const c of chats) {
    if (!sameWorkspacePath(c.Workspace, wsPath)) continue;
    if (
      resolveAgentActivity({
        status: c.Status,
        hasPermission: !!chatPermissions[c.ID],
        kind: 'chat',
      }) === 'running'
    ) {
      return true;
    }
  }
  for (const t of terminals) {
    if (t.Kind !== 'session' && t.Kind !== 'new') continue;
    if (!sameWorkspacePath(t.Workspace, wsPath)) continue;
    if (
      resolveAgentActivity({
        status: t.Status,
        hasPermission: false,
        kind: 'terminal',
        busy: !!terminalBusy[t.ID],
      }) === 'running'
    ) {
      return true;
    }
  }
  return false;
}

interface Props {
  tabs: WorkspaceTab[];
  // null = 首页
  activeTabId: string | null;
  onSelectTab(id: string | null): void;
  onCloseTab(id: string): void;
}

export default function TitleBar({ tabs, activeTabId, onSelectTab, onCloseTab }: Props) {
  const { t: tr } = useTranslation();
  const [maximised, setMaximised] = useState(false);
  const chats = useAppStore((s) => s.chats);
  const terminals = useAppStore((s) => s.terminals);
  const chatPermissions = useAppStore((s) => s.chatPermissions);
  const terminalBusy = useAppStore((s) => s.terminalBusy);

  const runningByTab = useMemo(() => {
    const m = new Map<string, boolean>();
    for (const t of tabs) {
      m.set(t.id, workspaceHasRunning(t.id, chats, terminals, chatPermissions, terminalBusy));
    }
    return m;
  }, [tabs, chats, terminals, chatPermissions, terminalBusy]);

  // 查询最大化状态；绑定在纯浏览器/测试环境不存在时不抛错
  const queryMaximised = useCallback(() => {
    try {
      const p = WindowIsMaximised();
      if (p && typeof p.then === 'function') {
        p.then((v) => setMaximised(Boolean(v))).catch(() => {});
      }
    } catch {
      // 非 Wails 环境（浏览器调试）忽略
    }
  }, []);

  useEffect(() => {
    queryMaximised();
    // 手动缩放窗口会高频触发 resize：防抖后再查，避免每帧一次 IPC
    let timer: number | undefined;
    const onResize = () => {
      if (timer !== undefined) window.clearTimeout(timer);
      timer = window.setTimeout(() => {
        timer = undefined;
        queryMaximised();
      }, 120);
    };
    window.addEventListener('resize', onResize);
    return () => {
      window.removeEventListener('resize', onResize);
      if (timer !== undefined) window.clearTimeout(timer);
    };
  }, [queryMaximised]);

  const toggleMaximise = useCallback(() => {
    try {
      WindowToggleMaximise();
      setMaximised((v) => !v); // 乐观更新，随后的 resize 防抖查询会校正
    } catch {
      // 忽略非 Wails 环境
    }
  }, []);

  // 双击标题栏空白处 = 最大化/还原（与原生窗口一致）；点在页签/按钮上不触发
  const onDoubleClick = (e: ReactMouseEvent<HTMLElement>) => {
    if ((e.target as HTMLElement).closest('button, a, input, select, textarea')) return;
    toggleMaximise();
  };

  return (
    <header
      className="kshell-drag flex h-8 shrink-0 items-stretch border-b border-border bg-card pl-2"
      onDoubleClick={onDoubleClick}
    >
      <div className="flex shrink-0 items-center gap-1.5 pr-3">
        {/* 品牌图标随 public/appicon.png 走构建产物；品牌名不进 i18n（与窗口标题/托盘一致） */}
        <img src="/appicon.png" alt="Capybara" className="h-4 w-4" draggable={false} />
        <span className="text-[13px] font-semibold tracking-wide">Capybara</span>
      </div>

      {/* 页签区：首页 + 工作区页签（可关、中键关闭）；溢出横向滚动 */}
      <nav className="flex min-w-0 flex-1 items-stretch overflow-x-auto" aria-label={tr('ui.titlebar.tabs_aria')}>
        <button
          className={cn(tabBase, activeTabId === null && tabActive)}
          aria-current={activeTabId === null}
          onClick={() => onSelectTab(null)}
        >
          {tr('ui.titlebar.home')}
          {activeTabId === null ? (
            <span className={TAB_UNDERLINE} />
          ) : (
            <span className={TAB_UNDERLINE_PREVIEW} aria-hidden="true" />
          )}
        </button>

        {tabs.map((t) => {
          const active = activeTabId === t.id;
          const running = runningByTab.get(t.id);
          return (
            <div
              key={t.id}
              className={cn(tabBase, active && tabActive)}
              // 整条页签可点（名称右侧的关闭钮间隙/留白此前点不动）
              onClick={() => onSelectTab(t.id)}
              onAuxClick={(e) => {
                if (e.button === 1) {
                  e.preventDefault();
                  onCloseTab(t.id);
                }
              }}
            >
              {running ? <AgentActivityIcon activity="running" /> : null}
              <button className="kshell-no-drag min-w-0 truncate" title={t.id}>
                {t.name}
              </button>
              <button
                className={cn(
                  'kshell-no-drag ml-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-sm text-sm leading-none text-muted-foreground transition-opacity hover:bg-muted hover:text-foreground',
                  active ? 'opacity-70 hover:opacity-100' : 'opacity-0 group-hover:opacity-100',
                )}
                aria-label={tr('ui.titlebar.close_tab', { name: t.name })}
                onClick={(e) => {
                  e.stopPropagation(); // 只关，不顺带切到该页签
                  onCloseTab(t.id);
                }}
              >
                ×
              </button>
              {active ? (
                <span className={TAB_UNDERLINE} />
              ) : (
                <span className={TAB_UNDERLINE_PREVIEW} aria-hidden="true" />
              )}
            </div>
          );
        })}
      </nav>

      {/* 设置固定贴最右（窗口控件之前） */}
      <button
        className={cn(tabBase, 'kshell-no-drag px-3', activeTabId === SETTINGS_TAB_ID && tabActive)}
        aria-current={activeTabId === SETTINGS_TAB_ID}
        onClick={() => onSelectTab(SETTINGS_TAB_ID)}
      >
        {tr('ui.titlebar.settings')}
        {activeTabId === SETTINGS_TAB_ID ? (
          <span className={TAB_UNDERLINE} />
        ) : (
          <span className={TAB_UNDERLINE_PREVIEW} aria-hidden="true" />
        )}
      </button>

      <div className="mx-1 w-px shrink-0 self-center bg-border" aria-hidden="true" />

      <div className="flex shrink-0 items-stretch">
        <button className={controlBase} aria-label={tr('ui.titlebar.minimize')} onClick={() => WindowMinimise()}>
          <MinGlyph />
        </button>
        <button
          className={controlBase}
          aria-label={maximised ? tr('ui.titlebar.restore') : tr('ui.titlebar.maximize')}
          onClick={toggleMaximise}
        >
          {maximised ? <RestoreGlyph /> : <MaxGlyph />}
        </button>
        {/* 「关闭」经 Wails Quit 触发 BeforeClose，由配置的关闭行为（收进托盘 / 直接退出）决定 */}
        <button
          className={cn(controlBase, 'hover:bg-destructive hover:text-destructive-foreground')}
          aria-label={tr('ui.titlebar.close')}
          title={tr('ui.titlebar.close_window')}
          onClick={() => Quit()}
        >
          <CloseGlyph />
        </button>
      </div>
    </header>
  );
}
