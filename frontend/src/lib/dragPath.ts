// 文件拖放共享常量与工具：
// - DRAG_MIME 携带绝对路径（拖入终端/聊天时插入用）
// - REL_MIME 携带 '/' 分隔相对路径（树内 drop 定位 MoveEntry 的 src 用）
// 两者在同一个 dragstart 里一起写入 dataTransfer。
export const DRAG_MIME = 'application/x-kshell-path';
export const REL_MIME = 'application/x-kshell-relpath';

// quotePathForShell 路径含空白时包双引号，避免 shell 端被拆词
export function quotePathForShell(path: string): string {
  return /\s/.test(path) ? `"${path}"` : path;
}
