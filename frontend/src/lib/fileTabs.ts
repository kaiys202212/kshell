// 文件区页签状态：至多一个可替换的「预览」页签；双击或开始编辑后固定。

export type FileTab = {
  path: string;
  preview: boolean;
};

export type FileTabsState = {
  tabs: FileTab[];
  activePath: string | null;
};

export function emptyFileTabs(): FileTabsState {
  return { tabs: [], activePath: null };
}

/** 页签标题用路径最后一段。 */
export function fileTabLabel(path: string): string {
  const n = path.replace(/\\/g, '/');
  const i = n.lastIndexOf('/');
  return i >= 0 ? n.slice(i + 1) : n || path;
}

export function activateTab(state: FileTabsState, path: string): FileTabsState {
  if (!state.tabs.some((t) => t.path === path)) return state;
  return { ...state, activePath: path };
}

/** 单击：已打开则激活；否则替换现有预览页签（没有则新建）。 */
export function openPreview(state: FileTabsState, path: string): FileTabsState {
  if (state.tabs.some((t) => t.path === path)) {
    return { ...state, activePath: path };
  }
  const tabs = [...state.tabs.filter((t) => !t.preview), { path, preview: true }];
  return { tabs, activePath: path };
}

/** 双击：固定已有页签，或新开固定页签（不挤掉别人的预览页签）。 */
export function openPinned(state: FileTabsState, path: string): FileTabsState {
  const i = state.tabs.findIndex((t) => t.path === path);
  if (i >= 0) {
    const tabs = state.tabs.map((t, idx) => (idx === i ? { ...t, preview: false } : t));
    return { tabs, activePath: path };
  }
  return { tabs: [...state.tabs, { path, preview: false }], activePath: path };
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
