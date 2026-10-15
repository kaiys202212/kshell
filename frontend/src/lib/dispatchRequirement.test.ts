// dispatchRequirement 单测：派发流程各步调用顺序，重点断言 markRequirementDispatched
// 收到真实 chat.ID（不再是 '' 占位）。api 层整体打桩，store 用真实单例。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { dispatchRequirement } from './dispatchRequirement';
import { useAppStore } from '../state/store';
import type { ChatInfo, ToolInfo } from './api';

const mocks = vi.hoisted(() => ({
  getTools: vi.fn(),
  openWorkspaceACP: vi.fn(),
  sendChatPrompt: vi.fn(),
  markRequirementDispatched: vi.fn(),
}));
vi.mock('./api', () => mocks);

const chat = (over: Partial<ChatInfo>): ChatInfo => ({
  ID: 'chat-1',
  Kind: 'session',
  SessionID: 's1',
  Workspace: 'D:\\ws\\proj',
  Title: 'T',
  ToolID: 'tool-a',
  Status: 'ready',
  ExitCode: 0,
  Error: '',
  ...over,
});

afterEach(() => {
  cleanupStore();
});

function cleanupStore() {
  useAppStore.setState({ chats: [], lastAgentId: '' });
}

beforeEach(() => {
  vi.clearAllMocks();
  cleanupStore();
  mocks.sendChatPrompt.mockResolvedValue(undefined);
  mocks.markRequirementDispatched.mockResolvedValue(undefined);
});

describe('dispatchRequirement', () => {
  it('无 ACP 工具时抛 no_acp_tool，不建会话', async () => {
    mocks.getTools.mockResolvedValue([
      { ID: 'tool-x', ACP: { Available: false, Source: '' } } as unknown as ToolInfo,
    ]);
    await expect(dispatchRequirement('r1', '文本', 'D:\\ws')).rejects.toThrow('no_acp_tool');
    expect(mocks.openWorkspaceACP).not.toHaveBeenCalled();
  });

  it('优先用当前默认 agent（lastAgentId 命中）的工具', async () => {
    useAppStore.setState({
      chats: [chat({ ID: 'chat-9', ToolID: 'tool-last' })],
      lastAgentId: 'chat-9',
    });
    mocks.openWorkspaceACP.mockResolvedValue({ Kind: 'chat', Chat: chat({ ID: 'chat-new' }) });

    await dispatchRequirement('r1', '文本', 'D:\\ws\\proj');

    expect(mocks.openWorkspaceACP).toHaveBeenCalledWith('D:\\ws\\proj', 'tool-last');
    expect(mocks.getTools).not.toHaveBeenCalled();
  });

  it('完整流程：建会话 → 发文本 → markRequirementDispatched 传真实 chat.ID → setLastAgentId', async () => {
    useAppStore.setState({ chats: [], lastAgentId: '' });
    mocks.getTools.mockResolvedValue([
      { ID: 'tool-a', ACP: { Available: true, Source: '' } } as unknown as ToolInfo,
    ]);
    mocks.openWorkspaceACP.mockResolvedValue({ Kind: 'chat', Chat: chat({ ID: 'chat-real' }) });

    await dispatchRequirement('r1', '需求文本', 'D:\\ws\\proj');

    expect(mocks.openWorkspaceACP).toHaveBeenCalledWith('D:\\ws\\proj', 'tool-a');
    expect(mocks.sendChatPrompt).toHaveBeenCalledWith('chat-real', '需求文本');
    // 关键断言：第三个参数为真实 chat.ID，不再是 '' 占位
    expect(mocks.markRequirementDispatched).toHaveBeenCalledWith('r1', 'D:\\ws\\proj', 'chat-real');
    expect(useAppStore.getState().lastAgentId).toBe('chat-real');
  });

  it('sendChatPrompt 失败：向上抛且不落盘派发标记', async () => {
    useAppStore.setState({ chats: [], lastAgentId: '' });
    mocks.getTools.mockResolvedValue([
      { ID: 'tool-a', ACP: { Available: true, Source: '' } } as unknown as ToolInfo,
    ]);
    mocks.openWorkspaceACP.mockResolvedValue({ Kind: 'chat', Chat: chat({ ID: 'chat-real' }) });
    mocks.sendChatPrompt.mockRejectedValue(new Error('send boom'));

    await expect(dispatchRequirement('r1', '文本', 'D:\\ws')).rejects.toThrow('send boom');
    expect(mocks.markRequirementDispatched).not.toHaveBeenCalled();
  });
});
