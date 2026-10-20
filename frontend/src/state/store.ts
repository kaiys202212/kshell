// 全局前端状态（Zustand）：工作区列表、打开的页签、弹窗窗口状态。
// 页签（openTabs/activeTabId）经 persist 中间件持久化到 localStorage（key kshell-tabs），
// 重开应用后恢复上次的工作区页签；其余字段的数据来源都是 Go 绑定层，刷新即重取，不持久化。
import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import i18n from 'i18next';
import type { ChatBlock, ChatCommand, ChatInfo, ChatModelsView, ChatPermissionRequest, ChatQueueItem, ChatUpdate, HubMessage, LanguageInfo, TerminalInfo, Workspace } from '../lib/api';
import type { AgentNotice } from '../lib/agentNotify';
import type { AppearanceInfo } from '../lib/appearance';
import { applyChatUpdate, type TimelineItem } from './chatUpdate';
import { mergeMirrorList } from './mirrorMerge';
import { normalizeWorkspacePath, sameWorkspacePath } from '../lib/workspacePath';

// toast 的自增 id（模块级：store 单例，保证 id 唯一即可）
let nextToastId = 1;

// 首页选中项目的独立 localStorage 键（不入 kshell-tabs：值为单个路径字符串，
// 读写都围绕它自身进行，无需走 persist 的整包序列化）
const SELECTED_PROJECT_KEY = 'kshell.home.selectedProject';

// 读初始选中项目：空串/读取异常（隐私模式等）都视为未选中
function loadSelectedProject(): string | null {
  try {
    return localStorage.getItem(SELECTED_PROJECT_KEY) || null;
  } catch {
    return null;
  }
}

// agent 通知的自增 id（同上）
let nextNoticeId = 1;

// ChatView 本地消息队列的自增 id（同上）
let nextQueueMsgId = 1;

// hub 本地瞬态条目的负数自增 id（与后端落盘 ID 空间隔离，避免 React key 撞车）
let nextHubLocalId = -1;

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

// ChatView 本地队列的排队消息：running 时用户发送的消息先进队列，
// 出队（会话回 ready）时才真正调 sendChatPrompt*，不进时间线。
// blocks 有值表示带附件（发送走 sendChatPromptBlocks），否则走 sendChatPrompt。
export interface ChatQueuedMessage {
  id: number;
  text: string;
  blocks?: ChatBlock[];
}

// 队列长度上限：防止连环入队把输入区撑爆，超出丢最旧并提示
const CHAT_QUEUE_CAP = 50;

// tQueueFull 取「队列已满」提示文案（i18n：store 层不能写死用户可见文案）
function tQueueFull(): string {
  return i18n.t('ui.chat.queue_full');
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

  // ssh 工作区打开/扫描失败后的不可达标记（不持久化；Home 卡片徽章用）
  unreachableWorkspaces: Record<string, true>;
  markUnreachable(wsPath: string): void;
  clearUnreachable(wsPath: string): void;

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

  // 每个聊天的可用模型（agent 会话建立时报告）与技能命令（available_commands_update 推送）
  chatModels: Record<string, ChatModelsView | null>;
  setChatModels(id: string, view: ChatModelsView | null): void;
  chatCommands: Record<string, ChatCommand[]>;
  setChatCommands(id: string, cmds: ChatCommand[]): void;

  // ChatView 本地消息队列（agent 运行中用户再发消息的暂存；不持久化，随会话消亡）
  chatQueues: Record<string, ChatQueuedMessage[]>;
  // 出队失败暂停标记（chatID → true）：置位后自动出队停摆，手动发送解除。
  // 放 store 而非组件 ref：对账/镜像重建可能重挂 ChatView，ref 会丢状态。
  queuedPaused: Record<string, true>;
  enqueueChat(id: string, msg: { text: string; blocks?: ChatBlock[] }): void;
  dequeueChatHead(id: string): ChatQueuedMessage | null;
  // 出队发送失败：条目插回队首并置暂停标记（阻断自动出队，防持续性失败烧穿队列）；
  // 手动发送（enqueueChat 或 clearQueuedPause）解除暂停。
  requeueChatHead(id: string, msg: ChatQueuedMessage): void;
  queuedPauseActive(id: string): boolean;
  clearQueuedPause(id: string): void;
  removeQueuedChat(id: string, msgId: number): void;
  clearChatQueue(id: string): void;

  // 后端待发送队列镜像（chat:queue 全量快照，任何来源入队都可见）。
  // 与 chatQueues（ChatView 本地暂存，尚未交给后端）共存：展示时后端在前。
  chatBackendQueues: Record<string, ChatQueueItem[]>;
  setChatBackendQueue(id: string, items: ChatQueueItem[]): void;
  removeChatBackendQueueItem(id: string, itemID: string): void;
  // 队列版本号（会话级自增）：回填等慢速异步路径据此识别过期快照，不覆盖新状态
  chatQueueRevs: Record<string, number>;
  bumpChatQueueRev(id: string): void;

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

  // agent 通知气泡队列（notify:agent 事件；不持久化，10s 自动消失由气泡组件驱动）。
  // 注意：这是**瞬态**气泡队列，不再驱动 AgentHub 面板——Hub 消息走 hubMessages。
  agentNotices: AgentNotice[];
  pushAgentNotice(p: Omit<AgentNotice, 'id'>): void;
  dismissAgentNotice(id: number): void;
  clearAgentNotices(): void;

  // AgentHub 持久消息历史（来自 Go 侧 hub.Store；不持久化到 localStorage，源在磁盘）。
  // 由 App 订阅 hub:changed 后重拉填充，跨重启保留。
  hubMessages: HubMessage[];
  setHubMessages(list: HubMessage[] | ((prev: HubMessage[]) => HubMessage[])): void;
  // pushHubLocal 往 hubMessages 追加一条本地瞬态提示（共享命令回显等）：
  // 仅前端内存展示，id 取负数避免与后端落盘 ID 冲突，重拉历史后自然消失。
  pushHubLocal(text: string): void;

  // 通知气泡点击后的「切到对应页签」请求（termKey = 终端/聊天 manager 的 key）。
  // 中心区页签选中态在各 WorkspaceTabView 的本地 state 里，只能经这里中转：
  // 每个视图监听该字段，发现自己持有该 key 的页签就选中并按 seq 清除。
  focusTermKey: { termKey: string; seq: number } | null;
  requestFocusTerm(termKey: string): void;
  clearFocusTerm(seq: number): void;

  // 汇总聊天窗「未 @ 时」的默认投递目标：最后一次交互（@发送/点击行）的 chat.ID。
  // 不持久化：会话不跨重启存活，陈旧 ID 会误投。
  lastAgentId: string | null;
  setLastAgentId(id: string | null): void;

  // 气泡提示音开关（默认开）；随 kshell-tabs 持久化，播放时读当前值
  notifySound: boolean;
  setNotifySound(v: boolean): void;

  // 首页当前选中的项目路径（null = 未选中；独立 localStorage 键，跨重启保留）。
  // easynew 与需求列表以它为默认投递目标，Home 卡片点击切换。
  selectedProject: string | null;
  setSelectedProject(path: string | null): void;
}

// clampLayout 把任意输入收敛到合法范围（拖动、持久化恢复、测试都走这里）。
export function clampLayout(v: number): number {
  if (!Number.isFinite(v)) return LAYOUT_MIN;
  return Math.min(LAYOUT_MAX, Math.max(LAYOUT_MIN, Math.round(v)));
}

// dedupeTabs 合并持久化恢复的页签里大小写不同的同一路径（存量自愈）：
// 历史版本按精确字符串去重，同一目录可能存了 D:\x 与 d:\x 两个页签。
// 保留先出现者；activeTabId 指向被丢弃者时改指保留者。
export function dedupeTabs(
  tabs: WorkspaceTab[],
  activeTabId: string | null,
): { tabs: WorkspaceTab[]; activeTabId: string | null } {
  const kept: WorkspaceTab[] = [];
  const canonical = new Map<string, string>(); // 归一化路径 → 保留的页签 id
  for (const t of tabs) {
    const key = normalizeWorkspacePath(t.id);
    if (canonical.has(key)) continue;
    canonical.set(key, t.id);
    kept.push(t);
  }
  const active =
    activeTabId === null ? null : (canonical.get(normalizeWorkspacePath(activeTabId)) ?? activeTabId);
  return { tabs: kept, activeTabId: active };
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
          // Windows 路径大小写不敏感：同一目录在不同数据源（会话 cwd / 手动项目表）里
          // 大小写可能不同，精确比较会把同一工作区开成两个页签
          const existing = s.openTabs.find((t) => sameWorkspacePath(t.id, ws.Path));
          if (existing) {
            return { activeTabId: existing.id };
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

      unreachableWorkspaces: {},
      markUnreachable: (wsPath) =>
        set((s) =>
          wsPath && !s.unreachableWorkspaces[wsPath]
            ? { unreachableWorkspaces: { ...s.unreachableWorkspaces, [wsPath]: true } }
            : {},
        ),
      clearUnreachable: (wsPath) =>
        set((s) => {
          if (!wsPath || !s.unreachableWorkspaces[wsPath]) return {};
          const next = { ...s.unreachableWorkspaces };
          delete next[wsPath];
          return { unreachableWorkspaces: next };
        }),

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
          chatModels: omitKey(s.chatModels, id),
          chatCommands: omitKey(s.chatCommands, id),
          chatQueues: omitKey(s.chatQueues, id),
          queuedPaused: omitKey(s.queuedPaused, id),
          chatBackendQueues: omitKey(s.chatBackendQueues, id),
          chatQueueRevs: omitKey(s.chatQueueRevs, id),
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
          // 状态跟随 agent 实际活动：一轮结束回到 ready，错误记录；
          // running 除发送侧乐观置位外，turn_done 后 agent 继续输出的实时更新也拉回
          // （codebuddy 等 ACP 实现会提前返回 prompt 响应，之后仍推工具调用等流水）。
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
          } else if (
            u.Type === 'thought' || u.Type === 'assistant' || u.Type === 'tool' || u.Type === 'plan'
          ) {
            // 无状态可变时不新建数组：流式分片频率下，引用变化会让订阅 chats 的
            // 列表（会话树/页签/hub）每片重渲染
            const needRunning = s.chats.some(
              (c) => c.ID === id && c.Status !== 'running' && c.Status !== 'exited',
            );
            if (needRunning) {
              chats = s.chats.map((c) => {
                if (c.ID !== id || c.Status === 'running' || c.Status === 'exited') return c;
                return { ...c, Status: 'running' };
              });
            }
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
            // 取真实最大值（upsert 可能把高 Seq 放到非末尾）；空数组重置为 0。
            // 非空落位只升不降：历史回放落位前若有实时更新抢先抬高过 Seq（种子窗口竞态），
            // 回退会让随后补放的旧轮 turn_done 越过门控把 running 打回 ready。
            [id]: Math.max(
              items.length > 0 ? (s.chatSeq[id] ?? 0) : 0,
              items.length > 0 ? Math.max(...items.map((it) => it.seq)) : 0,
            ),
          },
        })),
      removeChatState: (id) =>
        set((s) => ({
          chatItems: omitKey(s.chatItems, id),
          chatSeq: omitKey(s.chatSeq, id),
          chatModels: omitKey(s.chatModels, id),
          chatCommands: omitKey(s.chatCommands, id),
          chatBackendQueues: omitKey(s.chatBackendQueues, id),
          chatQueueRevs: omitKey(s.chatQueueRevs, id),
        })),

      chatPermissions: {},
      setChatPermission: (id, req) =>
        set((s) => {
          // 聊天已从镜像移除（用户关了页签）时忽略迟到的事件推送：
          // 否则 Wails 管道里的旧 permission 会重建孤儿 key，弹窗挂在已关闭的聊天上
          if (req && !s.chats.some((c) => c.ID === id)) return {};
          const next = { ...s.chatPermissions };
          if (req) next[id] = req;
          else delete next[id];
          return { chatPermissions: next };
        }),

      chatModels: {},
      setChatModels: (id, view) =>
        set((s) => ({ chatModels: { ...s.chatModels, [id]: view } })),
      chatCommands: {},
      setChatCommands: (id, cmds) =>
        set((s) => ({ chatCommands: { ...s.chatCommands, [id]: cmds } })),

      chatQueues: {},
      queuedPaused: {} as Record<string, true>,
      enqueueChat: (id, msg) => {
        // 手动发送解除出队暂停（入队与直发都是手动干预）；解除逻辑并入同一个
        // set updater，避免在 action 内引用 useAppStore（会构成 TS7022 循环推断）
        set((s) => {
          const q = s.chatQueues[id] ?? [];
          let next: ChatQueuedMessage[];
          if (q.length >= CHAT_QUEUE_CAP) {
            next = [...q.slice(q.length - CHAT_QUEUE_CAP + 1), { ...msg, id: nextQueueMsgId++ }];
            // 超限提示只在越界的那次入队发出，避免连环发送时 toast 刷屏
            s.notify(tQueueFull(), 'error');
          } else {
            next = [...q, { ...msg, id: nextQueueMsgId++ }];
          }
          if (!s.queuedPaused[id]) return { chatQueues: { ...s.chatQueues, [id]: next } };
          const paused = { ...s.queuedPaused };
          delete paused[id];
          return { chatQueues: { ...s.chatQueues, [id]: next }, queuedPaused: paused };
        });
      },
      dequeueChatHead: (id) => {
        // head 经 set 闭包取走：action 内不得引用 useAppStore.getState()——
        // 那会构成 store 自身初始化器的循环类型推断（TS7022 把整个 store 推成 any）
        let head: ChatQueuedMessage | null = null;
        set((s) => {
          const q = s.chatQueues[id];
          if (!q || q.length === 0) return {};
          head = q[0];
          const rest = q.slice(1);
          const next = { ...s.chatQueues };
          if (rest.length === 0) delete next[id];
          else next[id] = rest;
          return { chatQueues: next };
        });
        return head;
      },
      requeueChatHead: (id, msg) =>
        set((s) => {
          const q = s.chatQueues[id] ?? [];
          const paused = { ...s.queuedPaused, [id]: true as const };
          return {
            chatQueues: { ...s.chatQueues, [id]: [msg, ...q] },
            queuedPaused: paused,
          };
        }),
      queuedPauseActive: (id): boolean => {
        // 读操作：从 getState 取，不进 set（避免订阅者把它当订阅源）。
        // 显式返回类型：action 内引用 useAppStore 会让 TS 循环推断（TS7022/TS7023）。
        return !!useAppStore.getState().queuedPaused[id];
      },
      clearQueuedPause: (id) =>
        set((s) => {
          if (!s.queuedPaused[id]) return {};
          const paused = { ...s.queuedPaused };
          delete paused[id];
          return { queuedPaused: paused };
        }),
      removeQueuedChat: (id, msgId) =>
        set((s) => {
          const q = s.chatQueues[id];
          if (!q) return {};
          const next = q.filter((m) => m.id !== msgId);
          const queues = { ...s.chatQueues };
          if (next.length === 0) delete queues[id];
          else queues[id] = next;
          return { chatQueues: queues };
        }),
      clearChatQueue: (id) =>
        set((s) => {
          if (!s.chatQueues[id] && !s.queuedPaused[id]) return {};
          const queues = { ...s.chatQueues };
          delete queues[id];
          const paused = { ...s.queuedPaused };
          delete paused[id];
          return { chatQueues: queues, queuedPaused: paused };
        }),

      chatBackendQueues: {},
      setChatBackendQueue: (id, items) =>
        set((s) => {
          const next = { ...s.chatBackendQueues };
          // 空快照删键：组件侧用 `?? EMPTY` 兜底，键是否存在不影响渲染
          if (items.length === 0) delete next[id];
          else next[id] = items;
          return { chatBackendQueues: next };
        }),
      removeChatBackendQueueItem: (id, itemID) =>
        set((s) => {
          const q = s.chatBackendQueues[id];
          if (!q) return {};
          const rest = q.filter((it) => it.ID !== itemID);
          if (rest.length === q.length) return {};
          const next = { ...s.chatBackendQueues };
          if (rest.length === 0) delete next[id];
          else next[id] = rest;
          return {
            chatBackendQueues: next,
            // 乐观移除也推进版本：挂载回填迟到时不得复活已删条目
            chatQueueRevs: { ...s.chatQueueRevs, [id]: (s.chatQueueRevs[id] ?? 0) + 1 },
          };
        }),

      chatQueueRevs: {},
      bumpChatQueueRev: (id) =>
        set((s) => ({
          chatQueueRevs: { ...s.chatQueueRevs, [id]: (s.chatQueueRevs[id] ?? 0) + 1 },
        })),

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

      appearance: { mode: 'system', resolved: 'dark', fontSize: 13, showWhitespace: false },
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

      hubMessages: [],
      setHubMessages: (list) =>
        set((s) => ({ hubMessages: typeof list === 'function' ? list(s.hubMessages) : list })),
      pushHubLocal: (text) =>
        set((s) => ({
          // id 用负数自增（同毫秒多条也不撞 key），不落盘，重拉历史即消失
          hubMessages: [
            ...s.hubMessages,
            { id: nextHubLocalId--, kind: 'agent', text, workspace: '', chatId: '', ts: Date.now() },
          ],
        })),

      notifySound: true,
      setNotifySound: (notifySound) => set({ notifySound }),

      selectedProject: loadSelectedProject(),
      setSelectedProject: (path) => {
        set({ selectedProject: path });
        try {
          if (path === null) localStorage.removeItem(SELECTED_PROJECT_KEY);
          else localStorage.setItem(SELECTED_PROJECT_KEY, path);
        } catch {
          // 写失败（隐私模式等）静默：内存态仍然可用
        }
      },

      focusTermKey: null,
      requestFocusTerm: (termKey) => set({ focusTermKey: { termKey, seq: nextFocusSeq++ } }),
      clearFocusTerm: (seq) =>
        set((s) => (s.focusTermKey?.seq === seq ? { focusTermKey: null } : {})),

      lastAgentId: null,
      setLastAgentId: (lastAgentId) => set({ lastAgentId }),
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
      // 恢复时合并大小写重复的存量页签（见 dedupeTabs 注释）
      merge: (persisted, current) => {
        const p = (persisted ?? {}) as Partial<AppState>;
        const deduped = dedupeTabs(p.openTabs ?? [], p.activeTabId ?? null);
        return { ...current, ...p, openTabs: deduped.tabs, activeTabId: deduped.activeTabId };
      },
    },
  ),
);
