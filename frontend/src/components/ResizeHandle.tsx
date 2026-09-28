// 三栏宽度拖动条（工作区页签左右两栏）：受控组件，只上报原始目标宽度，
// clamp 由父级负责（store.setLayout → clampLayout）——只有键盘调整在组件内按 min/max 收敛，
// 因为 Home/End 的语义就是「到 min/max」，与 aria-valuemin/valuemax 保持一致。
//
// 扁平化视觉：4px 竖向细条，默认透明，hover / 拖动中走主色块；无阴影、无抓取圆点。
// 拖动期间给 body 加 col-resize 光标并禁用选中（离开手柄后事件由 pointer capture 兜住）。
import { useEffect, useRef, useState } from 'react';
import type { KeyboardEvent, PointerEvent as ReactPointerEvent } from 'react';
import { cn } from '../lib/cn';
import { LAYOUT_MAX, LAYOUT_MIN } from '../state/store';

// 键盘单步调整量：16px 约两字符宽，与常见桌面应用的列宽拖动手柄一致
const KEYBOARD_STEP = 16;

interface Props {
  width: number; // 当前宽度（受控，px）
  onResize(width: number): void; // 拖动中持续回调原始目标宽度（父级负责 clamp）
  side: 'left' | 'right'; // left=左栏手柄（向右拖更宽）；right=右栏手柄（向左拖更宽）
  label: string; // aria-label，例如「调整会话列表宽度」
  min?: number; // 默认 200
  max?: number; // 默认 560
  defaultWidth?: number; // 双击恢复的目标宽度；缺省则双击无效果
}

// 一次拖动会话：pointerId 用于忽略其它指针，prev* 用于结束后精确还原 body 原样式
interface DragSession {
  pointerId: number;
  startX: number;
  startWidth: number;
  prevCursor: string;
  prevUserSelect: string;
}

export default function ResizeHandle({
  width,
  onResize,
  side,
  label,
  min = LAYOUT_MIN,
  max = LAYOUT_MAX,
  defaultWidth,
}: Props) {
  const [dragging, setDragging] = useState(false);
  const dragRef = useRef<DragSession | null>(null);

  // 还原成进入拖动前的值（不假设 body 原本为空）
  const restoreBodyStyle = (session: DragSession) => {
    document.body.style.cursor = session.prevCursor;
    document.body.style.userSelect = session.prevUserSelect;
  };

  // 兜底：拖动中组件被卸载（切/关页签）时必须还原 body 样式，否则整个应用留着 col-resize
  useEffect(
    () => () => {
      const session = dragRef.current;
      if (!session) return;
      dragRef.current = null;
      restoreBodyStyle(session);
    },
    [],
  );

  const handlePointerDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return; // 只认主键：右键/中键不进入拖动
    // jsdom 与部分 WebView 没有 pointer capture：能力守卫，缺失时退化为普通事件流
    e.currentTarget.setPointerCapture?.(e.pointerId);
    dragRef.current = {
      pointerId: e.pointerId,
      startX: e.clientX,
      startWidth: width,
      prevCursor: document.body.style.cursor,
      prevUserSelect: document.body.style.userSelect,
    };
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
    setDragging(true);
  };

  const handlePointerMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    const session = dragRef.current;
    if (!session || session.pointerId !== e.pointerId) return;
    const delta = e.clientX - session.startX;
    if (delta === 0) return; // 原位抖动不产生回调
    // 右栏手柄向左拖才是变宽，方向取反
    onResize(session.startWidth + (side === 'left' ? delta : -delta));
  };

  const endDrag = (e: ReactPointerEvent<HTMLDivElement>) => {
    const session = dragRef.current;
    if (!session || session.pointerId !== e.pointerId) return;
    dragRef.current = null;
    const el = e.currentTarget;
    try {
      if (el.hasPointerCapture?.(e.pointerId)) el.releasePointerCapture?.(e.pointerId);
    } finally {
      // 释放捕获失败也要还原 body 样式与拖动态，不能把 col-resize 光标留在整个应用上
      restoreBodyStyle(session);
      setDragging(false);
    }
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    // 方向语义与拖动一致：left 手柄 ArrowRight 变宽；right 手柄 ArrowLeft 变宽
    const widen = side === 'left' ? 'ArrowRight' : 'ArrowLeft';
    const narrow = side === 'left' ? 'ArrowLeft' : 'ArrowRight';
    let next: number | null = null;
    if (e.key === widen) next = width + KEYBOARD_STEP;
    else if (e.key === narrow) next = width - KEYBOARD_STEP;
    else if (e.key === 'Home') next = min;
    else if (e.key === 'End') next = max;
    if (next === null) return;
    e.preventDefault();
    onResize(Math.min(max, Math.max(min, next)));
  };

  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label={label}
      aria-valuenow={width}
      aria-valuemin={min}
      aria-valuemax={max}
      tabIndex={0}
      className={cn(
        'h-full w-1 shrink-0 cursor-col-resize touch-none bg-transparent outline-none transition-colors',
        'hover:bg-primary focus-visible:bg-primary',
        dragging && 'bg-primary',
      )}
      onPointerDown={handlePointerDown}
      onPointerMove={handlePointerMove}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
      onDoubleClick={() => {
        if (defaultWidth !== undefined) onResize(defaultWidth);
      }}
      onKeyDown={handleKeyDown}
    />
  );
}
