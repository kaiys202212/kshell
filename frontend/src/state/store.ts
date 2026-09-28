// 全局前端状态（Zustand）：工作区列表、打开的页签、上下文篮、弹窗窗口状态。
// 不做持久化：数据来源都是 Go 绑定层，刷新即重取。
import { create } from 'zustand';
import type { Workspace } from '../lib/api';

// 一个页签对应一个打开的工作区（按路径去重，可多开、可关闭）
export interface WorkspaceTab {
  id: string; // 工作区路径，作为页签唯一标识
  name: string;
}

interface AppState {
  workspaces: Workspace[];
  setWorkspaces(list: Workspace[]): void;

  openTabs: WorkspaceTab[];
  activeTabId: string | null; // null 表示首页
  openTab(ws: Workspace): void; // 已打开的只激活，不重复开
  closeTab(id: string): void;
  setActiveTab(id: string | null): void;

  basket: string[]; // 上下文篮：勾选的文件路径
  toggleBasket(path: string): void;

  // 弹出的终端窗口状态（窗口标题 → 是否存活）：
  // SessionList 恢复成功把对应项置 true，"window:closed" 事件把对应项还原为 false
  //（事件 payload 是 Go 侧的完整窗口标题，超长会按 80 rune 截断，匹配时用前缀兜底）
  windowStatus: Record<string, boolean>;
  setWindowStatus(title: string, open: boolean): void;
}

export const useAppStore = create<AppState>((set) => ({
  workspaces: [],
  setWorkspaces: (list) => set({ workspaces: list }),

  openTabs: [],
  activeTabId: null,
  openTab: (ws) =>
    set((s) => {
      if (s.openTabs.some((t) => t.id === ws.Path)) {
        return { activeTabId: ws.Path };
      }
      return {
        openTabs: [...s.openTabs, { id: ws.Path, name: ws.Name }],
        activeTabId: ws.Path,
      };
    }),
  closeTab: (id) =>
    set((s) => {
      const openTabs = s.openTabs.filter((t) => t.id !== id);
      let activeTabId = s.activeTabId;
      if (s.activeTabId === id) {
        // 关掉当前页签时激活右侧邻居，没有邻居就回到首页
        const idx = s.openTabs.findIndex((t) => t.id === id);
        activeTabId = openTabs[Math.min(idx, openTabs.length - 1)]?.id ?? null;
      }
      return { openTabs, activeTabId };
    }),
  setActiveTab: (id) => set({ activeTabId: id }),

  basket: [],
  toggleBasket: (path) =>
    set((s) => ({
      basket: s.basket.includes(path)
        ? s.basket.filter((p) => p !== path)
        : [...s.basket, path],
    })),

  windowStatus: {},
  setWindowStatus: (title, open) =>
    set((s) => ({ windowStatus: { ...s.windowStatus, [title]: open } })),
}));
