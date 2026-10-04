import { describe, expect, it, vi } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import AgentActivityIcon from './AgentActivityIcon';

describe('AgentActivityIcon', () => {
  it('idle 不渲染', () => {
    const { container } = render(<AgentActivityIcon activity="idle" />);
    expect(container.firstChild).toBeNull();
  });

  it('running / awaiting / waiting 带对应 aria-label', () => {
    vi.useFakeTimers();
    const { rerender } = render(<AgentActivityIcon activity="running" />);
    expect(screen.getByLabelText('执行中')).toBeInTheDocument();
    const spin = screen.getByLabelText('执行中').querySelector('[data-spin]') as HTMLElement;
    expect(spin).toBeTruthy();
    expect(spin.style.transform).toBe('rotate(0deg)');
    act(() => {
      vi.advanceTimersByTime(80);
    });
    expect(spin.style.transform).toBe('rotate(45deg)');
    rerender(<AgentActivityIcon activity="awaiting" />);
    expect(screen.getByLabelText('待用户确认')).toBeInTheDocument();
    rerender(<AgentActivityIcon activity="waiting" />);
    expect(screen.getByLabelText('等待用户')).toBeInTheDocument();
    expect(screen.getByLabelText('等待用户').querySelector('[data-spin]')).toBeNull();
    vi.useRealTimers();
  });
});
