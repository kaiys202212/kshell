// 文件拖放共享常量与工具：
// - DRAG_MIME 携带绝对路径（拖入终端/聊天时插入用）
// - REL_MIME 携带 '/' 分隔相对路径（树内 drop 定位 MoveEntry 的 src、拖入终端插相对路径用）
// - DRAG_ROOT_MIME 携带拖拽源工作区根绝对路径（终端 drop 判断相对路径是否可信用）
// 三者在同一个 dragstart 里一起写入 dataTransfer。
import { sameWorkspacePath } from './workspacePath';

export const DRAG_MIME = 'application/x-kshell-path';
export const REL_MIME = 'application/x-kshell-relpath';
export const DRAG_ROOT_MIME = 'application/x-kshell-dragroot';

// quotePathForShell 路径含空白时包双引号，避免 shell 端被拆词
export function quotePathForShell(path: string): string {
  return /\s/.test(path) ? `"${path}"` : path;
}

// pickTerminalDropPath 决定拖入终端时插入的路径：
// 拖拽源工作区与终端工作区一致才信任相对路径（agent 会话 cwd = 工作区根）；
// 跨工作区拖拽或数据缺失时回退绝对路径，避免插入看似有效实则指错根的相对路径。
export function pickTerminalDropPath(
  getData: (mime: string) => string,
  termWorkspace: string,
): string {
  const rel = getData(REL_MIME);
  const root = getData(DRAG_ROOT_MIME);
  if (rel && root && sameWorkspacePath(root, termWorkspace)) return rel;
  return getData(DRAG_MIME);
}
