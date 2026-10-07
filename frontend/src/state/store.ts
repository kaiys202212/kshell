// 全局前端状态（Zustand）：工作区列表、打开的页签、弹窗窗口状态。
// 页签（openTabs/activeTabId）经 persist 中间件持久化到 localStorage（key kshell-tabs），
// 重开应用后恢复上次的工作区页签；其余字段的数据来源都是 Go 绑定层，刷新即重取，不持久化。
import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type { ChatInfo, ChatPermissionRequest, ChatUpdate, LanguageInfo, TerminalInfo, Workspace } from '../lib/api';
import type { AgentNotice } from '../lib/agentNotify';
import type { AppearanceInfo } from '../lib/appearance';
import { applyChatUpdate, type TimelineItem } from './chatUpdate';
import { mergeMirrorList } from './mirrorMerge';

// toast 的自增 id（模块级：store 单例，保证 id 唯一即可）
let nextToastId = 1;

// agent 通知的自增 id（同上）
let nextNoticeId = 1;

// 通知气泡点击跳转的自增序号：同一 termKey 连续点击也要能再次触发页签切换
let nextFocusSeq = 1;

// agent 通知队列上限：窗口隐藏期间 hook 照常上报，超过上限丢最旧的
//（气泡一次最多展示 3 条 + 计数，8 条缓冲足够覆盖正常节奏）。
const AGENT_NOTICE_CAP = 8;

// omitKey 返回去掉某个键的浅拷贝（不改原对象）。
function omitKey<T>(rec: Record<string, T>, key: string): Record<string, T> {
  const out: Record<string, T> = {};
  for (const k of Object.keys(rec)) if (k !== key) out[k] = rec[k];
  return out;
}

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

  // git 状态镜像：wsPath → { relPath('/' 分隔) → 状态码 }；
  // 数据来自 Go GitStatus；非 git 仓库时映射为空（无键=未加载，空 map=非仓库）。
  gitStatus: Record<string, Record<string, string>>;
  gitBranch: Record<string, string>; // wsPath → 虚拟根分支
  gitDirBranches: Record<string, Record<string, string>>; // wsPath → relPath → 分支
  setGitStatus(wsPath: string, status: Record<string, string>, branch?: string, dirs?: Record<string, string>): void;

  // 轻量全局提示（操作失败等）：toast 队列，notify 只负责追加，
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

  // ACP 聊天会话镜像（来自 Go 侧 Open*/ListChats 与 chat:* 事件；不持久化，刷新即重取）
  chats: ChatInfo[];
  upsertChat(info: ChatInfo): void;
  removeChat(id: string): void;
  markChatExited(id: string, exitCode: number, error: string): void;
  setChats(list: ChatInfo[]): void;

  // 每个聊天的流式时间线与已消费的最大 Seq（去重/断线续传用）
  chatItems: Record<string, TimelineItem[]>;
  chatSeq: Record<string, number>;
  applyChat(id: string, u: ChatUpdate): void;
  setChatItems(id: string, items: TimelineItem[]): void;
  removeChatState(id: string): void;

  // 待用户回应的权限请求（同一时刻每条聊天最多一个）
  chatPermissions: Record<string, ChatPermissionRequest | null>;
  setChatPermission(id: string, req: ChatPermissionRequest | null): void;

  // 终端静默启发 busy（不持久化；由 terminalBusy.ts 定时器驱动）
  terminalBusy: Record<string, true>;
  setTerminalBusy(id: string, busy: boolean): void;

  // 已归档的磁盘会话 ID（来自 Go，不持久化）
  archivedIDs: string[];
  setArchivedIDs(ids: string[]): void;
  // agent 调用 suggest_archive 后弹出的确认
  archivePrompt: { ref: string; summary: string } | null;
  setArchivePrompt(prompt: { ref: string; summary: string } | null): void;

  // 工作区页签三栏宽度（持久化，跨会话保留）
  layout: LayoutSizes;
  setLayout(patch: Partial<LayoutSizes>): void;

  // 新建会话选用的工具（'' = 自动：该工作区最常用；持久化，记住用户的选择）
  newSessionTool: string;
  setNewSessionTool(id: string): void;

  // 颜色模式（来自 Go 侧 GetAppearance / appearance:changed 事件；不持久化，刷新即重取）
  appearance: AppearanceInfo;
  setAppearance(info: AppearanceInfo): void;

  // 语言配置（来自 Go 侧 GetLanguage / language:changed 事件；不持久化。
  // 两处写入都收敛在 main.tsx（启动接线 + 事件订阅），组件只读，避免双写）
  language: LanguageInfo;
  setLanguage(info: LanguageInfo): void;

  // agent 通知气泡队列（notify:agent 事件；不持久化，10s 自动消失由气泡组件驱动）
  agentNotices: AgentNotice[];
  pushAgentNotice(p: Omit<AgentNotice, 'id'>): void;
  dismissAgentNotice(id: number): void;
  clearAgentNotices(): void;

  // 通知气泡点击后的「切到对应页签」请求（termKey = 终端/聊天 manager 的 key）。
  // 中心区页签选中态在各 WorkspaceTabView 的本地 state 里，只能经这里中转：
  // 每个视图监听该字段，发现自己持有该 key 的页签就选中并按 seq 清除。
  focusTermKey: { termKey: string; seq: number } | null;
  requestFocusTerm(termKey: string): void;
  clearFocusTerm(seq: number): void;

  // 气泡提示音开关（默认开）；随 kshell-tabs 持久化，播放时读当前值
  notifySound: boolean;
  setNotifySound(v: boolean): void;
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

      gitStatus: {},
      gitBranch: {},
      gitDirBranches: {},
      setGitStatus: (wsPath, status, branch = '', dirs = {}) =>
        set((s) => ({
          gitStatus: { ...s.gitStatus, [wsPath]: status },
          gitBranch: { ...s.gitBranch, [wsPath]: branch },
          gitDirBranches: { ...s.gitDirBranches, [wsPath]: dirs },
        })),

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
          if (idx < 0) {
            return { terminals: [...s.terminals, info] };
          }
          const terminals = s.terminals.slice();
          terminals[idx] = info;
          return { terminals };
        }),
      removeTerminal: (id) =>
        set((s) => ({
          terminals: s.terminals.filter((t) => t.ID !== id),
          terminalBusy: omitKey(s.terminalBusy, id),
        })),
      markTerminalExited: (id, exitCode) =>
        set((s) => ({
          terminals: s.terminals.map((t) =>
            t.ID === id ? { ...t, Status: 'exited', ExitCode: exitCode } : t,
          ),
          terminalBusy: omitKey(s.terminalBusy, id),
        })),
      setTerminals: (list) => set((s) => ({ terminals: mergeMirrorList(s.terminals, list) })),

      chats: [],
      upsertChat: (info) =>
        set((s) => {
          const idx = s.chats.findIndex((c) => c.ID === info.ID);
          if (idx < 0) {
            return { chats: [...s.chats, info] };
          }
          const chats = s.chats.slice();
          chats[idx] = info;
          return { chats };
        }),
      removeChat: (id) =>
        set((s) => ({
          chats: s.chats.filter((c) => c.ID !== id),
          chatItems: omitKey(s.chatItems, id),
          chatSeq: omitKey(s.chatSeq, id),
          chatPermissions: omitKey(s.chatPermissions, id),
        })),
      markChatExited: (id, exitCode, error) =>
        set((s) => ({
          chats: s.chats.map((c) =>
            c.ID === id ? { ...c, Status: 'exited', ExitCode: exitCode, Error: error } : c,
          ),
        })),
      setChats: (list) => set((s) => ({ chats: mergeMirrorList(s.chats, list) })),

      chatItems: {},
      chatSeq: {},
      applyChat: (id, u) =>
        set((s) => {
          const last = s.chatSeq[id] ?? 0;
          if (u.Seq <= last) return {};
          const items = applyChatUpdate(s.chatItems[id] ?? [], u);
          // 仅负责「一轮结束回到 ready」与错误记录；running 由发送侧乐观置位。
          // 只有状态真的变化时才新建 chats，避免每个流式分片都触发列表重渲染。
          let chats = s.chats;
          if (u.Type === 'error' || u.Type === 'turn_done') {
            chats = s.chats.map((c) => {
              if (c.ID !== id) return c;
              // 已退出是终态：迟到的 error/turn_done 不得把它复活成 ready
              if (c.Status === 'exited') {
                return u.Type === 'error' ? { ...c, Error: u.Text ?? c.Error } : c;
              }
              if (u.Type === 'error') return { ...c, Status: 'ready', Error: u.Text ?? c.Error };
              return { ...c, Status: 'ready' };
            });
          }
          return {
            chatItems: { ...s.chatItems, [id]: items },
            chatSeq: { ...s.chatSeq, [id]: u.Seq },
            chats,
          };
        }),
      setChatItems: (id, items) =>
        set((s) => ({
          chatItems: { ...s.chatItems, [id]: items },
          chatSeq: {
            ...s.chatSeq,
            // 取真实最大值（upsert 可能把高 Seq 放到非末尾）；空数组重置为 0
            [id]: items.length > 0 ? Math.max(...items.map((it) => it.seq)) : 0,
          },
        })),
      removeChatState: (id) =>
        set((s) => ({
          chatItems: omitKey(s.chatItems, id),
          chatSeq: omitKey(s.chatSeq, id),
        })),

      chatPermissions: {},
      setChatPermission: (id, req) =>
        set((s) => {
          const next = { ...s.chatPermissions };
          if (req) next[id] = req;
          else delete next[id];
          return { chatPermissions: next };
        }),

      terminalBusy: {},
      setTerminalBusy: (id, busy) =>
        set((s) => {
          if (busy) {
            if (s.terminalBusy[id]) return {};
            return { terminalBusy: { ...s.terminalBusy, [id]: true } };
          }
          if (!(id in s.terminalBusy)) return {};
          return { terminalBusy: omitKey(s.terminalBusy, id) };
        }),

      archivedIDs: [],
      setArchivedIDs: (archivedIDs) => set({ archivedIDs }),
      archivePrompt: null,
      setArchivePrompt: (archivePrompt) => set({ archivePrompt }),

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

      appearance: { mode: 'system', resolved: 'dark', fontSize: 13 },
      setAppearance: (appearance) => set({ appearance }),

      language: { configured: 'en', resolved: 'en' },
      setLanguage: (language) => set({ language }),

      agentNotices: [],
      pushAgentNotice: (p) =>
        set((s) => {
          const notices = [...s.agentNotices, { ...p, id: nextNoticeId++ }];
          return {
            agentNotices: notices.length > AGENT_NOTICE_CAP ? notices.slice(notices.length - AGENT_NOTICE_CAP) : notices,
          };
        }),
      dismissAgentNotice: (id) =>
        set((s) => ({ agentNotices: s.agentNotices.filter((n) => n.id !== id) })),
      clearAgentNotices: () => set({ agentNotices: [] }),

      notifySound: true,
      setNotifySound: (notifySound) => set({ notifySound }),

      focusTermKey: null,
      requestFocusTerm: (termKey) => set({ focusTermKey: { termKey, seq: nextFocusSeq++ } }),
      clearFocusTerm: (seq) =>
        set((s) => (s.focusTermKey?.seq === seq ? { focusTermKey: null } : {})),
    }),
    {
      name: 'kshell-tabs',
      // 只持久化页签、三栏宽度、新建会话的工具选择与通知提示音开关：
      // 工作区/终端/提示要么来自 Go 侧、要么是易失的内存态
      partialize: (s) => ({
        openTabs: s.openTabs,
        activeTabId: s.activeTabId,
        layout: s.layout,
        newSessionTool: s.newSessionTool,
        notifySound: s.notifySound,
      }),
    },
  ),
);
