// AgentHub 消息归因：把 hub 消息的 workspace/chatId/termKey 反查成
// 「项目名 + session 名」供气泡与列表展示。纯函数，便于单测。
// 旧历史消息可能缺字段：缺什么少显示什么，全缺返回空对象。
import type { ChatInfo, TerminalInfo } from './api';
import type { HubMessage } from './api';

export interface HubAttribution {
  project?: string;
  session?: string;
}

// baseName 取路径最后一段（兼容 / 与 \，容忍尾随分隔符）。
export function baseName(p: string): string {
  const n = p.replace(/[\\/]+$/, '');
  const i = Math.max(n.lastIndexOf('/'), n.lastIndexOf('\\'));
  return i >= 0 ? n.slice(i + 1) : n;
}

// titleByChatId：chats 按 ID 找标题（空标题视为未命中）。
function titleByChatId(chats: ChatInfo[], chatId?: string): string | undefined {
  if (!chatId) return undefined;
  const c = chats.find((x) => x.ID === chatId);
  return c?.Title || undefined;
}

// titleByTermKey：chats / terminals 按 Key（termKey）找标题。
function titleByTermKey(chats: ChatInfo[], terminals: TerminalInfo[], termKey?: string): string | undefined {
  if (!termKey) return undefined;
  const c = chats.find((x) => x.Key === termKey);
  if (c?.Title) return c.Title;
  const tm = terminals.find((x) => x.Key === termKey);
  return tm?.Title || undefined;
}

// workspaceOf：消息自带 workspace 优先；否则按 chatId / termKey 反查。
function workspaceOf(
  m: Pick<HubMessage, 'workspace' | 'chatId' | 'termKey'>,
  chats: ChatInfo[],
  terminals: TerminalInfo[],
): string | undefined {
  if (m.workspace) return m.workspace;
  if (m.chatId) {
    const c = chats.find((x) => x.ID === m.chatId);
    if (c?.Workspace) return c.Workspace;
  }
  if (m.termKey) {
    const tm = terminals.find((x) => x.Key === m.termKey);
    if (tm?.Workspace) return tm.Workspace;
  }
  return undefined;
}

// resolveAttribution 把一条 hub 消息解析成展示用归因。
// - project：workspace 路径末段基名。
// - session：优先 chatId 命中的会话标题，其次 termKey 命中，再退回 `#<id>` 原始标识。
// 全部未知时返回 {}，调用方不渲染归因行。
export function resolveAttribution(
  m: Pick<HubMessage, 'workspace' | 'chatId' | 'termKey'>,
  chats: ChatInfo[],
  terminals: TerminalInfo[],
): HubAttribution {
  const out: HubAttribution = {};
  const ws = workspaceOf(m, chats, terminals);
  if (ws) out.project = baseName(ws);
  const session =
    titleByChatId(chats, m.chatId) ?? titleByTermKey(chats, terminals, m.termKey);
  if (session) out.session = session;
  else if (m.chatId) out.session = `#${m.chatId}`;
  else if (m.termKey) out.session = `#${m.termKey}`;
  return out;
}

// attributionText 把归因拼成展示文本：「项目 · 会话」，缺项跳过，全缺返回空串。
export function attributionText(a: HubAttribution): string {
  return [a.project, a.session].filter(Boolean).join(' · ');
}
