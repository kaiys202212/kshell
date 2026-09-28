// 全局 Toast 体系（Radix Toast + store）：Toaster 挂在应用根部，从 store 读
// toasts/dismissToast。自动关闭（duration）触发 onOpenChange(false) 时回写 store。
// info/success 用卡片底色+左侧色条区分，error 用 destructive 反色强调。
import * as ToastPrimitive from '@radix-ui/react-toast';
import { useAppStore } from '../../state/store';
import type { Toast } from '../../state/store';
import { cn } from '../../lib/cn';

const TOAST_DURATION_MS = 3000;

// 各语气的容器与左侧色条配色
const toneClass: Record<Toast['tone'], { root: string; bar: string }> = {
  info: { root: 'bg-card border border-border text-foreground', bar: 'bg-primary' },
  success: { root: 'bg-card border border-border text-foreground', bar: 'bg-primary' },
  error: { root: 'bg-destructive text-destructive-foreground', bar: 'bg-destructive-foreground' },
};

export function Toaster() {
  const toasts = useAppStore((s) => s.toasts);
  const dismissToast = useAppStore((s) => s.dismissToast);

  return (
    <ToastPrimitive.Provider swipeDirection="right">
      {toasts.map((t) => (
        <ToastPrimitive.Root
          key={t.id}
          duration={TOAST_DURATION_MS}
          onOpenChange={(open) => {
            if (!open) dismissToast(t.id);
          }}
          className={cn(
            'flex items-stretch gap-2.5 overflow-hidden rounded-md px-3 py-2.5 text-sm shadow-lg',
            toneClass[t.tone].root,
          )}
        >
          <span aria-hidden className={cn('w-1 shrink-0 rounded-full', toneClass[t.tone].bar)} />
          <ToastPrimitive.Title className="flex-1 self-center leading-snug">
            {t.title}
          </ToastPrimitive.Title>
          <ToastPrimitive.Close
            aria-label="关闭提示"
            className="shrink-0 self-center opacity-60 transition-opacity hover:opacity-100"
          >
            ×
          </ToastPrimitive.Close>
        </ToastPrimitive.Root>
      ))}
      <ToastPrimitive.Viewport className="fixed bottom-4 right-4 z-50 flex w-80 max-w-[90vw] flex-col gap-2 outline-none" />
    </ToastPrimitive.Provider>
  );
}
