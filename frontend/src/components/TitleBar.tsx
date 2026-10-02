// 自绘标题栏（无边框窗口）：左侧 logo、中间页签（首页 + 工作区页签 + 设置）、右侧窗口控件。
// 拖拽靠 CSS --wails-draggable:drag（见 style.css 的 .kshell-drag），可点元素必须 no-drag。
// 窗口控件走 Wails runtime：最小化 / 最大化切换 / 关闭；最大化图标靠
// WindowIsMaximised + resize 事件（防抖）校正——Wails v2 不推窗口状态事件，
// 用户用 Win+↑ 或贴边改变状态时只能靠尺寸变化间接发现。
import { useCallback, useEffect, useState, type MouseEvent as ReactMouseEvent } from 'react';
import {
  WindowHide,
  WindowIsMaximised,
  WindowMinimise,
  WindowToggleMaximise,
} from '../../wailsjs/runtime/runtime';
import { cn } from '../lib/cn';
import { TAB_ACTIVE, TAB_BASE, TAB_UNDERLINE } from '../lib/ui';
import { SETTINGS_TAB_ID, type WorkspaceTab } from '../state/store';

// 页签基础态：扁平下划线激活态，不做胶囊填充
const tabBase = `kshell-no-drag ${TAB_BASE} h-7 max-w-44`;
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

interface Props {
  tabs: WorkspaceTab[];
  // null = 首页
  activeTabId: string | null;
  onSelectTab(id: string | null): void;
  onCloseTab(id: string): void;
}

export default function TitleBar({ tabs, activeTabId, onSelectTab, onCloseTab }: Props) {
  const [maximised, setMaximised] = useState(false);

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
        <span className="text-[13px] font-semibold tracking-wide">
          <span className="text-primary">k</span>shell
        </span>
      </div>

      {/* 页签区：首页 + 工作区页签（可关、中键关闭）；溢出横向滚动 */}
      <nav className="flex min-w-0 flex-1 items-stretch overflow-x-auto" aria-label="页签">
        <button
          className={cn(tabBase, activeTabId === null && tabActive)}
          aria-current={activeTabId === null}
          onClick={() => onSelectTab(null)}
        >
          首页
          {activeTabId === null && <span className={TAB_UNDERLINE} />}
        </button>

        {tabs.map((t) => {
          const active = activeTabId === t.id;
          return (
            <div
              key={t.id}
              className={cn('group', tabBase, active && tabActive)}
              onAuxClick={(e) => {
                if (e.button === 1) {
                  e.preventDefault();
                  onCloseTab(t.id);
                }
              }}
            >
              <button
                className="kshell-no-drag min-w-0 truncate"
                title={t.id}
                onClick={() => onSelectTab(t.id)}
              >
                {t.name}
              </button>
              <button
                className={cn(
                  'kshell-no-drag ml-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-sm text-sm leading-none text-muted-foreground transition-opacity hover:bg-muted hover:text-foreground',
                  active ? 'opacity-70 hover:opacity-100' : 'opacity-0 group-hover:opacity-100',
                )}
                aria-label={`关闭 ${t.name}`}
                onClick={() => onCloseTab(t.id)}
              >
                ×
              </button>
              {active && <span className={TAB_UNDERLINE} />}
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
        设置
        {activeTabId === SETTINGS_TAB_ID && (
          <span className={TAB_UNDERLINE} />
        )}
      </button>

      <div className="mx-1 w-px shrink-0 self-center bg-border" aria-hidden="true" />

      <div className="flex shrink-0 items-stretch">
        <button className={controlBase} aria-label="最小化" onClick={() => WindowMinimise()}>
          <MinGlyph />
        </button>
        <button
          className={controlBase}
          aria-label={maximised ? '还原' : '最大化'}
          onClick={toggleMaximise}
        >
          {maximised ? <RestoreGlyph /> : <MaxGlyph />}
        </button>
        {/* 「关闭」与原生 X 同语义：收进系统托盘（真正退出走托盘菜单，见 desktop.BeforeClose） */}
        <button
          className={cn(controlBase, 'hover:bg-destructive hover:text-destructive-foreground')}
          aria-label="关闭"
          title="关闭窗口（收进托盘）"
          onClick={() => WindowHide()}
        >
          <CloseGlyph />
        </button>
      </div>
    </header>
  );
}
