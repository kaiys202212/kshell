// 统一空态：图标 + 主文案 + muted 副文案，居中竖排。
import type { ReactNode } from 'react';
import { cn } from '../../lib/cn';

export interface EmptyStateProps {
  icon?: ReactNode;
  title: string;
  hint?: string;
  className?: string;
}

export function EmptyState({ icon, title, hint, className }: EmptyStateProps) {
  return (
    <div className={cn('flex flex-col items-center justify-center gap-1.5 py-10', className)}>
      {icon && (
        <div className="flex h-10 w-10 items-center justify-center rounded-full bg-primary/10 text-primary">
          {icon}
        </div>
      )}
      <p className="text-sm font-medium">{title}</p>
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  );
}
