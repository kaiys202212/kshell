// 通用弹层组件测试：验证 Radix Dialog 封装保留默认行为（渲染/Esc 关闭），
// 且遮罩/内容带统一遮罩色与进出动画。
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Dialog } from './dialog';

// 项目未开 vitest globals，RTL 的自动 cleanup 不生效；
// Radix Portal 挂在 document.body 上，跨测试残留会污染 document 级查询，须手动清理
afterEach(cleanup);

describe('Dialog', () => {
  it('open 时经 Portal 渲染内容', () => {
    render(
      <Dialog open onOpenChange={() => {}}>
        {/* Radix 要求 Content 内有 Title，真实调用点均自带，这里同步给出 */}
        <DialogPrimitive.Title className="sr-only">标题</DialogPrimitive.Title>
        <p>内容可见</p>
      </Dialog>,
    );
    expect(screen.getByText('内容可见')).toBeInTheDocument();
  });

  it('Escape 触发 onOpenChange(false)（Radix 默认行为未被封装破坏）', () => {
    const onOpenChange = vi.fn();
    render(
      <Dialog open onOpenChange={onOpenChange}>
        <DialogPrimitive.Title className="sr-only">标题</DialogPrimitive.Title>
        <p>内容</p>
      </Dialog>,
    );
    fireEvent.keyDown(document.body, { key: 'Escape' });
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it('遮罩用统一遮罩色，内容引用 kshell-pop-in 动画（遮罩 fade-in）', () => {
    render(
      <Dialog open onOpenChange={() => {}}>
        <DialogPrimitive.Title className="sr-only">标题</DialogPrimitive.Title>
        <p>内容</p>
      </Dialog>,
    );
    const overlay = document.querySelector('.fixed.inset-0');
    expect(overlay?.className).toContain('bg-[var(--overlay)]');
    const content = screen.getByRole('dialog');
    expect(content.className).toContain('bg-card');
    expect(content.style.animation).toContain('kshell-pop-in');
    expect(overlay?.getAttribute('style')).toContain('kshell-fade-in');
  });

  it('未 open 时不渲染内容', () => {
    render(
      <Dialog open={false} onOpenChange={() => {}}>
        <p>隐藏内容</p>
      </Dialog>,
    );
    expect(screen.queryByText('隐藏内容')).not.toBeInTheDocument();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });
});
