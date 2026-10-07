/** Git 面板同步源的本地记忆（按工作区 + 仓库相对路径）。空串表示跟随默认解析。 */
const PREFIX = 'kshell-git-sync-remote:';

export function syncRemoteKey(wsPath: string, repoRel: string): string {
  return `${PREFIX}${wsPath}\0${repoRel || ''}`;
}

export function loadSyncRemote(wsPath: string, repoRel: string): string {
  try {
    const v = localStorage.getItem(syncRemoteKey(wsPath, repoRel));
    return v && v.trim() ? v : '';
  } catch {
    return '';
  }
}

export function saveSyncRemote(wsPath: string, repoRel: string, remote: string): void {
  try {
    localStorage.setItem(syncRemoteKey(wsPath, repoRel), remote);
  } catch {
    /* ignore quota */
  }
}
