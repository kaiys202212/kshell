// ACP 聊天时间线的纯归并逻辑：把 Go 侧推送的 ChatUpdate 流合并进时间线数组。
// 独立成纯函数便于测试；按 Seq 去重可同时容忍 History 回放与实时事件的重叠。
import type { ChatPlanEntry, ChatToolCall, ChatUpdate } from '../lib/api';

export interface TimelineItem {
  key: string;
  type: 'user' | 'assistant' | 'thought' | 'tool' | 'plan' | 'turn_done' | 'error';
  text?: string;
  tool?: ChatToolCall;
  plan?: ChatPlanEntry[];
  stopReason?: string;
  seq: number;
}

// 全新的空时间线（与 store 的 chatItems[id] 默认值一致）
export function emptyChatItems(): TimelineItem[] {
  return [];
}

// applyChatUpdate 是纯函数：按 Seq 去重后归并一条更新，返回新数组（不改入参）。
export function applyChatUpdate(items: TimelineItem[], u: ChatUpdate): TimelineItem[] {
  // 只要已存在任一 Seq >= 当前，就视为重复/乱序，直接丢弃
  if (items.some((it) => it.seq >= u.Seq)) return items;

  if (u.Type === 'assistant' || u.Type === 'thought' || u.Type === 'user') {
    const key = u.MessageID ?? `${u.Type}:${u.Seq}`;
    const idx = items.findIndex((it) => it.key === key && it.type === u.Type);
    if (idx >= 0) {
      const next = items.slice();
      next[idx] = { ...next[idx], text: (next[idx].text ?? '') + (u.Text ?? ''), seq: u.Seq };
      return next;
    }
    return [...items, { key, type: u.Type as TimelineItem['type'], text: u.Text ?? '', seq: u.Seq }];
  }

  if (u.Type === 'tool' && u.Tool) {
    const key = u.ToolCallID ?? u.Tool.ToolCallID;
    const idx = items.findIndex((it) => it.key === key && it.type === 'tool');
    if (idx >= 0) {
      const next = items.slice();
      next[idx] = { ...next[idx], tool: u.Tool, seq: u.Seq };
      return next;
    }
    return [...items, { key, type: 'tool', tool: u.Tool, seq: u.Seq }];
  }

  if (u.Type === 'plan') {
    return [...items, { key: `plan:${u.Seq}`, type: 'plan', plan: u.Plan ?? [], seq: u.Seq }];
  }

  // turn_done / error：作为分界事件追加（不参与文本合并）
  return [...items, {
    key: `${u.Type}:${u.Seq}`,
    type: u.Type as TimelineItem['type'],
    text: u.Text,
    stopReason: u.StopReason,
    seq: u.Seq,
  }];
}
