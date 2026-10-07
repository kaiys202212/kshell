// ResizeHandle 组件测试：拖动方向（左栏向右变宽 / 右栏向左变宽）、拖动期间 body 样式
// 与结束还原、键盘调整（±16px 与 Home/End）、双击恢复默认宽度、无障碍属性。
// 说明：jsdom 没有真实 pointer capture（Element 上无 setPointerCapture），组件内部做了
// 能力守卫，因此这里直接在元素上派发 pointermove 即可，事件流与真实捕获等价。
import '@testing-library/jest-dom/vitest';
import type { ComponentProps } from 'react';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import ResizeHandle from './ResizeHandle';
import { tt } from '../test/i18n';

afterEach(cleanup);

const LABEL = tt('ui.workspace.resize_sessions');

// 渲染受控手柄并取回 spy；父级负责 clamp，本组件只上报原始目标宽度
function setup(overrides: Partial<ComponentProps<typeof ResizeHandle>> = {}) {
  const onResize = vi.fn();
  render(
    <ResizeHandle
      width={300}
      onResize={onResize}
      side="left"
      label={LABEL}
      {...overrides}
    />,
  );
  return { onResize, handle: screen.getByRole('separator', { name: LABEL }) };
}

describe('ResizeHandle', () => {
  it('无障碍属性完整且键盘可达', () => {
    const { handle } = setup({ min: 200, max: 560 });
    expect(handle).toHaveAttribute('aria-orientation', 'vertical');
    expect(handle).toHaveAttribute('aria-valuenow', '300');
    expect(handle).toHaveAttribute('aria-valuemin', '200');
    expect(handle).toHaveAttribute('aria-valuemax', '560');
    expect(handle).toHaveAttribute('tabindex', '0');
  });

  it('左栏手柄向右拖：上报 起始宽度 + 位移', () => {
    const { onResize, handle } = setup({ width: 300, side: 'left' });

    fireEvent.pointerDown(handle, { pointerId: 1, button: 0, clientX: 100 });
    // 拖动中 body 允许越过拖拽文本选择（桌面应用语义）
    expect(document.body.style.cursor).toBe('col-resize');
    expect(document.body.style.userSelect).toBe('none');

    fireEvent.pointerMove(handle, { pointerId: 1, clientX: 140 });
    expect(onResize).toHaveBeenLastCalledWith(340);

    fireEvent.pointerMove(handle, { pointerId: 1, clientX: 160 });
    expect(onResize).toHaveBeenLastCalledWith(360);
  });

  it('右栏手柄向左拖：同样变宽（方向取反）', () => {
    const { onResize, handle } = setup({ width: 300, side: 'right' });

    fireEvent.pointerDown(handle, { pointerId: 1, button: 0, clientX: 400 });
    fireEvent.pointerMove(handle, { pointerId: 1, clientX: 360 });
    expect(onResize).toHaveBeenLastCalledWith(340);
  });

  it('拖动结束（pointerup）还原 body 样式，且后续 move 不再回调', () => {
    const { onResize, handle } = setup({ width: 300, side: 'left' });

    fireEvent.pointerDown(handle, { pointerId: 1, button: 0, clientX: 100 });
    fireEvent.pointerMove(handle, { pointerId: 1, clientX: 120 });
    expect(onResize).toHaveBeenCalledTimes(1);

    fireEvent.pointerUp(handle, { pointerId: 1, clientX: 120 });
    expect(document.body.style.cursor).toBe('');
    expect(document.body.style.userSelect).toBe('');

    onResize.mockClear();
    fireEvent.pointerMove(handle, { pointerId: 1, clientX: 200 });
    expect(onResize).not.toHaveBeenCalled();
  });

  it('pointercancel 同样结束拖动并还原 body 样式', () => {
    const { handle } = setup({ width: 300, side: 'left' });

    fireEvent.pointerDown(handle, { pointerId: 2, button: 0, clientX: 100 });
    expect(document.body.style.cursor).toBe('col-resize');

    fireEvent.pointerCancel(handle, { pointerId: 2 });
    expect(document.body.style.cursor).toBe('');
    expect(document.body.style.userSelect).toBe('');
  });

  it('其它指针（pointerId 不匹配）的移动不参与拖动', () => {
    const { onResize, handle } = setup({ width: 300, side: 'left' });

    fireEvent.pointerDown(handle, { pointerId: 1, button: 0, clientX: 100 });
    fireEvent.pointerMove(handle, { pointerId: 9, clientX: 300 });
    expect(onResize).not.toHaveBeenCalled();
  });

  it('键盘 ArrowRight/ArrowLeft 每次调整 16px（左栏：右=变宽）', () => {
    const { onResize, handle } = setup({ width: 300, side: 'left' });

    fireEvent.keyDown(handle, { key: 'ArrowRight' });
    expect(onResize).toHaveBeenLastCalledWith(316);

    fireEvent.keyDown(handle, { key: 'ArrowLeft' });
    expect(onResize).toHaveBeenLastCalledWith(284);
  });

  it('键盘方向语义与拖动一致（右栏：ArrowLeft 变宽）', () => {
    const { onResize, handle } = setup({ width: 300, side: 'right' });

    fireEvent.keyDown(handle, { key: 'ArrowLeft' });
    expect(onResize).toHaveBeenLastCalledWith(316);

    fireEvent.keyDown(handle, { key: 'ArrowRight' });
    expect(onResize).toHaveBeenLastCalledWith(284);
  });

  it('键盘 Home/End 到 min/max，越界方向按 min/max 收敛', () => {
    const { onResize, handle } = setup({ width: 300, side: 'left', min: 150, max: 400 });

    fireEvent.keyDown(handle, { key: 'Home' });
    expect(onResize).toHaveBeenLastCalledWith(150);

    fireEvent.keyDown(handle, { key: 'End' });
    expect(onResize).toHaveBeenLastCalledWith(400);
  });

  it('已在最小值时继续按变小方向不越界', () => {
    const { onResize, handle } = setup({ width: 200, side: 'left', min: 200, max: 560 });

    fireEvent.keyDown(handle, { key: 'ArrowLeft' });
    expect(onResize).toHaveBeenLastCalledWith(200);
  });

  it('双击回调 defaultWidth', () => {
    const { onResize, handle } = setup({ defaultWidth: 288 });

    fireEvent.doubleClick(handle);
    expect(onResize).toHaveBeenCalledTimes(1);
    expect(onResize).toHaveBeenCalledWith(288);
  });

  it('未传 defaultWidth 时双击无效果', () => {
    const { onResize, handle } = setup();

    fireEvent.doubleClick(handle);
    expect(onResize).not.toHaveBeenCalled();
  });
});
