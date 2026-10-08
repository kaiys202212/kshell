// agent 通知气泡：右下角堆叠展示 agent 完成与等待确认事件。
// 最多同时 3 条，超出折叠为「+N」计数条；每条 10s 自动消失，hover 暂停倒计时；
// 点击按 termKey 切到对应页签并关闭该气泡。
// 主窗不可见（document.hidden）或失焦时不渲染卡片，避免与 Go 侧原生气泡重复；
// 队列仍由 notify:agent 入队，供原生气泡点击后的 notify:focus 复用。
// 主题跟随 appearance 机制：样式全部用语义 token（bg-card / text-foreground 等），
// data-theme 切换时自动适配亮/暗色。
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { cn } from '../lib/cn';
import { eventLabel, toolDisplayName, type AgentNotice } from '../lib/agentNotify';
import { sameWorkspacePath } from '../lib/workspacePath';
import { useAppStore } from '../state/store';

const MAX_VISIBLE = 3;
const AUTO_DISMISS_MS = 10000;

// focusNoticeTarget 把 termKey 归因到内嵌终端/聊天，激活其所在工作区页签并
// 发起中心区页签切换请求。归因失败（外部窗口 window:<标题> 等）不动页签，
// 由调用方只关闭气泡。
export function focusNoticeTarget(termKey: string) {
  const { terminals, chats } = useAppStore.getState();
  const target = terminals.find((t) => t.Key === termKey) ?? chats.find((c) => c.Key === termKey);
  if (!target) return;
  const { openTabs, workspaces, setActiveTab, openTab, requestFocusTerm } = useAppStore.getState();
  const tab = openTabs.find((t) => sameWorkspacePath(t.id, target.Workspace));
  if (tab) {
    setActiveTab(tab.id);
  } else {
    // 工作区页签没开过就顺手打开（工作区列表里没有时只能等它出现后再跳）
    const w = workspaces.find((x) => sameWorkspacePath(x.Path, target.Workspace));
    if (w) openTab(w);
  }
  requestFocusTerm(termKey);
}

// handleNotifyFocus 响应 Go 原生气泡点击发出的 notify:focus：聚焦 + dismiss 同 termKey。
export function handleNotifyFocus(termKey: string) {
  if (!termKey) return;
  focusNoticeTarget(termKey);
  const { agentNotices, dismissAgentNotice } = useAppStore.getState();
  for (const n of agentNotices) {
    if (n.termKey === termKey) dismissAgentNotice(n.id);
  }
}

function NoticeCard({ notice }: { notice: AgentNotice }) {
  const { t } = useTranslation();
  const dismiss = useAppStore((s) => s.dismissAgentNotice);
  // hover 暂停倒计时：挂起时记录剩余时长，移开后按剩余时间继续
  const remainingRef = useRef(AUTO_DISMISS_MS);
  const startedAtRef = useRef(0);
  const timerRef = useRef<number | null>(null);

  const disarm = () => {
    if (timerRef.current !== null) {
      window.clearTimeout(timerRef.current);
      remainingRef.current -= Date.now() - startedAtRef.current;
      timerRef.current = null;
    }
  };
  const arm = () => {
    startedAtRef.current = Date.now();
    timerRef.current = window.setTimeout(() => dismiss(notice.id), remainingRef.current);
  };

  useEffect(() => {
    arm();
    return disarm;
    // 通知条目的生命周期从入队到移除，id 不变，无需依赖数组项
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const attention = eventLabel(notice.event) === 'waiting_confirm';
  const body = notice.summary || notice.workspace;
  return (
    <button
      type="button"
      className={cn(
        'pointer-events-auto w-72 rounded border border-border bg-card px-3 py-2 text-left shadow-lg transition-colors hover:bg-muted/60',
        attention && 'border-l-2 border-l-primary',
      )}
      onMouseEnter={disarm}
      onMouseLeave={arm}
      onClick={() => {
        focusNoticeTarget(notice.termKey);
        dismiss(notice.id);
      }}
    >
      <span className="block truncate text-xs font-medium text-foreground">
        {toolDisplayName(notice.tool)} {t(`ui.notify.${eventLabel(notice.event)}`)}
      </span>
      {body && <span className="mt-0.5 line-clamp-2 block text-xs text-muted-foreground">{body}</span>}
    </button>
  );
}

export default function NotificationBubble() {
  const { t } = useTranslation();
  const notices = useAppStore((s) => s.agentNotices);
  const [docHidden, setDocHidden] = useState(() => typeof document !== 'undefined' && document.hidden);
  const [focused, setFocused] = useState(
    () => typeof document !== 'undefined' && typeof document.hasFocus === 'function' && document.hasFocus(),
  );

  useEffect(() => {
    const onVis = () => setDocHidden(document.hidden);
    const onFocus = () => setFocused(true);
    const onBlur = () => setFocused(false);
    document.addEventListener('visibilitychange', onVis);
    window.addEventListener('focus', onFocus);
    window.addEventListener('blur', onBlur);
    return () => {
      document.removeEventListener('visibilitychange', onVis);
      window.removeEventListener('focus', onFocus);
      window.removeEventListener('blur', onBlur);
    };
  }, []);

  // 主窗不可见/失焦时由 Go 原生气泡承接展示，此处不渲染避免双份刷屏
  if (docHidden || !focused || notices.length === 0) return null;

  // store 队列按入队顺序追加（旧→新），取末尾即最新 3 条；渲染时倒序，
  // 最新事件在最上（贴近阅读起点）。过期 / hover 倒计时 / 点击均按 notice.id 独立运作，
  // 与渲染顺序解耦，倒序不影响这些行为。
  const visible = notices.slice(-MAX_VISIBLE).reverse();
  const overflow = notices.length - visible.length;
  return (
    <div
      className="pointer-events-none fixed bottom-4 right-4 z-50 flex flex-col items-end gap-2"
      aria-label={t('ui.notify.aria')}
    >
      {visible.map((n) => (
        <NoticeCard key={n.id} notice={n} />
      ))}
      {overflow > 0 && (
        <div className="pointer-events-auto rounded border border-border bg-card px-2 py-1 text-xs text-muted-foreground shadow-lg">
          +{overflow}
        </div>
      )}
    </div>
  );
}
