import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import AgentActivityIcon from './AgentActivityIcon';

describe('AgentActivityIcon', () => {
  it('idle 不渲染', () => {
    const { container } = render(<AgentActivityIcon activity="idle" />);
    expect(container.firstChild).toBeNull();
  });

  it('running / awaiting / waiting 带对应 aria-label', () => {
    const { rerender } = render(<AgentActivityIcon activity="running" />);
    expect(screen.getByLabelText('执行中')).toBeInTheDocument();
    expect(screen.getByLabelText('执行中').querySelector('.animate-spin')).toBeTruthy();
    rerender(<AgentActivityIcon activity="awaiting" />);
    expect(screen.getByLabelText('待用户确认')).toBeInTheDocument();
    rerender(<AgentActivityIcon activity="waiting" />);
    expect(screen.getByLabelText('等待用户')).toBeInTheDocument();
    expect(screen.getByLabelText('等待用户').querySelector('.animate-spin')).toBeNull();
  });
});
