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
  label: string; // 唯一展示名，形如 Claude Code#1
}

// buildMentionOptions 生成补全项：按工具展示名分组编号，保证同名唯一。
// 编号按输入顺序（即 store.chats 顺序），稳定可预期。
export function buildMentionOptions(agents: MentionAgent[]): MentionOption[] {
  const counts = new Map<string, number>();
  return agents.map((a) => {
    const base = toolDisplayName(a.toolId);
    const n = (counts.get(base) ?? 0) + 1;
    counts.set(base, n);
    return { id: a.id, label: `${base}#${n}` };
  });
}

// resolveMentionTarget 解析投递目标：
// - 输入里有命中补全项的 @ 时，取最后一个命中项（v1 简化：不做多目标广播）；
// - 无 @ 时回落到 lastAgentId；
// - @ 了不存在的名字视为无效，返回 null（宁可报错也不误投）。
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
  // 有 @ 但无命中（乱写）→ 不投，避免误发
  if (/@\S/.test(text)) return null;
  return lastAgentId;
}
