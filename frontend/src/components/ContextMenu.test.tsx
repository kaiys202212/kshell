// ContextMenu 组件测试：portal 渲染、Esc / 点击外部关闭、
// 菜单项 onPointerDown 触发 onSelect 且不冒泡到 window 的关闭监听。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import ContextMenu from './ContextMenu';

afterEach(cleanup);

const items = [
  { label: '动作一', onSelect: vi.fn() },
  { label: '动作二', onSelect: vi.fn(), danger: true },
];

describe('ContextMenu', () => {
  it('渲染菜单项（portal 到 body），点项触发 onSelect 且菜单不冒泡关闭', () => {
    const onClose = vi.fn();
    render(<ContextMenu x={10} y={20} items={items} onClose={onClose} />);

    const menu = screen.getByRole('menu');
    expect(menu.parentElement).toBe(document.body);
    expect(screen.getByRole('menuitem', { name: '动作二' }).className).toContain(
      'text-destructive',
    );

    fireEvent.pointerDown(screen.getByRole('menuitem', { name: '动作一' }));
    expect(items[0].onSelect).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled(); // stopPropagation 生效
  });

  it('Esc 与 window pointerdown 关闭', () => {
    const onClose = vi.fn();
    render(<ContextMenu x={10} y={20} items={items} onClose={onClose} />);

    fireEvent.keyDown(window, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.pointerDown(window);
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it('坐标贴边收敛不溢出窗口（含下限兜底）', () => {
    render(<ContextMenu x={window.innerWidth + 100} y={50} items={items} onClose={() => {}} />);
    const menu = screen.getByRole('menu');
    expect((menu as HTMLElement).style.left).toBe(`${window.innerWidth - 160}px`);

    // 负坐标/极端小窗口：Math.max(0, ...) 保证不出左、上边界
    cleanup();
    render(<ContextMenu x={-100} y={-100} items={items} onClose={() => {}} />);
    const clamped = screen.getByRole('menu') as HTMLElement;
    expect(clamped.style.left).toBe('0px');
    expect(clamped.style.top).toBe('0px');
  });

  it('使用不透明 bg-card 而非未定义的 bg-popover', () => {
    const { container } = render(
      <ContextMenu x={10} y={10} items={[{ label: 'x', onSelect: vi.fn() }]} onClose={vi.fn()} />,
    );
    const menu = container.ownerDocument.body.querySelector('[role="menu"]');
    expect(menu?.className).toContain('bg-card');
    expect(menu?.className).not.toContain('bg-popover');
  });
});
