// dispatchRequirement 单测：派发流程各步调用顺序，重点断言 markRequirementDispatched
// 收到真实 chat.ID（不再是 '' 占位）。api 层整体打桩，store 用真实单例。
import '@testing-library/jest-dom/vitest';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { dispatchRequirement, reopenRequirementSession } from './dispatchRequirement';
import { useAppStore } from '../state/store';
import type { ChatInfo, ToolInfo } from './api';
import { tt } from '../test/i18n';

const mocks = vi.hoisted(() => ({
  getTools: vi.fn(),
  openWorkspaceACP: vi.fn(),
  sendChatPrompt: vi.fn(),
  markRequirementDispatched: vi.fn(),
  setChatModel: vi.fn(),
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
  useAppStore.setState({ chats: [], lastAgentId: '', toasts: [] });
}

beforeEach(() => {
  vi.clearAllMocks();
  cleanupStore();
  mocks.sendChatPrompt.mockResolvedValue(undefined);
  mocks.markRequirementDispatched.mockResolvedValue(undefined);
  mocks.setChatModel.mockResolvedValue(undefined);
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

  it('完整流程：建会话 → 发文本 → markRequirementDispatched 传真实 chat.ID 与标题 → setLastAgentId', async () => {
    useAppStore.setState({ chats: [], lastAgentId: '' });
    mocks.getTools.mockResolvedValue([
      { ID: 'tool-a', ACP: { Available: true, Source: '' } } as unknown as ToolInfo,
    ]);
    mocks.openWorkspaceACP.mockResolvedValue({ Kind: 'chat', Chat: chat({ ID: 'chat-real', Title: '会话标题' }) });

    await dispatchRequirement('r1', '需求文本', 'D:\\ws\\proj');

    expect(mocks.openWorkspaceACP).toHaveBeenCalledWith('D:\\ws\\proj', 'tool-a');
    expect(mocks.sendChatPrompt).toHaveBeenCalledWith('chat-real', '需求文本');
    // 关键断言：第三参为真实 chat.ID，第四参为会话标题快照
    expect(mocks.markRequirementDispatched).toHaveBeenCalledWith('r1', 'D:\\ws\\proj', 'chat-real', '会话标题');
    expect(useAppStore.getState().lastAgentId).toBe('chat-real');
    // 无 defaults 时不切模型
    expect(mocks.setChatModel).not.toHaveBeenCalled();
  });

  it('defaults.toolId 有效：替代自动选择（lastAgentId 指向其他工具也不影响）', async () => {
    useAppStore.setState({
      chats: [chat({ ID: 'chat-9', ToolID: 'tool-last' })],
      lastAgentId: 'chat-9',
    });
    mocks.getTools.mockResolvedValue([
      { ID: 'tool-def', ACP: { Available: true, Source: '' } } as unknown as ToolInfo,
    ]);
    mocks.openWorkspaceACP.mockResolvedValue({ Kind: 'chat', Chat: chat({ ID: 'chat-real' }) });

    await dispatchRequirement('r1', '需求文本', 'D:\\ws\\proj', { toolId: 'tool-def', modelId: null });

    expect(mocks.openWorkspaceACP).toHaveBeenCalledWith('D:\\ws\\proj', 'tool-def');
  });

  it('defaults.modelId 有值：建会话后先 setChatModel 再 sendChatPrompt', async () => {
    useAppStore.setState({ chats: [], lastAgentId: '' });
    mocks.getTools.mockResolvedValue([
      { ID: 'tool-def', ACP: { Available: true, Source: '' } } as unknown as ToolInfo,
    ]);
    mocks.openWorkspaceACP.mockResolvedValue({ Kind: 'chat', Chat: chat({ ID: 'chat-real' }) });

    await dispatchRequirement('r1', '需求文本', 'D:\\ws\\proj', { toolId: 'tool-def', modelId: 'm2' });

    expect(mocks.setChatModel).toHaveBeenCalledWith('chat-real', 'm2');
    // 时序：setChatModel 必须先于 sendChatPrompt，确保首轮就用对模型
    const modelOrder = mocks.setChatModel.mock.invocationCallOrder[0];
    const sendOrder = mocks.sendChatPrompt.mock.invocationCallOrder[0];
    expect(modelOrder).toBeLessThan(sendOrder);
  });

  it('defaults.toolId ACP 不可用：回落自动选择并 notify 提示', async () => {
    useAppStore.setState({ chats: [], lastAgentId: '' });
    mocks.getTools.mockResolvedValue([
      { ID: 'tool-def', ACP: { Available: false, Source: '' } } as unknown as ToolInfo,
      { ID: 'tool-a', ACP: { Available: true, Source: '' } } as unknown as ToolInfo,
    ]);
    mocks.openWorkspaceACP.mockResolvedValue({ Kind: 'chat', Chat: chat({ ID: 'chat-real' }) });

    await dispatchRequirement('r1', '需求文本', 'D:\\ws\\proj', { toolId: 'tool-def', modelId: 'm2' });

    // 回落到 resolveToolID 自动选择（无 lastAgentId → 第一个 ACP 可用工具）
    expect(mocks.openWorkspaceACP).toHaveBeenCalledWith('D:\\ws\\proj', 'tool-a');
    expect(
      useAppStore.getState().toasts.some((t) => t.title.includes(tt('ui.hub.default_tool_unavailable'))),
    ).toBe(true);
  });

  it('defaults.modelId 切模型失败：不阻断派发，仍发送需求文本', async () => {
    useAppStore.setState({ chats: [], lastAgentId: '' });
    mocks.getTools.mockResolvedValue([
      { ID: 'tool-def', ACP: { Available: true, Source: '' } } as unknown as ToolInfo,
    ]);
    mocks.openWorkspaceACP.mockResolvedValue({ Kind: 'chat', Chat: chat({ ID: 'chat-real' }) });
    mocks.setChatModel.mockRejectedValue(new Error('model boom'));

    await dispatchRequirement('r1', '需求文本', 'D:\\ws\\proj', { toolId: 'tool-def', modelId: 'm2' });

    expect(mocks.sendChatPrompt).toHaveBeenCalledWith('chat-real', '需求文本');
    expect(mocks.markRequirementDispatched).toHaveBeenCalled();
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

describe('reopenRequirementSession', () => {
  it('重新打开：解析工具 → openWorkspaceACP → 更新派发标记（新 chat.ID 与标题）→ setLastAgentId，返回新会话', async () => {
    useAppStore.setState({ chats: [chat({ ID: 'chat-9', ToolID: 'tool-last' })], lastAgentId: 'chat-9' });
    const newChat = chat({ ID: 'chat-new', Title: '重开标题' });
    mocks.openWorkspaceACP.mockResolvedValue({ Kind: 'chat', Chat: newChat });

    const opened = await reopenRequirementSession({
      id: 'r1', text: '需求', priority: 'medium', tags: [], ts: 1,
      dispatchedTo: 'D:\\ws\\proj', chatId: 'chat-dead',
    });

    expect(opened.Chat?.ID).toBe('chat-new');
    expect(mocks.openWorkspaceACP).toHaveBeenCalledWith('D:\\ws\\proj', 'tool-last');
    expect(mocks.sendChatPrompt).not.toHaveBeenCalled(); // 重开不发需求文本
    expect(mocks.markRequirementDispatched).toHaveBeenCalledWith('r1', 'D:\\ws\\proj', 'chat-new', '重开标题');
    expect(useAppStore.getState().lastAgentId).toBe('chat-new');
  });

  it('无 dispatchedTo：抛错且不建会话', async () => {
    await expect(
      reopenRequirementSession({ id: 'r1', text: '需求', priority: 'medium', tags: [], ts: 1, chatId: 'c1' }),
    ).rejects.toThrow('no_workspace');
    expect(mocks.openWorkspaceACP).not.toHaveBeenCalled();
  });

  it('建会话失败：向上抛且不落盘派发标记', async () => {
    mocks.getTools.mockResolvedValue([
      { ID: 'tool-a', ACP: { Available: true, Source: '' } } as unknown as ToolInfo,
    ]);
    mocks.openWorkspaceACP.mockRejectedValue(new Error('open boom'));

    await expect(
      reopenRequirementSession({
        id: 'r1', text: '需求', priority: 'medium', tags: [], ts: 1,
        dispatchedTo: 'D:\\ws\\proj', chatId: 'chat-dead',
      }),
    ).rejects.toThrow('open boom');
    expect(mocks.markRequirementDispatched).not.toHaveBeenCalled();
  });
});
