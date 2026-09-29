// 紧凑「工具选择」下拉（新建会话用）：手写实现，不引入 Radix dropdown-menu 原语。
// tools 由父级过滤（只会传「已安装且有可执行文件」的工具，来自 GetTools()）；
// value='' 表示「自动（该工作区最常用）」，触发器此时显示「自动」，工具列表变化后找不到
// 对应项也回退「自动」；tools 为空显示「无可用工具」并禁用（不崩）。
// 展开状态可受控（open/onOpenChange）：父级「新建会话」按钮把它点开，选中某项后由
// onSelect 直接把该 agent 的会话跑起来。
import { useEffect, useRef, useState } from 'react';
import type { KeyboardEvent } from 'react';
import type { ToolInfo } from '../lib/api';
import { cn } from '../lib/cn';

interface Props {
  tools: ToolInfo[];
  value: string;
  onChange(id: string): void;
  /** 选中任一工具（含「自动」）后额外回调：新建会话用它直接启动终端 */
  onSelect?(id: string): void;
  /** 受控展开：不传时组件自管展开态 */
  open?: boolean;
  onOpenChange?(open: boolean): void;
  disabled?: boolean;
}

export default function ToolPicker({
  tools,
  value,
  onChange,
  onSelect,
  open,
  onOpenChange,
  disabled,
}: Props) {
  const [innerOpen, setInnerOpen] = useState(false);
  const controlled = open !== undefined;
  const isOpen = controlled ? open : innerOpen;
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);

  const empty = tools.length === 0;
  const isDisabled = disabled === true || empty;
  const selected = tools.find((t) => t.ID === value) ?? null;

  // 受控/非受控统一的展开写入
  const setOpen = (next: boolean) => {
    if (controlled) onOpenChange?.(next);
    else setInnerOpen(next);
  };

  // 展开期间监听 document 的 pointerdown（覆盖鼠标与触摸，且早于 click）：落在面板外就关闭
  useEffect(() => {
    if (!isOpen) return;
    const onPointerDown = (e: PointerEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('pointerdown', onPointerDown);
    return () => document.removeEventListener('pointerdown', onPointerDown);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isOpen, controlled, onOpenChange]);

  // 被禁用（含工具列表清空）时收起，避免留下悬空面板
  useEffect(() => {
    if (isDisabled) setOpen(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isDisabled, controlled, onOpenChange]);

  const close = (restoreFocus: boolean) => {
    setOpen(false);
    if (restoreFocus) triggerRef.current?.focus();
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== 'Escape' || !isOpen) return;
    // 不回传给上层：全局 Escape（快速切换器等）不该被本地下拉的关闭动作吃掉
    e.stopPropagation();
    close(true);
  };

  // 选择一项：记住选择（onChange，持久化）并交给父级（onSelect，新建会话立即启动）
  const pick = (id: string) => {
    setOpen(false);
    onChange(id);
    onSelect?.(id);
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
        aria-expanded={isOpen}
        disabled={isDisabled}
        onClick={() => setOpen(!isOpen)}
      >
        <span className="min-w-0 truncate">
          {empty ? '无可用工具' : `工具：${selected?.Name ?? '自动'}`}
        </span>
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

      {isOpen && (
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
            onClick={() => pick('')}
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
                onClick={() => pick(t.ID)}
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
        </div>
      )}
    </div>
  );
}
