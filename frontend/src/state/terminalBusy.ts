import { useAppStore } from './store';

export const TERMINAL_SILENCE_MS = 2500;
/** 点开页签 / resize 后的 TUI 重绘窗口：这段里的输出不把空闲终端标成执行中。 */
export const TERMINAL_RESIZE_GRACE_MS = 800;

const timers = new Map<string, ReturnType<typeof setTimeout>>();
const suppressedUntil = new Map<string, number>();

export function suppressTerminalBusy(id: string, ms = TERMINAL_RESIZE_GRACE_MS): void {
  if (!id) return;
  suppressedUntil.set(id, Date.now() + ms);
}

export function bumpTerminalBusy(id: string, opts?: { force?: boolean }): void {
  if (!id) return;
  const idle = !useAppStore.getState().terminalBusy[id];
  const until = suppressedUntil.get(id) ?? 0;
  if (!opts?.force && idle && Date.now() < until) return;
  useAppStore.getState().setTerminalBusy(id, true);
  const prev = timers.get(id);
  if (prev) clearTimeout(prev);
  timers.set(
    id,
    setTimeout(() => {
      timers.delete(id);
      useAppStore.getState().setTerminalBusy(id, false);
    }, TERMINAL_SILENCE_MS),
  );
}

export function clearTerminalBusy(id: string): void {
  const prev = timers.get(id);
  if (prev) clearTimeout(prev);
  timers.delete(id);
  suppressedUntil.delete(id);
  useAppStore.getState().setTerminalBusy(id, false);
}
