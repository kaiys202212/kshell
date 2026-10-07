/** Git 面板上下分割比例的本地记忆（按工作区 + 仓库相对路径）。 */
const PREFIX = 'kshell-git-split:';
const DEFAULT_SPLIT = 0.55;
const MIN_SPLIT = 0.22;
const MAX_SPLIT = 0.8;

export function splitKey(wsPath: string, repoRel: string): string {
  return `${PREFIX}${wsPath}\0${repoRel || ''}`;
}

export function clampSplit(v: number): number {
  if (!Number.isFinite(v)) return DEFAULT_SPLIT;
  return Math.min(MAX_SPLIT, Math.max(MIN_SPLIT, v));
}

export function loadSplit(wsPath: string, repoRel: string): number {
  try {
    const raw = localStorage.getItem(splitKey(wsPath, repoRel));
    if (raw == null || raw === '') return DEFAULT_SPLIT;
    return clampSplit(Number(raw));
  } catch {
    return DEFAULT_SPLIT;
  }
}

export function saveSplit(wsPath: string, repoRel: string, split: number): void {
  try {
    localStorage.setItem(splitKey(wsPath, repoRel), String(clampSplit(split)));
  } catch {
    /* ignore quota */
  }
}
