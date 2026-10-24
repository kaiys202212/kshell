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

  it('user 事件携带附件 meta 时保留到时间线项', () => {
    const items = applyChatUpdate(emptyChatItems(), {
      Seq: 1,
      Type: 'user',
      MessageID: 'u1',
      Text: '看这个',
      Attachments: [{ Name: 'a.txt' }, { MimeType: 'image/png' }],
    });
    expect(items[0].attachments).toHaveLength(2);
    expect(items[0].attachments?.[0].Name).toBe('a.txt');
    expect(items[0].attachments?.[1].MimeType).toBe('image/png');
  });

  // 后端把 image/resource_link 块拆成独立分片推送：同 MessageID 归并时
  // attachments 必须追加而非丢弃（此前文本归并分支直接丢掉 u.Attachments）。
  it('同 MessageID 分片归并时 attachments 追加合并', () => {
    let items = emptyChatItems();
    items = applyChatUpdate(items, { Seq: 1, Type: 'assistant', MessageID: 'm1', Text: '结果' });
    items = applyChatUpdate(items, {
      Seq: 2,
      Type: 'assistant',
      MessageID: 'm1',
      Text: '如图',
      Attachments: [{ Kind: 'image', Path: 'D:\\c\\1.png', MimeType: 'image/png' }],
    });
    const m1 = items.find((it) => it.key === 'm1');
    expect(m1?.text).toBe('结果如图');
    expect(m1?.attachments).toHaveLength(1);
    expect(m1?.attachments?.[0].Kind).toBe('image');
  });

  // 无新附件的归并不应产生新数组引用：避免无关分片导致下游 memo/比较失效
  it('归并无新附件时保留原 attachments 引用', () => {
    let items = emptyChatItems();
    items = applyChatUpdate(items, {
      Seq: 1,
      Type: 'user',
      MessageID: 'u1',
      Text: 'a',
      Attachments: [{ Name: 'a.txt' }],
    });
    const before = items.find((it) => it.key === 'u1')?.attachments;
    items = applyChatUpdate(items, { Seq: 2, Type: 'user', MessageID: 'u1', Text: 'b' });
    expect(items.find((it) => it.key === 'u1')?.attachments).toBe(before);
  });
});
