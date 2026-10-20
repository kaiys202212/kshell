// 汇总聊天窗的 @ 提及解析：把跨项目 agent 列表编译成唯一展示名，
// 再从用户输入里解析出投递目标。纯函数，便于单测。
import { toolDisplayName } from './agentNotify';
import { baseName } from './hubAttribution';

export interface MentionAgent {
  id: string;      // chat.ID
  toolId: string;  // 如 claude / cursor
  title: string;   // 会话标题
  workspace: string;
}

export interface MentionOption {
  id: string;
  label: string;  // 路由标签：工具名#会话 id，稳定且无空格
  insert: string; // 写入输入框 / 展示用：工具名#会话标题（撞名时退回 id）
  detail?: string; // 第二行补充「项目名 · 会话标题」，仅展示
}

// mentionFace 是 @ 标签里给人看的那段：会话标题优先，否则项目基名。
function mentionFace(a: MentionAgent): string {
  const title = a.title.trim();
  if (title) return title;
  return a.workspace ? baseName(a.workspace) : '';
}

// visibleMentionLabel 拼写入草稿和气泡的 @ 标签。
// 路由仍认工具名#id（见 label）：id 不随标题改名或列表重排漂移。
// 同一工具下展示名相同时退回 id，避免两段相同文字投错会话；
// 标题相同但项目不同时用「标题 · 项目」区分。
export function visibleMentionLabel(agent: MentionAgent, all: MentionAgent[]): string {
  const tool = toolDisplayName(agent.toolId);
  const route = `${tool}#${agent.id}`;
  const name = mentionFace(agent);
  if (!name) return route;
  const peers = all.filter((b) => toolDisplayName(b.toolId) === tool);
  const sameName = peers.filter((b) => mentionFace(b) === name);
  if (sameName.length <= 1) return `${tool}#${name}`;
  const title = agent.title.trim();
  const project = agent.workspace ? baseName(agent.workspace) : '';
  if (title && project) {
    const qualified = `${title} · ${project}`;
    const sameQ = peers.filter((b) => {
      const t = b.title.trim();
      const p = b.workspace ? baseName(b.workspace) : '';
      return t !== '' && p !== '' && `${t} · ${p}` === qualified;
    });
    if (sameQ.length === 1) return `${tool}#${qualified}`;
  }
  return route;
}

// buildMentionOptions 生成补全项。
// label 用会话 id：早先按列表顺序编号（#1/#2）会在 scan:done 重建 chats 后漂移。
// insert 用会话标题（芯片上的名字），输入框和消息气泡不再露出一长串 id。
export function buildMentionOptions(agents: MentionAgent[]): MentionOption[] {
  return agents.map((a) => {
    const project = a.workspace ? baseName(a.workspace) : '';
    const parts = [project, a.title].filter(Boolean);
    return {
      id: a.id,
      label: `${toolDisplayName(a.toolId)}#${a.id}`,
      insert: visibleMentionLabel(a, agents),
      detail: parts.length > 0 ? parts.join(' · ') : undefined,
    };
  });
}

// mentionTokens 列出草稿里可能出现的 @ 标签写法（路由 id 与写入标题两种），
// 长串优先，避免一个标签是另一个标签前缀时先截短。
function mentionTokens(options: MentionOption[]): string[] {
  const out = new Set<string>();
  for (const o of options) {
    out.add(`@${o.label}`);
    if (o.insert && o.insert !== o.label) out.add(`@${o.insert}`);
  }
  return [...out].sort((a, b) => b.length - a.length);
}

// stripLeadingMentions 去掉正文开头的 @ 标签（可多个、空格分隔）。
// Hub 的 @ 标签只是路由标记；斜杠技能要求 "/" 落在正文首字符才会被 agent 识别。
// 只剥离开头：正文中间与邮箱里的 @ 保持原样。
export function stripLeadingMentions(text: string, options: MentionOption[]): string {
  const tokens = mentionTokens(options);
  let out = text.trimStart();
  for (;;) {
    const hit = tokens.find((tok) => out.startsWith(tok));
    if (!hit) return out;
    out = out.slice(hit.length).trimStart();
  }
}

// stripMentionTags 移除文本中所有已知 @ 路由标签（开头与正文中间都算）：
// @ 标签只供 Hub 解析投递目标，不应进入 agent 会话，也不应在气泡里展示。
// 只清理当前会话列表能生成的标签，普通 @ 与邮箱保持原样；长标签优先，
// 避免标题是另一标签前缀时误截断。只归一化标签两侧的空白，正文内部的
// 缩进、连续空格与换行原样保留（投递代码片段不能被改写）。
export function stripMentionTags(text: string, options: MentionOption[]): string {
  // 先统一替换成哨兵，再一次性处理接缝：直接删标签会把两侧空白拼在一起，
  // 而全局折叠空白会抹掉正文缩进。
  const MARK = '\u0001';
  let out = text;
  let removed = false;
  for (const tok of mentionTokens(options)) {
    if (out.includes(tok)) {
      out = out.split(tok).join(MARK);
      removed = true;
    }
  }
  // 没命中标签时原样返回，不因清洗改写用户原文（如连续空格）。
  if (!removed) return out;
  return out
    // 行首标签连同其后空白删掉；标签独占一行时连换行一起删，不留空行。
    .replace(new RegExp(`^[ \\t]*${MARK}[ \\t]*\\r?\\n?`, 'gm'), '')
    // 行尾标签：连同其前空白一起删，避免行尾残留空格。
    .replace(new RegExp(`[ \\t]*${MARK}[ \\t]*(?=\\r?\\n|$)`, 'g'), '')
    // 行内标签：接缝收敛为一个空格。
    .replace(new RegExp(`[ \\t]*${MARK}[ \\t]*`, 'g'), ' ')
    .trim();
}

// displayMentionTitles 把文本里的 @工具#会话ID 换成 @工具#会话标题，仅供展示
//（落盘原文可以仍是 id）。标题与项目都空时保留原文。长 id 优先，避免前缀截断。
export function displayMentionTitles(text: string, agents: MentionAgent[]): string {
  const options = [...buildMentionOptions(agents)].sort((a, b) => b.label.length - a.label.length);
  let out = text;
  for (const opt of options) {
    if (opt.insert === opt.label) continue;
    out = out.replaceAll(`@${opt.label}`, `@${opt.insert}`);
  }
  return out;
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
  // 长标签优先匹配，避免前缀误伤。id 形式与标题形式都认：
  // 旧草稿里仍是 @工具#id，新草稿写的是 @工具#会话标题。
  const tokens: { id: string; at: string }[] = [];
  for (const opt of options) {
    tokens.push({ id: opt.id, at: `@${opt.label}` });
    if (opt.insert && opt.insert !== opt.label) tokens.push({ id: opt.id, at: `@${opt.insert}` });
  }
  tokens.sort((a, b) => b.at.length - a.at.length);
  let found: string | null = null;
  let foundIndex = -1;
  for (const tok of tokens) {
    const idx = text.lastIndexOf(tok.at);
    if (idx >= 0 && idx > foundIndex) {
      foundIndex = idx;
      found = tok.id;
    }
  }
  if (found) return found;
  // 仅把「词首 @」当作提及尝试：邮箱 a@b.com 的 @ 不在词首，不构成无效提及。
  if (/(^|\s)@[^\s@]+/.test(text)) return null;
  return lastAgentId;
}
