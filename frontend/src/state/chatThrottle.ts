// chatThrottle：chat:update 流式分片微批节流器。
// 背景：agent 流式输出可达 50-60 事件/秒 × 多会话（desktop.log 实测「idle 定时
// 不排」每分钟数千条），每条 assistant/thought 文本分片都 applyChat 会让长文本
// markdown 全量重渲染，把 WebView 主线程打满、页面假死（AgentHub 新建卡死的
// 根因之一）。本节流器把文本分片按 chatID 微批合并，flush 周期 ~100ms；
// 结构化事件（tool/plan/turn_done/error/user）不节流、立即生效——
// turn_done 到达时还要先 flush 该 chat 的缓冲，保证 turn 结束不留尾巴。
import type { ChatUpdate } from '../lib/api';

// 与 ChatUpdate 结构兼容的子集（测试用最小对象即可构造）。
export type ChatUpdateLike = Pick<ChatUpdate, 'Seq' | 'Type'> &
  Partial<Pick<ChatUpdate, 'Text' | 'MessageID' | 'Tool' | 'Plan' | 'StopReason'>>;

// applyFn(id, update) 的签名与 store.applyChat 对齐。
type ApplyFn = (id: string, u: ChatUpdateLike) => void;

// 非文本流事件：立即生效。user 虽按文本合并处理，但频率极低（一轮一条），
// 归入立即路径，避免发送侧乐观置 running 被延迟。
const IMMEDIATE_TYPES = new Set(['tool', 'plan', 'turn_done', 'error', 'user']);

// flushIntervalMs 微批周期：100ms 下每秒最多 ~10 次 store 更新/重渲染，
// 流式观感仍是「连续打字」，UI 保持可交互。
const flushIntervalMs = 100;

interface BufferedItem {
  u: ChatUpdateLike;
  // 合并分片的分组键：同一 MessageID 的连续分片才串接文本。
  key: string;
}

interface ChatBuffer {
  timer: ReturnType<typeof setTimeout> | null;
  // 按到达顺序排队；文本分片与 flush 后的新分组追加，不重排。
  items: BufferedItem[];
}

export interface ChatThrottle {
  push: (id: string, u: ChatUpdateLike) => void;
  drop: (id: string) => void;
  dispose: () => void;
}

export function createChatThrottle(apply: ApplyFn): ChatThrottle {
  const buffers = new Map<string, ChatBuffer>();
  let disposed = false;

  const flush = (id: string) => {
    const buf = buffers.get(id);
    if (!buf) return;
    buf.timer = null;
    const items = buf.items;
    buf.items = [];
    // 相邻同 key 的文本分片合并成一条（Seq 取最大，Text 串接），
    // 每组一条 applyChat，重渲染次数从「分片数」降到「分组数」。
    let i = 0;
    while (i < items.length) {
      const first = items[i];
      if (first.u.Type === 'assistant' || first.u.Type === 'thought') {
        let text = first.u.Text ?? '';
        let seq = first.u.Seq;
        let j = i + 1;
        while (j < items.length && items[j].key === first.key &&
               (items[j].u.Type === 'assistant' || items[j].u.Type === 'thought')) {
          text += items[j].u.Text ?? '';
          seq = Math.max(seq, items[j].u.Seq);
          j++;
        }
        apply(id, { ...first.u, Text: text, Seq: seq });
        i = j;
      } else {
        apply(id, first.u);
        i++;
      }
    }
    if (buf.items.length === 0 && buf.timer === null) {
      buffers.delete(id);
    }
  };

  const schedule = (id: string) => {
    const buf = buffers.get(id);
    if (!buf || buf.timer !== null) return;
    buf.timer = setTimeout(() => flush(id), flushIntervalMs);
  };

  return {
    push(id, u) {
      if (disposed) return;
      if (IMMEDIATE_TYPES.has(u.Type)) {
        // 结构化事件立即生效；该 chat 若有缓冲文本，先 flush 再应用，
        // 保证时间线按 Seq 顺序进入 store（否则后到的高 Seq 会让 store 把
        // 缓冲的低 Seq 分片当乱序丢弃，丢文本）。
        flush(id);
        apply(id, u);
        return;
      }
      const buf = buffers.get(id);
      if (!buf) {
        // 无缓冲：首片立即应用，降延迟（流式观感不打折）。
        apply(id, u);
        buffers.set(id, { timer: null, items: [] });
        return;
      }
      // 同一 MessageID（或无 MessageID 时按 Seq）分组的连续分片
      const key = u.MessageID ?? `${u.Type}:${u.Seq}`;
      buf.items.push({ u, key });
      schedule(id);
    },
    drop(id) {
      const buf = buffers.get(id);
      if (!buf) return;
      if (buf.timer !== null) clearTimeout(buf.timer);
      buffers.delete(id);
    },
    dispose() {
      disposed = true;
      for (const [, buf] of buffers) {
        if (buf.timer !== null) clearTimeout(buf.timer);
      }
      buffers.clear();
    },
  };
}
