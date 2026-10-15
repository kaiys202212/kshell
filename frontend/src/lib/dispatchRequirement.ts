// 需求派发：把一条需求投递到目标工作区的 ACP agent 会话。
// 步骤：选工具 → openWorkspaceACP 建会话 → sendChatPrompt 发需求文本 →
// markRequirementDispatched 落盘已派发标记 → setLastAgentId 更新默认投递目标。
// 每步失败直接向上抛，由调用方（Home 卡片 onDrop）统一 catch 后 notify。
//
// 放在 lib 的成环性说明：本文件值引用 state/store 的 useAppStore；
// store.ts 对 lib/* 只有 import type（值链 api→terminalBusy→store 不经过本文件，
// 且 store 不 import 本文件），因此无循环依赖。
import { getTools, markRequirementDispatched, openWorkspaceACP, sendChatPrompt } from './api';
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
  // chatID 暂传 '' 占位，后续任务接入真实会话 ID
  await markRequirementDispatched(id, wsPath, '');
  useAppStore.getState().setLastAgentId(chat.ID);
}
