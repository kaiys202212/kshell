// 需求派发与重新打开：把一条需求投递到目标工作区的 ACP agent 会话。
// 派发步骤：选工具 → openWorkspaceACP 建会话 → sendChatPrompt 发需求文本 →
// markRequirementDispatched 落盘已派发标记（含会话标题快照）→ setLastAgentId。
// 重开步骤（会话已不存在时）：选工具 → openWorkspaceACP → markRequirementDispatched
// 更新绑定，不发需求文本（会话历史还在，只是进程没了）。
// 每步失败直接向上抛，由调用方统一 catch 后 notify。
//
// 放在 lib 的成环性说明：本文件值引用 state/store 的 useAppStore；
// store.ts 对 lib/* 只有 import type（值链 api→terminalBusy→store 不经过本文件，
// 且 store 不 import 本文件），因此无循环依赖。
import { getTools, markRequirementDispatched, openWorkspaceACP, sendChatPrompt, type OpenedSession, type Requirement } from './api';
import { useAppStore } from '../state/store';

// resolveToolID 选派发用的 ACP 工具：优先当前默认 agent（lastAgentId 对应 chat 存在），
// 否则取第一个 ACP 可用的工具；都没有返回 null（调用方提示 no_acp_tool）。
export async function resolveToolID(): Promise<string | null> {
  const { chats, lastAgentId } = useAppStore.getState();
  const last = lastAgentId ? chats.find((c) => c.ID === lastAgentId) : undefined;
  if (last?.ToolID) return last.ToolID;
  const tools = await getTools();
  const acp = tools.find((x) => x.ACP?.Available === true);
  return acp?.ID ?? null;
}

// dispatchRequirement 执行完整派发流程；失败向上抛，由调用方统一呈现。
export async function dispatchRequirement(id: string, text: string, wsPath: string): Promise<void> {
  const toolID = await resolveToolID();
  if (!toolID) throw new Error('no_acp_tool');
  const result = await openWorkspaceACP(wsPath, toolID);
  if (!result.Chat) throw new Error('no_chat');
  const chat = result.Chat;
  await sendChatPrompt(chat.ID, text);
  // 落盘派发标记：关联真实会话 ID 与标题快照，归档时据此判断会话能否自动关闭
  await markRequirementDispatched(id, wsPath, chat.ID, chat.Title);
  useAppStore.getState().setLastAgentId(chat.ID);
}

// reopenRequirementSession 重新打开已失效会话（应用重启后 chatId 不再指向活跃会话）：
// 在原工作区建新会话并更新需求的会话绑定。不发需求文本——会话历史仍在磁盘，
// 重开只是恢复一个可继续对话的入口。失败向上抛，由调用方统一呈现。
export async function reopenRequirementSession(r: Requirement): Promise<OpenedSession> {
  if (!r.dispatchedTo) throw new Error('no_workspace');
  const toolID = await resolveToolID();
  if (!toolID) throw new Error('no_acp_tool');
  const result = await openWorkspaceACP(r.dispatchedTo, toolID);
  if (!result.Chat) throw new Error('no_chat');
  const chat = result.Chat;
  await markRequirementDispatched(r.id, r.dispatchedTo, chat.ID, chat.Title);
  useAppStore.getState().upsertChat(chat);
  useAppStore.getState().setLastAgentId(chat.ID);
  return result;
}
