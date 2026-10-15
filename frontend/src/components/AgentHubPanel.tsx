// AgentHubPanel：首页的跨项目 agent 汇总管理聊天窗。
// 与 ChatView 的区别：ChatView 绑定单个 chat.ID，本组件是跨会话聚合视图——
// 左侧列出所有运行中的 ACP agent（按项目分组、点击跳转），右侧合并展示
// 用户发出的消息与 agent 结束通知（summary 为 agent 回复正文）。
import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Loader2 } from 'lucide-react';
import type { ChatCommand, ChatInfo, ChatModelsView, HubArchiveMonth, HubMessage, ToolInfo } from '../lib/api';
import {
  cancelChat,
  clearHubMessages,
  getTools,
  listChatCommands,
  listChatModels,
  listHubArchive,
  listHubMessages,
  loadHubArchive,
  openWorkspaceACP,
  readClipboardPaste,
  recallHubMessage,
  sendChatPrompt,
  setChatModel,
} from '../lib/api';
import { buildMentionOptions, resolveMentionTarget, type MentionAgent, type MentionOption } from '../lib/agentMentions';
import { toolDisplayName } from '../lib/agentNotify';
import { loadLastCreate, loadToolModels, saveLastCreate, saveToolModels } from '../lib/agenthubDefaults';
import { quotePathForShell } from '../lib/dragPath';
import { backendError, translateBackend } from '../lib/errors';
import { attributionText, baseName, resolveAttribution } from '../lib/hubAttribution';
import { markdownBubbleClass } from '../lib/markdownStyles';
import { sameWorkspacePath } from '../lib/workspacePath';
import { cn } from '../lib/cn';
import { LIST_ROW, LIST_ROW_ACTIVE, MONO } from '../lib/ui';
import { useAppStore } from '../state/store';
import { Button } from './ui/button';
import { EmptyState } from './ui/empty-state';
import { Dialog } from './ui/dialog';
import * as DialogPrimitive from '@radix-ui/react-dialog';

// 消息流里渲染的一条：用户发出（右）或 agent 回复（左）。
// 数据源是 Go 侧 hub.Store 的持久历史（store.hubMessages），不再依赖瞬态 agentNotices。

// kshell 客户端命令：由本前端解析执行，不投递给 agent。name 不含前导斜杠。
const HUB_COMMANDS: { name: string; descKey: string }[] = [
  { name: 'new', descKey: 'ui.hub.cmd_new_desc' },
  { name: 'easynew', descKey: 'ui.hub.cmd_easynew_desc' },
  { name: 'stop', descKey: 'ui.hub.cmd_stop_desc' },
  { name: 'model', descKey: 'ui.hub.cmd_model_desc' },
  { name: 'clear', descKey: 'ui.hub.cmd_clear_desc' },
];
const HUB_COMMAND_NAMES = new Set(HUB_COMMANDS.map((c) => c.name.toLowerCase()));

// 快速回复预设短语（钉钉式卡片快捷回复）：点击直接发给该卡片归因的 agent。
const QUICK_REPLIES = ['ui.hub.quick_continue', 'ui.hub.quick_ok', 'ui.hub.quick_rephrase'] as const;

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
      const result = await openWorkspaceACP(wsPath, toolId);
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
        saveToolModels(toolId, view.Models.map((m) => m.ID));
      }
      // 用户选了模型且与 agent 当前模型不同：建后再 SetModel
      if (modelId && view && view.Current !== modelId) {
        try {
          await setChatModel(chat.ID, modelId);
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

// 补全列表条目：客户端命令与 agent 技能混排，用 kind 区分选中后的行为。
interface SlashItem {
  name: string;
  desc: string;
  kind: 'client' | 'agent';
}

// statusKey 把 chat.Status 映射到状态点文案 key（未知按空闲处理）。
function statusKey(status: string): string {
  if (status === 'running' || status === 'starting') return 'ui.hub.status_running';
  if (status === 'exited') return 'ui.hub.status_exited';
  return 'ui.hub.status_ready';
}

// statusDotClass 状态点配色：运行中用主色（Teal），退出用 muted，其余描边。
function statusDotClass(status: string): string {
  if (status === 'running' || status === 'starting') return 'bg-primary';
  if (status === 'exited') return 'bg-muted-foreground/40';
  return 'border border-muted-foreground bg-transparent';
}

// wsBaseName 取路径最后一段，作为项目分组标题。
function wsBaseName(ws: string): string {
  return baseName(ws);
}

// lastHubAgentId 从 hub 消息历史倒序找「最近一次交互」的目标 agent：
// 消息经归因字段反查 chat，再确认该 chat 仍是当前 mention 候选。
// 字段匹配方式与 hubAttribution 相同（chatId 按会话 ID、termKey 按会话 Key、
// workspace 用 sameWorkspacePath 归并），但优先级不同：精确匹配（chatId/termKey）
// 先于模糊匹配（workspace）；user 与 agent 消息都参与匹配（user 消息也带目标
// agent 的归因字段）。
// 已知局限：同 workspace 多 chat 时 chats.find 取列表第一个，未必是最近交互
// 的那个（降级行为，可接受）。找不到返回 null。
function lastHubAgentId(messages: HubMessage[], options: MentionOption[], chats: ChatInfo[]): string | null {
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i];
    const c =
      chats.find((x) => x.ID === m.chatId) ??
      chats.find((x) => m.termKey && x.Key === m.termKey) ??
      chats.find((x) => m.workspace && sameWorkspacePath(x.Workspace, m.workspace));
    if (c && options.some((o) => o.id === c.ID)) return c.ID;
  }
  return null;
}

export default function AgentHubPanel() {
  const { t } = useTranslation();
  const chats = useAppStore((s) => s.chats);
  const hubMessages = useAppStore((s) => s.hubMessages);
  const lastAgentId = useAppStore((s) => s.lastAgentId);
  // 消息归因：workspace/chatId/termKey → 「项目 · 会话」文本。
  const attrOf = useAttribution();

  const emptyTarget = t('ui.hub.new_agent');
  const [draft, setDraft] = useState('');
  const [dialogOpen, setDialogOpen] = useState(false);
  // /easynew 直建中的本地提示（不进 hub store，成功后由后端写欢迎词）
  const [easynewRunning, setEasynewRunning] = useState(false);
  // 归档入口：展开时列出月份，选中月份后加载该月旧消息内联展示。
  const [historyOpen, setHistoryOpen] = useState(false);
  const [archiveMonths, setArchiveMonths] = useState<HubArchiveMonth[]>([]);
  const [archiveMessages, setArchiveMessages] = useState<HubMessage[]>([]);
  const [activeMonth, setActiveMonth] = useState<string | null>(null);

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
  // agent 技能按目标缓存：target 变化或打开命令模式时拉取，失败静默（补全是辅助功能）。
  const [agentCmds, setAgentCmds] = useState<ChatCommand[]>([]);
  // /model 内联下拉：非 null 时在输入区上方展示模型列表。
  const [modelMenu, setModelMenu] = useState<{ targetId: string; view: ChatModelsView } | null>(null);

  // @ 补全项：展示名 + 会话 id，顺序无关（Claude Code#c1 ...）
  const options = useMemo(
    () =>
      buildMentionOptions(
        chats.map<MentionAgent>((c) => ({
          id: c.ID,
          toolId: c.ToolID,
          title: c.Title,
          workspace: c.Workspace,
        })),
      ),
    [chats],
  );

  // 按 Workspace 分组（用 sameWorkspacePath 归并大小写/分隔符差异）
  const groups = useMemo(() => {
    const out: { workspace: string; rows: ChatInfo[] }[] = [];
    for (const c of chats) {
      const g = out.find((x) => sameWorkspacePath(x.workspace, c.Workspace));
      if (g) g.rows.push(c);
      else out.push({ workspace: c.Workspace, rows: [c] });
    }
    return out;
  }, [chats]);

  // @ 补全：草稿末尾是未闭合的 @ 片段时给出候选（v1 只按草稿尾匹配，不做光标跟踪）。
  // mentionQuery 为 null 表示当前不在 @ 输入中。
  const mentionQuery = useMemo(() => {
    const m = /(?:^|\s)@([^\s@]*)$/.exec(draft);
    return m ? m[1].toLowerCase() : null;
  }, [draft]);
  const mentionMatches = useMemo(() => {
    if (mentionQuery === null) return [];
    return options.filter((o) => o.label.toLowerCase().includes(mentionQuery));
  }, [mentionQuery, options]);

  // applyMention 把草稿尾部的 @ 片段替换成完整标签（补一个尾随空格方便继续输入）
  const applyMention = (label: string) => {
    setDraft((d) => d.replace(/@([^\s@]*)$/, `@${label} `));
  };

  // ---- 斜杠命令补全：draft 命中 /xxx 时激活 ----
  const slashQuery = /^\/([^\s]*)$/.exec(draft)?.[1] ?? null;
  const inSlashMode = slashQuery !== null;
  // 默认投递目标回落链：hub 最近一次交互的 agent 优先（比 lastAgentId 更能反映
  // hub 视角的最新目标），再退 store.lastAgentId。斜杠命令目标解析与 send 共用。
  const fallbackId = useMemo(
    () => lastHubAgentId(hubMessages, options, chats) ?? lastAgentId,
    [hubMessages, options, chats, lastAgentId],
  );
  // 目标解析：优先 @ 提及，其次回落链（与 send 一致）。
  const currentTargetId = useMemo(
    () => resolveMentionTarget(draft, options, fallbackId),
    [draft, options, fallbackId],
  );
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

  // 客户端命令在前，agent 技能去重（同名以客户端命令优先）在后。
  const slashMatches = useMemo<SlashItem[]>(() => {
    if (slashQuery === null) return [];
    const q = slashQuery.toLowerCase();
    const client = HUB_COMMANDS.filter((c) => c.name.startsWith(q)).map<SlashItem>((c) => ({
      name: c.name,
      desc: t(c.descKey),
      kind: 'client',
    }));
    const agent = agentCmds
      .filter((c) => c.Name && !HUB_COMMAND_NAMES.has(c.Name.toLowerCase()) && c.Name.toLowerCase().startsWith(q))
      .map<SlashItem>((c) => ({ name: c.Name, desc: c.Description ?? '', kind: 'agent' }));
    return [...client, ...agent];
  }, [slashQuery, agentCmds, t]);
  const slashOpen = slashQuery !== null && !slashClosed && slashMatches.length > 0;

  // draft 变化即重置补全状态：Esc 关闭后继续输入要能重新弹出
  useEffect(() => {
    setSlashClosed(false);
    setSlashIndex(0);
  }, [draft]);

  // runClientCommand 执行 kshell 客户端命令，不投递给 agent。
  const runClientCommand = (name: string) => {
    // stop / model 需要目标 agent：先校验，避免无目标时还把输入清掉
    if ((name === 'stop' || name === 'model') && !currentTargetId) {
      useAppStore.getState().notify(t('ui.hub.need_target'), 'error');
      return;
    }
    setDraft('');
    setSlashClosed(false);
    setSlashIndex(0);
    if (name === 'new') {
      openNewAgentDialog();
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

  // runEasyNew /easynew 快建：lastCreate 的项目/工具仍有效时直接走创建流程（不弹窗），
  // 失效或没有默认值则打开新建弹窗让用户补选（补选后同样落默认值）。
  // 创建互斥：runAgentCreate 内部持锁；这里对弹窗入口另查锁，避免创建中又打开对话框重复操作。
  const runEasyNew = () => {
    if (createPromise !== null) {
      useAppStore.getState().notify(t('ui.hub.easynew_busy'), 'info');
      return;
    }
    const last = loadLastCreate();
    const wsOk =
      last &&
      useAppStore.getState().workspaces.some((w) => sameWorkspacePath(w.Path, last.wsPath));
    if (!last || !wsOk) {
      openNewAgentDialog();
      return;
    }
    setEasynewRunning(true);
    // 工具有效性需异步拉工具列表校验（store 无镜像），确认有效才直建
    void getTools()
      .then((list) => list.filter((x) => x.ACP?.Available === true))
      .then((acp) => {
        if (!last || !acp.some((x) => x.ID === last.toolId)) {
          openNewAgentDialog();
          return;
        }
        return runAgentCreate(last.wsPath, last.toolId, last.modelId, {
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

  // applySlash：客户端命令直接执行，agent 技能切入 draft 后正常发送。
  const applySlash = (item: SlashItem) => {
    if (item.kind === 'client') {
      runClientCommand(item.name);
      return;
    }
    setDraft(`/${item.name} `);
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
      })
      .catch((e: unknown) =>
        useAppStore.getState().notify(t('ui.hub.model_switch_failed', { err: backendError(e) }), 'error'),
      );
  };

  // focusRow 跳转：照搬 ActiveTerminalsPanel.focusRow——切到该 workspace 页签并按 termKey 聚焦
  const focusRow = (c: ChatInfo) => {
    const { openTabs, setActiveTab, requestFocusTerm, setLastAgentId } = useAppStore.getState();
    setLastAgentId(c.ID);
    const tab = openTabs.find((x) => sameWorkspacePath(x.id, c.Workspace));
    if (tab) setActiveTab(tab.id);
    if (c.Key) requestFocusTerm(c.Key);
  };

  const send = () => {
    const text = draft.trim();
    if (!text) return;
    // 用户主动发消息即视为已接管，清掉 /easynew 的进行中提示
    setEasynewRunning(false);
    // 回落链与 currentTargetId 共用 fallbackId（见上）。
    const targetId = resolveMentionTarget(text, options, fallbackId);
    if (!targetId) {
      useAppStore.getState().notify(t('ui.hub.need_target'), 'error');
      return;
    }
    setDraft('');
    // 用户消息由 Go 侧落盘并经 hub:changed 回流（单一数据源），失败只提示不回滚。
    void sendChatPrompt(targetId, text)
      .then(() => {
        useAppStore.getState().setLastAgentId(targetId);
      })
      .catch((e: unknown) => {
        useAppStore.getState().notify(t('ui.chat.send_failed', { err: backendError(e) }), 'error');
      });
  };

  // quickTarget 从 agent 卡片归因解析目标 chat ID：chatId 优先，termKey 反查兜底。
  const quickTarget = (m: HubMessage): string | null => {
    if (m.chatId && chats.some((c) => c.ID === m.chatId)) return m.chatId;
    if (m.termKey) {
      const c = chats.find((x) => x.Key === m.termKey);
      if (c) return c.ID;
    }
    return null;
  };

  // quickReply 直接以该卡片归因的 agent 为目标发送预设短语（免输 @）。
  const quickReply = (m: HubMessage, key: string) => {
    const targetId = quickTarget(m);
    if (!targetId) {
      useAppStore.getState().notify(t('ui.hub.need_target'), 'error');
      return;
    }
    const text = t(key);
    void sendChatPrompt(targetId, text)
      .then(() => {
        useAppStore.getState().setLastAgentId(targetId);
      })
      .catch((e: unknown) => {
        useAppStore.getState().notify(t('ui.chat.send_failed', { err: backendError(e) }), 'error');
      });
  };

  // recall 撤回排队中的用户消息：后端真移除 + 标记已撤回；乐观更新，hub:changed 回流兜底。
  const recall = (m: HubMessage) => {
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
  };

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
      className="flex min-h-0 flex-1 flex-col gap-2.5 rounded border border-border bg-card p-3 text-sm"
    >
      <div className="flex items-center justify-between gap-2">
        <h2 className="text-sm font-medium">{t('ui.hub.title')}</h2>
        <div className="flex items-center gap-2">
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
                      <span className="whitespace-pre-wrap">{m.text}</span>
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

      {chats.length === 0 ? (
        <EmptyState title={t('ui.hub.empty')} />
      ) : (
        // 会话列表限高自滚：agent 多时不无限撑高，把空间留给消息流
        <div className="flex max-h-44 shrink-0 flex-col gap-2 overflow-y-auto">
          {groups.map((g) => (
            <div key={g.workspace} className="flex flex-col gap-1">
              <span className={cn(MONO, 'truncate')} title={g.workspace}>
                {wsBaseName(g.workspace)}
              </span>
              <ul aria-label={t('ui.hub.title')} className="m-0 flex list-none flex-col gap-1.5 p-0">
                {g.rows.map((c) => (
                  <li key={c.ID}>
                    <button
                      type="button"
                      className={cn(
                        LIST_ROW,
                        'flex w-full min-w-0 items-center gap-1.5 px-2.5 py-1.5 text-left',
                        lastAgentId === c.ID && LIST_ROW_ACTIVE,
                      )}
                      onClick={() => focusRow(c)}
                    >
                      <span
                        aria-hidden
                        className={cn('h-1.5 w-1.5 shrink-0 rounded-full', statusDotClass(c.Status))}
                      />
                      <span className="min-w-0 truncate font-medium">
                        {/* 无标题会话兜底显示项目基名，避免出现「新会话」同名列无法区分 */}
                        {c.Title || wsBaseName(c.Workspace) || emptyTarget}
                      </span>
                      <span className="ml-auto shrink-0 truncate text-[11px] text-muted-foreground">
                        {toolDisplayName(c.ToolID)} · {t(statusKey(c.Status))}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      )}

      {/* 水平内边距避免气泡贴边/挤滚动条；gap-3 拉开卡片垂直间距 */}
      <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto px-3 py-2" role="log" aria-live="polite">
        {hubMessages.map((m) => {
          const attr = attrOf(m);
          return m.kind === 'user' ? (
            <div key={`h${m.id}`} className="text-right">
              {attr && <div className="mb-0.5 text-[11px] text-muted-foreground">{attr}</div>}
              {/* 气泡内强制居左：外层 text-right 只负责把卡片推到行尾，避免长文贴右边缘 */}
              <div className="group relative inline-block max-w-[85%]">
                {m.status === 'recalled' ? (
                  // 已撤回：灰显卡片 + 标签，原文保留（在原消息上标记，不删除）
                  <div className="selectable inline-block break-words overflow-hidden rounded-md bg-muted px-3 py-1.5 text-left text-muted-foreground">
                    <span className="mb-0.5 block text-[11px] opacity-80">{t('ui.hub.recalled')}</span>
                    <span className="whitespace-pre-wrap opacity-70">{m.text}</span>
                  </div>
                ) : (
                  <>
                    <div className="selectable inline-block break-words overflow-hidden rounded-md bg-primary px-3 py-1.5 text-left text-primary-foreground">
                      <span className="whitespace-pre-wrap">{m.text}</span>
                    </div>
                    {m.status === 'queued' && (
                      <div className="absolute right-1 bottom-full mb-1 hidden group-hover:flex">
                        <button
                          type="button"
                          className="rounded border border-border bg-card px-2 py-0.5 text-[11px] text-muted-foreground hover:text-destructive"
                          onClick={() => recall(m)}
                        >
                          {t('ui.hub.recall')}
                        </button>
                      </div>
                    )}
                  </>
                )}
              </div>
            </div>
          ) : (
            <div key={`h${m.id}`} className="text-left">
              <div className="group inline-block max-w-[85%]">
                <div className="selectable break-words overflow-hidden rounded-md bg-muted px-3 py-1.5 [&_pre]:whitespace-pre-wrap">
                  {/* 归因行：工具 · 项目 · 会话；缺项跳过，全缺只剩工具名 */}
                  <div className="mb-0.5 text-[11px] text-muted-foreground">
                    {[toolDisplayName(m.tool ?? ''), attr].filter(Boolean).join(' · ')}
                  </div>
                  {/* 正文过 translateBackend：ack 卡片的 wire key 翻译成当前语言，普通文本原样返回 */}
                  <div className={markdownBubbleClass}>
                    <ReactMarkdown remarkPlugins={[remarkGfm]}>{translateBackend(m.text)}</ReactMarkdown>
                  </div>
                </div>
                {/* 快速回复：hover 显示预设短语，点击直达该卡片归因的 agent */}
                <div className="mt-1 hidden flex-wrap gap-1 group-hover:flex">
                  {QUICK_REPLIES.map((key) => (
                    <button
                      key={key}
                      type="button"
                      className="rounded border border-border bg-card px-2 py-0.5 text-[11px] text-muted-foreground hover:bg-muted/60 hover:text-foreground"
                      onClick={() => quickReply(m, key)}
                    >
                      {t(key)}
                    </button>
                  ))}
                </div>
              </div>
            </div>
          );
        })}
      </div>

      <div className="flex flex-col gap-1 border-t border-border pt-2">
        {easynewRunning && (
          <div className="flex items-center gap-1.5 rounded border border-border bg-card px-2 py-1 text-xs text-muted-foreground">
            <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />
            <span>{t('ui.hub.easynew_running')}</span>
          </div>
        )}
        {mentionMatches.length > 0 && (
          <ul
            aria-label={t('ui.hub.mention_aria')}
            className="m-0 flex list-none flex-col gap-0.5 rounded border border-border bg-card p-1 text-xs"
          >
            {mentionMatches.map((o) => (
              <li key={o.id}>
                <button
                  type="button"
                  className="w-full rounded px-2 py-1 text-left hover:bg-muted/50"
                  onClick={() => applyMention(o.label)}
                >
                  <span className="block font-mono">@{o.label}</span>
                  {/* 第二行：项目 · 会话标题（label 保持短标签，详情只做展示） */}
                  {o.detail && <span className="block truncate text-muted-foreground">{o.detail}</span>}
                </button>
              </li>
            ))}
          </ul>
        )}
        {modelMenu && (
          <div className="rounded border border-border bg-card p-1 text-xs">
            <div className="flex items-center justify-between px-2 py-0.5 text-muted-foreground">
              <span className="truncate">
                {t('ui.hub.model_label')}
                {options.find((o) => o.id === modelMenu.targetId)?.label
                  ? ` · ${options.find((o) => o.id === modelMenu.targetId)?.label}`
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
        {slashOpen && (
          <ul
            role="listbox"
            aria-label={t('ui.hub.cmd_aria')}
            className="mb-1 max-h-48 overflow-y-auto rounded border border-border bg-card py-1 text-xs shadow-md"
          >
            {slashMatches.map((c, i) => (
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
                <span className="font-medium">/{c.name}</span>
                {c.desc && <span className="ml-2 text-muted-foreground">{c.desc}</span>}
              </li>
            ))}
          </ul>
        )}
        <textarea
          className="min-h-16 w-full resize-none rounded border border-border bg-card p-2 text-sm"
          placeholder={t('ui.hub.input_placeholder')}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onPaste={() => void handlePaste()}
          onKeyDown={(e) => {
            if (slashOpen && slashMatches.length > 0) {
              if (e.key === 'ArrowDown') {
                e.preventDefault();
                setSlashIndex((i) => Math.min(i + 1, slashMatches.length - 1));
                return;
              }
              if (e.key === 'ArrowUp') {
                e.preventDefault();
                setSlashIndex((i) => Math.max(i - 1, 0));
                return;
              }
              if (e.key === 'Enter' || e.key === 'Tab') {
                e.preventDefault();
                applySlash(slashMatches[Math.min(slashIndex, slashMatches.length - 1)]);
                return;
              }
              if (e.key === 'Escape') {
                e.preventDefault();
                setSlashClosed(true);
                return;
              }
            }
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault();
              send();
            }
          }}
        />
        <div className="flex justify-end">
          <Button size="sm" onClick={send}>
            {t('ui.hub.send')}
          </Button>
        </div>
      </div>

      <NewAgentDialog open={dialogOpen} onOpenChange={setDialogOpen} />
    </section>
  );
}

// NewAgentDialog：单窗体新建。项目/工具/模型三字段同屏，一次点击完成创建
// （session/new 不接受模型参数，模型在 runAgentCreate 里建会话后再 SetModel）。
// 打开时按 lastCreate 预填；模型下拉来自本地缓存（loadToolModels），空值即「默认模型」。
function NewAgentDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation();
  const workspaces = useAppStore((s) => s.workspaces);
  const [tools, setTools] = useState<ToolInfo[]>([]);
  const [wsPath, setWsPath] = useState('');
  const [toolId, setToolId] = useState('');
  const [busy, setBusy] = useState(false);
  const [modelId, setModelId] = useState('');
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

  // 模型下拉选项：当前工具的本地缓存，渲染期直接同步读（localStorage 成本极低，
  // toolId 变化或缓存被写入后下次渲染自然拿到最新列表）
  const modelOptions = toolId ? loadToolModels(toolId) : [];

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
            {/* 无缓存只显示「默认模型」空选项；有缓存则直接列缓存模型 */}
            {modelOptions.length === 0 ? (
              <option value="">{t('ui.hub.default_model')}</option>
            ) : (
              modelOptions.map((m) => (
                <option key={m} value={m}>
                  {m}
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
