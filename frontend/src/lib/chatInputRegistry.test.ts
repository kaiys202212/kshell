import { beforeEach, describe, expect, it } from 'vitest';
import { appendChatInput, registerChatInput, unregisterChatInput } from './chatInputRegistry';

describe('chatInputRegistry', () => {
  beforeEach(() => unregisterChatInput('c1'));
  it('注册后 append 投递到对应聊天输入框', () => {
    const got: string[] = [];
    registerChatInput('c1', { append: (t) => got.push(t) });
    expect(appendChatInput('c1', 'a')).toBe(true);
    expect(appendChatInput('c1', 'b')).toBe(true);
    expect(got).toEqual(['a', 'b']);
  });
  it('未注册的 id 返回 false', () => {
    expect(appendChatInput('nope', 'x')).toBe(false);
  });
  it('注销后不再投递', () => {
    registerChatInput('c1', { append: () => {} });
    unregisterChatInput('c1');
    expect(appendChatInput('c1', 'x')).toBe(false);
  });
});
