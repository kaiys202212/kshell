// AgentHubPanel：跨项目汇总运行中的 ACP agent，@ 指定目标发消息、跳转定位、新建 agent、粘贴。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ChatInfo, ToolInfo } from '../lib/api';
import { useAppStore } from '../state/store';
import '../test/i18n';
import { tt } from '../test/i18n';
import AgentHubPanel from './AgentHubPanel';

const api = vi.hoisted(() => ({
  sendChatPrompt: vi.fn(),
  openWorkspaceACP: vi.fn(),
  getTools: vi.fn(),
  readClipboardPaste: vi.fn(),
}));
vi.mock('../lib/api', () => api);

function chat(over: Partial<ChatInfo>): ChatInfo {
  return {
    ID: 'c1',
    Key: 'new:1',
    Kind: 'new',
    SessionID: '',
    Workspace: 'D:\\ws\\a',
    Title: '重构认证',
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
  api.sendChatPrompt.mockResolvedValue(undefined);
  api.openWorkspaceACP.mockResolvedValue({ Kind: 'chat' });
  api.getTools.mockResolvedValue([]);
  api.readClipboardPaste.mockResolvedValue({ Text: '', Path: '' });
  useAppStore.setState({
    chats: [],
    agentNotices: [],
    workspaces: [],
    openTabs: [],
    activeTabId: null,
    lastAgentId: null,
    focusTermKey: null,
    toasts: [],
  });
});

afterEach(cleanup);

describe('AgentHubPanel', () => {
  it('无 agent 时显示空态', () => {
    useAppStore.setState({ chats: [] });
    render(<AgentHubPanel />);
    expect(screen.getByText(tt('ui.hub.empty'))).toBeTruthy();
  });

  it('列出运行中的 agent 及其工具名', () => {
    useAppStore.setState({
      chats: [
        {
          ID: 'c1',
          Key: 'new:1',
          Kind: 'new',
          SessionID: '',
          Workspace: 'D:\\ws\\a',
          Title: '重构认证',
          ToolID: 'claude',
          Status: 'ready',
          ExitCode: 0,
          Error: '',
          Prompted: true,
        },
      ],
    });
    render(<AgentHubPanel />);
    expect(screen.getByText(/Claude Code/)).toBeTruthy();
    expect(screen.getByText('重构认证')).toBeTruthy();
  });

  it('点击 agent 行：记录 lastAgentId 并跳转定位', () => {
    const setLast = vi.spyOn(useAppStore.getState(), 'setLastAgentId');
    const setActive = vi.spyOn(useAppStore.getState(), 'setActiveTab');
    const focus = vi.spyOn(useAppStore.getState(), 'requestFocusTerm');
    useAppStore.setState({
      chats: [chat({ ID: 'c1', Key: 'new:1', Workspace: 'D:\\ws\\a' })],
      openTabs: [{ id: 'D:\\ws\\a', name: 'a' }],
    });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByText('重构认证'));
    expect(setLast).toHaveBeenCalledWith('c1');
    expect(setActive).toHaveBeenCalledWith('D:\\ws\\a');
    expect(focus).toHaveBeenCalledWith('new:1');
  });

  it('@ 指定目标发送：投递到该 agent 并记录 lastAgentId', async () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1', ToolID: 'claude', Title: '重构认证' })] });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '@Claude Code#1 帮我看下' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', '@Claude Code#1 帮我看下'));
    expect(useAppStore.getState().lastAgentId).toBe('c1');
    expect((box as HTMLTextAreaElement).value).toBe('');
  });

  it('未 @ 且无最后交互：提示 need_target 且不发送', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: null });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '随便看看' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
    expect(useAppStore.getState().toasts.some((t) => t.title === tt('ui.hub.need_target'))).toBe(true);
    expect((box as HTMLTextAreaElement).value).toBe('随便看看');
  });

  it('未 @ 但有最后交互：发给 lastAgentId', async () => {
    useAppStore.setState({ chats: [chat({ ID: 'c9' })], lastAgentId: 'c9' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '继续' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c9', '继续'));
  });

  it('渲染 agent 结束通知的回复正文', () => {
    useAppStore.setState({
      agentNotices: [
        { id: 1, tool: 'codex', event: 'Stop', termKey: 'k', workspace: 'D:\\ws\\a', summary: '已完成重构' },
      ],
    });
    render(<AgentHubPanel />);
    expect(screen.getByText('已完成重构')).toBeTruthy();
    expect(screen.getByText(/Codex/)).toBeTruthy();
  });

  it('粘贴图片路径追加进草稿（含空格加引号）', async () => {
    api.readClipboardPaste.mockResolvedValue({ Text: '', Path: 'D:\\ws\\a b\\pic.png' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.paste(box);
    await waitFor(() => expect((box as HTMLTextAreaElement).value).toBe('"D:\\ws\\a b\\pic.png"'));
  });

  it('输入 @ 显示补全候选，点击补全为完整标签', () => {
    useAppStore.setState({
      chats: [
        chat({ ID: 'c1', ToolID: 'claude', Title: '甲' }),
        chat({ ID: 'c2', ToolID: 'cursor', Title: '乙' }),
      ],
    });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '@Cur' } });
    const list = screen.getByRole('list', { name: tt('ui.hub.mention_aria') });
    expect(list).toHaveTextContent('@Cursor#1');
    fireEvent.click(screen.getByRole('button', { name: '@Cursor#1' }));
    expect((box as HTMLTextAreaElement).value).toBe('@Cursor#1 ');
  });

  it('新建 agent：选项目与 ACP 工具后调用 openWorkspaceACP 并登记新 chat', async () => {
    const tools: ToolInfo[] = [
      { ID: 'claude', Name: 'Claude Code', BinPath: '', Version: '', Installed: true, Source: 'path', ACP: { Available: true, Source: 'builtin' } },
      { ID: 'legacy', Name: 'Legacy', BinPath: '', Version: '', Installed: true, Source: 'path' },
    ];
    api.getTools.mockResolvedValue(tools);
    api.openWorkspaceACP.mockResolvedValue({
      Kind: 'chat',
      Chat: chat({ ID: 'n1', Workspace: 'D:\\ws\\a', ToolID: 'claude' }),
    });
    useAppStore.setState({
      workspaces: [{ Path: 'D:\\ws\\a', Name: 'a', LastUsed: '', SessionCount: 0, ToolCounts: {}, Source: 'manual' }],
    });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.new_agent') }));
    fireEvent.change(await screen.findByLabelText(tt('ui.hub.field_project')), { target: { value: 'D:\\ws\\a' } });
    fireEvent.change(await screen.findByLabelText(tt('ui.hub.field_tool')), { target: { value: 'claude' } });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.create') }));
    await waitFor(() => expect(api.openWorkspaceACP).toHaveBeenCalledWith('D:\\ws\\a', 'claude'));
    await waitFor(() => expect(useAppStore.getState().chats.some((c) => c.ID === 'n1')).toBe(true));
    expect(useAppStore.getState().lastAgentId).toBe('n1');
  });
});
