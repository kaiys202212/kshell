import { describe, expect, it, vi } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import AgentActivityIcon from './AgentActivityIcon';
import { tt } from '../test/i18n';

describe('AgentActivityIcon', () => {
  it('idle 不渲染', () => {
    const { container } = render(<AgentActivityIcon activity="idle" />);
    expect(container.firstChild).toBeNull();
  });

  it('running / awaiting / waiting 带对应 aria-label', () => {
    vi.useFakeTimers();
    const { rerender } = render(<AgentActivityIcon activity="running" />);
    expect(screen.getByLabelText(tt('ui.agent_activity.running'))).toBeInTheDocument();
    const spin = screen.getByLabelText(tt('ui.agent_activity.running')).querySelector('[data-spin]') as HTMLElement;
    expect(spin).toBeTruthy();
    expect(spin.style.transform).toBe('rotate(0deg)');
    act(() => {
      vi.advanceTimersByTime(80);
    });
    expect(spin.style.transform).toBe('rotate(45deg)');
    rerender(<AgentActivityIcon activity="awaiting" />);
    expect(screen.getByLabelText(tt('ui.agent_activity.awaiting'))).toBeInTheDocument();
    rerender(<AgentActivityIcon activity="waiting" />);
    expect(screen.getByLabelText(tt('ui.agent_activity.waiting'))).toBeInTheDocument();
    expect(screen.getByLabelText(tt('ui.agent_activity.waiting')).querySelector('[data-spin]')).toBeNull();
    vi.useRealTimers();
  });
});
