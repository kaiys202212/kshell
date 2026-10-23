// AgentHubPanel：首页的跨项目 agent 汇总管理聊天窗。
// 与 ChatView 的区别：ChatView 绑定单个 chat.ID，本组件是跨会话聚合视图——
// 左侧列出所有运行中的 ACP agent（按项目分组、点击跳转），右侧合并展示
// 用户发出的消息与 agent 结束通知（summary 为 agent 回复正文）。
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { ArrowUpRight, CircleHelp, Loader2, Settings2 } from 'lucide-react';
import type { ChatCommand, ChatInfo, ChatModelsView, HubArchiveMonth, HubMessage, Session, TerminalInfo, ToolInfo, Workspace } from '../lib/api';
import {
  cancelChat,
  clearHubMessages,
  closeChat,
  getTools,
  getModelConfig,
  getSessions,
  hubCommand,
  hubPickTarget,
  listChatCommands,
  listChatModels,
  listHubArchive,
  listHubMessages,
  loadHubArchive,
  onToolsUpdated,
  onHubBusy,
  openSessionACP,
  openWorkspaceACP,
  readClipboardPaste,
  recallHubMessage,
  sendChatPrompt,
  setChatModel,
  saveHubRouteMode,
} from '../lib/api';
import {
  buildMentionOptions,
  displayMentionTitles,
  resolveMentionTarget,
  stripLeadingMentions,
  stripMentionTags,
  type MentionAgent,
  type MentionOption,
} from '../lib/agentMentions';
import { toolDisplayName } from '../lib/agentNotify';
import { formatPlatformModelName, hubInjectedModelsForTool, loadDefaultCreate, loadLastCreate, loadToolModelOptions, loadToolModels, loadHubRouteMode, mergeHubModelOptions, rememberLastModel, saveDefaultCreate, saveHubRouteMode as persistRouteModeLocal, saveLastCreate, saveToolModels, type HubRouteMode, type ToolModelOption } from '../lib/agenthubDefaults';
import { saveHubDefaultCreate } from '../lib/api';
import { quotePathForShell } from '../lib/dragPath';
import { backendError, translateBackend } from '../lib/errors';
import { isHubNumber, isNewHubCommand, normalizeHubProbe } from '../lib/hubCommands';
import { isHubWelcome, replyState, terminalActive } from '../lib/hubReply';
import { resolveHubSessionIdFromSessions } from '../lib/hubSessionResolve';
import { dispatchRequirement } from '../lib/dispatchRequirement';
import { REQUIREMENT_MIME } from './RequirementList';
import { attributionText, baseName, resolveAttribution } from '../lib/hubAttribution';
import { chipSessionLabel } from '../lib/hubChipLabel';
import { markdownBubbleClass } from '../lib/markdownStyles';
import { sameWorkspacePath } from '../lib/workspacePath';
import { cn } from '../lib/cn';
import { LIST_ROW } from '../lib/ui';
import { useAppStore } from '../state/store';
import { Button } from './ui/button';
import { ToolIcon } from './ui/tool-icons';
import { Dialog } from './ui/dialog';
import FindSessionDialog from './FindSessionDialog';
import HubMessageList, { type HubQuickAction } from './HubMessageList';
import * as DialogPrimitive from '@radix-ui/react-dialog';

// 消息流里渲染的一条：用户发出（右）或 agent 回复（左）。
// 数据源是 Go 侧 hub.Store 的持久历史（store.hubMessages），不再依赖瞬态 agentNotices。

// kshell 客户端命令：由本前端解析执行，不投递给 agent。name 不含前导 #。
// / 前缀只留给 agent 技能（见 cmdMatches）；# 模式补全与执行走本列表。
const HUB_COMMANDS: { name: string; descKey: string }[] = [
  { name: 'new', descKey: 'ui.hub.cmd_new_desc' },
  { name: 'easynew', descKey: 'ui.hub.cmd_easynew_desc' },
  { name: 'stop', descKey: 'ui.hub.cmd_stop_desc' },
  { name: 'model', descKey: 'ui.hub.cmd_model_desc' },
  { name: 'clear', descKey: 'ui.hub.cmd_clear_desc' },
  { name: 'find', descKey: 'ui.hub.cmd_find_desc' },
];
const HUB_COMMAND_NAMES = new Set(HUB_COMMANDS.map((c) => c.name.toLowerCase()));

// 创建互斥锁（session 级，防 /easynew 与对话框并发重复建会话）。
// 持有 promise 引用而非布尔：只有创建 Promise 自己的 finally 才释放锁，
// 避免「旧 finally 清掉新创建持有的锁」的竞态。常规释放路径是创建
// Promise 的 finally；面板卸载 effect 只是兜底（覆盖创建长挂/测试隔离场景）。
let createPromise: Promise<boolean> | null = null;

// AgentCreateDeps：可复用创建流程的外部依赖（api/store 回调），对话框与 /easynew 共用。
interface AgentCreateDeps {
  t: (key: string, opts?: Record<string, unknown>) => string;
  registerChat: (chat: ChatInfo) => void;
  onClose: () => void;
}

// runAgentCreate 一次点完创建全流程：建会话 → 记默认值 → 缓存模型列表 → 按需 SetModel。
// 返回 true 表示走到「已建会话」分支（无论后续 SetModel 成败）；false 表示前置校验失败或建会话抛错。
// 原两步逻辑的时序约束保留：session/new 不接受模型参数，只能建后再 SetModel，全部在一次调用内自动完成。
export async function runAgentCreate(
  wsPath: string,
  toolId: string,
  modelId: string | null,
  deps: AgentCreateDeps,
): Promise<boolean> {
  const { t, registerChat, onClose } = deps;
  if (createPromise !== null) {
    useAppStore.getState().notify(t('ui.hub.easynew_busy'), 'info');
    return false;
  }
  const promise = doCreate();
  createPromise = promise;
  return promise;

  // doCreate 真正的创建流程体：finally 里只清自己持有的锁，
  // 避免旧创建的 finally 清掉期间新创建已持有的锁。
  async function doCreate(): Promise<boolean> {
    try {
      // modelId 同步传给后端：Claude 经 settings 注入 ANTHROPIC_MODEL，避免仅靠建后 SetModel
      const result = await openWorkspaceACP(wsPath, toolId, modelId);
      if (!result.Chat) {
        onClose();
        return true;
      }
      const chat = result.Chat;
      registerChat(chat);
      // 成功即记默认值：下次 /easynew 或打开对话框时预填
      saveLastCreate({ wsPath, toolId, modelId });
      // 拉模型列表缓存，供下次预填模型下拉；失败静默（默认值是辅助功能）
      const view = await listChatModels(chat.ID).catch(() => null);
      if (view && view.Models && view.Models.length > 0) {
        // 保留 Name：Cursor Auto 等 ACP 展示名与 modelId（如 default[]）不同
        saveToolModels(
          toolId,
          view.Models.map((m) => ({ id: m.ID, name: m.Name || m.ID })),
        );
      }
      // 用户选了模型且与 agent 当前模型不同：建后再 SetModel。
      // view 拉取失败（null）也要切——否则握手竞态会让 easynew 永远停在 default。
      if (modelId && (!view || view.Current !== modelId)) {
        try {
          await setChatModel(chat.ID, modelId);
          // 乐观更新 store：后端会推 chat:models，这里立刻反映，避免下拉卡在 default
          if (view) {
            useAppStore.getState().setChatModels(chat.ID, { ...view, Current: modelId });
          } else {
            const refreshed = await listChatModels(chat.ID).catch(() => null);
            if (refreshed) useAppStore.getState().setChatModels(chat.ID, refreshed);
          }
        } catch (e: unknown) {
          // 会话已建成，切模型失败只提示，不回滚
          useAppStore.getState().notify(t('ui.hub.model_switch_failed', { err: backendError(e) }), 'error');
        }
      }
      return true;
    } catch (e: unknown) {
      useAppStore.getState().notify(backendError(e), 'error');
      return false;
    } finally {
      if (createPromise === promise) createPromise = null;
    }
  }
}

// 消息流里 agent 气泡 / 用户气泡的归因行：项目 · 会话（chats/terminals 反查）。
// 组件内共用 chats 与 terminals 镜像，抽成工厂避免两处重复传参。
function useAttribution() {
  const chats = useAppStore((s) => s.chats);
  const terminals = useAppStore((s) => s.terminals);
  return useMemo(
    () => (m: Pick<HubMessage, 'workspace' | 'chatId' | 'termKey'>) =>
      attributionText(resolveAttribution(m, chats, terminals)),
    [chats, terminals],
  );
}

// 补全列表条目：hash 模式为客户端命令（直接执行），slash 模式为 agent 技能（切入 draft）。
interface SlashItem {
  name: string;
  desc: string;
  kind: 'client' | 'agent';
}

// statusDotClass 状态点配色：运行中/握手中/等待登录用主色（Teal），退出用 muted，其余描边。
function statusDotClass(status: string): string {
  if (status === 'running' || status === 'starting' || status === 'authenticating') return 'bg-primary';
  if (status === 'exited') return 'bg-muted-foreground/40';
  return 'border border-muted-foreground bg-transparent';
}

// wsBaseName 取路径最后一段，作为项目分组标题。
function wsBaseName(ws: string): string {
  return baseName(ws);
}

// PENDING_PREFIX 标记「目标会话存在但未激活」：仅用于用户显式点「继续」等
// 快捷操作（activatePending），不再作为无 @ 发送的默认回落目标。
const PENDING_PREFIX = 'pending:';

// hubPendingSessionId 从一条 hub 消息解析待激活磁盘会话 ID（同步可得的两种来源）：
// sessionId 快照优先，其次 termKey 的 session: 前缀。都没有返回 null。
function hubPendingSessionId(m: HubMessage): string | null {
  if (m.sessionId) return m.sessionId;
  if (m.termKey?.startsWith('session:')) return m.termKey.slice('session:'.length);
  return null;
}

// hubMessageAttributed 消息是否带可解析归因（有 chatId / termKey / sessionId）。
// 本地提示/回执无归因，不算「最后一条可归因消息」。
function hubMessageAttributed(m: HubMessage): boolean {
  return !!(m.chatId || m.termKey || m.sessionId);
}

// lastHubAgentId 只看「最后一条可归因消息」：
// - 已激活且在 mention 候选中 → 返回 chat ID（默认接收者）
// - 未激活 / TUI·终端 / 非 ACP / 归因失效 → null（无默认，走 hubPickTarget）
// 不向前扫描更旧 ACP，也不用 store.lastAgentId 顶替。
function lastHubAgentId(
  messages: HubMessage[],
  options: MentionOption[],
  chats: ChatInfo[],
  tools: ToolInfo[],
  terminals: TerminalInfo[],
): string | null {
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i];
    if (!hubMessageAttributed(m)) continue;
    // 最后一条可归因即止：不可投递则无默认接收者。
    if (m.kind === 'agent' && replyState(m, tools, terminals) !== 'acp') return null;
    const c =
      chats.find((x) => x.ID === m.chatId) ??
      chats.find((x) => m.termKey && x.Key === m.termKey);
    if (c && options.some((o) => o.id === c.ID)) return c.ID;
    return null;
  }
  return null;
}

// 自动滚动偏好的 localStorage key（JSON 布尔；缺省 true = 默认跟随最新消息）
const AUTO_SCROLL_KEY = 'kshell.hub.autoScroll';

// readAutoScrollPref 读自动滚动偏好；缺失/损坏一律回落 true（默认开启）。
function readAutoScrollPref(): boolean {
  try {
    const raw = localStorage.getItem(AUTO_SCROLL_KEY);
    if (raw === null) return true;
    return JSON.parse(raw) === true;
  } catch {
    return true;
  }
}

// writeAutoScrollPref 写偏好；写失败静默（辅助功能）。
function writeAutoScrollPref(v: boolean): void {
  try {
    localStorage.setItem(AUTO_SCROLL_KEY, JSON.stringify(v));
  } catch {
    // 隐私模式/配额：静默
  }
}

export default function AgentHubPanel() {
  const { t } = useTranslation();
  const chats = useAppStore((s) => s.chats);
  const hubMessages = useAppStore((s) => s.hubMessages);
  const terminals = useAppStore((s) => s.terminals);
  // 消息归因：workspace/chatId/termKey → 「项目 · 会话」文本。
  const attrOf = useAttribution();
  // 工具 ACP 能力（近似判定，见 hubReply.ts）：拉一次探测结果，tools:updated 后重拉。
  // 探测未完成/工具未知时按只读保守展示，权威判定在后端 ForSessionACP。
  const [tools, setTools] = useState<ToolInfo[]>([]);
  useEffect(() => {
    let cancelled = false;
    const load = () => {
      void getTools()
        .then((list) => {
          if (!cancelled) setTools(list);
        })
        .catch(() => {});
    };
    load();
    const off = onToolsUpdated(load);
    return () => {
      cancelled = true;
      off();
    };
  }, []);

  const emptyTarget = t('ui.hub.new_agent');
  const [draft, setDraft] = useState('');
  const [dialogOpen, setDialogOpen] = useState(false);
  // 「默认设置」对话框开关：设置默认 agent 工具/模型，供 easynew 与需求派发使用
  const [defaultsOpen, setDefaultsOpen] = useState(false);
  // 使用说明弹窗：介绍 ACP 支持范围、各工具的平台模型注入情况与常用操作
  const [helpOpen, setHelpOpen] = useState(false);
  // #find 查找对话框：open 控制显隐，initialQuery 承接 `#find 关键词` 的初始过滤词
  const [findOpen, setFindOpen] = useState(false);
  const [findQuery, setFindQuery] = useState('');
  // /easynew 直建中的本地提示（不进 hub store，成功后由后端写欢迎词）
  const [easynewRunning, setEasynewRunning] = useState(false);
  // hub:busy：后端激活/新建会话期间的动画提示（OpenSession/CreateNew 前后 Emit）。
  const [hubBusy, setHubBusy] = useState(false);
  useEffect(() => onHubBusy(setHubBusy), []);
  // 归档入口：展开时列出月份，选中月份后加载该月旧消息内联展示。
  const [historyOpen, setHistoryOpen] = useState(false);
  const [archiveMonths, setArchiveMonths] = useState<HubArchiveMonth[]>([]);
  const [archiveMessages, setArchiveMessages] = useState<HubMessage[]>([]);
  const [activeMonth, setActiveMonth] = useState<string | null>(null);
  // 自动滚动：开启时新消息把消息流滚到底；关闭后自由翻历史不被打断。
  const [autoScroll, setAutoScroll] = useState(readAutoScrollPref);
  // 输入路由模式：auto=意图路由（默认）、agent=全部走 ACP、force_auto=直接工具循环。
  // localStorage 为 UI 侧真值，变更时写穿后端 config.yaml（跨端副本，微信桥可读）。
  const [routeMode, setRouteMode] = useState<HubRouteMode>(loadHubRouteMode);
  const changeRouteMode = (mode: HubRouteMode) => {
    setRouteMode(mode);
    persistRouteModeLocal(mode);
    saveHubRouteMode(mode).catch((e) => {
      useAppStore.getState().notify(t('ui.hub.route_save_failed', { err: backendError(e) }), 'error');
    });
  };
  const logRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);

  // 消息或开关变化时按需滚底：重新勾选立即到底，取消后不再跟随。
  // 依赖最后一条消息 id（而非数组引用）：引用必变但内容可能晚到，id 才代表「真有新消息」。
  // 滚动放 double rAF：markdown 图片/代码高亮是异步布局，同步滚一次 height 还会涨，
  // 两帧后再滚保证落在最终位置。另挂 ResizeObserver 兜底：自动滚动开启期间任何
  // 内容高度增长（图片解码完成等）都重滚到底，覆盖 rAF 之后再变化的场景。
  const lastMsgId = hubMessages.length > 0 ? hubMessages[hubMessages.length - 1].id : 0;
  useEffect(() => {
    if (!autoScroll) return;
    let raf1 = 0;
    let raf2 = 0;
    raf1 = requestAnimationFrame(() => {
      raf2 = requestAnimationFrame(() => {
        const el = logRef.current;
        if (el) el.scrollTop = el.scrollHeight;
      });
    });
    return () => {
      cancelAnimationFrame(raf1);
      cancelAnimationFrame(raf2);
    };
  }, [lastMsgId, autoScroll]);

  useEffect(() => {
    if (!autoScroll) return;
    const el = logRef.current;
    if (!el || typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(() => {
      el.scrollTop = el.scrollHeight;
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, [autoScroll]);

  // 切换自动滚动；落盘放在 updater 外（updater 应为纯函数，StrictMode 双调用不重复写盘）
  const toggleAutoScroll = () => {
    const next = !autoScroll;
    writeAutoScrollPref(next);
    setAutoScroll(next);
  };

  // 挂载时拉取持久历史（Hub 消息源在 Go 侧磁盘；hub:changed 事件由 App 订阅后回填 store）。
  useEffect(() => {
    let cancelled = false;
    void listHubMessages()
      .then((list) => {
        if (!cancelled) useAppStore.getState().setHubMessages(list);
      })
      .catch(() => {
        // 拉取失败静默：Hub 是辅助视图，不打断首页
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // 斜杠补全：draft 首词为 /xxx 时激活。
  const [slashClosed, setSlashClosed] = useState(false);
  const [slashIndex, setSlashIndex] = useState(0);
  // @ 补全键盘状态：与 # 弹层对称（index 高亮 + Esc 关闭标记）。
  const [mentionClosed, setMentionClosed] = useState(false);
  const [mentionIndex, setMentionIndex] = useState(0);
  // agent 技能按目标缓存：target 变化或打开命令模式时拉取，失败静默（补全是辅助功能）。
  const [agentCmds, setAgentCmds] = useState<ChatCommand[]>([]);
  // /model 内联下拉：非 null 时在输入区上方展示模型列表。
  const [modelMenu, setModelMenu] = useState<{ targetId: string; view: ChatModelsView } | null>(null);

  // @ 补全项：insert 为写入草稿的标签（工具名#会话标题），label 为路由串。
  const mentionAgents = useMemo(
    () =>
      chats.map<MentionAgent>((c) => ({
        id: c.ID,
        toolId: c.ToolID,
        title: c.Title,
        workspace: c.Workspace,
      })),
    [chats],
  );
  const options = useMemo(() => buildMentionOptions(mentionAgents), [mentionAgents]);

  // userBubbleText 用户气泡不展示 Hub @ 路由标签：历史消息先按会话标题
  // 替换 id 写法，再整体移除标签；新消息投递前已剥离，气泡天然干净。
  const userBubbleText = (text: string) =>
    stripMentionTags(displayMentionTitles(text, mentionAgents), options);

  // mentionLabelForChat 给「刚激活、尚未进入渲染期 chats」的会话构造写入标签。
  // 必须基于实时 store 的全量会话：visibleMentionLabel 的同名消歧依赖完整列表，
  // 只用单元素列表会生成与补全候选不一致、resolveMentionTarget 也命中不了的标签。
  const mentionLabelForChat = (chat: ChatInfo): string | undefined => {
    const agents = useAppStore.getState().chats.map<MentionAgent>((c) => ({
      id: c.ID,
      toolId: c.ToolID,
      title: c.Title,
      workspace: c.Workspace,
    }));
    if (!agents.some((a) => a.id === chat.ID)) {
      agents.push({ id: chat.ID, toolId: chat.ToolID, title: chat.Title, workspace: chat.Workspace });
    }
    return buildMentionOptions(agents).find((o) => o.id === chat.ID)?.insert;
  };

  // @ 补全：草稿末尾是未闭合的 @ 片段时给出候选（v1 只按草稿尾匹配，不做光标跟踪）。
  // mentionQuery 为 null 表示当前不在 @ 输入中。
  const mentionQuery = useMemo(() => {
    const m = /(?:^|\s)@([^\s@]*)$/.exec(draft);
    return m ? m[1].toLowerCase() : null;
  }, [draft]);
  const mentionMatches = useMemo(() => {
    if (mentionQuery === null) return [];
    return options.filter((o) =>
      o.insert.toLowerCase().includes(mentionQuery) ||
      o.label.toLowerCase().includes(mentionQuery) ||
      (o.detail?.toLowerCase().includes(mentionQuery) ?? false),
    );
  }, [mentionQuery, options]);

  // applyMention 把草稿尾部的 @ 片段替换成完整标签（补一个尾随空格方便继续输入）
  const applyMention = (label: string) => {
    setDraft((d) => d.replace(/@([^\s@]*)$/, `@${label} `));
  };

  // ---- 命令补全：draft 尾部为 /xxx（agent 技能）或整段为 #xxx（客户端命令）时激活 ----
  // 斜杠按尾部 token 匹配：@ 目标（chips/补全写入）在前时仍能继续补全技能。
  // 但 / 之前除 @ 标签外不能有其他文字：技能要求 "/" 位于正文首字符，
  // 否则选中也不会执行，弹层只会误导。
  const slashMatch = /(?:^|\s)\/([^\s]*)$/.exec(draft);
  const slashQuery =
    slashMatch && stripLeadingMentions(draft.slice(0, slashMatch.index), options) === ''
      ? slashMatch[1]
      : null;
  const hashQuery = /^#([^\s]*)$/.exec(draft)?.[1] ?? null;
  const inSlashMode = slashQuery !== null;
  const inHashMode = hashQuery !== null;
  // 默认投递目标：仅最后一条可归因且已激活 ACP；否则 null（走待选含 en）。
  // 不用 store.lastAgentId——与微信 Console 一致，避免空 hub 时桌面直发、微信待选分叉。
  const fallbackId = useMemo(
    () => lastHubAgentId(hubMessages, options, chats, tools, terminals),
    [hubMessages, options, chats, tools, terminals],
  );
  // 目标解析：优先 @ 提及，其次回落链（与 send 一致）。
  const currentTargetId = useMemo(
    () => resolveMentionTarget(draft, options, fallbackId),
    [draft, options, fallbackId],
  );
  // 待激活 chip 数据：currentTargetId 为 pending: 前缀时，从命中的那条 hub 消息
  // 取展示信息（会话标题快照 / 工具名）。精确命中已激活 chat 时为 null（普通 chip
  // 高亮接管）。取「lastHubAgentId 所看的那条」最稳妥：同样倒序扫，命中 pending
  // 的第一条消息即 chip 的来源。
  const pendingTarget = useMemo(() => {
    if (!currentTargetId?.startsWith(PENDING_PREFIX)) return null;
    const sessionID = currentTargetId.slice(PENDING_PREFIX.length);
    for (let i = hubMessages.length - 1; i >= 0; i--) {
      const m = hubMessages[i];
      if (hubPendingSessionId(m) !== sessionID) continue;
      return {
        sessionID,
        title: m.sessionTitle || sessionID,
        tool: m.tool || '',
        workspace: m.workspace || '',
      };
    }
    return { sessionID, title: sessionID, tool: '', workspace: '' };
  }, [currentTargetId, hubMessages]);
  // mentionPending 待激活 chip 点击：先激活原会话，成功后按普通 chip 语义把
  // 新会话的 @ 标签追加进输入框。
  const mentionPending = () => {
    if (!pendingTarget) return;
    void activatePending(pendingTarget.sessionID).then((chat) => {
      if (!chat) return;
      // 不调 mentionSession：then 回调捕获的是点击轮渲染的旧闭包，其 options
      // 还不含刚激活的 chat；按实时 store 的全量列表构造，保证同名消歧与补全一致。
      const label = mentionLabelForChat(chat);
      if (!label) return;
      setDraft((d) => (d.trim() ? `${d.trimEnd()} @${label} ` : `@${label} `));
      inputRef.current?.focus();
    });
  };
  // 命令模式打开且目标变化时拉取 agent 技能；失败静默。
  // 依赖用 inSlashMode 而非 slashQuery：避免每次按键都重拉。
  useEffect(() => {
    if (!inSlashMode || !currentTargetId) {
      setAgentCmds([]);
      return;
    }
    let cancelled = false;
    void listChatCommands(currentTargetId)
      .then((cmds) => {
        if (!cancelled) setAgentCmds(cmds);
      })
      .catch(() => {
        if (!cancelled) setAgentCmds([]);
      });
    return () => {
      cancelled = true;
    };
  }, [inSlashMode, currentTargetId]);

  // slash 模式只列 agent 技能；hash 模式只列客户端命令（# 前缀，两模式互不混排）。
  const cmdMatches = useMemo<SlashItem[]>(() => {
    if (slashQuery !== null) {
      const q = slashQuery.toLowerCase();
      return agentCmds
        .filter((c) => c.Name && c.Name.toLowerCase().startsWith(q))
        .map<SlashItem>((c) => ({ name: c.Name, desc: c.Description ?? '', kind: 'agent' }));
    }
    if (hashQuery !== null) {
      const q = hashQuery.toLowerCase();
      return HUB_COMMANDS.filter((c) => c.name.toLowerCase().startsWith(q)).map<SlashItem>((c) => ({
        name: c.name,
        desc: t(c.descKey),
        kind: 'client',
      }));
    }
    return [];
  }, [slashQuery, hashQuery, agentCmds, t]);
  const cmdOpen = (slashQuery !== null || hashQuery !== null) && !slashClosed && cmdMatches.length > 0;

  // draft 变化即重置补全状态：Esc 关闭后继续输入要能重新弹出
  useEffect(() => {
    setSlashClosed(false);
    setSlashIndex(0);
    setMentionClosed(false);
    setMentionIndex(0);
  }, [draft]);

  // 键盘高亮项滚动跟随：index / 候选变化后把选中 li 滚进可视区。
  // block:'nearest' 只做最小滚动，不牵动外层消息流容器；jsdom 无 scrollIntoView，可选调用防御。
  const cmdListRef = useRef<HTMLUListElement>(null);
  const mentionListRef = useRef<HTMLUListElement>(null);
  useEffect(() => {
    if (!cmdOpen) return;
    const el = cmdListRef.current?.children[Math.min(slashIndex, cmdMatches.length - 1)];
    if (el instanceof HTMLElement) el.scrollIntoView?.({ block: 'nearest' });
  }, [slashIndex, cmdMatches, cmdOpen]);
  useEffect(() => {
    if (mentionMatches.length === 0) return;
    const el = mentionListRef.current?.children[Math.min(mentionIndex, mentionMatches.length - 1)];
    if (el instanceof HTMLElement) el.scrollIntoView?.({ block: 'nearest' });
  }, [mentionIndex, mentionMatches, mentionClosed]);
  // 候选集在弹层开着期间收缩（后台 scan:done 重建 chats / agentCmds 异步到达）：
  // 重置 effect 只盯 draft，这里把高亮下标夹回范围，避免渲染出无高亮列表。
  useEffect(() => {
    setSlashIndex((i) => Math.min(i, Math.max(cmdMatches.length - 1, 0)));
  }, [cmdMatches]);
  useEffect(() => {
    setMentionIndex((i) => Math.min(i, Math.max(mentionMatches.length - 1, 0)));
  }, [mentionMatches]);

  // runClientCommand 执行 kshell 客户端命令（# 前缀触发），不投递给 agent。
  const runClientCommand = (name: string) => {
    // stop / model 需要目标 agent：先校验，避免无目标时还把输入清掉
    if ((name === 'stop' || name === 'model') && !currentTargetId) {
      useAppStore.getState().notify(t('ui.hub.need_target_drag'), 'error');
      return;
    }
    setDraft('');
    setSlashClosed(false);
    setSlashIndex(0);
    if (name === 'new') {
      openNewAgentDialog();
      return;
    }
    if (name === 'find') {
      openFindDialog('');
      return;
    }
    if (name === 'easynew') {
      runEasyNew();
      return;
    }
    if (name === 'clear') {
      useAppStore.getState().setHubMessages([]);
      useAppStore.getState().clearAgentNotices();
      // 清空落盘 live（归档保留）；失败不阻断，UI 已清
      void clearHubMessages().catch(() => {});
      useAppStore.getState().notify(t('ui.hub.cleared'), 'info');
      return;
    }
    if (!currentTargetId) return; // 理论不可达；保底
    // pending: 目标 = 最近交互的会话未激活但有磁盘会话 ID 快照：先激活原会话，
    // 再对激活出的真实 chat ID 执行命令；激活失败报错且不执行（与 send 同语义，
    // 绝不把命令落到 pending: 前缀串上或同 workspace 其他会话上）。
    if (currentTargetId.startsWith(PENDING_PREFIX)) {
      const sessionID = currentTargetId.slice(PENDING_PREFIX.length);
      if (name !== 'stop' && name !== 'model') return; // 其余命令无 pending 语义，防御
      void activatePending(sessionID).then((chat) => {
        if (!chat) return;
        if (name === 'stop') {
          void cancelChat(chat.ID)
            .then(() => useAppStore.getState().notify(t('ui.hub.stop_done'), 'info'))
            .catch((e: unknown) =>
              useAppStore.getState().notify(t('ui.hub.stop_failed', { err: backendError(e) }), 'error'),
            );
          return;
        }
        void listChatModels(chat.ID)
          .then((view) => {
            if (!view || !view.Models || view.Models.length === 0) {
              useAppStore.getState().notify(t('ui.hub.no_models'), 'error');
              return;
            }
            setModelMenu({ targetId: chat.ID, view });
          })
          .catch((e: unknown) => useAppStore.getState().notify(backendError(e), 'error'));
      });
      return;
    }
    if (name === 'stop') {
      void cancelChat(currentTargetId)
        .then(() => useAppStore.getState().notify(t('ui.hub.stop_done'), 'info'))
        .catch((e: unknown) =>
          useAppStore.getState().notify(t('ui.hub.stop_failed', { err: backendError(e) }), 'error'),
        );
      return;
    }
    if (name === 'model') {
      void listChatModels(currentTargetId)
        .then((view) => {
          if (!view || !view.Models || view.Models.length === 0) {
            useAppStore.getState().notify(t('ui.hub.no_models'), 'error');
            return;
          }
          setModelMenu({ targetId: currentTargetId, view });
        })
        .catch((e: unknown) => useAppStore.getState().notify(backendError(e), 'error'));
    }
  };

  // runEasyNew /easynew 快建：默认设置/上次记录仍有效时直接走创建流程（不弹窗），
  // 失效或没有默认值则打开新建弹窗让用户补选（补选后同样落默认值）。
  // 取值优先级：toolId/modelId 来自显式默认设置（loadDefaultCreate），缺失回落
  // lastCreate 全量；wsPath 优先当前选中项目（首页卡片点击），否则 lastCreate.wsPath。
  // 创建互斥：runAgentCreate 内部持锁；这里对弹窗入口另查锁，避免创建中又打开对话框重复操作。
  const runEasyNew = () => {
    if (createPromise !== null) {
      useAppStore.getState().notify(t('ui.hub.easynew_busy'), 'info');
      return;
    }
    const defaults = loadDefaultCreate();
    const last = loadLastCreate();
    // 项目来源：选中项目优先，其次上次创建记录；两者皆无 → 弹窗补选
    const wsPath = useAppStore.getState().selectedProject ?? last?.wsPath;
    const wsOk =
      wsPath &&
      useAppStore.getState().workspaces.some((w) => sameWorkspacePath(w.Path, wsPath));
    if (!wsPath || !wsOk) {
      openNewAgentDialog();
      return;
    }
    setEasynewRunning(true);
    // 工具有效性需异步拉工具列表校验（store 无镜像）：默认设置的工具可用则用之，
    // 不可用提示回落；无默认设置时回落 lastCreate.toolId，再不可用才弹窗补选
    void getTools()
      .then((list) => list.filter((x) => x.ACP?.Available === true))
      .then((acp) => {
        let toolId = defaults?.toolId && acp.some((x) => x.ID === defaults.toolId) ? defaults.toolId : '';
        if (!toolId && defaults?.toolId) {
          // 默认工具不可用（卸载/ACP 掉线）：提示后回落自动选择
          useAppStore.getState().notify(t('ui.hub.default_tool_unavailable'), 'info');
        }
        if (!toolId && last && acp.some((x) => x.ID === last.toolId)) {
          toolId = last.toolId;
        }
        if (!toolId) {
          openNewAgentDialog();
          return;
        }
        // 模型：默认设置显式选了模型才恒用之；未选（null）时回落 lastCreate.modelId
        //（上次模型），再无则 null = 工具默认模型。默认设置只固定工具，不锁死模型。
        const modelId = defaults?.modelId ?? last?.modelId ?? null;
        return runAgentCreate(wsPath, toolId, modelId, {
          t,
          registerChat: (chat) => {
            useAppStore.getState().upsertChat(chat);
            useAppStore.getState().setLastAgentId(chat.ID);
          },
          onClose: () => {},
        });
      })
      .then((ok) => {
        // 创建流程已结束（成功/失败或弹窗补选）即清提示：会话已登记，
        // 欢迎词随后由后端写入 hub，spinner 使命到此为止，不做额外监听。
        setEasynewRunning(false);
      })
      .catch(() => setEasynewRunning(false));
  };

  // openNewAgentDialog 打开新建弹窗；创建进行中拦截并提示，防并发重复建会话。
  const openNewAgentDialog = () => {
    if (createPromise !== null) {
      useAppStore.getState().notify(t('ui.hub.easynew_busy'), 'info');
      return;
    }
    setDialogOpen(true);
  };

  // applySlash：客户端命令直接执行，agent 技能替换草稿尾部 / 词后正常发送。
  // 只替换尾部 token，保留前面的 @ 目标（chips 写入的标签不因选技能被清掉）。
  const applySlash = (item: SlashItem) => {
    if (item.kind === 'client') {
      runClientCommand(item.name);
      return;
    }
    setDraft((d) => d.replace(/(^|\s)\/[^\s]*$/, (_m, lead: string) => `${lead}/${item.name} `));
    setSlashClosed(false);
    setSlashIndex(0);
  };

  const switchModel = (modelID: string) => {
    const menu = modelMenu;
    if (!menu) return;
    setModelMenu(null);
    void setChatModel(menu.targetId, modelID)
      .then(() => {
        // 乐观更新 store 视图，供 ChatView 等复用
        useAppStore.getState().setChatModels(menu.targetId, { ...menu.view, Current: modelID });
        // 回写「上次模型」：下次新建该工具会话时预填（查不到会话信息则跳过）
        const info = useAppStore.getState().chats.find((c) => c.ID === menu.targetId);
        if (info) rememberLastModel(info.ToolID, info.Workspace, modelID);
      })
      .catch((e: unknown) =>
        useAppStore.getState().notify(t('ui.hub.model_switch_failed', { err: backendError(e) }), 'error'),
      );
  };

  // closeSession 关闭空闲会话：走与 WorkspaceTab 一致的正确组合——后端 Manager.Close
  // （closeChat，幂等，真摘除会话；cancelChat 只取消当前轮，对空闲会话是 no-op），
  // 成功后才从 store 摘除（失败保留行，可重试），列表随 chats 状态自动收缩（面板保持开）。
  const closeSession = (id: string) => {
    void closeChat(id)
      .then(() => useAppStore.getState().removeChat(id))
      .catch((e: unknown) =>
        useAppStore.getState().notify(t('ui.hub.close_session_failed', { err: backendError(e) }), 'error'),
      );
  };

  // sessionDragLabel 从 mention 候选反查拖拽用的 @ 标签；找不到兜底会话标题。
  const sessionDragLabel = (c: ChatInfo): string => {
    const opt = options.find((o) => o.id === c.ID);
    return opt?.insert ?? (c.Title || wsBaseName(c.Workspace) || emptyTarget);
  };

  // mentionSession 把会话的 @ 标签保留式追加进输入框（语义同 quickReply rephrase：
  // 不清空已有草稿，空格分隔），聚焦输入框，待用户补写后手动发送。
  // chips 平铺后点击 chip 主体即触发本函数（原 @ 图标行为并入主体）。
  const mentionSession = (c: ChatInfo) => {
    const label = sessionDragLabel(c);
    setDraft((d) => (d.trim() ? `${d.trimEnd()} @${label} ` : `@${label} `));
    inputRef.current?.focus();
  };

  // openFindDialog 打开 #find 查找对话框：keyword 为 `#find 关键词` 的初始过滤词。
  const openFindDialog = (keyword: string) => {
    setFindQuery(keyword);
    setFindOpen(true);
  };

  // refuseTerminalSession 终端页签在册时禁止抢拉 ACP：toast 既有文案并返回 true
  //（调用方中止）。与后端 OpenSessionACP / wxExecutor 同一 session: 前缀语义。
  const refuseTerminalSession = (sessionID: string): boolean => {
    if (!terminalActive(`session:${sessionID}`, terminals, sessionID)) return false;
    useAppStore.getState().notify(t('err.hub.terminal_active'), 'error');
    return true;
  };

  // pickFoundSession 激活选中的已发现会话：OpenSessionACP 建聊天（Go 侧对已退出
  // 会话自动重建），成功后 registerChat 登记 + 把该 chat 的 @ 标签追加进草稿
  // （语义同 mentionSession），清掉 `#find ...` 草稿残留并聚焦输入框。
  // 注意新 chat ID 与 GetSessions 的 session ID 无关，label 从返回的 Chat 构造。
  const pickFoundSession = (s: Session) => {
    if (refuseTerminalSession(s.ID)) return;
    void openSessionACP(s.ID)
      .then((res) => {
        if (!res.Chat) return;
        const chat = res.Chat;
        useAppStore.getState().upsertChat(chat);
        useAppStore.getState().setLastAgentId(chat.ID);
        // label 与 @ 补全候选用同一套生成逻辑（含全量会话的同名消歧），
        // 保证草稿里的提及可被 resolveMentionTarget 命中
        const label = mentionLabelForChat(chat);
        // 语义同 mentionSession：保留式追加 @ 标签（已有内容空格分隔）；
        // `#find ...` 命令残留一并清掉（find 分支不消费草稿，激活成功后重建）。
        setDraft((d) => {
          const kept = d.replace(/#find(\s+\S+)?/g, '').trim();
          if (!label) return kept;
          return kept ? `${kept} @${label} ` : `@${label} `;
        });
        setFindOpen(false);
        inputRef.current?.focus();
      })
      .catch((e: unknown) => {
        useAppStore
          .getState()
          .notify(t('ui.hub.find_activate_failed', { err: backendError(e) }), 'error');
      });
  };

  // jumpToSession 跳到该会话所属项目页签并定位其对话页：openTab 激活工作区
  // （不在工作区列表时手工构造，与 WorkspaceTab 的 SSH 兜底同构），
  // 再经 requestFocusTerm(chat.Key) 让该工作区中心区切到会话页签；
  // Key 缺失（无从定位）时只开页签。
  const jumpToSession = (c: ChatInfo) => {
    const st = useAppStore.getState();
    const ws = st.workspaces.find((w) => sameWorkspacePath(w.Path, c.Workspace));
    st.openTab(
      ws ?? {
        Path: c.Workspace,
        Name: wsBaseName(c.Workspace),
        LastUsed: '',
        SessionCount: 0,
        ToolCounts: {},
        Source: 'manual',
      } satisfies Workspace,
    );
    if (c.Key) st.requestFocusTerm(c.Key);
  };

  // 输入框拖放：接收浮动面板拖出的 @ 目标，追加进草稿（已有内容时空格分隔）。
  // 需求拖拽（REQUIREMENT_MIME）跳过：那是面板级 drop 目标的职责，
  // 不能把需求文本粘进输入框草稿（同一拖拽事件会同时命中两层）。
  const handleDrop = (e: React.DragEvent<HTMLTextAreaElement>) => {
    if (e.dataTransfer.types.includes(REQUIREMENT_MIME)) return;
    e.preventDefault();
    const text = e.dataTransfer.getData('text/plain');
    if (!text) return;
    setDraft((d) => (d ? `${d.trimEnd()} ${text}` : text));
  };

  // 面板级需求拖放：RequirementList 拖出的需求条目投递到需求自身 project。
  // dragOver 校验 MIME 后 preventDefault（允许 drop）并高亮面板；drop 解析
  // payload 调 dispatchRequirement（带上默认设置），成功/失败均提示。
  const [reqDragOver, setReqDragOver] = useState(false);

  // runSharedCommand 把输入透传给 Go 侧共享命令层（微信/桌面端同源），
  // 回复逐条作为本地系统提示渲染进 hub 消息流（不落盘、不投给 agent）。
  const runSharedCommand = (text: string) => {
    setDraft('');
    void hubCommand(text)
      .then((replies) => {
        for (const r of replies) useAppStore.getState().pushHubLocal(r.Text);
      })
      .catch((e: unknown) => useAppStore.getState().notify(backendError(e), 'error'));
  };

  const handleRequirementDragOver = (e: React.DragEvent<HTMLElement>) => {
    if (!e.dataTransfer.types.includes(REQUIREMENT_MIME)) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'copy';
    setReqDragOver(true);
  };
  const handleRequirementDrop = (e: React.DragEvent<HTMLElement>) => {
    if (!e.dataTransfer.types.includes(REQUIREMENT_MIME)) return;
    e.preventDefault();
    setReqDragOver(false);
    let payload: { id?: string; text?: string; project?: string };
    try {
      payload = JSON.parse(e.dataTransfer.getData(REQUIREMENT_MIME)) as { id?: string; text?: string; project?: string };
    } catch {
      return;
    }
    const { id, text, project } = payload;
    // project 缺失（旧 payload 或无 project 存量条目）：无处投递，直接失败提示
    if (!id || !text || !project) {
      useAppStore.getState().notify(t('ui.requirements.dispatch_failed', { err: t('ui.requirements.no_project_drag_hint') }), 'error');
      return;
    }
    void dispatchRequirement(id, text, project, loadDefaultCreate() ?? undefined)
      .then((res) => {
        // 在途守卫命中：同一需求已有一次派发进行中，本次未执行，仅提示
        if (res === 'busy') {
          useAppStore.getState().notify(t('ui.requirements.dispatch_busy'), 'error');
          return;
        }
        useAppStore.getState().notify(t('ui.requirements.dispatch_ok', { name: wsBaseName(project) }), 'info');
      })
      .catch((err: unknown) => {
        useAppStore
          .getState()
          .notify(t('ui.requirements.dispatch_failed', { err: backendError(err) }), 'error');
      });
  };

  // activatePending 激活一个 pending 会话（磁盘会话 ID）并登记进 store：
  // send() 的 pending 分支与 #stop/#model 的 pending 处理共用。激活成功返回
  // 新 chat Info（调用方继续对 chat.ID 操作）；失败 toast find_activate_failed
  // 并返回 null（调用方必须停止后续动作，不落模糊匹配误投）。
  const activatePending = async (sessionID: string): Promise<ChatInfo | null> => {
    if (refuseTerminalSession(sessionID)) return null;
    try {
      const res = await openSessionACP(sessionID);
      if (!res.Chat) return null;
      const chat = res.Chat;
      useAppStore.getState().upsertChat(chat);
      useAppStore.getState().setLastAgentId(chat.ID);
      return chat;
    } catch (e: unknown) {
      useAppStore.getState().notify(t('ui.hub.find_activate_failed', { err: backendError(e) }), 'error');
      return null;
    }
  };

  const send = () => {
    const text = draft.trim();
    if (!text) return;
    // @ 标签只供 Hub 路由：命令探测与投递正文都用剥离后的 body，
    // 路由解析 resolveMentionTarget 仍看原始草稿（需要标签才能选目标）。
    const body = stripMentionTags(text, options);
    if (!body) {
      useAppStore.getState().notify(t('ui.hub.empty_message'), 'error');
      return;
    }
    // 纯数字输入：命令层的「编号切换/菜单选择」语义，透传给共享命令层处理
    //（#l/#f 的编号映射在 Go 侧，前端无从判断，统一透传）。
    // 判定用原始草稿：`@目标 3` 是回答 agent 的正文（剥离后恰为 "3"），
    // 不应被误当成菜单编号命令。
    if (isHubNumber(text)) {
      runSharedCommand(text);
      return;
    }
    // 仅当 trim 后以 #（含全角＃）开头才视为 kshell 客户端命令意图；行中 # 词
    // （如颜色 #333、issue #123）原样透传给 agent。命令探测用全角归一化副本
    // （＃/全角空格/冒号/数字，与 Go 侧 normalizeInput 同口径），原文透传不改写。
    // 取首词校验，未知命令提示并保留草稿。
    const probe = normalizeHubProbe(body);
    if (probe.startsWith('#')) {
      const cmdToken = probe.split(/\s+/)[0] ?? '';
      const name = cmdToken.slice(1).toLowerCase();
      // 旧命令表优先命中，行为零改动；未命中再查共享命令层新别名
      //（#? #l #la #en #n #s #m #g #f #h #st #c），仍未知才提示。
      if (!HUB_COMMAND_NAMES.has(name) && !isNewHubCommand(probe)) {
        useAppStore.getState().notify(t('ui.hub.unknown_cmd', { cmd: cmdToken }), 'error');
        return;
      }
      if (!HUB_COMMAND_NAMES.has(name)) {
        runSharedCommand(body);
        return;
      }
      // #find：不透传给 agent。带关键词（`#find claude`）时以剩余文本为初始过滤词
      // 打开对话框；裸 `#find` 由下方单 token 宽松执行路径处理（关键词为空）。
      if (name === 'find' && probe !== cmdToken) {
        setDraft('');
        // 探测归一化是逐字符等长替换，按原文字符串切片保持关键词原样
        openFindDialog(body.slice(cmdToken.length).trim());
        return;
      }
      // 整体就是单个已知命令 token（尾空格已被 trim 掉）：宽松执行而非投递，
      // 避免 #stop 因补全未弹而裸发给 agent。
      if (probe === cmdToken) {
        runClientCommand(name);
        return;
      }
    }
    // 用户主动发消息即视为已接管，清掉 /easynew 的进行中提示
    setEasynewRunning(false);
    // 回落链与 currentTargetId 共用 fallbackId（见上）。
    const targetId = resolveMentionTarget(text, options, fallbackId);
    if (!targetId) {
      // 无精确目标：把正文交给共享命令层列会话并暂存，用户回复数字后由
      // HubCommand（数字透传路径）完成激活与发送；微信桥同语义。仅当后端
      // 确实建立了待选菜单才清草稿（无会话/其他菜单进行中时保留正文）；
      // 等待期间用户若改了草稿，也不覆盖新内容。
      void hubPickTarget(text)
        .then((view) => {
          if (view.Pending) {
            // 原话先于菜单即时可见（本地瞬态；权威落盘在数字选中后的发送链）。
            useAppStore.getState().pushHubLocalUser(text);
          }
          for (const r of view.Replies) useAppStore.getState().pushHubLocal(r.Text);
          if (view.Pending) setDraft((d) => (d.trim() === text ? '' : d));
        })
        .catch((e: unknown) => useAppStore.getState().notify(backendError(e), 'error'));
      return;
    }
    // pending: 目标 = 最近交互的会话未激活（chats 查不到），但有磁盘会话 ID 快照：
    // 先 openSessionACP 激活原会话，再向新 chat 投递。激活失败报错不投递——
    // 上下文丢失必须可见，绝不落 workspace 模糊匹配误投同项目会话。
    if (targetId.startsWith(PENDING_PREFIX)) {
      const sessionID = targetId.slice(PENDING_PREFIX.length);
      // 激活前再查一次终端在册（渲染与点击之间终端可能已打开）：终端激活态会话
      // 不允许从 Hub 抢激活（会与终端 PTY 形成双写方），提示后不投递、不清草稿。
      if (refuseTerminalSession(sessionID)) return;
      setDraft('');
      void activatePending(sessionID).then((chat) => {
        if (!chat) return;
        // 激活与发送分开 catch：失败阶段不同，文案不同（激活失败 vs 发送失败），
        // 混在一个 catch 里会把投递失败误报成「激活会话失败」误导排查。
        void sendChatPrompt(chat.ID, body)
          .then(() => {
            useAppStore.getState().setLastAgentId(chat.ID);
          })
          .catch((e: unknown) => {
            useAppStore.getState().notify(t('ui.chat.send_failed', { err: backendError(e) }), 'error');
          });
      });
      return;
    }
    setDraft('');
    // 用户消息由 Go 侧落盘并经 hub:changed 回流（单一数据源），失败只提示不回滚。
    void sendChatPrompt(targetId, body)
      .then(() => {
        useAppStore.getState().setLastAgentId(targetId);
      })
      .catch((e: unknown) => {
        useAppStore.getState().notify(t('ui.chat.send_failed', { err: backendError(e) }), 'error');
      });
  };

  // quickTarget 从 agent 卡片归因解析目标 chat ID：chatId 优先，termKey 反查兜底。
  // 只读消息（非 ACP 工具/终端激活态）返回 null：快捷按钮已隐藏，防御误调用。
  const quickTarget = (m: HubMessage): string | null => {
    if (replyState(m, tools, terminals) !== 'acp') return null;
    if (m.chatId && chats.some((c) => c.ID === m.chatId)) return m.chatId;
    if (m.termKey) {
      const c = chats.find((x) => x.Key === m.termKey);
      if (c) return c.ID;
    }
    return null;
  };

  // resolveHubSessionId 从 hub 记录解析磁盘会话 ID（目标未激活时精确 resume 用）。
  // 来源见 resolveHubSessionIdFromSessions（含 Cursor acp-sessions 时间邻近兜底）。
  // 已有 sessionId / session: termKey 时不打 getSessions；否则读扫描缓存，失败当空列表。
  const resolveHubSessionId = async (m: HubMessage): Promise<string | null> => {
    if (m.sessionId) return m.sessionId;
    if (m.termKey?.startsWith('session:')) return m.termKey.slice('session:'.length);
    try {
      const sessions = await getSessions();
      return resolveHubSessionIdFromSessions(m, sessions);
    } catch {
      return resolveHubSessionIdFromSessions(m, []);
    }
  };

  // activateAndThen 目标会话未激活时的「先激活再处理」通用路径：解析磁盘会话 ID →
  // openSessionACP 激活（Go 侧对已退出会话自动重建）→ 登记进 store → 回调 onReady
  // 交给调用方决定后续动作（发送 / 填 @ 标签）。解析不到 / 激活失败都报错提示，
  // 不把消息投给空白会话（上下文丢失必须可见）。
  const activateAndThen = (m: HubMessage, onReady: (chat: ChatInfo) => void | Promise<void>) => {
    void resolveHubSessionId(m)
      .then(async (sessionID) => {
        if (!sessionID) {
          useAppStore.getState().notify(t('ui.hub.need_target'), 'error');
          return;
        }
        if (refuseTerminalSession(sessionID)) return;
        const res = await openSessionACP(sessionID);
        if (!res.Chat) return;
        const chat = res.Chat;
        useAppStore.getState().upsertChat(chat);
        useAppStore.getState().setLastAgentId(chat.ID);
        await onReady(chat);
        useAppStore.getState().setLastAgentId(chat.ID);
      })
      .catch((e: unknown) => {
        useAppStore.getState().notify(t('ui.hub.find_activate_failed', { err: backendError(e) }), 'error');
      });
  };

  // quickReply 处理快捷回复点击：
  // - send：把 quick_continue_text（实际文本，与按钮文案解耦）发给归因 agent；
  //   目标未激活（chats 反查不到，如会话已关闭/重启后未恢复）时先激活再发送；
  // - rephrase：保留式追加 @目标 到输入框（不清空用户正在输入的草稿），聚焦由用户
  //   补写后手动发送；目标未激活时同样先激活，激活后填新会话的 @ 标签，不自动发送。
  // 只读态（非 ACP / 终端占用）直接 return：快捷按钮应已隐藏；勿把 quickTarget
  // 的 null 误当成「待激活」去 openSessionACP。
  // useCallback：稳定引用交给 memo(HubMessageList)，draft 重渲时不拖消息树。
  const quickReply = useCallback(
    (m: HubMessage, action: HubQuickAction) => {
      // 欢迎词无任务可续：按钮应已隐藏；防御误调用勿 sendChatPrompt。
      if (isHubWelcome(m.text)) return;
      if (replyState(m, tools, terminals) !== 'acp') return;
      const targetId = quickTarget(m);
      if (!targetId) {
        if (action === 'send') {
          const text = t('ui.hub.quick_continue_text');
          activateAndThen(m, async (chat) => {
            await sendChatPrompt(chat.ID, text);
          });
          return;
        }
        // rephrase：激活成功后填 @ 标签。走到此分支说明点击时目标未激活，闭包里的
        // options 必不含新 chat，按实时 store 的全量列表构造（同名消歧与补全一致）。
        activateAndThen(m, (chat) => {
          const label = mentionLabelForChat(chat);
          if (!label) return;
          setDraft((d) => (d.trim() ? `${d.trimEnd()} @${label} ` : `@${label} `));
          inputRef.current?.focus();
        });
        return;
      }
      if (action === 'send') {
        void sendChatPrompt(targetId, t('ui.hub.quick_continue_text'))
          .then(() => {
            useAppStore.getState().setLastAgentId(targetId);
          })
          .catch((e: unknown) => {
            useAppStore.getState().notify(t('ui.chat.send_failed', { err: backendError(e) }), 'error');
          });
        return;
      }
      // rephrase：从 mention 候选反查展示标签（会话标题）；找不到兜底提示。
      const label = options.find((o) => o.id === targetId)?.insert;
      if (!label) {
        useAppStore.getState().notify(t('ui.hub.need_target_drag'), 'error');
        return;
      }
      setDraft((d) => (d.trim() ? `${d.trimEnd()} @${label} ` : `@${label} `));
      inputRef.current?.focus();
    },
    // quickTarget / activateAndThen / mentionLabelForChat 闭包随 render；依赖会随
    // chats/tools/terminals/options/t 变化——消息有更新时本应刷新回调，draft 不变这些时稳定。
    // eslint-disable-next-line react-hooks/exhaustive-deps -- 与既有闭包语义对齐，避免无谓扩依赖
    [tools, terminals, options, t, chats],
  );

  // recall 撤回排队中的用户消息：后端真移除 + 标记已撤回；乐观更新，hub:changed 回流兜底。
  const recall = useCallback((m: HubMessage) => {
    void recallHubMessage(m.id)
      .then(() => {
        useAppStore
          .getState()
          .setHubMessages(useAppStore.getState().hubMessages.map((x) => (x.id === m.id ? { ...x, status: 'recalled' } : x)));
        useAppStore.getState().notify(t('ui.hub.recall_done'), 'info');
      })
      .catch((e: unknown) => {
        useAppStore.getState().notify(t('ui.hub.recall_failed', { err: backendError(e) }), 'error');
      });
  }, [t]);

  // openHistory 展开归档入口：拉月份列表。
  const openHistory = () => {
    const next = !historyOpen;
    setHistoryOpen(next);
    if (!next) return;
    void listHubArchive()
      .then(setArchiveMonths)
      .catch(() => setArchiveMonths([]));
  };

  // selectArchiveMonth 加载某月归档消息内联展示（与 live 分开，避免重复与乱序）。
  const selectArchiveMonth = (month: string) => {
    setActiveMonth(month);
    void loadHubArchive(month)
      .then(setArchiveMessages)
      .catch(() => setArchiveMessages([]));
  };

  // 粘贴：优先用剪贴板里的落盘路径（图片），其次文本；路径含空格加引号避免 shell 拆词
  const handlePaste = async () => {
    try {
      const paste = await readClipboardPaste();
      const add = paste.Path ? quotePathForShell(paste.Path) : paste.Text;
      if (!add) return;
      setDraft((d) => (d ? `${d} ${add}` : add));
    } catch {
      // 读取失败静默：粘贴是辅助操作，不打断输入
    }
  };

  return (
    <section
      aria-label={t('ui.hub.title')}
      onDragOver={handleRequirementDragOver}
      onDragLeave={(e) => {
        // 只在离开面板边界时取消高亮（子元素间移动会触发 dragLeave，用 contains 过滤）
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setReqDragOver(false);
      }}
      onDrop={handleRequirementDrop}
      data-testid="hub-panel"
      className={cn(
        'relative flex min-h-0 flex-1 flex-col gap-2.5 rounded border border-border bg-card p-3 text-sm',
        reqDragOver && 'ring-2 ring-primary/40',
      )}
    >
      <div className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <h2 className="shrink-0 text-sm font-medium">{t('ui.hub.title')}</h2>
          <Button
            size="icon"
            variant="ghost"
            className="h-6 w-6 shrink-0 text-muted-foreground"
            aria-label={t('ui.hub.help_aria')}
            title={t('ui.hub.help_aria')}
            onClick={() => setHelpOpen(true)}
          >
            <CircleHelp aria-hidden className="h-3.5 w-3.5" />
          </Button>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {/* 默认设置入口：设置默认 agent 工具/模型（供 easynew 与需求派发使用） */}
          <Button
            size="sm"
            variant="outline"
            aria-label={t('ui.hub.default_settings')}
            onClick={() => setDefaultsOpen(true)}
          >
            <Settings2 aria-hidden className="h-3.5 w-3.5" />
          </Button>
          <Button
            size="sm"
            variant="outline"
            aria-label={t('ui.hub.history')}
            onClick={openHistory}
          >
            {t('ui.hub.history')}
          </Button>
          <Button
            size="sm"
            variant="secondary"
            aria-label={t('ui.hub.new_agent')}
            onClick={openNewAgentDialog}
          >
            + {t('ui.hub.new_agent')}
          </Button>
        </div>
      </div>

      {historyOpen && (
        // 归档区限高自滚：不挤占消息流与输入区的固定布局
        <div className="flex max-h-40 shrink-0 flex-col gap-1 overflow-y-auto rounded border border-border bg-card p-2 text-xs">
          <span className="text-muted-foreground">{t('ui.hub.archive_title')}</span>
          {archiveMonths.length === 0 ? (
            <span className="text-muted-foreground">{t('ui.hub.archive_empty')}</span>
          ) : (
            <ul className="m-0 flex list-none flex-wrap gap-1 p-0">
              {archiveMonths.map((m) => (
                <li key={m.month}>
                  <button
                    type="button"
                    className={cn(
                      'rounded border border-border px-2 py-0.5 hover:bg-muted/50',
                      activeMonth === m.month && 'bg-muted font-medium',
                    )}
                    onClick={() => selectArchiveMonth(m.month)}
                  >
                    {m.month} · {m.count}
                  </button>
                </li>
              ))}
            </ul>
          )}
          {archiveMessages.length > 0 && (
            <div className="mt-1 flex flex-col gap-1 border-t border-border pt-1">
              {archiveMessages.map((m) => {
                const attr = attrOf(m);
                // 与 live 同口径：用户消息纯文本，agent 回复带归因 + markdown
                return (
                  <div key={`a${m.id}`} className="text-muted-foreground">
                    <div className="text-[11px]">
                      {[m.kind === 'user' ? '' : toolDisplayName(m.tool ?? ''), attr]
                        .filter(Boolean)
                        .join(' · ')}
                    </div>
                    {m.kind === 'user' ? (
                      <span className="whitespace-pre-wrap">{userBubbleText(m.text)}</span>
                    ) : (
                      <div className={cn(markdownBubbleClass, '[&_pre]:whitespace-pre-wrap')}>
                        <ReactMarkdown remarkPlugins={[remarkGfm]}>{translateBackend(m.text)}</ReactMarkdown>
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          )}
        </div>
      )}

      {/* 水平内边距避免气泡贴边/挤滚动条；gap-3 拉开卡片垂直间距。
          消息流独立 memo：draft 变化不重跑 ReactMarkdown。 */}
      <div ref={logRef} className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto px-3 py-2" role="log" aria-live="polite">
        <HubMessageList tools={tools} onRecall={recall} onQuickReply={quickReply} />
      </div>

      <div data-testid="hub-input-area" className="flex flex-col gap-1 border-t border-border pt-2">
        {(easynewRunning || hubBusy) && (
          <div className="flex items-center gap-1.5 rounded border border-border bg-card px-2 py-1 text-xs text-muted-foreground">
            <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />
            <span>{hubBusy ? t('ui.hub.starting_agent') : t('ui.hub.easynew_running')}</span>
          </div>
        )}
        {mentionMatches.length > 0 && !mentionClosed && (
          // 结构与 # 弹层对齐：role=listbox/option + 键盘高亮（bg-muted），列表限高自滚
          <ul
            ref={mentionListRef}
            role="listbox"
            aria-label={t('ui.hub.mention_aria')}
            className="m-0 flex max-h-48 list-none flex-col gap-0.5 overflow-y-auto rounded border border-border bg-card p-1 text-xs"
          >
            {mentionMatches.map((o, i) => (
              <li
                key={o.id}
                role="option"
                aria-selected={i === mentionIndex}
                className={cn(
                  'cursor-pointer rounded px-2 py-1 hover:bg-muted/50',
                  i === mentionIndex && 'bg-muted',
                )}
                onMouseDown={(e) => {
                  e.preventDefault();
                  applyMention(o.insert);
                }}
              >
                <span className="block">@{o.insert}</span>
                {/* 第二行：项目 · 会话标题（insert 已是会话标题，详情补充项目） */}
                {o.detail && <span className="block truncate text-muted-foreground">{o.detail}</span>}
              </li>
            ))}
          </ul>
        )}
        {modelMenu && (
          <div className="rounded border border-border bg-card p-1 text-xs">
            <div className="flex items-center justify-between px-2 py-0.5 text-muted-foreground">
              <span className="truncate">
                {t('ui.hub.model_label')}
                {options.find((o) => o.id === modelMenu.targetId)?.insert
                  ? ` · ${options.find((o) => o.id === modelMenu.targetId)?.insert}`
                  : ''}
              </span>
              <button
                type="button"
                aria-label={t('ui.hub.cancel')}
                className="text-muted-foreground hover:text-destructive"
                onClick={() => setModelMenu(null)}
              >
                ×
              </button>
            </div>
            <ul aria-label={t('ui.hub.model_label')} className="m-0 flex list-none flex-col gap-0.5 p-0">
              {modelMenu.view.Models.map((m) => (
                <li key={m.ID}>
                  <button
                    type="button"
                    className={cn(
                      'w-full rounded px-2 py-1 text-left hover:bg-muted/50',
                      m.ID === modelMenu.view.Current && 'font-medium',
                    )}
                    onClick={() => switchModel(m.ID)}
                  >
                    {m.Name}
                    {m.Description && <span className="ml-2 text-muted-foreground">{m.Description}</span>}
                  </button>
                </li>
              ))}
            </ul>
          </div>
        )}
        {cmdOpen && (
          <ul
            ref={cmdListRef}
            role="listbox"
            aria-label={t('ui.hub.cmd_aria')}
            className="mb-1 max-h-48 overflow-y-auto rounded border border-border bg-card py-1 text-xs shadow-md"
          >
            {cmdMatches.map((c, i) => (
              <li
                key={`${c.kind}:${c.name}`}
                role="option"
                aria-selected={i === slashIndex}
                className={cn('cursor-pointer px-2 py-1', i === slashIndex && 'bg-muted')}
                onMouseDown={(e) => {
                  e.preventDefault();
                  applySlash(c);
                }}
              >
                {/* hash 模式列 # 命令，slash 模式列 / 技能（前缀随模式切换） */}
                <span className="font-medium">{inHashMode ? '#' : '/'}{c.name}</span>
                {c.desc && <span className="ml-2 text-muted-foreground">{c.desc}</span>}
              </li>
            ))}
          </ul>
        )}
        {/* 会话 chips 平铺：每个会话一枚，点击主体即 @ 该会话（原 @ 图标行为并入主体）；
            默认接收目标（下一句话的投递对象，@ 提及优先于回落链）高亮标注。
            flex-wrap 超宽自动换行继续平铺；拖拽 chip 仍可把 @ 标签放进输入框。
            容器限高自滚（同归档区口径）：会话多时约两行后滚动，不挤压消息流。 */}
        <div
          data-testid="hub-session-chips"
          className="flex max-h-20 shrink-0 flex-wrap items-center gap-1 overflow-y-auto"
        >
          {/* 无会话时不渲染空态提示（2026-10-20 用户反馈）：留空容器即可，chips 有会话才出现 */}
          {chats.map((c) => {
              const busy = c.Status === 'running' || c.Status === 'starting' || c.Status === 'authenticating';
              const isTarget = c.ID === currentTargetId;
              return (
                <div
                  key={c.ID}
                  draggable
                  onDragStart={(e) => {
                    e.dataTransfer.setData('text/plain', `@${sessionDragLabel(c)}`);
                    e.dataTransfer.effectAllowed = 'copy';
                  }}
                  className={cn(
                    LIST_ROW,
                    // 默认接收目标高亮：下一句话将投给该会话，需一眼可辨
                    isTarget ? 'border-primary/60 bg-primary/10' : '',
                    'flex max-w-[21rem] min-w-0 items-center gap-1 rounded-full px-2.5 py-1 text-xs',
                  )}
                >
                  <button
                    type="button"
                    title={t('ui.hub.mention_session')}
                    // 点击主体是「追加 @ 标签」（可连点），非切换，故不用 toggle 语义；
                    // 高亮态用 aria-current 表达（可见文本已足够读屏，不再叠加 aria-label）
                    aria-current={isTarget ? 'true' : undefined}
                    className="flex min-w-0 flex-1 items-center gap-1.5 text-left"
                    onClick={() => mentionSession(c)}
                  >
                    <span
                      aria-hidden
                      className={cn('h-1.5 w-1.5 shrink-0 rounded-full', statusDotClass(c.Status))}
                    />
                    <span className="min-w-0 truncate font-medium">
                      {/* 始终带项目前两字前缀，跨项目同名会话可一眼区分 */}
                      {chipSessionLabel(c.Title, c.Workspace, emptyTarget)}
                    </span>
                    <span className="shrink-0" title={toolDisplayName(c.ToolID)}>
                      <ToolIcon toolID={c.ToolID} label={toolDisplayName(c.ToolID)} />
                    </span>
                  </button>
                  {/* 跳转：开项目页签定位该会话对话页。与 × 同为行内独立 button，无冒泡联动 */}
                  <button
                    type="button"
                    aria-label={t('ui.hub.jump_session')}
                    className="shrink-0 text-muted-foreground hover:text-foreground"
                    onClick={() => jumpToSession(c)}
                  >
                    <ArrowUpRight aria-hidden className="h-3.5 w-3.5" />
                  </button>
                  {/* 空闲会话才可关闭（忙碌 agent 正在跑任务） */}
                  {!busy && (
                    <button
                      type="button"
                      aria-label={t('ui.hub.close_session')}
                      className="shrink-0 text-muted-foreground hover:text-destructive"
                      onClick={() => closeSession(c.ID)}
                    >
                      ×
                    </button>
                  )}
                </div>
              );
            })}
          {/* 待激活 chip：默认投递目标命中未激活会话（pending: 前缀）时出现，
              虚线边框区分已激活 chips；点击先激活原会话再 @ 填入（mentionPending）。
              无已激活会话时也可出现：空状态下用户仍能看到待激活目标。 */}
          {pendingTarget && (
            <div className="flex max-w-[21rem] min-w-0 items-center gap-1 rounded-full border border-dashed border-primary/60 bg-primary/10 px-2.5 py-1 text-xs">
              <button
                type="button"
                title={t('ui.hub.pending_chip_aria')}
                aria-current="true"
                className="flex min-w-0 flex-1 items-center gap-1.5 text-left"
                onClick={mentionPending}
              >
                <span aria-hidden className="h-1.5 w-1.5 shrink-0 rounded-full bg-muted-foreground/40" />
                <span className="min-w-0 truncate font-medium">
                  {chipSessionLabel(pendingTarget.title, pendingTarget.workspace)}
                  <span className="ml-1 text-[11px] font-normal text-muted-foreground">
                    {t('ui.hub.pending_chip')}
                  </span>
                </span>
                {pendingTarget.tool && (
                  <span className="shrink-0" title={toolDisplayName(pendingTarget.tool)}>
                    <ToolIcon toolID={pendingTarget.tool} label={toolDisplayName(pendingTarget.tool)} />
                  </span>
                )}
              </button>
            </div>
          )}
        </div>
        <textarea
          ref={inputRef}
          className="min-h-16 w-full resize-none rounded border border-border bg-card p-2 text-sm"
          placeholder={t('ui.hub.input_placeholder')}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onPaste={(e) => {
            // 拦截原生粘贴：文本/图片路径统一走 handlePaste（后端读剪贴板），
            // 否则原生插入与 handlePaste 追加各写一遍，草稿出现重复文字
            e.preventDefault();
            void handlePaste();
          }}
          onDragOver={(e) => e.preventDefault()}
          onDrop={handleDrop}
          onKeyDown={(e) => {
            if (cmdOpen && cmdMatches.length > 0) {
              if (e.key === 'ArrowDown') {
                e.preventDefault();
                setSlashIndex((i) => Math.min(i + 1, cmdMatches.length - 1));
                return;
              }
              if (e.key === 'ArrowUp') {
                e.preventDefault();
                setSlashIndex((i) => Math.max(i - 1, 0));
                return;
              }
              if (e.key === 'Enter' || e.key === 'Tab') {
                e.preventDefault();
                applySlash(cmdMatches[Math.min(slashIndex, cmdMatches.length - 1)]);
                return;
              }
              if (e.key === 'Escape') {
                e.preventDefault();
                setSlashClosed(true);
                return;
              }
            }
            // @ 补全键盘导航（与 # 弹层同模型）；Enter/Tab 填入高亮项，Esc 关闭后
            // 恢复正常发送语义。两弹层按草稿形态互斥（#x 单 token vs @y 尾片段）。
            if (mentionMatches.length > 0 && !mentionClosed) {
              if (e.key === 'ArrowDown') {
                e.preventDefault();
                setMentionIndex((i) => Math.min(i + 1, mentionMatches.length - 1));
                return;
              }
              if (e.key === 'ArrowUp') {
                e.preventDefault();
                setMentionIndex((i) => Math.max(i - 1, 0));
                return;
              }
              if (e.key === 'Enter' || e.key === 'Tab') {
                e.preventDefault();
                applyMention(mentionMatches[Math.min(mentionIndex, mentionMatches.length - 1)].insert);
                return;
              }
              if (e.key === 'Escape') {
                e.preventDefault();
                setMentionClosed(true);
                return;
              }
            }
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault();
              send();
            }
          }}
        />
        {/* 发送行：路由模式 + 自动滚动开关放发送按钮左侧 */}
        <div className="flex items-center justify-end gap-2">
          <label className="flex items-center gap-1 text-[11px] text-muted-foreground">
            {t('ui.hub.route_mode_label')}
            <select
              data-testid="hub-route-mode"
              className="rounded border border-border bg-card px-1 py-0.5 text-[11px]"
              value={routeMode}
              onChange={(e) => changeRouteMode(e.target.value as HubRouteMode)}
            >
              <option value="auto">{t('ui.hub.route_mode_auto')}</option>
              <option value="agent">{t('ui.hub.route_mode_agent')}</option>
              <option value="force_auto">{t('ui.hub.route_mode_force_auto')}</option>
            </select>
          </label>
          <label className="flex cursor-pointer items-center gap-1 text-[11px] text-muted-foreground">
            <input
              type="checkbox"
              data-testid="hub-auto-scroll"
              checked={autoScroll}
              onChange={toggleAutoScroll}
            />
            {t('ui.hub.auto_scroll')}
          </label>
          <Button size="sm" onClick={send}>
            {t('ui.hub.send')}
          </Button>
        </div>
      </div>

      <NewAgentDialog open={dialogOpen} onOpenChange={setDialogOpen} />
      <DefaultSettingsDialog open={defaultsOpen} onOpenChange={setDefaultsOpen} />
      <HelpDialog open={helpOpen} onOpenChange={setHelpOpen} />
      <FindSessionDialog
        open={findOpen}
        initialQuery={findQuery}
        onClose={() => setFindOpen(false)}
        onPick={pickFoundSession}
      />
    </section>
  );
}

// NewAgentDialog：单窗体新建。项目/工具/模型三字段同屏，一次点击完成创建
// （session/new 不接受模型参数，模型在 runAgentCreate 里建会话后再 SetModel）。
// 打开时按 lastCreate 预填；模型下拉合并 kshell 注入模型（设置页 model.models，
// 打开时拉 getModelConfig）与本地缓存（loadToolModels），空值即「默认模型」。
function NewAgentDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation();
  const workspaces = useAppStore((s) => s.workspaces);
  const [tools, setTools] = useState<ToolInfo[]>([]);
  const [wsPath, setWsPath] = useState('');
  const [toolId, setToolId] = useState('');
  const [busy, setBusy] = useState(false);
  const [modelId, setModelId] = useState('');
  // kshell 注入模型清单：打开时拉取，失败静默（下拉是辅助功能，回落纯缓存）
  // name 带平台展示前缀，id 仍为裸模型名（SetModel 用）。
  const [injectedModels, setInjectedModels] = useState<ToolModelOption[]>([]);
  // seq 使关闭后在途的 create 失效，避免其 setState 污染下次打开。
  const seqRef = useRef(0);
  // 记录上次 open 值：预填只在 open 从 false→true 时执行，
  // 对话框开着期间 workspaces 后台刷新不应把用户已选字段重置。
  const prevOpenRef = useRef(false);

  // registerChat 把已建会话登记进 store（幂等：upsert 以 ID 去重）。
  const registerChat = (chat: ChatInfo) => {
    useAppStore.getState().upsertChat(chat);
    useAppStore.getState().setLastAgentId(chat.ID);
  };

  // 打开对话框时才拉工具列表（避免常驻首页也发请求），并按 lastCreate 预填。
  // 预填校验：wsPath 仍在工作区列表、toolId 仍在可用工具列表，失效置空该字段（回落默认选中）。
  // 仅在 open 从 false→true 时跑一次；开着期间 workspaces 刷新不重跑预填。
  useEffect(() => {
    const opening = open && !prevOpenRef.current;
    prevOpenRef.current = open;
    if (!opening) return;
    const last = loadLastCreate();
    void getTools()
      .then((list) => {
        const acp = list.filter((x) => x.ACP?.Available === true);
        setTools(acp);
        setToolId((cur) => {
          if (cur && acp.some((x) => x.ID === cur)) return cur;
          if (last && acp.some((x) => x.ID === last.toolId)) return last.toolId;
          return acp[0]?.ID ?? '';
        });
      })
      .catch(() => {});
    // kshell 注入模型（设置页默认平台的模型列表）：拉取失败静默，回落仅缓存
    void getModelConfig()
      .then((cfg) => {
        const def = cfg.Platforms?.find((p) => p.ID === cfg.DefaultPlatform) ?? cfg.Platforms?.[0];
        const platName = def?.Name ?? '';
        const opts: ToolModelOption[] = [];
        for (const raw of def?.Models ?? []) {
          const id = typeof raw === 'string' ? raw.trim() : '';
          if (!id) continue;
          opts.push({ id, name: formatPlatformModelName(platName, id) });
        }
        setInjectedModels(opts);
      })
      .catch(() => setInjectedModels([]));
    setWsPath((cur) => {
      if (cur && workspaces.some((w) => sameWorkspacePath(w.Path, cur))) return cur;
      if (last && workspaces.some((w) => sameWorkspacePath(w.Path, last.wsPath))) return last.wsPath;
      return workspaces[0]?.Path ?? '';
    });
    // 模型预选：lastCreate.modelId 在该工具的缓存模型列表里才预选，否则空（默认模型）
    if (last?.modelId && loadToolModels(last.toolId).includes(last.modelId)) {
      setModelId(last.modelId);
    } else {
      setModelId('');
    }
  }, [open, workspaces]);

  // 关闭时重置内部状态；runAgentCreate 已把会话登记进 store，这里只做清场。
  useEffect(() => {
    if (open) return;
    seqRef.current++; // 使关闭后在途的 create 失效
    setBusy(false);
  }, [open]);

  // 卸载兜底：面板被整个卸载（切页/测试清理）时释放模块级互斥锁，
  // 覆盖创建长挂/测试隔离场景；常规释放路径是创建 Promise 自己的 finally。
  useEffect(() => {
    return () => {
      createPromise = null;
    };
  }, []);

  // create 一次点完：busy → runAgentCreate（建会话+记默认值+缓存模型+按需 SetModel）→ 关窗。
  const create = async () => {
    if (!wsPath || !toolId || busy) return;
    setBusy(true);
    const seq = seqRef.current;
    const ok = await runAgentCreate(wsPath, toolId, modelId || null, { t, registerChat, onClose: () => onOpenChange(false) });
    if (seq === seqRef.current) setBusy(false);
    // 关窗也带 seq 守卫：对话框被关闭重开后，在途的 create 不得关掉新窗体
    if (ok && seq === seqRef.current) onOpenChange(false);
  };

  // 模型下拉选项：kshell 注入模型在前、工具缓存模型在后，按 id 去重；文案用 name。
  // Cursor 不合并平台注入（ACP 不支持该通道）。渲染期同步读 localStorage。
  const modelOptions = mergeHubModelOptions(
    hubInjectedModelsForTool(toolId, injectedModels),
    toolId ? loadToolModelOptions(toolId) : [],
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange} className="w-80">
      <DialogPrimitive.Title className="mb-3 text-sm font-medium">{t('ui.hub.new_agent_title')}</DialogPrimitive.Title>
      <div className={cn('relative', busy && 'pointer-events-none')}>
        <label className="mb-2 flex flex-col gap-1 text-xs text-muted-foreground">
          {t('ui.hub.field_project')}
          <select
            className="rounded border border-border bg-card px-2 py-1 text-sm text-foreground"
            value={wsPath}
            onChange={(e) => setWsPath(e.target.value)}
            disabled={busy}
          >
            {workspaces.map((w) => (
              <option key={w.Path} value={w.Path}>
                {w.Name}
              </option>
            ))}
          </select>
        </label>
        <label className="mb-2 flex flex-col gap-1 text-xs text-muted-foreground">
          {t('ui.hub.field_tool')}
          <select
            className="rounded border border-border bg-card px-2 py-1 text-sm text-foreground"
            value={toolId}
            onChange={(e) => {
              setToolId(e.target.value);
              // 工具切换即刷新模型下拉；原选中项在新工具缓存里不存在则回落默认
              setModelId((cur) => (cur && loadToolModels(e.target.value).includes(cur) ? cur : ''));
            }}
            disabled={busy}
          >
            {tools.map((x) => (
              <option key={x.ID} value={x.ID}>
                {toolDisplayName(x.ID)}
              </option>
            ))}
          </select>
        </label>
        <label className="mb-4 flex flex-col gap-1 text-xs text-muted-foreground">
          {t('ui.hub.field_model')}
          <select
            className="rounded border border-border bg-card px-2 py-1 text-sm text-foreground"
            value={modelId}
            onChange={(e) => setModelId(e.target.value)}
            disabled={busy}
          >
            {/* 无缓存只显示「默认模型」空选项；有缓存则直接列缓存模型（文案用 name） */}
            {modelOptions.length === 0 ? (
              <option value="">{t('ui.hub.default_model')}</option>
            ) : (
              modelOptions.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name}
                </option>
              ))
            )}
          </select>
        </label>
        {tools.length === 0 && <p className="mb-3 text-xs text-muted-foreground">{t('ui.hub.no_tools')}</p>}
        {/* busy 时半透明 spinner 覆盖层：挡住表单交互；文案只在按钮上，避免同名节点歧义 */}
        {busy && (
          <div
            className="absolute inset-0 flex items-center justify-center rounded bg-card/70 text-muted-foreground"
            aria-label={t('ui.hub.creating')}
          >
            <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
          </div>
        )}
      </div>
      <div className="flex justify-end gap-2">
        <Button size="sm" variant="outline" disabled={busy} onClick={() => onOpenChange(false)}>
          {t('ui.hub.cancel')}
        </Button>
        <Button size="sm" disabled={!wsPath || !toolId || busy} onClick={() => void create()}>
          {busy ? t('ui.hub.creating') : t('ui.hub.create')}
        </Button>
      </div>
    </Dialog>
  );
}

// DefaultSettingsDialog：显式设置「默认 agent 工具 + 模型」，供快速新建（easynew）
// 与需求派发使用。工具下拉仅列 ACP Available 工具；模型下拉数据源与 NewAgentDialog
// 完全一致（kshell 注入模型 + loadToolModels 缓存去重），空值即「默认模型」。
// 打开时按 loadDefaultCreate 预填（失效置空回落）；无已存设置时工具为空、必须选后才能保存。
function DefaultSettingsDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation();
  const [tools, setTools] = useState<ToolInfo[]>([]);
  const [toolId, setToolId] = useState('');
  const [modelId, setModelId] = useState('');
  // kshell 注入模型清单：打开时拉取，失败静默（与 NewAgentDialog 同语义）
  // name 带平台展示前缀，id 仍为裸模型名。
  const [injectedModels, setInjectedModels] = useState<ToolModelOption[]>([]);
  // 预填只在 open 从 false→true 时执行一次，开着期间后台刷新不重置用户已选字段
  const prevOpenRef = useRef(false);

  useEffect(() => {
    const opening = open && !prevOpenRef.current;
    prevOpenRef.current = open;
    if (!opening) return;
    const saved = loadDefaultCreate();
    void getTools()
      .then((list) => {
        const acp = list.filter((x) => x.ACP?.Available === true);
        setTools(acp);
        // 已存工具仍在可用列表才预填，否则空选择（保存按钮禁用兜底）
        setToolId(saved && acp.some((x) => x.ID === saved.toolId) ? saved.toolId : '');
      })
      .catch(() => {});
    void getModelConfig()
      .then((cfg) => {
        const def = cfg.Platforms?.find((p) => p.ID === cfg.DefaultPlatform) ?? cfg.Platforms?.[0];
        const platName = def?.Name ?? '';
        const opts: ToolModelOption[] = [];
        for (const raw of def?.Models ?? []) {
          const id = typeof raw === 'string' ? raw.trim() : '';
          if (!id) continue;
          opts.push({ id, name: formatPlatformModelName(platName, id) });
        }
        setInjectedModels(opts);
      })
      .catch(() => setInjectedModels([]));
    // 模型预选：已存 modelId 在该工具的缓存模型列表里才预选，否则空（默认模型）
    setModelId(saved?.modelId && loadToolModels(saved.toolId).includes(saved.modelId) ? saved.modelId : '');
  }, [open]);

  const save = () => {
    if (!toolId) return;
    saveDefaultCreate({ toolId, modelId: modelId || null });
    // 同步写穿后端 config.yaml（跨端副本）：微信桥 #en 的默认工具读这里。
    // 失败静默：localStorage 仍是桌面端的事实来源，仅跨端能力降级。
    void saveHubDefaultCreate(toolId, modelId || null).catch(() => {});
    onOpenChange(false);
  };

  // 模型下拉选项：与 NewAgentDialog 同口径（Cursor 不合并平台注入）
  const modelOptions = mergeHubModelOptions(
    hubInjectedModelsForTool(toolId, injectedModels),
    toolId ? loadToolModelOptions(toolId) : [],
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange} className="w-80">
      <DialogPrimitive.Title className="mb-3 text-sm font-medium">{t('ui.hub.default_settings_title')}</DialogPrimitive.Title>
      <p className="mb-3 text-xs text-muted-foreground">{t('ui.hub.default_settings_desc')}</p>
      <label className="mb-2 flex flex-col gap-1 text-xs text-muted-foreground">
        {t('ui.hub.field_tool')}
        <select
          className="rounded border border-border bg-card px-2 py-1 text-sm text-foreground"
          value={toolId}
          onChange={(e) => {
            setToolId(e.target.value);
            // 工具切换即刷新模型下拉；原选中项在新工具缓存里不存在则回落默认
            setModelId((cur) => (cur && loadToolModels(e.target.value).includes(cur) ? cur : ''));
          }}
        >
          {tools.map((x) => (
            <option key={x.ID} value={x.ID}>
              {toolDisplayName(x.ID)}
            </option>
          ))}
        </select>
      </label>
      <label className="mb-4 flex flex-col gap-1 text-xs text-muted-foreground">
        {t('ui.hub.field_model')}
        <select
          className="rounded border border-border bg-card px-2 py-1 text-sm text-foreground"
          value={modelId}
          onChange={(e) => setModelId(e.target.value)}
        >
          {/* 头部恒有空选项：已设模型可清回「默认模型」（与 NewAgentDialog 的空值语义一致） */}
          {modelOptions.length === 0 ? (
            <option value="">{t('ui.hub.default_model')}</option>
          ) : (
            <>
              <option value="">{t('ui.hub.default_model')}</option>
              {modelOptions.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name}
                </option>
              ))}
            </>
          )}
        </select>
      </label>
      {tools.length === 0 && <p className="mb-3 text-xs text-muted-foreground">{t('ui.hub.no_tools')}</p>}
      <div className="flex justify-end gap-2">
        <Button size="sm" variant="outline" onClick={() => onOpenChange(false)}>
          {t('ui.hub.cancel')}
        </Button>
        <Button size="sm" disabled={!toolId} onClick={save}>
          {t('ui.hub.save')}
        </Button>
      </div>
    </Dialog>
  );
}

// HelpDialog：说明 AgentHub 的 ACP 支持范围、各工具的平台模型注入情况与常用操作。
// 内容与后端能力对齐：ACPProvider 目前由 claude / codebuddy / cursor / trae 实现，
// 平台模型注入见 launch.finalizeACP 与 providers.ModelInjector。
function HelpDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation();
  const supported = [
    {
      tool: 'Claude Code',
      acp: t('ui.hub.help_claude_acp'),
      injected: true,
      detail: t('ui.hub.help_claude_model'),
    },
    {
      tool: 'CodeBuddy',
      acp: t('ui.hub.help_codebuddy_acp'),
      injected: true,
      detail: t('ui.hub.help_codebuddy_model'),
    },
    {
      tool: 'TRAE CLI',
      acp: t('ui.hub.help_trae_acp'),
      injected: false,
      detail: t('ui.hub.help_trae_model'),
    },
    {
      tool: 'Cursor',
      acp: t('ui.hub.help_cursor_acp'),
      injected: false,
      detail: t('ui.hub.help_cursor_model'),
    },
  ];
  const usage = [
    t('ui.hub.help_usage_1'),
    t('ui.hub.help_usage_2'),
    t('ui.hub.help_usage_3'),
    t('ui.hub.help_usage_4'),
    t('ui.hub.help_usage_5'),
  ];
  return (
    <Dialog open={open} onOpenChange={onOpenChange} className="max-h-[80vh] w-[34rem] overflow-y-auto">
      <DialogPrimitive.Title className="mb-2 text-sm font-medium">{t('ui.hub.help_title')}</DialogPrimitive.Title>
      <p className="mb-3 text-xs text-muted-foreground">{t('ui.hub.help_intro')}</p>

      <div className="mb-1.5 text-xs font-medium">{t('ui.hub.help_tools_title')}</div>
      <table className="mb-4 w-full table-fixed border-collapse text-left text-xs">
        <thead>
          <tr className="text-muted-foreground">
            <th className="w-[22%] border-b border-border py-1 pr-2 font-medium">{t('ui.hub.help_col_tool')}</th>
            <th className="w-[36%] border-b border-border py-1 pr-2 font-medium">{t('ui.hub.help_col_acp')}</th>
            <th className="w-[42%] border-b border-border py-1 font-medium">{t('ui.hub.help_col_model')}</th>
          </tr>
        </thead>
        <tbody>
          {supported.map((row) => (
            <tr key={row.tool} className="align-top">
              <td className="border-b border-border py-1.5 pr-2 font-medium">{row.tool}</td>
              <td className="border-b border-border py-1.5 pr-2 text-muted-foreground">{row.acp}</td>
              <td className="border-b border-border py-1.5">
                <span
                  className={cn(
                    'rounded px-1.5 py-0.5',
                    row.injected ? 'bg-primary/10 text-primary' : 'bg-muted text-muted-foreground',
                  )}
                >
                  {t(row.injected ? 'ui.hub.help_model_yes' : 'ui.hub.help_model_no')}
                </span>
                <div className="mt-1 text-muted-foreground">{row.detail}</div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      <div className="mb-1.5 text-xs font-medium">{t('ui.hub.help_usage_title')}</div>
      <ol className="m-0 list-decimal space-y-1 pl-4 text-xs text-muted-foreground">
        {usage.map((line) => (
          <li key={line}>{line}</li>
        ))}
      </ol>

      <div className="mt-4 flex justify-end">
        <Button size="sm" onClick={() => onOpenChange(false)}>
          {t('ui.hub.help_close')}
        </Button>
      </div>
    </Dialog>
  );
}
