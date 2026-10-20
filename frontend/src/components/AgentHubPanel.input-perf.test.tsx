// 输入框按键不得拖着重跑整表 ReactMarkdown（卡顿根因回归）。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ChatInfo } from '../lib/api';
import { useAppStore } from '../state/store';
import '../test/i18n';
import { tt } from '../test/i18n';
import AgentHubPanel from './AgentHubPanel';

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
  openWorkspaceACP: vi.fn(),
  openSessionACP: vi.fn(),
  getSessions: vi.fn(),
  getTools: vi.fn(),
  onToolsUpdated: vi.fn(),
  getModelConfig: vi.fn(),
  readClipboardPaste: vi.fn(),
  listChatCommands: vi.fn(),
  listChatModels: vi.fn(),
  setChatModel: vi.fn(),
  cancelChat: vi.fn(),
  closeChat: vi.fn(),
  listHubMessages: vi.fn(),
  listHubArchive: vi.fn(),
  loadHubArchive: vi.fn(),
  clearHubMessages: vi.fn(),
  recallHubMessage: vi.fn(),
  hubCommand: vi.fn(),
  hubPickTarget: vi.fn(),
  saveHubDefaultCreate: vi.fn().mockResolvedValue(undefined),
  saveHubRouteMode: vi.fn().mockResolvedValue(undefined),
  confirmHubTool: vi.fn().mockResolvedValue(undefined),
  listRequirements: vi.fn(),
  markRequirementDispatched: vi.fn(),
}));
vi.mock('../lib/api', () => api);

vi.mock('../lib/dispatchRequirement', () => ({ dispatchRequirement: vi.fn() }));

function chat(over: Partial<ChatInfo> = {}): ChatInfo {
  return {
    ID: 'c1',
    Key: 'new:1',
    Kind: 'new',
    SessionID: '',
    Workspace: 'D:\\ws\\a',
    Title: '会话甲',
    ToolID: 'claude',
    Status: 'ready',
    ExitCode: 0,
    Error: '',
    Prompted: true,
    ...over,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  api.getTools.mockResolvedValue([
    { ID: 'claude', Name: 'Claude', Installed: true, ACP: { Available: true } },
  ]);
  api.onToolsUpdated.mockImplementation(() => () => {});
  api.getModelConfig.mockResolvedValue({
    Enabled: false,
    Platforms: [],
    DefaultPlatform: '',
    ActiveModel: '',
  });
  api.listHubMessages.mockImplementation(async () => useAppStore.getState().hubMessages);
  api.listHubArchive.mockResolvedValue([]);
  api.readClipboardPaste.mockResolvedValue({ Text: '', Path: '' });
  api.listChatCommands.mockResolvedValue([]);
  api.hubPickTarget.mockResolvedValue({ Replies: [], Pending: false });
  useAppStore.setState({
    chats: [chat()],
    hubMessages: [
      {
        id: 1,
        kind: 'agent',
        chatId: 'c1',
        tool: 'claude',
        event: 'Stop',
        text: '## hello\n\n- a\n- b',
        ts: 1,
      },
      {
        id: 2,
        kind: 'agent',
        chatId: 'c1',
        tool: 'claude',
        event: 'Stop',
        text: 'second **md** body',
        ts: 2,
      },
    ],
    workspaces: [],
    openTabs: [],
    activeTabId: null,
    lastAgentId: 'c1',
    focusTermKey: null,
    terminals: [],
    toasts: [],
    selectedProject: null,
    agentNotices: [],
  });
});

afterEach(() => {
  cleanup();
  localStorage.clear();
});

describe('AgentHubPanel 输入性能', () => {
  it('改 draft 不重跑已有消息的 ReactMarkdown', async () => {
    render(<AgentHubPanel />);
    // 等 tools 加载与消息列表首渲
    await waitFor(() => expect(markdownRender.mock.calls.length).toBeGreaterThanOrEqual(2));
    const afterMount = markdownRender.mock.calls.length;

    const input = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(input, { target: { value: '打字中' } });
    fireEvent.change(input, { target: { value: '打字中继续' } });
    fireEvent.change(input, { target: { value: '打字中继续abc' } });

    expect(markdownRender.mock.calls.length).toBe(afterMount);
  });
});
