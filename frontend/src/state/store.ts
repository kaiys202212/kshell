// 全局前端状态（Zustand）：工作区列表、打开的页签、上下文篮、弹窗窗口状态。
// 页签（openTabs/activeTabId）经 persist 中间件持久化到 localStorage（key kshell-tabs），
// 重开应用后恢复上次的工作区页签；其余字段的数据来源都是 Go 绑定层，刷新即重取，不持久化。
import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type { TerminalInfo, Workspace } from '../lib/api';

// toast 的自增 id（模块级：store 单例，保证 id 唯一即可）
let nextToastId = 1;

// 一个页签对应一个打开的工作区（按路径去重，可多开、可关闭）
export interface WorkspaceTab {
  id: string; // 工作区路径，作为页签唯一标识
  name: string;
}

// 轻量提示条目：id 用于移除，tone 决定 Toaster 的配色
export interface Toast {
  id: number;
  title: string;
  tone: 'info' | 'success' | 'error';
}

// 设置页固定页签的保留标识（带命名空间前缀，不会与工作区路径冲突）
export const SETTINGS_TAB_ID = 'kshell:settings';

// 三栏宽度（工作区页签左右两栏，单位 px）+ 拖动范围：
// 默认给足列表/文件树的阅读宽度，上限留出中心区至少 360px。
export interface LayoutSizes {
  left: number;
  right: number;
}
export const LAYOUT_DEFAULT: LayoutSizes = { left: 340, right: 360 };
export const LAYOUT_MIN = 200;
export const LAYOUT_MAX = 720;

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
  // 用 Go GetBasket 的返回值整体重建镜像（应用挂载时调用，防刷新漂移）
  setBasket(paths: string[]): void;
  // 文件重命名后原地更新镜像路径（Go 侧篮子已同步改名，这里只跟镜像）
  renameBasketPath(oldPath: string, newPath: string): void;

  // git 状态镜像：wsPath → { relPath('/' 分隔) → 状态码 }；
  // 数据来自 Go GitStatus；非 git 仓库时映射为空（无键=未加载，空 map=非仓库）。
  gitStatus: Record<string, Record<string, string>>;
  setGitStatus(wsPath: string, status: Record<string, string>): void;

  // 轻量全局提示（篮满、操作失败等）：toast 队列，notify 只负责追加，
  // 自动关闭与移除由 Toaster 侧（Radix duration/onOpenChange）调 dismissToast 完成。
  toasts: Toast[];
  notify(title: string, tone?: Toast['tone']): void;
  dismissToast(id: number): void;

  // 弹出的终端窗口状态（窗口标题 → 是否存活）：
  // SessionList 恢复成功把对应项置 true，"window:closed" 事件把对应项还原为 false。
  // 键与事件 payload 都是 lib/title.ts terminalTitle 的归一化形态，严格相等匹配。
  windowStatus: Record<string, boolean>;
  setWindowStatus(title: string, open: boolean): void;

  // 内嵌终端页签镜像（来自 Go 侧 ListTerminals/Open* 的返回值）。
  // 不持久化：应用重启后 Go 侧进程已随之消失，镜像应从 ListTerminals 重建。
  terminals: TerminalInfo[];
  upsertTerminal(info: TerminalInfo): void;
  removeTerminal(id: string): void;
  markTerminalExited(id: string, exitCode: number): void;
  // 用 Go 侧 ListTerminals 的返回值整体重建镜像（前端重载后 Go 侧终端仍在运行）
  setTerminals(list: TerminalInfo[]): void;

  // 工作区页签三栏宽度（持久化，跨会话保留）
  layout: LayoutSizes;
  setLayout(patch: Partial<LayoutSizes>): void;

  // 新建会话选用的工具（'' = 自动：该工作区最常用；持久化，记住用户的选择）
  newSessionTool: string;
  setNewSessionTool(id: string): void;
}

// clampLayout 把任意输入收敛到合法范围（拖动、持久化恢复、测试都走这里）。
export function clampLayout(v: number): number {
  if (!Number.isFinite(v)) return LAYOUT_MIN;
  return Math.min(LAYOUT_MAX, Math.max(LAYOUT_MIN, Math.round(v)));
}

export const useAppStore = create<AppState>()(
  persist(
    (set) => ({
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
      setBasket: (paths) => set({ basket: paths }),
      // 文件/目录重命名后同步镜像：精确命中换新路径；
      // 位于改名目录之下的条目换前缀。与 Go 侧同口径用大小写不敏感比较（Windows）。
      renameBasketPath: (oldPath, newPath) =>
        set((s) => {
          const lower = oldPath.toLowerCase();
          const sep = lower.includes('/') ? '/' : '\\';
          const prefix = lower.endsWith(sep) ? lower : lower + sep;
          return {
            basket: s.basket.map((p) => {
              const lp = p.toLowerCase();
              if (lp === lower) return newPath;
              if (lp.startsWith(prefix)) return newPath + p.slice(oldPath.length);
              return p;
            }),
          };
        }),

      gitStatus: {},
      setGitStatus: (wsPath, status) =>
        set((s) => ({ gitStatus: { ...s.gitStatus, [wsPath]: status } })),

      toasts: [],
      notify: (title, tone = 'info') =>
        set((s) => {
          const toasts = [...s.toasts, { id: nextToastId++, title, tone }];
          // 上限保护：窗口失焦时 Radix 暂停自动关闭计时，连续操作会堆积
          return { toasts: toasts.length > 5 ? toasts.slice(toasts.length - 5) : toasts };
        }),
      dismissToast: (id) => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),

      windowStatus: {},
      setWindowStatus: (title, open) =>
        set((s) => ({ windowStatus: { ...s.windowStatus, [title]: open } })),

      terminals: [],
      upsertTerminal: (info) =>
        set((s) => {
          const idx = s.terminals.findIndex((t) => t.ID === info.ID);
          if (idx < 0) return { terminals: [...s.terminals, info] };
          const terminals = s.terminals.slice();
          terminals[idx] = info;
          return { terminals };
        }),
      removeTerminal: (id) =>
        set((s) => ({ terminals: s.terminals.filter((t) => t.ID !== id) })),
      markTerminalExited: (id, exitCode) =>
        set((s) => ({
          terminals: s.terminals.map((t) =>
            t.ID === id ? { ...t, Status: 'exited', ExitCode: exitCode } : t,
          ),
        })),
      setTerminals: (list) => set({ terminals: list }),

      layout: LAYOUT_DEFAULT,
      setLayout: (patch) =>
        set((s) => ({
          layout: {
            left: patch.left === undefined ? s.layout.left : clampLayout(patch.left),
            right: patch.right === undefined ? s.layout.right : clampLayout(patch.right),
          },
        })),

      newSessionTool: '',
      setNewSessionTool: (newSessionTool) => set({ newSessionTool }),
    }),
    {
      name: 'kshell-tabs',
      // 只持久化页签、三栏宽度与新建会话的工具选择：
      // 工作区/篮子/终端/提示要么来自 Go 侧、要么是易失的内存态
      partialize: (s) => ({
        openTabs: s.openTabs,
        activeTabId: s.activeTabId,
        layout: s.layout,
        newSessionTool: s.newSessionTool,
      }),
    },
  ),
);
