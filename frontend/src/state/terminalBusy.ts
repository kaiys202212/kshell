import { useAppStore } from './store';

export const TERMINAL_SILENCE_MS = 2500;

const timers = new Map<string, ReturnType<typeof setTimeout>>();

export function bumpTerminalBusy(id: string): void {
  if (!id) return;
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
  useAppStore.getState().setTerminalBusy(id, false);
}
