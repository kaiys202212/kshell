import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useAppStore } from './store';
import {
  TERMINAL_SILENCE_MS,
  bumpTerminalBusy,
  clearTerminalBusy,
  suppressTerminalBusy,
} from './terminalBusy';

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

  it('suppress 期内非 force 的 bump 不把空闲终端标 busy', () => {
    suppressTerminalBusy('t1', 500);
    bumpTerminalBusy('t1');
    expect(useAppStore.getState().terminalBusy.t1).toBeUndefined();
  });

  it('suppress 期内 force bump（用户输入）仍标 busy', () => {
    suppressTerminalBusy('t1', 500);
    bumpTerminalBusy('t1', { force: true });
    expect(useAppStore.getState().terminalBusy.t1).toBe(true);
  });

  it('已 busy 时 suppress 不阻断 bump 续命', () => {
    bumpTerminalBusy('t1');
    suppressTerminalBusy('t1', 500);
    vi.advanceTimersByTime(TERMINAL_SILENCE_MS - 100);
    bumpTerminalBusy('t1');
    vi.advanceTimersByTime(200);
    expect(useAppStore.getState().terminalBusy.t1).toBe(true);
  });

  it('suppress 过期后 bump 恢复置 busy', () => {
    suppressTerminalBusy('t1', 500);
    vi.advanceTimersByTime(500);
    bumpTerminalBusy('t1');
    expect(useAppStore.getState().terminalBusy.t1).toBe(true);
  });
});
