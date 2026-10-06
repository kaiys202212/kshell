// 工具徽标：已知工具渲染官方风格图标，未知工具用首字母方块。
import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import { ToolDot } from './tool-dot';

afterEach(cleanup);

describe('ToolDot', () => {
  it('渲染工具名与对应工具图标', () => {
    render(<ToolDot toolID="claude" />);
    expect(screen.getByText('Claude')).toBeInTheDocument();
    const icon = screen.getByTestId('tool-icon');
    expect(icon).toHaveAttribute('data-tool', 'claude');
    expect(icon.querySelector('svg')).not.toBeNull();
  });

  it('不渲染旧的色点圆标', () => {
    const { container } = render(<ToolDot toolID="claude" />);
    expect(container.querySelector('span.h-1\\.5.w-1\\.5.rounded-full')).toBeNull();
  });

  it.each(['codebuddy', 'codex', 'gemini', 'opencode', 'cursor'] as const)(
    '已知工具 %s 有独立图标',
    (id) => {
      render(<ToolDot toolID={id} showLabel={false} />);
      expect(screen.getByTestId('tool-icon')).toHaveAttribute('data-tool', id);
      expect(screen.getByTestId('tool-icon').querySelector('svg')).not.toBeNull();
    },
  );

  it('未知工具回退展示原始 ToolID 与首字母方块', () => {
    render(<ToolDot toolID="aider" />);
    expect(screen.getByText('aider')).toBeInTheDocument();
    const icon = screen.getByTestId('tool-icon');
    expect(icon).toHaveAttribute('data-tool', 'other');
    expect(icon).toHaveTextContent('A');
  });

  it('showLabel=false 时不显示工具名文字', () => {
    render(<ToolDot toolID="opencode" showLabel={false} />);
    expect(screen.queryByText('OpenCode')).not.toBeInTheDocument();
    expect(screen.getByTestId('tool-icon')).toHaveAttribute('data-tool', 'opencode');
  });
});
