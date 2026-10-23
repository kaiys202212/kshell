// 需求派发与重新打开：把一条需求投递到目标工作区的 ACP agent 会话。
// 派发步骤：选工具 → openWorkspaceACP 建会话 →（可选）setChatModel →
// sendChatPrompt 发需求文本 → markRequirementDispatched 落盘已派发标记
// （含会话标题快照）→ setLastAgentId。
// 重开步骤（会话已不存在时）：选工具 → openWorkspaceACP → markRequirementDispatched
// 更新绑定，不发需求文本（会话历史还在，只是进程没了）。
// 每步失败直接向上抛，由调用方统一 catch 后 notify。
//
// 放在 lib 的成环性说明：本文件值引用 state/store 的 useAppStore；
// store.ts 对 lib/* 只有 import type（值链 api→terminalBusy→store 不经过本文件，
// 且 store 不 import 本文件），因此无循环依赖。
import i18n from 'i18next';
import { getTools, markRequirementDispatched, openWorkspaceACP, sendChatPrompt, setChatModel, type OpenedSession, type Requirement } from './api';
import { backendError } from './errors';
import { usableForACP } from './toolUsable';
import { useAppStore } from '../state/store';

// resolveToolID 选派发用的 ACP 工具：优先当前默认 agent（lastAgentId 对应 chat 存在），
// 否则取第一个可用的 ACP 工具（lib/toolUsable：Auth=out 不可用）；都没有返回 null
//（调用方提示 no_acp_tool）。
export async function resolveToolID(): Promise<string | null> {
  const { chats, lastAgentId } = useAppStore.getState();
  const last = lastAgentId ? chats.find((c) => c.ID === lastAgentId) : undefined;
  if (last?.ToolID) return last.ToolID;
  const tools = await getTools();
  const acp = tools.find(usableForACP);
  return acp?.ID ?? null;
}

// DispatchDefaults 是用户显式设置的默认派发配置（来自 loadDefaultCreate）：
// toolId 经 ACP 可用性校验后优先于自动选择；modelId 在发需求文本前切换，
// 确保首轮回复就用对模型。
export interface DispatchDefaults {
  toolId: string;
  modelId: string | null;
}

// notifyFallbackTool 默认工具不可用时的提示；lib 层无 useTranslation，
// 直接用全局 i18n 实例（测试环境由 setup.ts 统一初始化）。
function notifyFallbackTool(): void {
  useAppStore.getState().notify(i18n.t('ui.hub.default_tool_unavailable'), 'info');
}

// 在途守卫：派发是长操作（建会话→切模型→发文本→落盘），期间重复触发
// （如连拖两次同一需求）会建两个会话、prompt 发两遍。模块级集合互斥，
// 已在途的 id 直接返回 'busy' 由调用方提示；finally 保证失败路径也清理。
const dispatchingIds = new Set<string>();

// DispatchResult：'busy' 表示该需求已有一次派发在途（本次未执行任何流程）
export type DispatchResult = 'ok' | 'busy';

// dispatchRequirement 执行完整派发流程；失败向上抛，由调用方统一呈现。
// defaults 提供时：toolId 经 getTools 校验 ACP 可用则替代自动选择，
// 不可用则提示并回落 resolveToolID；modelId 有值在建会话后、发文本前切换，
// 切换失败不阻断派发（首轮可能落到工具默认模型）。
export async function dispatchRequirement(
  id: string,
  text: string,
  wsPath: string,
  defaults?: DispatchDefaults,
): Promise<DispatchResult> {
  if (dispatchingIds.has(id)) return 'busy';
  dispatchingIds.add(id);
  try {
    let toolID: string | null = null;
    if (defaults?.toolId) {
      const tools = await getTools();
      const ok = tools.some((x) => x.ID === defaults.toolId && usableForACP(x));
      if (ok) {
        toolID = defaults.toolId;
      } else {
        notifyFallbackTool();
      }
    }
    if (!toolID) toolID = await resolveToolID();
    if (!toolID) throw new Error('no_acp_tool');
    const result = await openWorkspaceACP(wsPath, toolID, defaults?.modelId ?? null);
    if (!result.Chat) throw new Error('no_chat');
    const chat = result.Chat;
    if (defaults?.modelId) {
      try {
        await setChatModel(chat.ID, defaults.modelId);
      } catch (e: unknown) {
        // 切模型失败不阻断派发：提示后继续用工具默认模型发需求
        useAppStore
          .getState()
          .notify(i18n.t('ui.hub.model_switch_failed', { err: backendError(e) }), 'error');
      }
    }
    await sendChatPrompt(chat.ID, text);
    // 落盘派发标记：关联真实会话 ID 与标题快照、磁盘会话 ID（重启后恢复绑定），
    // 归档时据此判断会话能否自动关闭
    await markRequirementDispatched(id, wsPath, chat.ID, chat.Title, chat.SessionID);
    useAppStore.getState().setLastAgentId(chat.ID);
    return 'ok';
  } finally {
    dispatchingIds.delete(id);
  }
}

// reopenRequirementSession 重新打开已失效会话（应用重启后 chatId 不再指向活跃会话）：
// 在原工作区建新会话并更新需求的会话绑定。不发需求文本——会话历史仍在磁盘，
// 重开只是恢复一个可继续对话的入口。失败向上抛，由调用方统一呈现。
export async function reopenRequirementSession(r: Requirement): Promise<OpenedSession> {
  if (!r.dispatchedTo) throw new Error('no_workspace');
  const toolID = await resolveToolID();
  if (!toolID) throw new Error('no_acp_tool');
  const result = await openWorkspaceACP(r.dispatchedTo, toolID, null);
  if (!result.Chat) throw new Error('no_chat');
  const chat = result.Chat;
  await markRequirementDispatched(r.id, r.dispatchedTo, chat.ID, chat.Title, chat.SessionID);
  useAppStore.getState().upsertChat(chat);
  useAppStore.getState().setLastAgentId(chat.ID);
  return result;
}
