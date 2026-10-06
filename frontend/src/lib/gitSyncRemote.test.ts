import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { loadSyncRemote, saveSyncRemote, syncRemoteKey } from './gitSyncRemote';

describe('gitSyncRemote', () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => localStorage.clear());

  it('无记忆时返回空串（跟随默认解析）', () => {
    expect(loadSyncRemote('D:\\p', '')).toBe('');
  });

  it('存取往返', () => {
    saveSyncRemote('D:\\p', '', 'gitcode');
    expect(loadSyncRemote('D:\\p', '')).toBe('gitcode');
  });

  it('按 wsPath + repoRel 隔离，key 带前缀', () => {
    saveSyncRemote('D:\\p', 'nested', 'gitcode');
    expect(loadSyncRemote('D:\\p', '')).toBe('');
    expect(loadSyncRemote('D:\\other', 'nested')).toBe('');
    expect(loadSyncRemote('D:\\p', 'nested')).toBe('gitcode');
    expect(syncRemoteKey('D:\\p', '')).toContain('kshell-git-sync-remote:');
  });

  it('localStorage 抛错时降级为空串/静默', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('quota');
    });
    expect(loadSyncRemote('D:\\p', '')).toBe('');
    vi.restoreAllMocks();
  });
});
