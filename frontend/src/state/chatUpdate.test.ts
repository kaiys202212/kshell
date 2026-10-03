import { describe, expect, it } from 'vitest';
import type { ChatUpdate } from '../lib/api';
import { applyChatUpdate, emptyChatItems } from './chatUpdate';

describe('applyChatUpdate', () => {
  it('按 Seq 去重，累积同 MessageID 的流式文本', () => {
    let items = emptyChatItems();
    items = applyChatUpdate(items, { Seq: 1, Type: 'assistant', MessageID: 'm1', Text: '你' });
    items = applyChatUpdate(items, { Seq: 2, Type: 'assistant', MessageID: 'm1', Text: '好' });
    items = applyChatUpdate(items, { Seq: 2, Type: 'assistant', MessageID: 'm1', Text: 'X' });
    expect(items).toHaveLength(1);
    expect(items[0].text).toBe('你好');
  });

  it('tool 按 ToolCallID upsert', () => {
    let items = emptyChatItems();
    const u1: ChatUpdate = { Seq: 1, Type: 'tool', ToolCallID: 'c1', Tool: { ToolCallID: 'c1', Title: 'Read', Status: 'pending' } };
    const u2: ChatUpdate = { Seq: 2, Type: 'tool', ToolCallID: 'c1', Tool: { ToolCallID: 'c1', Title: 'Read', Status: 'completed' } };
    items = applyChatUpdate(items, u1);
    items = applyChatUpdate(items, u2);
    expect(items).toHaveLength(1);
    expect(items[0].tool?.Status).toBe('completed');
  });

  it('turn_done 追加事件项', () => {
    const items = applyChatUpdate(emptyChatItems(), { Seq: 1, Type: 'turn_done', StopReason: 'end_turn' });
    expect(items[0].type).toBe('turn_done');
  });

  it('upsert 后重放旧 Seq：用真实最大值去重，文本不重复累积', () => {
    let items = emptyChatItems();
    items = applyChatUpdate(items, { Seq: 1, Type: 'assistant', MessageID: 'm1', Text: 'A' });
    items = applyChatUpdate(items, { Seq: 2, Type: 'tool', ToolCallID: 'c1', Tool: { ToolCallID: 'c1', Title: 'Read' } });
    items = applyChatUpdate(items, { Seq: 3, Type: 'assistant', MessageID: 'm1', Text: 'B' });
    // 重放 Seq=3：真实最大值已藏在非末尾项（m1），必须被丢弃
    items = applyChatUpdate(items, { Seq: 3, Type: 'assistant', MessageID: 'm1', Text: 'B' });
    const m1 = items.find((it) => it.key === 'm1');
    expect(m1?.text).toBe('AB');
  });

  it('thought/user 也按 MessageID 累积', () => {
    let items = emptyChatItems();
    items = applyChatUpdate(items, { Seq: 1, Type: 'thought', MessageID: 't1', Text: '想' });
    items = applyChatUpdate(items, { Seq: 2, Type: 'thought', MessageID: 't1', Text: '一下' });
    items = applyChatUpdate(items, { Seq: 3, Type: 'user', MessageID: 'u1', Text: '你好' });
    items = applyChatUpdate(items, { Seq: 4, Type: 'user', MessageID: 'u1', Text: '吗' });
    expect(items.find((it) => it.key === 't1')?.text).toBe('想一下');
    expect(items.find((it) => it.key === 'u1')?.text).toBe('你好吗');
  });

  it('plan 追加为 plan 项并带条目', () => {
    const plan = [{ Content: '第一步', Status: 'pending' }];
    const items = applyChatUpdate(emptyChatItems(), { Seq: 1, Type: 'plan', Plan: plan });
    expect(items).toHaveLength(1);
    expect(items[0].type).toBe('plan');
    expect(items[0].plan).toEqual(plan);
  });

  it('error 追加为 error 项并带文本', () => {
    const items = applyChatUpdate(emptyChatItems(), { Seq: 1, Type: 'error', Text: '崩了' });
    expect(items[0].type).toBe('error');
    expect(items[0].text).toBe('崩了');
  });
});
