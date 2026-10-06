/** 提交图分支筛选的本地记忆（按工作区 + 仓库相对路径）。 */
const PREFIX = 'kshell-git-log-sel:';

export function logSelKey(wsPath: string, repoRel: string): string {
  return `${PREFIX}${wsPath}\0${repoRel || ''}`;
}

export function loadLogSel(wsPath: string, repoRel: string, fallback = 'current'): string {
  try {
    const v = localStorage.getItem(logSelKey(wsPath, repoRel));
    return v && v.trim() ? v : fallback;
  } catch {
    return fallback;
  }
}

export function saveLogSel(wsPath: string, repoRel: string, sel: string): void {
  try {
    localStorage.setItem(logSelKey(wsPath, repoRel), sel);
  } catch {
    /* ignore quota */
  }
}

/** 由筛选值得到 gitLog 的 mode / ref。 */
export function resolveLogFilter(sel: string): { mode: string; ref: string } {
  if (sel === 'all') return { mode: 'all', ref: '' };
  if (sel === 'current' || sel === '') return { mode: 'current', ref: '' };
  return { mode: 'ref', ref: sel };
}
