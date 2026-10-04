import { quotePathForShell } from './dragPath';

export interface ClipboardPaste {
  Text: string;
  Path: string;
}

// composeTerminalPaste 把原生剪贴板快照编成写入 PTY 的文本：
// 有文件/图片路径则插入路径（供 cursor-agent 等按路径挂图），否则插入纯文本。
export function composeTerminalPaste(clip: ClipboardPaste | null | undefined): string {
  if (!clip) return '';
  const path = (clip.Path ?? '').trim();
  if (path) return quotePathForShell(path);
  return clip.Text ?? '';
}

export function isPasteKey(ev: { key: string; ctrlKey: boolean; metaKey: boolean; shiftKey: boolean; altKey: boolean }): boolean {
  if (ev.altKey) return false;
  if ((ev.ctrlKey || ev.metaKey) && !ev.shiftKey && (ev.key === 'v' || ev.key === 'V')) return true;
  if (ev.shiftKey && !ev.ctrlKey && !ev.metaKey && ev.key === 'Insert') return true;
  return false;
}
