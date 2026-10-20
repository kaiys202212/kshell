// AgentHub 回复能力三态判定（纯函数）：与后端 wxExecutor/launch.ACPCapable
// 同一语义，一侧改动必须同步另一侧。动态判定，不落盘。
import type { HubMessage, TerminalInfo, ToolInfo } from './api';

export type HubReplyState = 'acp' | 'tool' | 'terminal';

// isACPTool 近似判定：适配器探测可用即支持；cursor 官方直连是探测之外的额外
// 路径（shim 兜底基本常在），按已安装近似。工具未知返回 false（只读展示），
// 权威判定在后端 ForSessionACP，激活失败仍有显式报错兜底。
export function isACPTool(tools: ToolInfo[], toolId?: string): boolean {
  if (!toolId) return false;
  if (toolId === 'cursor') {
    return tools.find((t) => t.ID === toolId)?.Installed === true;
  }
  return tools.find((t) => t.ID === toolId)?.ACP?.Available === true;
}

// terminalActive 判定：桌面端终端页签在册即占用。
// - termKey 以 session: 开头且 Key 精确匹配；或
// - sessionId 非空且存在 Key=session:<sessionId> 的终端
// new: 前缀不参与（chat/terminal 共用 new:<n> 形态，防误伤 ACP 新建会话；
// 与后端 terminalSessionIDs 同一约束）。
export function terminalActive(
  termKey: string | undefined,
  terminals: TerminalInfo[],
  sessionId?: string,
): boolean {
  if (termKey?.startsWith('session:') && terminals.some((t) => t.Key === termKey)) {
    return true;
  }
  if (sessionId && terminals.some((t) => t.Key === `session:${sessionId}`)) {
    return true;
  }
  return false;
}

// replyState 按序判定：非 ACP 工具 → tool（永久只读）；终端在册 → terminal
//（激活期只读，关闭后自动恢复）；其余 → acp（可回复，离线走 openSessionACP）。
export function replyState(m: HubMessage, tools: ToolInfo[], terminals: TerminalInfo[]): HubReplyState {
  if (!isACPTool(tools, m.tool)) return 'tool';
  if (terminalActive(m.termKey, terminals, m.sessionId)) return 'terminal';
  return 'acp';
}
