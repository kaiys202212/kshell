// 紧凑「工具选择」下拉（新建会话用）：手写实现，不引入 Radix dropdown-menu 原语。
// tools 由父级过滤（只会传「已安装且有可执行文件」的工具，来自 GetTools()）；
// value='' 表示「自动（该工作区最常用）」，触发器此时显示「自动」，工具列表变化后找不到
// 对应项也回退「自动」；tools 为空显示「无可用工具」并禁用（不崩）。
import { useEffect, useRef, useState } from 'react';
import type { KeyboardEvent } from 'react';
import type { ToolInfo } from '../lib/api';
import { cn } from '../lib/cn';

interface Props {
  tools: ToolInfo[];
  value: string;
  onChange(id: string): void;
  onExternal?(): void; // 菜单底部次要项「在外部终端打开」（不传则不渲染该项）
  disabled?: boolean;
}

export default function ToolPicker({ tools, value, onChange, onExternal, disabled }: Props) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);

  const empty = tools.length === 0;
  const isDisabled = disabled === true || empty;
  const selected = tools.find((t) => t.ID === value) ?? null;

  // 展开期间监听 document 的 pointerdown（覆盖鼠标与触摸，且早于 click）：落在面板外就关闭
  useEffect(() => {
    if (!open) return;
    const onPointerDown = (e: PointerEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('pointerdown', onPointerDown);
    return () => document.removeEventListener('pointerdown', onPointerDown);
  }, [open]);

  // 被禁用（含工具列表清空）时收起，避免留下悬空面板
  useEffect(() => {
    if (isDisabled) setOpen(false);
  }, [isDisabled]);

  const close = (restoreFocus: boolean) => {
    setOpen(false);
    if (restoreFocus) triggerRef.current?.focus();
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== 'Escape' || !open) return;
    // 不回传给上层：全局 Escape（快速切换器等）不该被本地下拉的关闭动作吃掉
    e.stopPropagation();
    close(true);
  };

  return (
    <div ref={rootRef} className="relative inline-block" onKeyDown={handleKeyDown}>
      <button
        ref={triggerRef}
        type="button"
        className={cn(
          'flex h-7 max-w-40 items-center gap-1 rounded border border-border bg-card px-2 text-xs transition-colors',
          'hover:bg-muted disabled:cursor-not-allowed disabled:opacity-60',
        )}
        aria-haspopup="listbox"
        aria-expanded={open}
        disabled={isDisabled}
        onClick={() => setOpen((o) => !o)}
      >
        <span className="min-w-0 truncate">{empty ? '无可用工具' : (selected?.Name ?? '自动')}</span>
        {/* 下箭头：扁平单色细线，跟随文字颜色 */}
        <svg
          viewBox="0 0 12 12"
          aria-hidden="true"
          className="h-3 w-3 shrink-0 text-muted-foreground"
        >
          <path
            d="M2.5 4.5 6 8l3.5-3.5"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </svg>
      </button>

      {open && (
        <div
          role="listbox"
          aria-label="选择工具"
          className="absolute left-0 top-full z-20 mt-1 w-44 rounded border border-border bg-card py-0.5"
        >
          {/* 「自动」= 交给 Go 侧按该工作区最常用的工具挑（value 为空串） */}
          <button
            type="button"
            role="option"
            aria-selected={value === ''}
            className={cn(
              'flex w-full items-center gap-2 px-2 py-1 text-left text-xs transition-colors',
              value === '' ? 'bg-primary/10 text-primary' : 'hover:bg-muted',
            )}
            onClick={() => {
              setOpen(false);
              onChange('');
            }}
          >
            <span className="min-w-0 truncate">自动（该工作区最常用）</span>
          </button>
          {tools.map((t) => {
            const active = t.ID === value;
            return (
              <button
                key={t.ID}
                type="button"
                role="option"
                aria-selected={active}
                className={cn(
                  'flex w-full items-center gap-2 px-2 py-1 text-left text-xs transition-colors',
                  active ? 'bg-primary/10 text-primary' : 'hover:bg-muted',
                )}
                onClick={() => {
                  setOpen(false);
                  onChange(t.ID);
                }}
              >
                <span className="min-w-0 truncate">{t.Name}</span>
                {t.Version && (
                  <span className="ml-auto shrink-0 text-[10px] text-muted-foreground">
                    {t.Version}
                  </span>
                )}
              </button>
            );
          })}
          {onExternal && (
            <button
              type="button"
              className="mt-0.5 flex w-full items-center border-t border-border px-2 py-1 text-left text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
              onClick={() => {
                setOpen(false);
                onExternal();
              }}
            >
              在外部终端打开
            </button>
          )}
        </div>
      )}
    </div>
  );
}
