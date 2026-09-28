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

  // 扫描进度：首页触发扫描时置 scanning，收到 scan:done 置 done。
  // 空态文案据此区分「扫描中」与「确实没有」。
  scanState: 'idle' | 'scanning' | 'done';
  setScanState(state: 'idle' | 'scanning' | 'done'): void;

  basket: string[]; // 上下文篮：勾选的文件路径（Go 侧篮子的镜像）
  // 以 Go ToggleBasket 的返回值为准同步（操作后是否在篮中），避免双份状态漂移
  syncBasket(path: string, inBasket: boolean): void;

  // 弹出的终端窗口状态（窗口标题 → 是否存活）：
  // SessionList 恢复成功把对应项置 true，"window:closed" 事件把对应项还原为 false。
  // 键与事件 payload 都是 lib/title.ts terminalTitle 的归一化形态，严格相等匹配。
  windowStatus: Record<string, boolean>;
  setWindowStatus(title: string, open: boolean): void;
}

export const useAppStore = create<AppState>((set) => ({
  workspaces: [],
  scanState: 'idle',
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

  setScanState: (scanState) => set({ scanState }),

  basket: [],
  syncBasket: (path, inBasket) =>
    set((s) => ({
      basket: inBasket
        ? s.basket.includes(path)
          ? s.basket
          : [...s.basket, path]
        : s.basket.filter((p) => p !== path),
    })),

  windowStatus: {},
  setWindowStatus: (title, open) =>
    set((s) => ({ windowStatus: { ...s.windowStatus, [title]: open } })),
}));
