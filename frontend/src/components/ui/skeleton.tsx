// 骨架屏占位：加载态的统一形状占位（配 animate-pulse 微动效）。
import type { ComponentProps } from 'react';
import { cn } from '../../lib/cn';

export function Skeleton({ className, ...props }: ComponentProps<'div'>) {
  return <div className={cn('animate-pulse rounded-md bg-muted', className)} {...props} />;
}
