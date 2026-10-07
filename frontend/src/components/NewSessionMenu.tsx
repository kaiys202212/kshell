// 「新建会话」下拉菜单：按钮本身就是入口，点开直接列 agent（含「自动」），选中即在该工作区开内嵌终端。
// 取代了旧版「新建会话按钮 + 右侧独立工具下拉框」两个控件并排的布局。
// 手写实现，不引入 Radix dropdown-menu 原语；tools 由父级过滤（只传「已安装且有可执行文件」的工具）。
// value 是上次使用的工具（'' = 自动），用于在菜单里打勾；工具列表变化后找不到对应项也不报错。
// 菜单里选中的动作语义是「开始一个新会话」，因此用 menuitemradio 标记当前项。
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { KeyboardEvent } from 'react';
import type { ToolInfo } from '../lib/api';
import { cn } from '../lib/cn';
import { Button } from './ui/button';

interface Props {
  tools: ToolInfo[];
  value: string;
  /** 记住选择（全局持久化） */
  onChange(id: string): void;
  /** 立即在该工作区新建会话（走默认会话模式） */
  onSelect(id: string): void;
  /** 启动中：临时禁用，避免连点起多个进程 */
  disabled?: boolean;
}

// 菜单项统一样式：选中态主色淡底 + 主色文字
const itemClass = (selected: boolean) =>
  cn(
    'flex w-full items-center gap-1.5 px-2 py-1 text-left text-xs transition-colors',
    selected ? 'bg-primary/10 text-primary' : 'hover:bg-muted',
  );

export default function NewSessionMenu({
  tools,
  value,
  onChange,
  onSelect,
  disabled,
}: Props) {
  const { t: tr } = useTranslation();
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

  // 被禁用（含工具列表清空、启动中）时收起，避免留下悬空面板
  useEffect(() => {
    if (isDisabled) setOpen(false);
  }, [isDisabled]);

  const handleKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== 'Escape' || !open) return;
    // 不回传给上层：全局 Escape（快速切换器等）不该被本地下拉的关闭动作吃掉
    e.stopPropagation();
    setOpen(false);
    triggerRef.current?.focus();
  };

  // 选择一项：记住选择（onChange，持久化）并立即新建会话
  const pick = (id: string) => {
    setOpen(false);
    onChange(id);
    onSelect(id);
  };

  return (
    <div ref={rootRef} className="relative" onKeyDown={handleKeyDown}>
      {/* 触发按钮统一走 Button（default variant）；补 w-full/text-xs 保持原占位与字号 */}
      <Button
        ref={triggerRef}
        type="button"
        className="w-full text-xs"
        title={selected ? tr('ui.new_session.title_with_last', { name: selected.Name }) : tr('ui.new_session.title')}
        aria-haspopup="menu"
        aria-expanded={open}
        disabled={isDisabled}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="min-w-0 truncate">
          {empty ? tr('ui.new_session.no_tools') : disabled ? tr('ui.new_session.starting') : tr('ui.new_session.title')}
        </span>
        {/* 下箭头：扁平单色细线，跟随文字颜色 */}
        <svg viewBox="0 0 12 12" aria-hidden="true" className="h-3 w-3 shrink-0 opacity-80">
          <path
            d="M2.5 4.5 6 8l3.5-3.5"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </svg>
      </Button>

      {open && (
        <div
          role="menu"
          aria-label={tr('ui.new_session.menu_aria')}
          className="absolute left-0 top-full z-20 mt-1 w-full min-w-40 rounded border border-border bg-card py-0.5 shadow-sm"
        >
          {/* 「自动」= 交给 Go 侧按该工作区最常用的工具挑（value 为空串） */}
          <button
            type="button"
            role="menuitemradio"
            aria-checked={value === ''}
            className={itemClass(value === '')}
            onClick={() => pick('')}
          >
            <CheckGlyph show={value === ''} />
            <span className="min-w-0 truncate">{tr('ui.new_session.auto')}</span>
          </button>
          {tools.map((t) => {
            const active = t.ID === value;
            return (
              <button
                key={t.ID}
                type="button"
                role="menuitemradio"
                aria-checked={active}
                className={itemClass(active)}
                onClick={() => pick(t.ID)}
              >
                <CheckGlyph show={active} />
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

// CheckGlyph 占位恒定的宽度，保证菜单项文字左对齐不被「有无勾」推来推去。
function CheckGlyph({ show }: { show: boolean }) {
  return (
    <span aria-hidden="true" className="w-3 shrink-0 text-center">
      {show ? '✓' : ''}
    </span>
  );
}
