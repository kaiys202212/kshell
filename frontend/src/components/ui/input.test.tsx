// 通用输入框组件测试。
import { describe, expect, it } from 'vitest';
import { render } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import { Input } from './input';

describe('Input', () => {
  it('渲染 <input> 并带统一的边框 / 焦点环样式', () => {
    const { container } = render(<Input aria-label="测试输入" />);
    const input = container.querySelector('input');
    expect(input).toBeInTheDocument();
    expect(input?.className).toContain('focus-visible:ring-2');
    expect(input?.className).toContain('border-input');
    expect(input?.className).toContain('rounded-[3px]');
  });

  it('size="sm" 应用小尺寸高度', () => {
    const { container } = render(<Input size="sm" />);
    expect(container.querySelector('input')?.className).toContain('h-7');
  });

  it('className 透传合并且可覆盖冲突类', () => {
    const { container } = render(<Input className="w-full" />);
    const cls = container.querySelector('input')?.className ?? '';
    expect(cls).toContain('w-full');
    // twMerge 合并后仍保留基础样式
    expect(cls).toContain('border-input');
  });
});
