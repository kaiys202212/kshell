// 工具色点徽标（S1）：固定色相小圆点 + 工具名。
// 替代会话行/首页卡片/终端页签里的纯色 Badge，让多工具一眼可辨。
import { badgeFor } from '../../lib/toolBadge';
import { cn } from '../../lib/cn';

export function ToolDot({
  toolID,
  className,
  showLabel = true,
}: {
  toolID: string;
  className?: string;
  /** 是否显示工具名文字；false 时只留色点（用于标题已含工具名的场景，避免重复） */
  showLabel?: boolean;
}) {
  const badge = badgeFor(toolID);
  return (
    <span className={cn('inline-flex min-w-0 items-center gap-1 text-[11px] text-muted-foreground', className)}>
      <span
        aria-hidden="true"
        className="h-1.5 w-1.5 shrink-0 rounded-full"
        style={{ background: badge.color }}
      />
      {showLabel && <span className="truncate">{badge.label}</span>}
    </span>
  );
}
