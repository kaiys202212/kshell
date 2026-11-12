// ChatView 输入框按键不得拖着重跑整条时间线的 ReactMarkdown（与 AgentHubPanel 同类回归）。
// 根因见 docs/plans/2026-11-12-chat-timeline-memo-design.md：draft 在组件根上，
// 时间线未 memo，长会话下每键全量重渲染造成主线程饥饿（文件树闪烁的根源）。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ChatInfo } from '../lib/api';
import { useAppStore } from '../state/store';
import '../test/i18n';
import { tt } from '../test/i18n';
import ChatView from './ChatView';

const markdownRender = vi.hoisted(() => vi.fn());

vi.mock('react-markdown', () => ({
  default: ({ children }: { children?: string }) => {
    markdownRender(children);
    return <div data-testid="md">{children}</div>;
  },
}));

vi.mock('remark-gfm', () => ({ default: () => null }));

const api = vi.hoisted(() => ({
  sendChatPrompt: vi.fn(),
  sendChatPromptDirect: vi.fn(),
  sendChatPromptBlocks: vi.fn(),
  listChatModels: vi.fn(),
  setChatModel: vi.fn(),
  listChatCommands: vi.fn(),
  listChatQueue: vi.fn(),
  removeChatQueued: vi.fn(),
  pickFile: vi.fn(),
  cancelChat: vi.fn(),
  respondChatPermission: vi.fn(),
  cancelChatPermission: vi.fn(),
  listChatConfigOptions: vi.fn(),
  setChatConfigOption: vi.fn(),
  respondChatElicitation: vi.fn(),
  cancelChatElicitation: vi.fn(),
  listChats: vi.fn(),
  openExternalURL: vi.fn().mockResolvedValue(undefined),
  openInDefaultApp: vi.fn(),
  openLocalPath: vi.fn(),
}));
vi.mock('../lib/api', () => api);

const CHAT: ChatInfo = {
  ID: 'c1', Kind: 'new', SessionID: 's1', Workspace: 'D:\\p', Title: '新会话',
  ToolID: 'claude', Status: 'ready', ExitCode: 0, Error: '',
};

beforeEach(() => {
  vi.clearAllMocks();
  api.listChatModels.mockResolvedValue(null);
  api.listChatCommands.mockResolvedValue([]);
  api.listChatConfigOptions.mockResolvedValue(null);
  api.listChatQueue.mockResolvedValue([]);
  api.listChats.mockResolvedValue([]);
  useAppStore.setState({
    chatItems: {},
    chatSeq: {},
    chatPermissions: {},
    chats: [CHAT],
    chatModels: {},
    chatCommands: {},
    chatConfigs: {},
    chatElicitations: {},
    chatQueues: {},
    chatBackendQueues: {},
    chatQueueRevs: {},
    queuedPaused: {},
    toasts: [],
  });
});

afterEach(cleanup);

describe('ChatView 输入性能', () => {
  it('改 draft 不重跑已有消息的 ReactMarkdown', async () => {
    useAppStore.setState({
      chatItems: {
        c1: [
          { key: 'm1', type: 'assistant', text: '## hello\n\n- a\n- b', seq: 1 },
          { key: 'm2', type: 'assistant', text: 'second **md** body', seq: 2 },
        ],
      },
    });
    render(<ChatView chat={CHAT} active />);
    // 等时间线首渲与挂载 effect（模型/技能/配置/队列兜底拉取）全部落定，
    // 避免异步回包落在打字事件之间造成计数漂移
    await waitFor(() => expect(markdownRender.mock.calls.length).toBeGreaterThanOrEqual(2));
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
    const afterMount = markdownRender.mock.calls.length;

    const input = screen.getByPlaceholderText(tt('ui.chat.input_placeholder'));
    fireEvent.change(input, { target: { value: '打字中' } });
    fireEvent.change(input, { target: { value: '打字中继续' } });
    fireEvent.change(input, { target: { value: '打字中继续abc' } });

    expect(markdownRender.mock.calls.length).toBe(afterMount);
  });
});
