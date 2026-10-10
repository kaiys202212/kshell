// 全局渲染错误边界测试：任一子树渲染抛错时显示可恢复错误页，而非卸载整棵树（黑屏诱因）。
import '@testing-library/jest-dom/vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ErrorBoundary } from './error-boundary';
import { tt } from '../../test/i18n';

function Boom(): never {
  throw new Error('boom-render');
}

afterEach(cleanup);

describe('ErrorBoundary', () => {
  it('子组件渲染抛错时显示错误标题、原始错误与重载按钮', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    render(
      <ErrorBoundary>
        <Boom />
      </ErrorBoundary>,
    );
    expect(screen.getByText(tt('ui.error_boundary.title'))).toBeInTheDocument();
    expect(screen.getByText(/boom-render/)).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: tt('ui.error_boundary.reload') }),
    ).toBeInTheDocument();
    spy.mockRestore();
  });

  it('子组件正常时原样渲染', () => {
    render(
      <ErrorBoundary>
        <p>正常子树</p>
      </ErrorBoundary>,
    );
    expect(screen.getByText('正常子树')).toBeInTheDocument();
  });
});
