import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useAppStore } from './store';
import { TERMINAL_SILENCE_MS, bumpTerminalBusy, clearTerminalBusy } from './terminalBusy';

describe('terminalBusy', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    useAppStore.setState({ terminalBusy: {} });
  });
  afterEach(() => {
    clearTerminalBusy('t1');
    vi.useRealTimers();
  });

  it('bump 置 busy；静默后清除', () => {
    bumpTerminalBusy('t1');
    expect(useAppStore.getState().terminalBusy.t1).toBe(true);
    vi.advanceTimersByTime(TERMINAL_SILENCE_MS);
    expect(useAppStore.getState().terminalBusy.t1).toBeUndefined();
  });

  it('连续 bump 重置计时', () => {
    bumpTerminalBusy('t1');
    vi.advanceTimersByTime(TERMINAL_SILENCE_MS - 100);
    bumpTerminalBusy('t1');
    vi.advanceTimersByTime(200);
    expect(useAppStore.getState().terminalBusy.t1).toBe(true);
    vi.advanceTimersByTime(TERMINAL_SILENCE_MS);
    expect(useAppStore.getState().terminalBusy.t1).toBeUndefined();
  });
});
