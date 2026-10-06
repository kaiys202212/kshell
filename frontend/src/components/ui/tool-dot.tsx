// 工具徽标：官方风格图标 + 可选工具名。
import { badgeFor } from '../../lib/toolBadge';
import { cn } from '../../lib/cn';
import { ToolIcon } from './tool-icons';

export function ToolDot({
  toolID,
  className,
  showLabel = true,
}: {
  toolID: string;
  className?: string;
  /** 是否显示工具名文字；false 时只留图标（标题已含工具名时避免重复） */
  showLabel?: boolean;
}) {
  const badge = badgeFor(toolID);
  return (
    <span className={cn('inline-flex min-w-0 items-center gap-1 text-[11px] text-muted-foreground', className)}>
      <ToolIcon toolID={toolID} label={badge.label} />
      {showLabel && <span className="truncate">{badge.label}</span>}
    </span>
  );
}
