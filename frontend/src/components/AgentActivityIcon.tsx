import { useEffect, useState } from 'react';
import { cn } from '../lib/cn';
import type { AgentActivity } from '../state/agentActivity';

// 用定时器改 transform，而不是 CSS animation。
// 全局样式在 prefers-reduced-motion 下把所有 animation-duration 压成 0.01ms，
// Windows「动画效果」关闭时 WebView2 会命中这条规则，CSS 转圈会停在第一帧。
function SpinningArc() {
  const [deg, setDeg] = useState(0);
  useEffect(() => {
    const id = window.setInterval(() => {
      setDeg((d) => (d + 45) % 360);
    }, 80);
    return () => window.clearInterval(id);
  }, []);
  return (
    <span data-spin="" className="inline-flex h-3.5 w-3.5" style={{ transform: `rotate(${deg}deg)` }}>
      <svg viewBox="0 0 14 14" className="h-3.5 w-3.5" aria-hidden="true">
        <path
          d="M12 7a5 5 0 1 1-3.5-4.77"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          strokeLinecap="round"
        />
      </svg>
    </span>
  );
}

interface Props {
  activity: AgentActivity;
  className?: string;
}

export default function AgentActivityIcon({ activity, className }: Props) {
  if (activity === 'idle') return null;
  if (activity === 'running') {
    return (
      <span
        className={cn('inline-flex h-3.5 w-3.5 shrink-0 text-success', className)}
        role="img"
        aria-label="执行中"
        title="执行中"
      >
        <SpinningArc />
      </span>
    );
  }
  if (activity === 'awaiting') {
    return (
      <span
        className={cn('inline-flex h-3 w-3 shrink-0 items-center justify-center text-warning', className)}
        role="img"
        aria-label="待用户确认"
        title="待用户确认"
      >
        <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-warning" aria-hidden="true" />
      </span>
    );
  }
  if (activity === 'waiting') {
    return (
      <span
        className={cn('inline-flex h-3 w-3 shrink-0 text-muted-foreground', className)}
        role="img"
        aria-label="等待用户"
        title="等待用户"
      >
        <svg viewBox="0 0 12 12" className="h-3 w-3" aria-hidden="true">
          <circle cx="6" cy="6" r="4.5" fill="none" stroke="currentColor" strokeWidth="1.5" />
        </svg>
      </span>
    );
  }
  return null;
}
