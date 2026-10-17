// chatThrottle：chat:update 流式分片微批节流。
// 背景：agent 流式输出可达 50-60 事件/秒 × 多会话，每条分片都 applyChat 会触发
// 长文本 markdown 全量重渲染，把 WebView 主线程打满、页面假死。节流器把
// assistant/thought 文本分片按 chatID 微批合并（~100ms flush），结构化事件
// （tool/plan/turn_done/error）不节流、立即生效。
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { createChatThrottle, type ChatUpdateLike } from './chatThrottle';

function textUpdate(seq: number, text: string, messageID = 'msg1'): ChatUpdateLike {
  return { Seq: seq, Type: 'assistant', Text: text, MessageID: messageID };
}

describe('createChatThrottle', () => {
  beforeEach(() => { vi.useFakeTimers(); });
  afterEach(() => { vi.useRealTimers(); });

  it('首条分片立即生效，不等待 flush 周期', () => {
    const applied: ChatUpdateLike[] = [];
    const t = createChatThrottle((id, u) => applied.push(u));
    t.push('c1', textUpdate(1, 'a'));
    expect(applied).toHaveLength(1);
    expect(applied[0].Text).toBe('a');
    t.dispose();
  });

  it('同一 MessageID 的后续分片进入缓冲，flush 时合并为一条', () => {
    const applied: ChatUpdateLike[] = [];
    const t = createChatThrottle((id, u) => applied.push(u));
    t.push('c1', textUpdate(1, 'a'));
    t.push('c1', textUpdate(2, 'b'));
    t.push('c1', textUpdate(3, 'c'));
    expect(applied).toHaveLength(1);
    vi.advanceTimersByTime(150);
    expect(applied).toHaveLength(2);
    expect(applied[1].Text).toBe('bc');
    expect(applied[1].Seq).toBe(3); // 合并分片取最大 Seq
    t.dispose();
  });

  it('不同 chat 的缓冲互不干扰', () => {
    const applied: { id: string; u: ChatUpdateLike }[] = [];
    const t = createChatThrottle((id, u) => applied.push({ id, u }));
    t.push('c1', textUpdate(1, 'a'));
    t.push('c2', textUpdate(10, 'x', 'msg9'));
    vi.advanceTimersByTime(150);
    expect(applied.map((e) => e.id)).toEqual(['c1', 'c2']);
    t.dispose();
  });

  it('flush 按消息分组顺序合并，不跨 MessageID 串文本', () => {
    const applied: ChatUpdateLike[] = [];
    const t = createChatThrottle((id, u) => applied.push(u));
    t.push('c1', textUpdate(1, 'a', 'm1'));
    t.push('c1', textUpdate(2, 'b', 'm1'));
    t.push('c1', textUpdate(3, 'c', 'm2'));
    vi.advanceTimersByTime(150);
    expect(applied).toHaveLength(3); // 首条 + m1 合并 + m2 首条
    expect(applied[1].Text).toBe('b');
    expect(applied[2].Text).toBe('c');
    t.dispose();
  });

  it('结构化事件立即生效；缓冲文本先 flush 保 Seq 顺序，不被 store 门控丢弃', () => {
    const applied: ChatUpdateLike[] = [];
    const t = createChatThrottle((id, u) => applied.push(u));
    t.push('c1', textUpdate(1, 'a'));
    t.push('c1', textUpdate(2, 'b')); // 缓冲中
    t.push('c1', { Seq: 5, Type: 'tool', Tool: { ToolCallID: 't1' } } as ChatUpdateLike);
    // tool 同步生效；若不先 flush，缓冲的 seq=2 会被 store 的 Seq 门控（<=5 丢弃）吞掉
    expect(applied.map((u) => u.Type)).toEqual(['assistant', 'assistant', 'tool']);
    expect(applied[1].Text).toBe('b');
    vi.advanceTimersByTime(150);
    expect(applied).toHaveLength(3);
    t.dispose();
  });

  it('turn_done 立即生效并 flush 该 chat 全部缓冲（turn 结束不留尾）', () => {
    const applied: ChatUpdateLike[] = [];
    const t = createChatThrottle((id, u) => applied.push(u));
    t.push('c1', textUpdate(1, 'a'));
    t.push('c1', textUpdate(2, 'b'));
    t.push('c1', { Seq: 9, Type: 'turn_done', StopReason: 'end_turn' } as ChatUpdateLike);
    expect(applied).toHaveLength(3); // 全部立即生效
    expect(applied.map((u) => u.Type)).toEqual(['assistant', 'assistant', 'turn_done']);
    vi.advanceTimersByTime(150);
    expect(applied).toHaveLength(3);
    t.dispose();
  });

  it('丢弃缓冲不 flush：drop 后该 chat 的缓冲分片不再应用', () => {
    const applied: ChatUpdateLike[] = [];
    const t = createChatThrottle((id, u) => applied.push(u));
    t.push('c1', textUpdate(1, 'a'));
    t.push('c1', textUpdate(2, 'b'));
    t.drop('c1');
    vi.advanceTimersByTime(150);
    expect(applied).toHaveLength(1); // 只有首条
    t.dispose();
  });

  it('disposed 后 push 不再应用任何事件', () => {
    const applied: ChatUpdateLike[] = [];
    const t = createChatThrottle((id, u) => applied.push(u));
    t.push('c1', textUpdate(1, 'a'));
    t.dispose();
    t.push('c1', textUpdate(2, 'b'));
    vi.advanceTimersByTime(150);
    expect(applied).toHaveLength(1);
  });
});
