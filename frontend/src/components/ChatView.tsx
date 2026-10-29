// 中心区 ACP 聊天页签：时间线 + 输入区 + 权限弹窗。常挂载、由父级 hidden 切换。
// 模型下拉 / 技能补全 / 附件（粘贴、拖拽、选择按钮）在本组件内实现。
import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import type { ChatAttachmentMeta, ChatBlock, ChatInfo, ChatQueueItem, ChatToolCall } from '../lib/api';
import {
  cancelChat,
  cancelChatElicitation,
  cancelChatPermission,
  listChatCommands,
  listChatConfigOptions,
  listChatModels,
  listChatQueue,
  listChats,
  openExternalURL,
  openInDefaultApp,
  openLocalPath,
  pickFile,
  removeChatQueued,
  respondChatElicitation,
  respondChatPermission,
  sendChatPrompt,
  sendChatPromptBlocks,
  setChatConfigOption,
  setChatModel,
} from '../lib/api';
import { appendChatInput, registerChatInput, unregisterChatInput } from '../lib/chatInputRegistry';
import { attachmentAction } from '../lib/chatAttachment';
import { decodePercentText } from '../lib/displayText';
import { DRAG_MIME, quotePathForShell } from '../lib/dragPath';
import { backendError, translateBackend } from '../lib/errors';
import { markdownBubbleClass } from '../lib/markdownStyles';
import { rememberChatModels, rememberLastModel } from '../lib/agenthubDefaults';
import { useAppStore } from '../state/store';
import type { ChatQueuedMessage } from '../state/store';
import type { TimelineItem } from '../state/chatUpdate';
import { cn } from '../lib/cn';
import { handleAnchorClick } from '../lib/openHref';
import { Button } from './ui/button';
import { Dialog } from './ui/dialog';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from './ui/dropdown-menu';
import ImageLightbox from './ImageLightbox';
import * as DialogPrimitive from '@radix-ui/react-dialog';

interface Props {
  chat: ChatInfo;
  active: boolean;
}

// 模块级稳定空数组：无消息时避免每次渲染返回新引用，防止自动滚动 effect 空转
const EMPTY_ITEMS: TimelineItem[] = [];
const EMPTY_COMMANDS: { Name: string; Description?: string; InputHint?: string }[] = [];
// 队列空时的稳定引用（同上，避免 store 订阅在空态反复触发重渲染）
const EMPTY_QUEUE: ChatQueuedMessage[] = [];
// 后端队列空时的稳定引用（同上）
const EMPTY_BACKEND_QUEUE: ChatQueueItem[] = [];

// fileUriFromPath 把本机绝对路径转 file URI（Windows 反斜杠归一 + 空格等编码），
// 供 resource_link 块引用；agent 与 kshell 同机，可直接按路径读取。
// encodeURI 不转义 #/?，路径含这些字符时会生成破损 URI，需显式补转义。
export function fileUriFromPath(path: string): string {
  const norm = path.replace(/\\/g, '/');
  const abs = norm.startsWith('/') ? norm : `/${norm}`;
  return encodeURI(`file://${abs}`).replace(/#/g, '%23').replace(/\?/g, '%3F');
}

function baseName(p: string): string {
  const norm = p.replace(/\\/g, '/');
  return norm.slice(norm.lastIndexOf('/') + 1) || p;
}

function arrayBufferToBase64(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf);
  let bin = '';
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
  return btoa(bin);
}

// 附件本地状态：block 是随消息发送的内容，label 是 chip 展示名。
interface ChatAttachment {
  block: ChatBlock;
  label: string;
}

// 粘贴附件的体积上限：超过则提示改用附件按钮（走路径引用，不占 payload）。
// 对图片同样生效：超大截图 base64 后会数倍膨胀，直塞绑定层易卡死 UI。
const MAX_PASTE_BYTES = 8 * 1024 * 1024;

// stopReason → i18n key（ACP 规范内的取值）；表外原因直接显示原始值。
const STOP_REASON_KEYS: Record<string, string> = {
  max_tokens: 'ui.chat.stop_reason.max_tokens',
  max_turn_requests: 'ui.chat.stop_reason.max_turn_requests',
  refusal: 'ui.chat.stop_reason.refusal',
  cancelled: 'ui.chat.stop_reason.cancelled',
};

// readAsDataURLBase64 读文件为 base64 载荷（去掉 data URL 前缀）；失败返回 null。
function readAsDataURLBase64(file: File): Promise<string | null> {
  return new Promise((resolve) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = typeof reader.result === 'string' ? reader.result : '';
      const idx = result.indexOf(',');
      resolve(idx >= 0 ? result.slice(idx + 1) : null);
    };
    reader.onerror = () => resolve(null);
    reader.readAsDataURL(file);
  });
}

// 工具调用结果的可读文本：文本内容直接拼接；diff 无高亮能力时降级为纯文本。
function toolContentText(t?: ChatToolCall): string {
  if (!t?.Content) return '';
  const parts: string[] = [];
  for (const raw of t.Content) {
    const item = raw as { type?: string; content?: { type?: string; text?: string }; path?: string; newText?: string };
    if (item?.type === 'content' && item.content?.type === 'text' && item.content.text) {
      parts.push(item.content.text);
    } else if (item?.type === 'diff') {
      parts.push(`--- ${item.path ?? ''}\n${item.newText ?? ''}`);
    }
  }
  return parts.join('\n');
}

// 附件 meta 的展示名：有 Name 用 Name；图片类显示「图片」，其余退回 MimeType。
function attachmentLabel(att: ChatAttachmentMeta, imageLabel: string): string {
  if (att.Name) return att.Name;
  if (att.MimeType?.startsWith('image/')) return imageLabel;
  return att.MimeType ?? '';
}

export default function ChatView({ chat, active }: Props) {
  const { t } = useTranslation();
  const id = chat.ID;
  const items = useAppStore((s) => s.chatItems[id]) ?? EMPTY_ITEMS;
  const permission = useAppStore((s) => s.chatPermissions[id]) ?? null;
  const models = useAppStore((s) => s.chatModels[id]) ?? null;
  const configs = useAppStore((s) => s.chatConfigs[id]) ?? null;
  const elicitation = useAppStore((s) => s.chatElicitations[id]) ?? null;
  const commands = useAppStore((s) => s.chatCommands[id]) ?? EMPTY_COMMANDS;
  const [draft, setDraft] = useState('');
  const [dropHint, setDropHint] = useState(false);
  const [attachments, setAttachments] = useState<ChatAttachment[]>([]);
  // elicitation 表单的本地作答状态：按 RequestID 变化重置默认值
  const [elicitationValues, setElicitationValues] = useState<Record<string, unknown>>({});
  // 图片附件 lightbox：Path 为落盘缓存绝对路径
  const [preview, setPreview] = useState<{ name?: string; path: string } | null>(null);
  const scrollRef = useRef<HTMLDivElement | null>(null);
  // 用户是否停在底部（近底 40px 内）：仅在底部时才自动滚动，避免读历史时被新消息拽走
  const atBottom = useRef(true);
  const running = chat.Status === 'running';
  const queued = useAppStore((s) => s.chatQueues[id]) ?? EMPTY_QUEUE;
  // 后端待发送队列（任何来源入队：AgentHub / 其他 ACP 客户端）；展示在后端在前
  const backendQueued = useAppStore((s) => s.chatBackendQueues[id]) ?? EMPTY_BACKEND_QUEUE;

  // 拖入文件落点经注册表转为附件 chip：外部（文件树/全局拖放）与页签内 drop 共用此入口。
  // 注册表约定传引号包裹的路径文本，这里剥引号后建 resource_link 块。
  useEffect(() => {
    registerChatInput(id, {
      append: (text) => {
        const p = text.trim().replace(/^"(.*)"$/, '$1');
        if (!p) return;
        setAttachments((a) => [
          ...a,
          { label: baseName(p), block: { Type: 'resource_link', URI: fileUriFromPath(p), Name: baseName(p) } },
        ]);
      },
    });
    return () => unregisterChatInput(id);
  }, [id]);

  // 挂载即拉取模型与技能列表做兜底初始化；agent 推送（chat:commands）会随后覆盖。
  useEffect(() => {
    let cancelled = false;
    void listChatModels(id)
      .then((v) => {
        if (!cancelled) useAppStore.getState().setChatModels(id, v);
        // 回写工具模型缓存：恢复型会话若错过 chat:models 推送，
        // 挂载拉取是兜底的名字来源，旧格式缓存靠它自愈。
        if (!cancelled) rememberChatModels(chat.ToolID, v);
      })
      .catch(() => {});
    void listChatCommands(id)
      .then((cmds) => {
        if (!cancelled) useAppStore.getState().setChatCommands(id, cmds);
      })
      .catch(() => {});
    // 通用配置项（mode 等）兜底拉取：agent 推送 chat:config 会随后覆盖
    void listChatConfigOptions(id)
      .then((v) => {
        if (!cancelled) useAppStore.getState().setChatConfigs(id, v);
      })
      .catch(() => {});
    // 后端待发送队列回填：事件订阅在 App 层，挂载瞬间可能已错过入队推送。
    // 回填期间若版本前进（收到 chat:queue 事件等），说明已有更新快照落地，
    // 迟到的过期响应直接丢弃，不覆盖新状态。
    const revAtStart = useAppStore.getState().chatQueueRevs[id] ?? 0;
    void listChatQueue(id)
      .then((list) => {
        if (cancelled) return;
        const st = useAppStore.getState();
        if ((st.chatQueueRevs[id] ?? 0) !== revAtStart) return;
        st.setChatBackendQueue(id, list);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [id]);

  // 点开页签时对账后端：turn_done 若丢失，前端会停在乐观 running，未发新任务却转圈。
  // 只在变为激活时对账，不能把 Status 放进依赖——发送后的乐观 running 会立刻被 ListChats 冲掉。
  useEffect(() => {
    if (!active || chat.Status !== 'running') return;
    let cancelled = false;
    void listChats()
      .then((list) => {
        if (!cancelled) useAppStore.getState().setChats(list);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
    // chat.Status 有意不进依赖，见上。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active, id]);

  useEffect(() => {
    if (!active) return;
    if (!atBottom.current) return;
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [items, active]);

  // ImageLightbox 走 Radix Portal 渲染到 body，不受父级 hidden 类约束：
  // ChatView 常驻挂载（切页签仅加 hidden 不卸载），失活时若不回收预览状态，
  // lightbox 遮罩会残留并遮挡新页签。
  useEffect(() => {
    if (!active) setPreview(null);
  }, [active]);

  // elicitation 请求变化时按字段类型重置作答：Default 优先，多选空数组，布尔 false，其余空串。
  useEffect(() => {
    if (!elicitation) {
      setElicitationValues({});
      return;
    }
    const init: Record<string, unknown> = {};
    for (const f of elicitation.Fields ?? []) {
      if (f.Default !== undefined) init[f.Key] = f.Default;
      else if (f.Kind === 'multiselect') init[f.Key] = [];
      else if (f.Kind === 'boolean') init[f.Key] = false;
      else init[f.Key] = '';
    }
    setElicitationValues(init);
    // 仅按 RequestID 重置：同一次请求内的其它字段变化不应清空用户作答
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [elicitation?.RequestID]);

  const handleScroll = () => {
    const el = scrollRef.current;
    if (!el) return;
    atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight <= 40;
  };

  // ---- 技能补全：draft 为首词 /xxx 时激活 ----
  const slashQuery = /^\/([^\s]*)$/.exec(draft)?.[1] ?? null;
  const [slashClosed, setSlashClosed] = useState(false);
  const [slashIndex, setSlashIndex] = useState(0);
  const slashMatches = useMemo(() => {
    if (slashQuery === null) return [];
    const q = slashQuery.toLowerCase();
    return commands.filter((c) => c.Name.toLowerCase().startsWith(q));
  }, [commands, slashQuery]);
  const slashOpen = slashQuery !== null && !slashClosed && slashMatches.length > 0;
  // draft 变化即重置补全状态：Esc 关闭后继续输入要能重新弹出
  useEffect(() => {
    setSlashClosed(false);
    setSlashIndex(0);
  }, [draft]);
  const applySlash = (name: string) => {
    setDraft(`/${name} `);
    setSlashClosed(false);
    setSlashIndex(0);
  };

  // ---- 附件 ----
  const notifyPasteFailed = () => {
    useAppStore.getState().notify(t('ui.chat.attach_paste_failed'), 'error');
  };

  const addFileAttachments = async (files: File[]) => {
    const next: ChatAttachment[] = [];
    for (const f of files) {
      if (f.size > MAX_PASTE_BYTES) {
        notifyPasteFailed();
        continue;
      }
      if (f.type.startsWith('image/')) {
        const data = await readAsDataURLBase64(f);
        if (data === null) {
          notifyPasteFailed();
          continue;
        }
        next.push({
          label: f.name || t('ui.chat.attachment_image'),
          block: { Type: 'image', Data: data, MimeType: f.type },
        });
        continue;
      }
      try {
        const buf = await f.arrayBuffer();
        next.push({
          label: f.name || f.type || t('ui.chat.attachment_file'),
          block: { Type: 'resource', MimeType: f.type || 'application/octet-stream', Data: arrayBufferToBase64(buf) },
        });
      } catch {
        notifyPasteFailed();
      }
    }
    if (next.length > 0) setAttachments((a) => [...a, ...next]);
  };

  const attachByPicker = async () => {
    let path: string;
    try {
      path = await pickFile(t('ui.chat.attach'));
    } catch (e) {
      useAppStore.getState().notify(backendError(e), 'error');
      return;
    }
    if (!path) return;
    setAttachments((a) => [
      ...a,
      { label: baseName(path), block: { Type: 'resource_link', URI: fileUriFromPath(path), Name: baseName(path) } },
    ]);
  };

  const removeAttachment = (index: number) => {
    setAttachments((arr) => arr.filter((_, i) => i !== index));
  };

  // doSend 真正发起发送（直接发送与队列出队共用）：乐观置 running，失败回滚并提示。
  // 出队来源的失败：条目插回队首 + 置 store 暂停标记，阻断后续自动出队——否则
  // fail 置回的 ready（或对账镜像的 ready）会构成新沿，持续性失败会逐条烧穿队列。
  // 暂停放 store：对账/镜像重建可能重挂本组件，组件 ref 会丢状态。
  const doSend = (payload: { text: string; blocks?: ChatBlock[] }, dequeueSource?: ChatQueuedMessage) => {
    // 乐观置 running：turn_done/error 的 chat:update 会把它改回 ready
    useAppStore.getState().upsertChat({ ...chat, Status: 'running' });
    const fail = (e: unknown) => {
      if (dequeueSource) useAppStore.getState().requeueChatHead(id, dequeueSource);
      useAppStore.getState().upsertChat({ ...chat, Status: 'ready' });
      useAppStore.getState().notify(t('ui.chat.send_failed', { err: backendError(e) }), 'error');
    };
    if (payload.blocks && payload.blocks.length > 0) {
      sendChatPromptBlocks(id, payload.blocks).catch(fail);
    } else {
      sendChatPrompt(id, payload.text).catch(fail);
    }
  };

  const send = () => {
    const text = draft.trim();
    if (!text && attachments.length === 0) return;
    // 文本块在前，附件块在后
    let blocks: ChatBlock[] | undefined;
    if (attachments.length > 0) {
      blocks = [];
      if (text) blocks.push({ Type: 'text', Text: text });
      for (const a of attachments) blocks.push(a.block);
    }
    setDraft('');
    setAttachments([]);
    // 手动发送解除出队暂停：用户看到错误后主动重试，说明愿意继续。
    // 入队与直发都算（入队路径在 enqueueChat 内解除，直发在这里解除）。
    useAppStore.getState().clearQueuedPause(id);
    if (running) {
      // agent 正在运行：消息进入本地队列，出队（回 ready）时才真正发送。
      // 手动发送同时解除出队暂停：用户看到错误后主动重试，说明愿意继续。
      useAppStore.getState().enqueueChat(id, { text, blocks });
      return;
    }
    doSend({ text, blocks });
  };

  // 出队：会话状态回到 ready 的转换沿且队列非空时，取队首真正发送。
  // 用 ref 记上一状态避免 ready→ready 的无关重渲染重复出队；首渲染即 ready
  // 也算转换沿（页面刷新后队列虽不持久化，但启动期入队可能在首渲染前发生）。
  const prevStatus = useRef<string | null>(null);
  useEffect(() => {
    const prev = prevStatus.current;
    prevStatus.current = chat.Status;
    if (prev === chat.Status) return;
    if (chat.Status !== 'ready') return;
    if (useAppStore.getState().queuedPauseActive(id)) return;
    // 出队与发送在 effect 中按序执行：dequeueChatHead 同步取走队首，
    // doSend 复用直接发送路径（失败插回队首 + 暂停）
    const head = useAppStore.getState().dequeueChatHead(id);
    if (!head) return;
    doSend({ text: head.text, blocks: head.blocks }, head);
    // doSend 闭包引用 chat（乐观更新用），状态沿触发时 chat 已是最新，无需进依赖
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [chat.Status, id]);

  // resume 在 agent 提前停止后一键续做：向同一会话再发一轮「继续」。
  // 会话上下文完整，agent 自然从断点接着做；时间线照常补录 user「继续」，透明可追溯。
  const resume = () => {
    if (running) return;
    useAppStore.getState().upsertChat({ ...chat, Status: 'running' });
    const fail = (e: unknown) => {
      useAppStore.getState().upsertChat({ ...chat, Status: 'ready' });
      useAppStore.getState().notify(t('ui.chat.send_failed', { err: backendError(e) }), 'error');
    };
    sendChatPrompt(id, t('ui.chat.resume_turn')).catch(fail);
  };

  // 撤销后端排队条目：先乐观移除，失败提示并按后端真实快照重拉对齐
  // （失败多为竞态：条目刚被投递/已在别处撤回）。
  const removeBackendQueued = async (item: ChatQueueItem) => {
    const store = useAppStore.getState();
    store.removeChatBackendQueueItem(id, item.ID);
    try {
      await removeChatQueued(id, item.ID);
    } catch (e) {
      store.notify(t('ui.chat.queue_remove_failed', { err: backendError(e) }), 'error');
      try {
        const list = await listChatQueue(id);
        useAppStore.getState().setChatBackendQueue(id, list);
      } catch {
        // 重拉也失败：保留乐观结果，下一次 chat:queue 事件或重新挂载会纠正
      }
    }
  };

  const switchModel = async (modelID: string) => {
    try {
      await setChatModel(id, modelID);
      // 乐观更新当前模型，避免等 agent 二次推送
      if (models) useAppStore.getState().setChatModels(id, { ...models, Current: modelID });
      // 回写「上次模型」：下次新建该工具会话时预填
      rememberLastModel(chat.ToolID, chat.Workspace, modelID);
    } catch (e) {
      useAppStore.getState().notify(t('ui.chat.model_switch_failed', { err: backendError(e) }), 'error');
    }
  };

  const switchConfig = async (optionID: string, value: string) => {
    // 每次都从 store 读最新快照：agent 可能已推送新状态，闭包里的 configs 会过期
    const latest = useAppStore.getState().chatConfigs[id];
    const previous = latest?.Options?.find((o) => o.ID === optionID)?.Current;
    const applyCurrent = (next: string) => {
      const snap = useAppStore.getState().chatConfigs[id];
      if (!snap) return;
      useAppStore.getState().setChatConfigs(id, {
        ...snap,
        Options: (snap.Options ?? []).map((o) => (o.ID === optionID ? { ...o, Current: next } : o)),
      });
    };
    // 先乐观更新当前值，避免等 agent 二次推送
    applyCurrent(value);
    try {
      await setChatConfigOption(id, optionID, value);
    } catch (e) {
      // 仅当界面上仍是本次尝试的值才回滚，避免覆盖 agent 期间推送的新状态
      const current = useAppStore.getState().chatConfigs[id]?.Options?.find((o) => o.ID === optionID)?.Current;
      if (current === value && previous !== undefined) applyCurrent(previous);
      useAppStore.getState().notify(t('ui.chat.config_switch_failed', { err: backendError(e) }), 'error');
    }
  };

  // Models 用 ?. 兜底：Go 侧 nil slice 序列化成 null，wailsjs 生成类型不可信
  const currentModelName =
    models?.Models?.find((m) => m.ID === models.Current)?.Name ?? models?.Current ?? '';
  // 配置行只展示 select 型非 model 项：模型已有独立下拉，避免重复选择器
  const configOptions = (configs?.Options ?? []).filter((o) => o.ID !== 'model' && o.Type === 'select');
  const showConfigRow = (models?.Models?.length ?? 0) > 0 || configOptions.length > 0;

  const setElicitationValue = (key: string, value: unknown) => {
    setElicitationValues((v) => ({ ...v, [key]: value }));
  };

  const toggleElicitationMulti = (key: string, value: string, checked: boolean) => {
    setElicitationValues((v) => {
      const cur = Array.isArray(v[key]) ? (v[key] as string[]) : [];
      return { ...v, [key]: checked ? [...cur, value] : cur.filter((x) => x !== value) };
    });
  };

  // 必填校验：空串 / null / undefined / 空数组视为未作答；false 与 0 是有效答案
  const elicitationRequiredMissing =
    (elicitation?.Fields ?? []).some((f) => {
      if (!f.Required) return false;
      const v = elicitationValues[f.Key];
      // 空白字符串不算作答；false / 0 是有效答案
      return v === undefined || v === null || (typeof v === 'string' && v.trim() === '') ||
        (Array.isArray(v) && v.length === 0);
    }) ?? false;

  const closeElicitation = () => {
    useAppStore.getState().setChatElicitation(id, null);
    setElicitationValues({});
  };

  const submitElicitation = () => {
    if (!elicitation) return;
    const req = elicitation;
    const content: Record<string, unknown> = {};
    for (const f of req.Fields ?? []) {
      const v = elicitationValues[f.Key];
      if (v === '' || v === undefined || v === null) continue;
      if (Array.isArray(v) && v.length === 0) continue;
      if (f.Kind === 'number' || f.Kind === 'integer') content[f.Key] = Number(v);
      else if (f.Kind === 'boolean') content[f.Key] = Boolean(v);
      else content[f.Key] = v;
    }
    closeElicitation();
    void respondChatElicitation(id, req.RequestID, 'accept', content);
  };

  const declineElicitation = () => {
    if (!elicitation) return;
    const req = elicitation;
    closeElicitation();
    void respondChatElicitation(id, req.RequestID, 'decline', {});
  };

  // 附件 chip 点击分派（attachmentAction 纯函数归类，副作用在此触发）：
  // preview → lightbox；default/local → 系统打开（失败 toast）；external → 外链
  // （openExternalURL 内部失败静默）；none → 不响应。
  const openAttachment = (meta: ChatAttachmentMeta) => {
    const action = attachmentAction(meta, chat.Workspace);
    switch (action.kind) {
      case 'preview':
        // meta.Name 可能为空，与 chip 展示同样用 attachmentLabel 兜底
        setPreview({ name: attachmentLabel(meta, t('ui.chat.attachment_image')), path: action.path });
        return;
      case 'default':
        openInDefaultApp(action.wsPath, action.rel).catch((e) => {
          useAppStore.getState().notify(t('ui.chat.attachment_open_failed', { err: backendError(e) }), 'error');
        });
        return;
      case 'local':
        openLocalPath(action.abs).catch((e) => {
          useAppStore.getState().notify(t('ui.chat.attachment_open_failed', { err: backendError(e) }), 'error');
        });
        return;
      case 'external':
        void openExternalURL(action.url).catch(() => {});
        return;
      case 'none':
        return;
    }
  };

  // 时间线附件 chips：user 靠右（justify-end）、assistant 靠左（justify-start）。
  // 可点的 meta 渲染 button，旧数据（attachmentAction 归 none）保持 span 不可点。
  const renderAttachmentChips = (atts: ChatAttachmentMeta[] | undefined, align: 'start' | 'end') => {
    if (!atts || atts.length === 0) return null;
    return (
      <div className={cn('mt-0.5 flex flex-wrap gap-1 text-xs text-muted-foreground', align === 'end' ? 'justify-end' : 'justify-start')}>
        {atts.map((att, i) => {
          const label = attachmentLabel(att, t('ui.chat.attachment_image'));
          const clickable = attachmentAction(att, chat.Workspace).kind !== 'none';
          return clickable ? (
            <button
              key={i}
              className="cursor-pointer rounded border border-border px-1 py-0.5 hover:bg-muted"
              onClick={() => openAttachment(att)}
            >
              {label}
            </button>
          ) : (
            <span key={i} className="rounded border border-border px-1 py-0.5">
              {label}
            </span>
          );
        })}
      </div>
    );
  };

  return (
    <div
      className="relative flex h-full min-h-0 flex-col"
      data-drop-zone={`chat:${id}`}
      data-kshell-workspace={chat.Workspace}
      onClickCapture={(e) => {
        handleAnchorClick(e.nativeEvent, { workspaceRoot: chat.Workspace });
      }}
      onDragOver={(e) => {
        if (!e.dataTransfer.types.includes(DRAG_MIME)) return;
        e.preventDefault();
        setDropHint(true);
      }}
      onDragLeave={() => setDropHint(false)}
      onDrop={(e) => {
        setDropHint(false);
        if (!e.dataTransfer.types.includes(DRAG_MIME)) return;
        e.preventDefault();
        const p = e.dataTransfer.getData(DRAG_MIME);
        if (!p) return;
        if (!appendChatInput(id, quotePathForShell(p))) {
          useAppStore.getState().notify(t('ui.chat.drop_unsupported'), 'error');
        }
      }}
    >
      {chat.Status === 'exited' && (
        <div className="shrink-0 break-words bg-muted px-2 py-0.5 text-xs text-muted-foreground">
          {t('ui.chat.exited')}
          {chat.ExitCode ? t('ui.chat.exited_code', { code: chat.ExitCode }) : ''}
          {chat.Error ? t('ui.chat.error_suffix', { error: translateBackend(chat.Error) }) : ''}
        </div>
      )}
      {chat.Status === 'authenticating' && (
        <div className="shrink-0 break-words bg-muted px-2 py-0.5 text-xs text-muted-foreground">
          {t('ui.chat.status_authenticating')}
        </div>
      )}
      {showConfigRow && (
        <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border px-3 py-1 text-xs">
          {models && models.Models && models.Models.length > 0 && (
            <>
              <span className="text-muted-foreground">{t('ui.chat.model_label')}</span>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <button className="rounded border border-border px-2 py-0.5 hover:bg-muted">
                    {currentModelName} <span className="text-muted-foreground">▾</span>
                  </button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start">
                  {models.Models?.map((m) => (
                    <DropdownMenuItem key={m.ID} onSelect={() => void switchModel(m.ID)}>
                      <span className={cn(m.ID === models.Current && 'font-medium')}>{m.Name}</span>
                      {m.Description && <span className="ml-2 text-muted-foreground">{m.Description}</span>}
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuContent>
              </DropdownMenu>
            </>
          )}
          {configOptions.map((opt) => (
            <div key={opt.ID} className="flex items-center gap-1">
              <span className="text-muted-foreground">{opt.Name}</span>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <button aria-label={opt.Name} className="rounded border border-border px-2 py-0.5 hover:bg-muted">
                    {(opt.Options ?? []).find((o) => o.Value === opt.Current)?.Name ?? opt.Current}{' '}
                    <span className="text-muted-foreground">▾</span>
                  </button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start">
                  {(opt.Options ?? []).map((o) => (
                    <DropdownMenuItem key={o.Value} onSelect={() => void switchConfig(opt.ID, o.Value)}>
                      <span className={cn(o.Value === opt.Current && 'font-medium')}>{o.Name}</span>
                      {o.Description && <span className="ml-2 text-muted-foreground">{o.Description}</span>}
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          ))}
        </div>
      )}
      <div ref={scrollRef} onScroll={handleScroll} className="flex-1 overflow-y-auto overflow-x-hidden px-3 py-2">
        {items.map((it) => {
          if (it.type === 'user' || it.type === 'assistant') {
            return (
              <div key={it.key} className={cn('mb-3', it.type === 'user' ? 'text-right' : '')}>
                {/* wrapper 用 text-right 让气泡靠右；气泡本体 text-left 保证文字左对齐 */}
                <div className={cn('selectable inline-block max-w-[85%] break-words overflow-hidden rounded-md px-3 py-1.5 text-left text-sm [&_pre]:whitespace-pre-wrap',
                  it.type === 'user' ? 'bg-primary text-primary-foreground' : 'bg-muted')}>
                {/* markdown 内容元素样式与 AgentHub/预览共用 markdownBubbleClass；pre 折行策略归容器管 */}
                {/* 展示层还原百分号编码文件名；存储原文不动 */}
                {it.type === 'assistant'
                  ? <div className={markdownBubbleClass}><ReactMarkdown remarkPlugins={[remarkGfm]}>{decodePercentText(it.text ?? '')}</ReactMarkdown></div>
                  : <span className="whitespace-pre-wrap">{decodePercentText(it.text ?? '')}</span>}
              </div>
              {renderAttachmentChips(it.attachments, it.type === 'user' ? 'end' : 'start')}
            </div>
          );
          }
          if (it.type === 'thought') {
            return (
              <details key={it.key} className="mb-3 text-xs text-muted-foreground">
                <summary className="cursor-pointer select-none italic">{t('ui.chat.thought')}</summary>
                <div className="selectable mt-1 whitespace-pre-wrap break-words rounded border border-border px-2 py-1 italic">
                  {decodePercentText(it.text ?? '')}
                </div>
              </details>
            );
          }
          if (it.type === 'tool' && it.tool) {
            const content = toolContentText(it.tool);
            return (
              <div key={it.key} className="mb-2 rounded border border-border px-2 py-1 text-xs">
                <span className="font-medium">{it.tool.Title || it.tool.Name || t('ui.chat.tool_fallback')}</span>
                <span className="ml-2 text-muted-foreground">{it.tool.Kind}{it.tool.Status ? ` · ${it.tool.Status}` : ''}</span>
                {content && (
                  <pre className="selectable mt-1 max-h-48 overflow-y-auto whitespace-pre-wrap break-words text-muted-foreground">
                    {content}
                  </pre>
                )}
              </div>
            );
          }
          if (it.type === 'plan') {
            return (
              <ul key={it.key} className="selectable mb-2 list-disc break-words pl-5 text-xs text-muted-foreground">
                {(it.plan ?? []).map((e, i) => <li key={i}>{e.Content}</li>)}
              </ul>
            );
          }
          if (it.type === 'error') {
            return (
              <div key={it.key} className="selectable mb-2 break-words text-xs text-destructive">
                {t('ui.chat.error_prefix', { text: translateBackend(it.text ?? '') })}
              </div>
            );
          }
          if (it.type === 'turn_done') {
            // 此前 turn_done 完全不渲染：agent 因 max_turn_requests / max_tokens
            // 等提前停止时毫无提示。正常结束（end_turn/空）维持不渲染。
            const stop = it.stopReason ?? '';
            if (stop === '' || stop === 'end_turn') return null;
            const known = STOP_REASON_KEYS[stop];
            return (
              <div
                key={it.key}
                data-testid="chat-turn-stopped"
                className="selectable mb-2 break-words text-xs text-muted-foreground"
              >
                <div className="flex items-center gap-2">
                  <span className="min-w-0 flex-1">{t('ui.chat.turn_stopped', { reason: known ? t(known) : stop })}</span>
                  <button
                    data-testid="chat-resume"
                    className="shrink-0 rounded border border-border px-2 py-0.5 hover:bg-muted"
                    onClick={resume}
                  >
                    {t('ui.chat.resume_turn')}
                  </button>
                </div>
                {it.text && (
                  // 后端在异常停止时附带 agent stderr 尾部（如 429 限流文本），折叠展示避免刷屏。
                  <details data-testid="chat-stderr-detail" className="mt-1">
                    <summary className="cursor-pointer select-none">{t('ui.chat.agent_stderr')}</summary>
                    <pre className="selectable mt-1 max-h-48 overflow-y-auto whitespace-pre-wrap break-words">
                      {it.text}
                    </pre>
                  </details>
                )}
              </div>
            );
          }
          return null;
        })}
      </div>

      <div className="shrink-0 border-t border-border p-2">
        {backendQueued.length + queued.length > 0 && (
          <div data-testid="chat-queue" className="mb-1 flex flex-col gap-0.5">
            {/* 后端队列在前：这些消息已交给 agent 排队，撤销走真移除 */}
            {backendQueued.map((item) => (
              <span key={`backend-${item.ID}`} className="flex items-center gap-1 rounded border border-border bg-card px-1.5 py-0.5 text-xs">
                <span className="min-w-0 flex-1 truncate text-muted-foreground">{item.Text || t('ui.chat.queue_attachment')}</span>
                <button
                  aria-label={t('ui.chat.queue_remove')}
                  data-testid={`chat-queue-remove-backend-${item.ID}`}
                  className="text-muted-foreground hover:text-destructive"
                  onClick={() => void removeBackendQueued(item)}
                >
                  ×
                </button>
              </span>
            ))}
            {/* 本地队列在后：尚未交给后端，撤回只出前端队列 */}
            {queued.map((m) => (
              <span key={m.id} className="flex items-center gap-1 rounded border border-border bg-card px-1.5 py-0.5 text-xs">
                <span className="min-w-0 flex-1 truncate text-muted-foreground">{m.text || t('ui.chat.queue_attachment')}</span>
                <button
                  aria-label={t('ui.chat.queue_remove')}
                  data-testid={`chat-queue-remove-${m.id}`}
                  className="text-muted-foreground hover:text-destructive"
                  onClick={() => useAppStore.getState().removeQueuedChat(id, m.id)}
                >
                  ×
                </button>
              </span>
            ))}
          </div>
        )}
        {attachments.length > 0 && (
          <div className="mb-1 flex flex-wrap gap-1">
            {attachments.map((a, i) => (
              <span key={i} className="flex items-center gap-1 rounded border border-border bg-card px-1.5 py-0.5 text-xs">
                {a.label}
                <button
                  aria-label={t('ui.chat.remove_attachment')}
                  className="text-muted-foreground hover:text-destructive"
                  onClick={() => removeAttachment(i)}
                >
                  ×
                </button>
              </span>
            ))}
          </div>
        )}
        {slashOpen && (
          <ul role="listbox" aria-label={t('ui.chat.input_placeholder')}
            className="mb-1 max-h-48 overflow-y-auto rounded border border-border bg-card py-1 text-xs shadow-md">
            {slashMatches.map((c, i) => (
              <li key={c.Name} role="option" aria-selected={i === slashIndex}
                className={cn('cursor-pointer px-2 py-1', i === slashIndex && 'bg-muted')}
                onMouseDown={(e) => {
                  e.preventDefault();
                  applySlash(c.Name);
                }}>
                <span className="font-medium">/{c.Name}</span>
                {c.Description && <span className="ml-2 text-muted-foreground">{c.Description}</span>}
                {c.InputHint && <span className="ml-1 text-muted-foreground">({c.InputHint})</span>}
              </li>
            ))}
          </ul>
        )}
        <textarea
          className="min-h-16 w-full resize-none rounded border border-border bg-card p-2 text-sm"
          placeholder={t('ui.chat.input_placeholder')}
          value={draft}
          disabled={chat.Status !== 'ready' && chat.Status !== 'running'}
          onChange={(e) => setDraft(e.target.value)}
          onPaste={(e) => {
            const files = e.clipboardData?.files;
            if (!files || files.length === 0) return;
            e.preventDefault();
            void addFileAttachments(Array.from(files));
          }}
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
                applySlash(slashMatches[Math.min(slashIndex, slashMatches.length - 1)].Name);
                return;
              }
              if (e.key === 'Escape') {
                e.preventDefault();
                setSlashClosed(true);
                return;
              }
            }
            if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); }
          }}
        />
        <div className="mt-1 flex items-center justify-end gap-2">
          <Button variant="ghost" size="sm" className="text-muted-foreground" onClick={() => void attachByPicker()}>
            {t('ui.chat.attach')}
          </Button>
          {running
            ? <Button variant="outline" size="sm" onClick={() => void cancelChat(id)}>{t('ui.chat.stop')}</Button>
            : <Button size="sm" onClick={send}>{t('ui.chat.send')}</Button>}
        </div>
      </div>

      {dropHint && (
        <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center rounded border-2 border-dashed border-primary bg-primary/10 text-sm">
          {t('ui.chat.drop_hint')}
        </div>
      )}

      {preview && (
        <ImageLightbox name={preview.name} path={preview.path} onClose={() => setPreview(null)} />
      )}

      {permission && (
        <Dialog
          open
          onOpenChange={(next) => {
            if (next) return;
            // Esc / 遮罩点击关闭等同「拒绝」：先乐观清掉弹窗，再通知后端取消该次请求
            useAppStore.getState().setChatPermission(id, null);
            void cancelChatPermission(id, permission.RequestID);
          }}
          className="w-96"
        >
          <DialogPrimitive.Title className="mb-1 text-sm font-medium">{t('ui.chat.permission_title')}</DialogPrimitive.Title>
          <p className="mb-3 text-xs text-muted-foreground">{permission.ToolCall.Title || permission.ToolCall.Name || t('ui.chat.tool_call_fallback')}</p>
          <div className="flex flex-col gap-2">
            {/* Go 侧 nil slice 序列化为 null（agent 推送空 Options 时），必须兜底空数组 */}
            {(permission.Options ?? []).map((o) => (
              <button key={o.OptionID} className="rounded border border-border px-3 py-1.5 text-sm"
                onClick={() => {
                  // 先乐观清掉弹窗，避免 api 往返期间遮罩不消失
                  useAppStore.getState().setChatPermission(id, null);
                  void respondChatPermission(id, permission.RequestID, o.OptionID);
                }}>
                {o.Name}
              </button>
            ))}
            <button className="rounded px-3 py-1.5 text-xs text-muted-foreground"
              onClick={() => {
                useAppStore.getState().setChatPermission(id, null);
                void cancelChatPermission(id, permission.RequestID);
              }}>{t('ui.chat.reject')}</button>
          </div>
        </Dialog>
      )}

      {elicitation && (
        <Dialog
          open
          onOpenChange={(next) => {
            if (next) return;
            // Esc / 遮罩点击等同放弃：清掉表单并通知后端取消该次请求
            closeElicitation();
            void cancelChatElicitation(id, elicitation.RequestID);
          }}
          className="w-[28rem]"
        >
          <DialogPrimitive.Title className="mb-1 text-sm font-medium">{t('ui.chat.elicitation_title')}</DialogPrimitive.Title>
          <p className="mb-3 break-words text-xs text-muted-foreground">{elicitation.Message}</p>
          <div className="flex flex-col gap-3">
            {(elicitation.Fields ?? []).map((f) => {
              const label = f.Title || f.Key;
              const controlId = `elicitation-${id}-${f.Key}`;
              const value = elicitationValues[f.Key];
              if (f.Kind === 'boolean') {
                return (
                  <label key={f.Key} className="flex items-center gap-2 text-sm">
                    <input
                      type="checkbox"
                      aria-label={f.Key}
                      checked={Boolean(value)}
                      onChange={(e) => setElicitationValue(f.Key, e.target.checked)}
                    />
                    <span>{label}</span>
                  </label>
                );
              }
              if (f.Kind === 'select') {
                return (
                  <label key={f.Key} htmlFor={controlId} className="flex flex-col gap-1 text-sm">
                    <span>{label}</span>
                    <select
                      id={controlId}
                      aria-label={f.Key}
                      className="rounded border border-border bg-card px-2 py-1 text-sm"
                      value={value === undefined || value === null ? '' : String(value)}
                      onChange={(e) => setElicitationValue(f.Key, e.target.value)}
                    >
                      <option value="" />
                      {(f.Options ?? []).map((o) => (
                        <option key={o.Value} value={o.Value}>{o.Title || o.Value}</option>
                      ))}
                    </select>
                  </label>
                );
              }
              if (f.Kind === 'multiselect') {
                const selected = Array.isArray(value) ? (value as string[]) : [];
                return (
                  <fieldset key={f.Key} aria-label={f.Key} className="flex flex-col gap-1 text-sm">
                    <legend>{label}</legend>
                    {(f.Options ?? []).map((o) => (
                      <label key={o.Value} className="flex items-center gap-2">
                        <input
                          type="checkbox"
                          aria-label={`${f.Key}:${o.Value}`}
                          checked={selected.includes(o.Value)}
                          onChange={(e) => toggleElicitationMulti(f.Key, o.Value, e.target.checked)}
                        />
                        <span>{o.Title || o.Value}</span>
                      </label>
                    ))}
                  </fieldset>
                );
              }
              const inputType = f.Kind === 'number' || f.Kind === 'integer' ? 'number' : 'text';
              return (
                <label key={f.Key} htmlFor={controlId} className="flex flex-col gap-1 text-sm">
                  <span>{label}</span>
                  <input
                    id={controlId}
                    type={inputType}
                    aria-label={f.Key}
                    className="rounded border border-border bg-card px-2 py-1 text-sm"
                    value={value === undefined || value === null ? '' : String(value)}
                    onChange={(e) => setElicitationValue(f.Key, e.target.value)}
                  />
                </label>
              );
            })}
          </div>
          <div className="mt-4 flex items-center justify-end gap-2">
            <Button variant="outline" size="sm" onClick={declineElicitation}>{t('ui.chat.elicitation_decline')}</Button>
            <Button size="sm" disabled={elicitationRequiredMissing} onClick={submitElicitation}>{t('ui.chat.elicitation_submit')}</Button>
          </div>
        </Dialog>
      )}
    </div>
  );
}
