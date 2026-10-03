// 通用输入框：统一边框/背景/焦点环，收编此前 4 处各写一遍的输入框样式。
import { cva, type VariantProps } from 'class-variance-authority';
import type { ComponentProps } from 'react';
import { cn } from '../../lib/cn';

const inputVariants = cva(
  'rounded-[3px] border border-input bg-card text-foreground placeholder:text-muted-foreground transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50',
  {
    variants: {
      size: { default: 'h-8 px-2 text-[13px]', sm: 'h-7 px-2 text-xs' },
    },
    defaultVariants: { size: 'default' },
  },
);

// 原生 input 的 size 是数字属性，与 cva 的尺寸 variant 冲突，Omit 后以 variant 为准
export interface InputProps extends Omit<ComponentProps<'input'>, 'size'>, VariantProps<typeof inputVariants> {}

export function Input({ className, size, ...props }: InputProps) {
  return <input className={cn(inputVariants({ size }), className)} {...props} />;
}
