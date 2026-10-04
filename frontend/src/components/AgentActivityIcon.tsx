import { cn } from '../lib/cn';
import type { AgentActivity } from '../state/agentActivity';

interface Props {
  activity: AgentActivity;
  className?: string;
}

export default function AgentActivityIcon({ activity, className }: Props) {
  if (activity === 'idle') return null;
  if (activity === 'running') {
    return (
      <span
        className={cn('inline-flex h-3 w-3 shrink-0 text-success', className)}
        role="img"
        aria-label="执行中"
        title="执行中"
      >
        <svg viewBox="0 0 12 12" className="h-3 w-3 animate-spin" aria-hidden="true">
          <circle cx="6" cy="6" r="4.5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeDasharray="18 8" />
        </svg>
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
  return (
    <span
      className={cn('inline-flex h-3 w-3 shrink-0 text-muted-foreground', className)}
      role="img"
      aria-label="运行完成"
      title="运行完成"
    >
      <svg viewBox="0 0 12 12" className="h-3 w-3" aria-hidden="true">
        <path
          d="M2.5 6.5 L5 9 L9.5 3.5"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    </span>
  );
}
