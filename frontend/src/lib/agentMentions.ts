// 汇总聊天窗的 @ 提及解析：把跨项目 agent 列表编译成唯一展示名，
// 再从用户输入里解析出投递目标。纯函数，便于单测。
import { toolDisplayName } from './agentNotify';

export interface MentionAgent {
  id: string;      // chat.ID
  toolId: string;  // 如 claude / cursor
  title: string;   // 会话标题
  workspace: string;
}

export interface MentionOption {
  id: string;
  label: string; // 唯一展示名，形如 Claude Code#c3
}

// buildMentionOptions 生成补全项：用工具展示名 + 会话 id（后端稳定自增 c1/c2）拼唯一展示名。
// 必须顺序无关：早先按列表顺序编号（#1/#2）会在 scan:done 重建 chats 后漂移，
// 同一条 @…#2 会投给不同会话。改用 id 后，label↔id 映射不随列表顺序变化。
export function buildMentionOptions(agents: MentionAgent[]): MentionOption[] {
  return agents.map((a) => ({
    id: a.id,
    label: `${toolDisplayName(a.toolId)}#${a.id}`,
  }));
}

// resolveMentionTarget 解析投递目标：
// - 输入里有命中补全项的 @ 时，取最后一个命中项（v1 简化：不做多目标广播）；
// - 无 @ 时回落到 lastAgentId；
// - 词首的 @（前为行首或空白）其后非空白一段未命中任何候选 label 时，视为无效提及，
//   返回 null（宁可报错也不误投）。
export function resolveMentionTarget(
  text: string,
  options: MentionOption[],
  lastAgentId: string | null,
): string | null {
  // 长标签优先匹配，避免前缀误伤
  const sorted = [...options].sort((a, b) => b.label.length - a.label.length);
  let found: string | null = null;
  let foundIndex = -1;
  for (const opt of sorted) {
    const at = `@${opt.label}`;
    const idx = text.lastIndexOf(at);
    if (idx >= 0 && idx > foundIndex) {
      foundIndex = idx;
      found = opt.id;
    }
  }
  if (found) return found;
  // 仅把「词首 @」当作提及尝试：邮箱 a@b.com 的 @ 不在词首，不构成无效提及。
  if (/(^|\s)@[^\s@]+/.test(text)) return null;
  return lastAgentId;
}
