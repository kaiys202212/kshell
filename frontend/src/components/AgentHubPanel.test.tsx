// AgentHubPanel：跨项目汇总运行中的 ACP agent，@ 指定目标发消息、跳转定位、新建 agent、粘贴。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
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
  listChatCommands: vi.fn(),
  listChatModels: vi.fn(),
  setChatModel: vi.fn(),
  cancelChat: vi.fn(),
  listHubMessages: vi.fn(),
  listHubArchive: vi.fn(),
  loadHubArchive: vi.fn(),
  clearHubMessages: vi.fn(),
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
  api.listChatCommands.mockResolvedValue([]);
  api.listChatModels.mockResolvedValue({ Current: '', Models: [] });
  api.setChatModel.mockResolvedValue(undefined);
  api.cancelChat.mockResolvedValue(undefined);
  api.listHubMessages.mockResolvedValue([]);
  api.listHubArchive.mockResolvedValue([]);
  api.loadHubArchive.mockResolvedValue([]);
  api.clearHubMessages.mockResolvedValue(undefined);
  useAppStore.setState({
    chats: [],
    agentNotices: [],
    hubMessages: [],
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
    fireEvent.change(box, { target: { value: '@Claude Code#c1 帮我看下' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', '@Claude Code#c1 帮我看下'));
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

  it('未 @ 时回落到 hub 消息里最近一次 agent 回复的会话，而非 store.lastAgentId', async () => {
    useAppStore.setState({
      chats: [
        chat({ ID: 'c1', ToolID: 'claude', Title: '甲' }),
        chat({ ID: 'c2', ToolID: 'claude', Title: '乙', Workspace: 'D:\\ws\\b' }),
      ],
      hubMessages: [
        { id: 1, kind: 'user', chatId: 'c1', workspace: 'D:\\ws\\a', text: '给甲的', ts: 1 },
        { id: 2, kind: 'agent', tool: 'claude', event: 'Stop', workspace: 'D:\\ws\\b', text: '乙的回复', ts: 2 },
      ],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '继续' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c2', '继续'));
  });

  it('未 @ 时回落也匹配 hub 里的用户消息（最后一条是 user 也生效）', async () => {
    useAppStore.setState({
      chats: [
        chat({ ID: 'c1', ToolID: 'claude', Title: '甲' }),
        chat({ ID: 'c2', ToolID: 'cursor', Title: '乙', Workspace: 'D:\\ws\\b' }),
      ],
      hubMessages: [
        { id: 1, kind: 'user', chatId: 'c1', workspace: 'D:\\ws\\a', text: '给甲的', ts: 1 },
        { id: 2, kind: 'user', chatId: 'c2', workspace: 'D:\\ws\\b', text: '给乙的', ts: 2 },
      ],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '继续' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c2', '继续'));
  });

  it('hub 消息经 termKey 反查 chat 后参与默认投递目标回落', async () => {
    useAppStore.setState({
      chats: [
        chat({ ID: 'c1', ToolID: 'claude', Title: '甲' }),
        chat({ ID: 'c2', ToolID: 'claude', Title: '乙', Key: 'new:2' }),
      ],
      hubMessages: [{ id: 1, kind: 'agent', tool: 'claude', event: 'Stop', termKey: 'new:2', text: '乙的回复', ts: 1 }],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '继续' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c2', '继续'));
  });

  it('hub 消息匹配不到任何 agent 时回落 store.lastAgentId', async () => {
    useAppStore.setState({
      chats: [chat({ ID: 'c1', ToolID: 'claude', Title: '甲' })],
      hubMessages: [{ id: 1, kind: 'agent', tool: 'codex', event: 'Stop', workspace: 'D:\\ws\\gone', text: '旧回复', ts: 1 }],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '继续' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', '继续'));
  });

  it('渲染持久历史中的用户消息与 agent 回复', async () => {
    api.listHubMessages.mockResolvedValue([
      { id: 1, kind: 'user', chatId: 'c1', text: '帮我修复', ts: 1 },
      { id: 2, kind: 'agent', tool: 'codex', event: 'Stop', text: '已完成重构', ts: 2 },
    ]);
    render(<AgentHubPanel />);
    expect(await screen.findByText('已完成重构')).toBeTruthy();
    expect(screen.getByText(/Codex/)).toBeTruthy();
    expect(screen.getByText('帮我修复')).toBeTruthy();
  });

  it('agent 回复正文按 markdown 渲染（加粗/列表），用户消息保持纯文本', async () => {
    api.listHubMessages.mockResolvedValue([
      { id: 1, kind: 'user', text: '**不是加粗**', ts: 1 },
      { id: 2, kind: 'agent', tool: 'claude', event: 'Stop', text: '**加粗** 与\n\n- 条目一\n- 条目二', ts: 2 },
    ]);
    render(<AgentHubPanel />);
    // agent 正文渲染出 strong / li
    expect(await screen.findByText('加粗').then((el) => el.closest('strong'))).toBeTruthy();
    expect(screen.getAllByRole('listitem').length).toBe(2);
    // 用户气泡不含 strong（markdown 未渲染）
    const user = screen.getByText('**不是加粗**');
    expect(user.closest('strong')).toBeNull();
  });

  it('agent 回复归因：气泡头部展示 工具 · 项目 · 会话标题', async () => {
    useAppStore.setState({
      chats: [chat({ ID: 'c1', Workspace: 'D:\\ws\\alpha', Title: '重构认证' })],
    });
    api.listHubMessages.mockResolvedValue([
      { id: 2, kind: 'agent', tool: 'claude', event: 'Stop', chatId: 'c1', workspace: 'D:\\ws\\alpha', text: '完成', ts: 2 },
    ]);
    render(<AgentHubPanel />);
    expect(await screen.findByText('Claude Code · alpha · 重构认证')).toBeTruthy();
  });

  it('旧消息缺 chats 反查：归因回退 #chatId；用户消息归因行在气泡上方', async () => {
    api.listHubMessages.mockResolvedValue([
      { id: 1, kind: 'user', chatId: 'cX', workspace: 'D:\\ws\\beta', text: '在吗', ts: 1 },
      { id: 2, kind: 'agent', tool: 'codex', event: 'Stop', chatId: 'cX', workspace: 'D:\\ws\\beta', text: '在', ts: 2 },
    ]);
    render(<AgentHubPanel />);
    expect(await screen.findByText('Codex · beta · #cX')).toBeTruthy();
    expect(screen.getByText('beta · #cX')).toBeTruthy();
  });

  it('归档旧消息同样走归因与 markdown 渲染', async () => {
    api.listHubArchive.mockResolvedValue([{ month: '2026-09', count: 1 }]);
    api.loadHubArchive.mockResolvedValue([
      { id: 1, kind: 'agent', tool: 'claude', event: 'Stop', chatId: 'c1', workspace: 'D:\\ws\\alpha', text: '- 旧消息', ts: 1 },
    ]);
    useAppStore.setState({ chats: [chat({ ID: 'c1', Workspace: 'D:\\ws\\alpha', Title: '重构认证' })] });
    render(<AgentHubPanel />);
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.hub.history') }));
    fireEvent.click(await screen.findByRole('button', { name: /2026-09/ }));
    expect(await screen.findByText('Claude Code · alpha · 重构认证')).toBeTruthy();
    expect(screen.getAllByRole('listitem').length).toBeGreaterThan(0);
  });

  it('agent 回复不随通知气泡（agentNotices）消失而消失', async () => {
    useAppStore.setState({
      agentNotices: [{ id: 9, tool: 'codex', event: 'Stop', termKey: 'k', workspace: 'D:\\ws\\a', summary: '气泡正文' }],
      hubMessages: [{ id: 2, kind: 'agent', tool: 'codex', event: 'Stop', text: '持久正文', ts: 2 }],
    });
    render(<AgentHubPanel />);
    expect(screen.getByText('持久正文')).toBeTruthy();
    // 气泡 10s 后被 dismiss（清空 agentNotices）——Hub 消息必须仍在
    useAppStore.getState().clearAgentNotices();
    expect(useAppStore.getState().agentNotices).toHaveLength(0);
    expect(screen.getByText('持久正文')).toBeTruthy();
  });

  it('拉取失败不阻塞：空历史时展示空态控件', async () => {
    api.listHubMessages.mockRejectedValue(new Error('boom'));
    render(<AgentHubPanel />);
    expect(screen.getByText(tt('ui.hub.empty'))).toBeTruthy();
  });

  it('历史入口：展开归档月份并加载旧消息', async () => {
    api.listHubArchive.mockResolvedValue([{ month: '2026-09', count: 3 }]);
    api.loadHubArchive.mockResolvedValue([
      { id: 1, kind: 'agent', tool: 'claude', event: 'Stop', text: '上月的旧消息', ts: 1 },
    ]);
    render(<AgentHubPanel />);
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.hub.history') }));
    const monthBtn = await screen.findByRole('button', { name: /2026-09/ });
    fireEvent.click(monthBtn);
    await waitFor(() => expect(api.loadHubArchive).toHaveBeenCalledWith('2026-09'));
    expect(await screen.findByText('上月的旧消息')).toBeTruthy();
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
    expect(list).toHaveTextContent('@Cursor#c2');
    // 第二行展示项目 · 会话标题
    expect(list).toHaveTextContent('a · 乙');
    fireEvent.click(screen.getByRole('button', { name: /@Cursor#c2/ }));
    expect((box as HTMLTextAreaElement).value).toBe('@Cursor#c2 ');
  });

  it('无标题会话在列表兜底显示项目基名', () => {
    useAppStore.setState({
      chats: [chat({ ID: 'c1', Title: '', Workspace: 'D:\\ws\\gamma' })],
    });
    render(<AgentHubPanel />);
    // 分组标题与会话兜底同名（同项目）：至少出现两次即证明兜底生效
    expect(screen.getAllByText('gamma').length).toBeGreaterThanOrEqual(2);
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

  it('键入 / 展示客户端命令，并可按前缀过滤', async () => {
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '/' } });
    const list = screen.getByRole('listbox', { name: tt('ui.hub.cmd_aria') });
    expect(list).toHaveTextContent('/new');
    expect(list).toHaveTextContent('/stop');
    expect(list).toHaveTextContent('/model');
    expect(list).toHaveTextContent('/clear');
    fireEvent.change(box, { target: { value: '/cl' } });
    const filtered = screen.getByRole('listbox', { name: tt('ui.hub.cmd_aria') });
    expect(filtered).toHaveTextContent('/clear');
    expect(filtered).not.toHaveTextContent('/new');
  });

  it('选中 /clear：清空本地 Hub 消息流与 agent 通知，且不投递后端', async () => {
    useAppStore.setState({
      chats: [chat({ ID: 'c1' })],
      lastAgentId: 'c1',
      agentNotices: [{ id: 1, tool: 'codex', event: 'Stop', termKey: 'k', workspace: 'D:\\ws\\a', summary: '已完成' }],
      hubMessages: [{ id: 1, kind: 'agent', tool: 'codex', event: 'Stop', text: '旧消息', ts: 1 }],
    });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '/clear' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByText('旧消息')).toBeNull());
    expect(useAppStore.getState().agentNotices).toHaveLength(0);
    expect(useAppStore.getState().hubMessages).toHaveLength(0);
  });

  it('agent 技能与客户端命令同名时去重（客户端优先，大小写不敏感）', async () => {
    api.listChatCommands.mockResolvedValue([{ Name: 'Clear', Description: 'agent clear' }]);
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '/' } });
    await waitFor(() => expect(api.listChatCommands).toHaveBeenCalledWith('c1'));
    // 等待拉取完成后列表仍只含一条 clear（客户端），不出现 agent 的 Clear 重复项
    await waitFor(() => {
      const list = screen.getByRole('listbox', { name: tt('ui.hub.cmd_aria') });
      const clearRows = within(list).getAllByText(/^\/clear$/i);
      expect(clearRows).toHaveLength(1);
    });
  });

  it('选中 /stop：对目标 agent 调 cancelChat', async () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '/stop' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.cancelChat).toHaveBeenCalledWith('c1'));
  });

  it('/stop 无目标时提示 need_target', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: null });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '/stop' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(api.cancelChat).not.toHaveBeenCalled();
    expect(useAppStore.getState().toasts.some((t) => t.title === tt('ui.hub.need_target'))).toBe(true);
    // 无目标时不消费输入，草稿保留
    expect((box as HTMLTextAreaElement).value).toBe('/stop');
  });

  it('agent 技能出现在命令列表，选中后切入草稿并可投递', async () => {
    api.listChatCommands.mockResolvedValue([{ Name: 'commit', Description: 'Commit changes' }]);
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '/' } });
    await waitFor(() =>
      expect(screen.getByRole('listbox', { name: tt('ui.hub.cmd_aria') })).toHaveTextContent('/commit'),
    );
    // 过滤到 /c：客户端命令 clear 在前，agent 技能 commit 在后。
    fireEvent.change(box, { target: { value: '/c' } });
    fireEvent.keyDown(box, { key: 'ArrowDown' });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect((box as HTMLTextAreaElement).value).toBe('/commit ');
    fireEvent.change(box, { target: { value: '/commit fix bug' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', '/commit fix bug'));
  });

  it('新建 agent：agent 报告模型时展示第二步，确认后切换模型并登记', async () => {
    api.getTools.mockResolvedValue([
      { ID: 'claude', Name: 'Claude Code', BinPath: '', Version: '', Installed: true, Source: 'path', ACP: { Available: true, Source: 'builtin' } },
    ]);
    api.openWorkspaceACP.mockResolvedValue({
      Kind: 'chat',
      Chat: chat({ ID: 'n1', Workspace: 'D:\\ws\\a', ToolID: 'claude' }),
    });
    api.listChatModels.mockResolvedValue({
      Current: 'm1',
      Models: [
        { ID: 'm1', Name: 'Model One' },
        { ID: 'm2', Name: 'Model Two' },
      ],
    });
    useAppStore.setState({
      workspaces: [{ Path: 'D:\\ws\\a', Name: 'a', LastUsed: '', SessionCount: 0, ToolCounts: {}, Source: 'manual' }],
    });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.new_agent') }));
    fireEvent.change(await screen.findByLabelText(tt('ui.hub.field_project')), { target: { value: 'D:\\ws\\a' } });
    fireEvent.change(await screen.findByLabelText(tt('ui.hub.field_tool')), { target: { value: 'claude' } });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.create') }));
    // 第二步：模型下拉出现，默认当前模型
    const select = await screen.findByLabelText(tt('ui.hub.model_label'));
    expect((select as HTMLSelectElement).value).toBe('m1');
    fireEvent.change(select, { target: { value: 'm2' } });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.model_confirm') }));
    await waitFor(() => expect(api.setChatModel).toHaveBeenCalledWith('n1', 'm2'));
    expect(useAppStore.getState().chats.some((c) => c.ID === 'n1')).toBe(true);
  });

  it('新建 agent：agent 无模型报告时跳过第二步直接登记', async () => {
    api.getTools.mockResolvedValue([
      { ID: 'claude', Name: 'Claude Code', BinPath: '', Version: '', Installed: true, Source: 'path', ACP: { Available: true, Source: 'builtin' } },
    ]);
    api.openWorkspaceACP.mockResolvedValue({
      Kind: 'chat',
      Chat: chat({ ID: 'n2', Workspace: 'D:\\ws\\a', ToolID: 'claude' }),
    });
    api.listChatModels.mockResolvedValue({ Current: '', Models: [] });
    useAppStore.setState({
      workspaces: [{ Path: 'D:\\ws\\a', Name: 'a', LastUsed: '', SessionCount: 0, ToolCounts: {}, Source: 'manual' }],
    });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.new_agent') }));
    fireEvent.change(await screen.findByLabelText(tt('ui.hub.field_project')), { target: { value: 'D:\\ws\\a' } });
    fireEvent.change(await screen.findByLabelText(tt('ui.hub.field_tool')), { target: { value: 'claude' } });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.create') }));
    await waitFor(() => expect(useAppStore.getState().chats.some((c) => c.ID === 'n2')).toBe(true));
    expect(api.setChatModel).not.toHaveBeenCalled();
  });

  it('新建期间取消：会话仍登记，且不把下次打开带进模型步骤', async () => {
    api.getTools.mockResolvedValue([
      { ID: 'claude', Name: 'Claude Code', BinPath: '', Version: '', Installed: true, Source: 'path', ACP: { Available: true, Source: 'builtin' } },
    ]);
    // 让 openWorkspaceACP 挂起，模拟建会话期间的关闭
    let resolveOpen: (v: unknown) => void = () => {};
    api.openWorkspaceACP.mockReturnValue(
      new Promise((res) => {
        resolveOpen = res;
      }),
    );
    api.listChatModels.mockResolvedValue({ Current: 'm1', Models: [{ ID: 'm1', Name: 'Model One' }] });
    useAppStore.setState({
      workspaces: [{ Path: 'D:\\ws\\a', Name: 'a', LastUsed: '', SessionCount: 0, ToolCounts: {}, Source: 'manual' }],
    });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.new_agent') }));
    fireEvent.change(await screen.findByLabelText(tt('ui.hub.field_project')), { target: { value: 'D:\\ws\\a' } });
    fireEvent.change(await screen.findByLabelText(tt('ui.hub.field_tool')), { target: { value: 'claude' } });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.create') }));
    // 关闭对话框（Cancel），随后在途的 openWorkspaceACP 才 resolve
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.cancel') }));
    resolveOpen({ Kind: 'chat', Chat: chat({ ID: 'n3', Workspace: 'D:\\ws\\a', ToolID: 'claude' }) });
    await waitFor(() => expect(useAppStore.getState().chats.some((c) => c.ID === 'n3')).toBe(true));
    // 再次打开应回到第一步（不残留模型步骤）
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.new_agent') }));
    expect(await screen.findByLabelText(tt('ui.hub.field_project'))).toBeTruthy();
  });
});
