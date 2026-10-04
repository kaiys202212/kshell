// 右键菜单：fixed 定位 + portal 到 body（FileTree 容器有 overflow，必须 portal）。
// 点击外部（window pointerdown）/ Esc 关闭。
// 菜单项必须用 onPointerDown 而非 onClick 执行：window 的 pointerdown 关闭监听
// 先于 click 触发，菜单项需 stopPropagation 后立即执行，否则菜单先被关掉。
// 菜单项由调用方传入（label + onSelect），danger 项用红色文字。
import { useEffect } from 'react';
import { createPortal } from 'react-dom';
import { cn } from '../lib/cn';

export interface MenuItem {
  label: string;
  onSelect(): void;
  danger?: boolean;
}

export default function ContextMenu({
  x,
  y,
  items,
  onClose,
}: {
  x: number;
  y: number;
  items: MenuItem[];
  onClose(): void;
}) {
  // 贴边收敛，避免菜单溢出窗口（min-w-36=144px + 内边距余量）
  const left = Math.min(x, window.innerWidth - 160);
  const top = Math.min(y, window.innerHeight - items.length * 30 - 8);

  useEffect(() => {
    const close = () => onClose();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('pointerdown', close);
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('pointerdown', close);
      window.removeEventListener('keydown', onKey);
    };
  }, [onClose]);

  return createPortal(
    <div
      role="menu"
      className="fixed z-50 min-w-36 rounded-md border border-border bg-popover p-1 shadow-md"
      style={{ left, top }}
    >
      {items.map((it) => (
        <button
          key={it.label}
          role="menuitem"
          className={cn(
            'block w-full rounded px-2 py-1 text-left text-xs transition-colors hover:bg-muted',
            it.danger && 'text-destructive',
          )}
          onPointerDown={(e) => {
            e.stopPropagation();
            it.onSelect();
          }}
        >
          {it.label}
        </button>
      ))}
    </div>,
    document.body,
  );
}
