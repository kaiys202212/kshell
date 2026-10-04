import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import AgentActivityIcon from './AgentActivityIcon';

describe('AgentActivityIcon', () => {
  it('idle 不渲染', () => {
    const { container } = render(<AgentActivityIcon activity="idle" />);
    expect(container.firstChild).toBeNull();
  });

  it('running / awaiting / completed 带对应 aria-label', () => {
    const { rerender } = render(<AgentActivityIcon activity="running" />);
    expect(screen.getByLabelText('执行中')).toBeInTheDocument();
    rerender(<AgentActivityIcon activity="awaiting" />);
    expect(screen.getByLabelText('待用户确认')).toBeInTheDocument();
    rerender(<AgentActivityIcon activity="completed" />);
    expect(screen.getByLabelText('运行完成')).toBeInTheDocument();
  });
});
