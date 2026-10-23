// 中心区会话页签顺序：按工作区路径持久化到 localStorage，
// 新建会话追加末尾，关闭后从顺序中剔除（merge 时自然丢掉）。

const STORAGE_KEY = 'kshell.centerTabOrder';

/** 拖拽排序专用 MIME，与文件路径拖入（DRAG_MIME）隔离 */
export const CENTER_TAB_MIME = 'application/x-kshell-center-tab';

/** 更多按钮预留宽度（px），测量溢出时扣掉 */
export const MORE_BTN_WIDTH = 28;

/**
 * 把已保存顺序与当前存活 ID 合并：保留仍存在的相对顺序，新 ID 按 liveIDs 出现顺序追加。
 */
export function mergeCenterTabOrder(saved: string[], liveIDs: string[]): string[] {
  const pending = new Set(liveIDs);
  const out: string[] = [];
  for (const id of saved) {
    if (pending.has(id)) {
      out.push(id);
      pending.delete(id);
    }
  }
  for (const id of liveIDs) {
    if (pending.has(id)) out.push(id);
  }
  return out;
}

/** 把 fromId 挪到 toId 当前所在位置（toId 被挤开）。 */
export function reorderCenterTabs(ids: string[], fromId: string, toId: string): string[] {
  if (fromId === toId) return ids.slice();
  const from = ids.indexOf(fromId);
  const to = ids.indexOf(toId);
  if (from < 0 || to < 0) return ids.slice();
  const next = ids.slice();
  next.splice(from, 1);
  next.splice(to, 0, fromId);
  return next;
}

/**
 * 计算因宽度不足而隐藏的页签 ID（激活项尽量保留）。
 * 先从右侧藏非激活，仍不够再从左侧藏。
 */
export function computeOverflowHidden(
  ids: string[],
  widths: number[],
  activeId: string | null,
  available: number,
  moreBtnWidth: number = MORE_BTN_WIDTH,
): string[] {
  if (ids.length === 0 || widths.length !== ids.length) return [];
  let total = 0;
  for (const w of widths) total += w;
  if (total <= available) return [];

  const avail = Math.max(0, available - moreBtnWidth);
  const hidden = new Set<string>();
  const activeIdx = activeId ? ids.indexOf(activeId) : -1;

  for (let i = ids.length - 1; i >= 0 && total > avail; i--) {
    if (i === activeIdx) continue;
    hidden.add(ids[i]!);
    total -= widths[i]!;
  }
  for (let i = 0; i < ids.length && total > avail; i++) {
    if (i === activeIdx || hidden.has(ids[i]!)) continue;
    hidden.add(ids[i]!);
    total -= widths[i]!;
  }
  return ids.filter((id) => hidden.has(id));
}

export function loadCenterTabOrder(wsPath: string): string[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const map = JSON.parse(raw) as Record<string, unknown>;
    const list = map[wsPath];
    return Array.isArray(list) ? list.filter((x): x is string => typeof x === 'string') : [];
  } catch {
    return [];
  }
}

export function saveCenterTabOrder(wsPath: string, ids: string[]): void {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    const map: Record<string, string[]> = raw ? (JSON.parse(raw) as Record<string, string[]>) : {};
    if (typeof map !== 'object' || map === null || Array.isArray(map)) {
      localStorage.setItem(STORAGE_KEY, JSON.stringify({ [wsPath]: ids }));
      return;
    }
    map[wsPath] = ids;
    localStorage.setItem(STORAGE_KEY, JSON.stringify(map));
  } catch {
    // 隐私模式等：忽略持久化失败
  }
}
