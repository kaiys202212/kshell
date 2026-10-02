// 工具色点组件测试。
import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import { ToolDot } from './tool-dot';

// 取内部色点元素（aria-hidden 的圆点）。
function dotOf(container: HTMLElement): HTMLElement {
  return container.querySelector('span[aria-hidden="true"]') as HTMLElement;
}

describe('ToolDot', () => {
  it('渲染工具名与色点', () => {
    const { container } = render(<ToolDot toolID="claude" />);
    expect(screen.getByText('Claude')).toBeInTheDocument();
    expect(dotOf(container)).not.toBeNull();
  });

  it('色点应用已知工具的固定色相', () => {
    const { container } = render(<ToolDot toolID="claude" />);
    // jsdom 会把十六进制归一化为 rgb()
    expect(dotOf(container).style.background).toBe('rgb(217, 119, 87)');
  });

  it('未知工具回退展示原始 ToolID 与中性色相', () => {
    const { container } = render(<ToolDot toolID="aider" />);
    expect(screen.getByText('aider')).toBeInTheDocument();
    expect(dotOf(container).style.background).toBe('var(--muted-foreground)');
  });
});
