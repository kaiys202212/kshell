// 通用弹层：统一遮罩/容器/进出动画，收编此前 3 套各写一遍的弹层方案。
// Esc 关闭、焦点圈定、遮罩点击关闭由 Radix Dialog 提供。
// 调用点在自己的 children 里放 DialogPrimitive.Title（Radix 可访问性要求），
// 定位/宽度差异通过 className 覆盖（cn 走 tailwind-merge，冲突类后者胜）。
import * as DialogPrimitive from '@radix-ui/react-dialog';
import type { ReactNode } from 'react';
import { cn } from '../../lib/cn';

export interface DialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: ReactNode;
  className?: string;
  // false 时拦截 Esc / 点遮罩，只允许调用方主动把 open 设为 false（如向导的「跳过」）。
  dismissible?: boolean;
}

export function Dialog({ open, onOpenChange, children, className, dismissible = true }: DialogProps) {
  const lock = (e: Event) => {
    if (!dismissible) e.preventDefault();
  };
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay
          className="fixed inset-0 bg-[var(--overlay)] backdrop-blur-[2px]"
          style={{ animation: 'kshell-fade-in var(--duration-base) var(--ease-out)' }}
        />
        <DialogPrimitive.Content
          aria-describedby={undefined}
          className={cn(
            'fixed left-1/2 top-[18%] z-50 -translate-x-1/2 rounded-md border border-border bg-card p-4 shadow-lg',
            className,
          )}
          style={{ animation: 'kshell-pop-in var(--duration-base) var(--ease-out)' }}
          onEscapeKeyDown={lock}
          onPointerDownOutside={lock}
          onInteractOutside={lock}
        >
          {children}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
