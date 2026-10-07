// 文件区页签状态：至多一个可替换的「预览」页签；双击或开始编辑后固定。
import i18next from 'i18next';

export type FileTabKind = 'file' | 'diff';

export type FileTab = {
  path: string;
  preview: boolean;
  kind?: FileTabKind;
};

export type FileTabsState = {
  tabs: FileTab[];
  activePath: string | null;
};

export function emptyFileTabs(): FileTabsState {
  return { tabs: [], activePath: null };
}

export function diffTabPath(side: 'working' | 'staged', repoRel: string, relPath: string): string {
  return `diff:${side}:${encodeURIComponent(repoRel)}:${encodeURIComponent(relPath)}`;
}

export function parseDiffTabPath(
  key: string,
): { side: 'working' | 'staged'; repoRel: string; path: string } | null {
  if (!key.startsWith('diff:')) return null;
  const parts = key.split(':');
  if (parts.length !== 4) return null;
  if (parts[1] !== 'working' && parts[1] !== 'staged') return null;
  return {
    side: parts[1],
    repoRel: decodeURIComponent(parts[2]),
    path: decodeURIComponent(parts[3]),
  };
}

export function commitDiffTabPath(repoRel: string, hash: string): string {
  return `diff:commit:${encodeURIComponent(repoRel)}:${encodeURIComponent(hash)}`;
}

export function parseCommitDiffTabPath(
  key: string,
): { repoRel: string; hash: string } | null {
  if (!key.startsWith('diff:commit:')) return null;
  const parts = key.split(':');
  if (parts.length !== 4 || parts[1] !== 'commit') return null;
  return {
    repoRel: decodeURIComponent(parts[2]),
    hash: decodeURIComponent(parts[3]),
  };
}

function baseName(p: string): string {
  const n = p.replace(/\\/g, '/');
  const i = n.lastIndexOf('/');
  return i >= 0 ? n.slice(i + 1) : n || p;
}

/** 页签标题用路径最后一段；diff 页签带「已暂存」后缀；commit diff 用短 hash。 */
export function fileTabLabel(path: string): string {
  const commit = parseCommitDiffTabPath(path);
  if (commit) {
    const short = commit.hash.slice(0, 7);
    return i18next.t('ui.files.commit_diff_tab', { hash: short });
  }
  const diff = parseDiffTabPath(path);
  if (diff) {
    const name = baseName(diff.path);
    return diff.side === 'staged' ? `${name} ${i18next.t('ui.files.staged_suffix')}` : name;
  }
  return baseName(path);
}

export function activateTab(state: FileTabsState, path: string): FileTabsState {
  if (!state.tabs.some((t) => t.path === path)) return state;
  return { ...state, activePath: path };
}

/** 单击：已打开则激活；否则替换现有预览页签（没有则新建）。 */
export function openPreview(state: FileTabsState, path: string, kind: FileTabKind = 'file'): FileTabsState {
  const existing = state.tabs.find((t) => t.path === path);
  if (existing) {
    return { ...state, activePath: path };
  }
  const tabs = [...state.tabs.filter((t) => !t.preview), { path, preview: true, kind }];
  return { tabs, activePath: path };
}

/** 双击：固定已有页签，或新开固定页签（不挤掉别人的预览页签）。 */
export function openPinned(state: FileTabsState, path: string, kind: FileTabKind = 'file'): FileTabsState {
  const i = state.tabs.findIndex((t) => t.path === path);
  if (i >= 0) {
    const tabs = state.tabs.map((t, idx) => (idx === i ? { ...t, preview: false } : t));
    return { tabs, activePath: path };
  }
  return { tabs: [...state.tabs, { path, preview: false, kind }], activePath: path };
}

export function pinTab(state: FileTabsState, path: string): FileTabsState {
  return openPinned(state, path);
}

/** 关闭后激活右侧邻居，没有则左侧。 */
export function closeTab(state: FileTabsState, path: string): FileTabsState {
  const i = state.tabs.findIndex((t) => t.path === path);
  if (i < 0) return state;
  const tabs = state.tabs.filter((t) => t.path !== path);
  if (state.activePath !== path) return { ...state, tabs };
  const neighbor = tabs[i] ?? tabs[i - 1] ?? null;
  return { tabs, activePath: neighbor?.path ?? null };
}
