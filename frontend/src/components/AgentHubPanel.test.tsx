// AgentHubPanel：跨项目汇总运行中的 ACP agent，@ 指定目标发消息、跳转定位、新建 agent、粘贴。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ChatInfo, ToolInfo } from '../lib/api';
import { useAppStore } from '../state/store';
import '../test/i18n';
import { tt } from '../test/i18n';
import { loadDefaultCreate, saveDefaultCreate, saveLastCreate, saveToolModels } from '../lib/agenthubDefaults';
import { REQUIREMENT_MIME } from './RequirementList';
import AgentHubPanel from './AgentHubPanel';

const api = vi.hoisted(() => ({
  sendChatPrompt: vi.fn(),
  openWorkspaceACP: vi.fn(),
  openSessionACP: vi.fn(),
  getSessions: vi.fn(),
  getTools: vi.fn(),
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
  listRequirements: vi.fn(),
  markRequirementDispatched: vi.fn(),
}));
vi.mock('../lib/api', () => api);

// dispatchRequirement 整体 mock：面板 drop 派发断言参数即可，流程细节在 lib 单测覆盖
const dispatchRequirement = vi.hoisted(() => vi.fn());
vi.mock('../lib/dispatchRequirement', () => ({ dispatchRequirement }));

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
  api.openSessionACP.mockResolvedValue({ Kind: 'chat' });
  api.getSessions.mockResolvedValue([]);
  api.getTools.mockResolvedValue([]);
  api.getModelConfig.mockResolvedValue({
    Enabled: false,
    Preset: '',
    OpenAIBaseURL: '',
    AnthropicBaseURL: '',
    Models: [],
    ActiveModel: '',
    APIKeySet: false,
  });
  api.readClipboardPaste.mockResolvedValue({ Text: '', Path: '' });
  api.listChatCommands.mockResolvedValue([]);
  api.listChatModels.mockResolvedValue({ Current: '', Models: [] });
  api.setChatModel.mockResolvedValue(undefined);
  api.cancelChat.mockResolvedValue(undefined);
  api.closeChat.mockResolvedValue(undefined);
  api.listHubMessages.mockResolvedValue([]);
  api.listHubArchive.mockResolvedValue([]);
  api.loadHubArchive.mockResolvedValue([]);
  api.clearHubMessages.mockResolvedValue(undefined);
  api.recallHubMessage.mockResolvedValue(undefined);
  api.listRequirements.mockResolvedValue([]);
  api.markRequirementDispatched.mockResolvedValue(undefined);
  dispatchRequirement.mockResolvedValue(undefined);
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
    selectedProject: null,
  });
});

afterEach(() => {
  cleanup();
  // 清掉用例写入的 lastCreate/模型缓存等 localStorage，消除用例间的默认值泄漏耦合
  localStorage.clear();
});

const acpTool = (id = 'claude'): ToolInfo => ({
  ID: id,
  Name: 'Claude Code',
  BinPath: '',
  Version: '',
  Installed: true,
  Source: 'path',
  ACP: { Available: true, Source: 'builtin' },
});

const workspaceA = {
  Path: 'D:\\ws\\a',
  Name: 'a',
  LastUsed: '',
  SessionCount: 0,
  ToolCounts: {},
  Source: 'manual' as const,
};

describe('AgentHubPanel', () => {
  it('无 agent 时胶囊显示空文案', () => {
    useAppStore.setState({ chats: [] });
    render(<AgentHubPanel />);
    expect(screen.getByRole('button', { name: tt('ui.hub.session_bar_aria') })).toHaveTextContent(
      tt('ui.hub.empty'),
    );
  });

  it('有会话时渲染浮动胶囊（默认目标名），旧固定列表不再渲染', () => {
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
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    const pill = screen.getByRole('button', { name: tt('ui.hub.session_bar_aria') });
    expect(pill).toHaveTextContent('重构认证');
    // 旧固定分组列表已删除：不再存在以 Hub 标题命名的列表
    expect(screen.queryByRole('list', { name: tt('ui.hub.title') })).toBeNull();
    // 面板未展开时不出现「工具 · 状态」行文案
    expect(screen.queryByText(/Claude Code · /)).toBeNull();
  });

  it('点胶囊展开面板列出全部会话，再点收起', () => {
    useAppStore.setState({
      chats: [chat({ ID: 'c1' }), chat({ ID: 'c2', Title: '乙', Workspace: 'D:\\ws\\b' })],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    const pill = screen.getByRole('button', { name: tt('ui.hub.session_bar_aria') });
    // 外层容器单个 region（session_panel_aria），内层 ul 不再带 aria-label
    expect(screen.queryByRole('region', { name: tt('ui.hub.session_panel_aria') })).toBeNull();
    fireEvent.click(pill);
    const region = screen.getByRole('region', { name: tt('ui.hub.session_panel_aria') });
    const panel = pill.parentElement as HTMLElement;
    expect(panel).toHaveTextContent('重构认证');
    expect(panel).toHaveTextContent('乙');
    expect(region).toHaveTextContent('重构认证');
    expect(region).toHaveTextContent('乙');
    fireEvent.click(pill);
    expect(screen.queryByRole('region', { name: tt('ui.hub.session_panel_aria') })).toBeNull();
  });

  it('点击面板会话行：记录 lastAgentId 并收起面板（不再跳工作区页签）', () => {
    const setLast = vi.spyOn(useAppStore.getState(), 'setLastAgentId');
    const setActive = vi.spyOn(useAppStore.getState(), 'setActiveTab');
    const focus = vi.spyOn(useAppStore.getState(), 'requestFocusTerm');
    useAppStore.setState({
      chats: [chat({ ID: 'c1', Key: 'new:1', Workspace: 'D:\\ws\\a' })],
      openTabs: [{ id: 'D:\\ws\\a', name: 'a' }],
      lastAgentId: null,
    });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.session_bar_aria') }));
    fireEvent.click(screen.getByText('重构认证'));
    expect(setLast).toHaveBeenCalledWith('c1');
    expect(screen.queryByRole('region', { name: tt('ui.hub.session_panel_aria') })).toBeNull();
    expect(setActive).not.toHaveBeenCalled();
    expect(focus).not.toHaveBeenCalled();
  });

  it('点空闲会话的 ×：该会话行从面板消失（removeChat + closeChat），面板保持展开；忙碌行无 ×', async () => {
    useAppStore.setState({
      chats: [chat({ ID: 'c1' }), chat({ ID: 'c2', Title: '乙', Status: 'running' })],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.session_bar_aria') }));
    // 仅空闲的 c1 有关闭按钮（c2 running 无）
    const closes = screen.getAllByRole('button', { name: tt('ui.hub.close_session') });
    expect(closes).toHaveLength(1);
    fireEvent.click(closes[0]);
    // 断言结果而非实现：c1 行从面板消失，c2 仍在，面板保持展开
    await waitFor(() => expect(screen.queryByText('重构认证')).toBeNull());
    expect(screen.getByText('乙')).toBeTruthy();
    expect(screen.getByRole('region', { name: tt('ui.hub.session_panel_aria') })).toBeTruthy();
    // 后端 Manager.Close 已被调用（幂等），不再调 cancelChat（那只会取消当前轮）
    await waitFor(() => expect(api.closeChat).toHaveBeenCalledWith('c1'));
    expect(api.cancelChat).not.toHaveBeenCalled();
  });

  it('关闭会话失败时提示 close_session_failed 且行不消失', async () => {
    api.closeChat.mockRejectedValue(new Error('boom'));
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.session_bar_aria') }));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.close_session') }));
    await waitFor(() =>
      expect(
        useAppStore
          .getState()
          .toasts.some((t) => t.title === tt('ui.hub.close_session_failed').replace('{{err}}', 'boom')),
      ).toBe(true),
    );
    // 失败不摘除：行仍在面板里，可重试（胶囊同文案，故在 region 内断言）
    expect(screen.getByRole('region', { name: tt('ui.hub.session_panel_aria') })).toHaveTextContent('重构认证');
  });

  it('会话行可拖拽：dragStart 写入 @ 标签到 dataTransfer', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.session_bar_aria') }));
    const panel = screen.getByRole('region', { name: tt('ui.hub.session_panel_aria') });
    const row = within(panel).getByText('重构认证').closest('div')!;
    const dt = {
      data: {} as Record<string, string>,
      setData(k: string, v: string) {
        this.data[k] = v;
      },
      getData(k: string) {
        return this.data[k];
      },
      effectAllowed: '',
    };
    fireEvent.dragStart(row, { dataTransfer: dt });
    expect(dt.getData('text/plain')).toBe('@Claude Code#c1');
    expect(dt.effectAllowed).toBe('copy');
  });

  it('输入框接收拖放：已有草稿时 trimEnd 后追加 @ 目标', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '帮我看看  ' } });
    const dt = {
      types: ['text/plain'],
      data: { 'text/plain': '@Claude Code#c1' } as Record<string, string>,
      setData(k: string, v: string) {
        this.data[k] = v;
      },
      getData(k: string) {
        return this.data[k];
      },
    };
    fireEvent.drop(box, { dataTransfer: dt });
    expect((box as HTMLTextAreaElement).value).toBe('帮我看看 @Claude Code#c1');
  });

  it('输入框接收拖放：空草稿直接设为 @ 目标', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    const dt = {
      types: ['text/plain'],
      data: { 'text/plain': '@Claude Code#c1' } as Record<string, string>,
      setData(k: string, v: string) {
        this.data[k] = v;
      },
      getData(k: string) {
        return this.data[k];
      },
    };
    fireEvent.drop(box, { dataTransfer: dt });
    expect((box as HTMLTextAreaElement).value).toBe('@Claude Code#c1');
  });

  it('面板展开时点击胶囊之外的区域收起', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.session_bar_aria') }));
    expect(screen.getByRole('region', { name: tt('ui.hub.session_panel_aria') })).toBeTruthy();
    fireEvent.mouseDown(document.body);
    expect(screen.queryByRole('region', { name: tt('ui.hub.session_panel_aria') })).toBeNull();
  });

  it('胶囊内联在标题文字后（无浮动定位），归档展开不改变位置', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const bar = screen.getByRole('button', { name: tt('ui.hub.session_bar_aria') }).parentElement as HTMLElement;
    // 内联在标题行：relative 锚定展开面板，不再是 absolute 浮动位移
    expect(bar.className).toContain('relative');
    expect(bar.className).not.toContain('absolute');
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.history') }));
    expect(bar.className).not.toContain('top-56');
  });

  it('面板会话行点 @ 图标：草稿保留式追加 @ 标签并收起面板', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder')) as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: '帮我看看' } });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.session_bar_aria') }));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.mention_session') }));
    // 尾随空格便于继续输入（与 quickReply rephrase 同语义）
    expect(box.value).toBe('帮我看看 @Claude Code#c1 ');
    expect(screen.queryByRole('region', { name: tt('ui.hub.session_panel_aria') })).toBeNull();
  });

  it('面板会话行点跳转图标：打开工作区页签并按 chat.Key 定位会话对话页', () => {
    const openTab = vi.spyOn(useAppStore.getState(), 'openTab');
    const focus = vi.spyOn(useAppStore.getState(), 'requestFocusTerm');
    useAppStore.setState({
      chats: [chat({ ID: 'c1', Key: 'new:1', Workspace: 'D:\\ws\\a' })],
      workspaces: [workspaceA],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.session_bar_aria') }));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.jump_session') }));
    expect(openTab).toHaveBeenCalledWith(workspaceA);
    expect(focus).toHaveBeenCalledWith('new:1');
    expect(screen.queryByRole('region', { name: tt('ui.hub.session_panel_aria') })).toBeNull();
  });

  it('跳转图标：工作区不在列表时手工构造页签打开（Key 缺失只开页签不定位）', () => {
    const openTab = vi.spyOn(useAppStore.getState(), 'openTab');
    const focus = vi.spyOn(useAppStore.getState(), 'requestFocusTerm');
    useAppStore.setState({
      chats: [chat({ ID: 'c1', Key: '', Workspace: 'D:\\ws\\x' })],
      workspaces: [],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.session_bar_aria') }));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.jump_session') }));
    expect(openTab).toHaveBeenCalledWith(expect.objectContaining({ Path: 'D:\\ws\\x', Name: 'x' }));
    expect(focus).not.toHaveBeenCalled();
    expect(screen.queryByRole('region', { name: tt('ui.hub.session_panel_aria') })).toBeNull();
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

  it('未 @ 且无最后交互：提示 need_target_drag 且不发送', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: null });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '随便看看' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
    expect(useAppStore.getState().toasts.some((t) => t.title === tt('ui.hub.need_target_drag'))).toBe(true);
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

  it('hub 消息 workspace 与 chat.Workspace 经 sameWorkspacePath 归并后命中回落', async () => {
    useAppStore.setState({
      chats: [
        chat({ ID: 'c1', ToolID: 'claude', Title: '甲' }),
        chat({ ID: 'c2', ToolID: 'claude', Title: '乙', Workspace: 'D:\\ws\\b' }),
      ],
      // 消息 workspace 小写 + 正斜杠：与 chat 的 Windows 反斜杠路径归并为同一项目
      hubMessages: [{ id: 1, kind: 'agent', tool: 'claude', event: 'Stop', workspace: 'd:/ws/b', text: '乙的回复', ts: 1 }],
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

  it('后端欢迎词 wire key 经 translateBackend 渲染成自然语言，普通回复原样展示', async () => {
    api.listHubMessages.mockResolvedValue([
      { id: 1, kind: 'agent', tool: 'claude', event: 'done', chatId: 'n1', text: 'hub.welcome|Claude Code', ts: 1 },
      { id: 2, kind: 'agent', tool: 'claude', event: 'Stop', text: '普通回复正文', ts: 2 },
    ]);
    render(<AgentHubPanel />);
    // wire key 被翻译：出现 en 资源里的欢迎词（带工具名插值，tt 不做插值故手动替换），
    // 不再有 key 字面量
    expect(await screen.findByText(tt('hub.welcome').replace('{{0}}', 'Claude Code'))).toBeTruthy();
    expect(screen.queryByText('hub.welcome|Claude Code')).toBeNull();
    // 未注册 key 的普通正文不被破坏
    expect(screen.getByText('普通回复正文')).toBeTruthy();
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

  it('用户气泡：文字在卡片内居左，卡片整体仍靠右；长串可断行不溢出', async () => {
    api.listHubMessages.mockResolvedValue([
      { id: 1, kind: 'user', chatId: 'c1', text: '很长的一条无空格长路径D:\\a\\b\\c\\d\\e\\f\\g', ts: 1 },
    ]);
    render(<AgentHubPanel />);
    const bubble = await screen.findByText(/很长的一条无空格长路径/);
    // 卡片容器：inline-block 且靠右（外层 text-right），但卡内文字居左且可断行
    const card = bubble.closest('div.selectable');
    expect(card).not.toBeNull();
    expect(card!.className).toContain('text-left');
    expect(card!.className).toContain('break-words');
    expect(card!.className).toContain('overflow-hidden');
  });

  it('消息流容器：带水平内边距防气泡贴滚动条，垂直间距拉开', async () => {
    useAppStore.setState({
      hubMessages: [{ id: 1, kind: 'user', text: '你好', ts: 1 }],
    });
    render(<AgentHubPanel />);
    const log = screen.getByRole('log');
    expect(log.className).toContain('px-3');
    expect(log.className).toContain('py-2');
    expect(log.className).toContain('gap-3');
    expect(log.className).not.toContain('gap-1');
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

  it('拉取失败不阻塞：无会话时胶囊显示空文案', async () => {
    api.listHubMessages.mockRejectedValue(new Error('boom'));
    render(<AgentHubPanel />);
    expect(screen.getByRole('button', { name: tt('ui.hub.session_bar_aria') })).toHaveTextContent(
      tt('ui.hub.empty'),
    );
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
    const list = screen.getByRole('listbox', { name: tt('ui.hub.mention_aria') });
    expect(list).toHaveTextContent('@Cursor#c2');
    // 第二行展示项目 · 会话标题
    expect(list).toHaveTextContent('a · 乙');
    // 候选为 role=option 的 li（与 # 弹层结构一致），mousedown 选中
    const option = within(list).getByText('@Cursor#c2').closest('li')!;
    fireEvent.mouseDown(option);
    expect((box as HTMLTextAreaElement).value).toBe('@Cursor#c2 ');
  });

  it('@ 补全键盘导航：ArrowDown/ArrowUp 切换高亮并夹取边界，Enter 填入不发送', () => {
    useAppStore.setState({
      chats: [
        chat({ ID: 'c1', ToolID: 'claude', Title: '甲' }),
        chat({ ID: 'c2', ToolID: 'cursor', Title: '乙' }),
      ],
    });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '@' } });
    const list = screen.getByRole('listbox', { name: tt('ui.hub.mention_aria') });
    const options = within(list).getAllByRole('option');
    expect(options).toHaveLength(2);
    // 初始高亮第一项
    expect(options[0]).toHaveAttribute('aria-selected', 'true');
    // 下移到第二项
    fireEvent.keyDown(box, { key: 'ArrowDown' });
    expect(options[1]).toHaveAttribute('aria-selected', 'true');
    expect(options[0]).toHaveAttribute('aria-selected', 'false');
    // 已到底再下移：夹取不越界
    fireEvent.keyDown(box, { key: 'ArrowDown' });
    expect(options[1]).toHaveAttribute('aria-selected', 'true');
    // 上移两步：夹回第一项
    fireEvent.keyDown(box, { key: 'ArrowUp' });
    fireEvent.keyDown(box, { key: 'ArrowUp' });
    expect(options[0]).toHaveAttribute('aria-selected', 'true');
    // Enter 填入高亮项对应 label，不发送
    fireEvent.keyDown(box, { key: 'Enter' });
    expect((box as HTMLTextAreaElement).value).toBe('@Claude Code#c1 ');
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
  });

  it('@ 补全 Tab 同样填入高亮项', () => {
    useAppStore.setState({
      chats: [
        chat({ ID: 'c1', ToolID: 'claude', Title: '甲' }),
        chat({ ID: 'c2', ToolID: 'cursor', Title: '乙' }),
      ],
    });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '@' } });
    fireEvent.keyDown(box, { key: 'ArrowDown' });
    fireEvent.keyDown(box, { key: 'Tab' });
    expect((box as HTMLTextAreaElement).value).toBe('@Cursor#c2 ');
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
  });

  it('弹层开着时 chats 后台收缩：高亮下标夹回最后一项，Enter 仍填入可见候选', () => {
    useAppStore.setState({
      chats: [
        chat({ ID: 'c1', ToolID: 'claude', Title: '甲' }),
        chat({ ID: 'c2', ToolID: 'cursor', Title: '乙' }),
      ],
    });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '@' } });
    // 高亮移到第二项后，后台 scan:done 使候选收缩为 1 个（act 包住触发同步重渲染）
    fireEvent.keyDown(box, { key: 'ArrowDown' });
    act(() => {
      useAppStore.setState({ chats: [chat({ ID: 'c1', ToolID: 'claude', Title: '甲' })] });
    });
    const list = screen.getByRole('listbox', { name: tt('ui.hub.mention_aria') });
    const options = within(list).getAllByRole('option');
    expect(options).toHaveLength(1);
    // 唯一候选保持高亮（不出现 aria-selected 全 false 的列表）
    expect(options[0]).toHaveAttribute('aria-selected', 'true');
    fireEvent.keyDown(box, { key: 'Enter' });
    expect((box as HTMLTextAreaElement).value).toBe('@Claude Code#c1 ');
  });

  it('@ 补全 Esc 关闭后，Enter 恢复发送语义（回落 lastAgentId 投递）', async () => {
    useAppStore.setState({
      chats: [
        chat({ ID: 'c1', ToolID: 'claude', Title: '甲' }),
        chat({ ID: 'c2', ToolID: 'cursor', Title: '乙' }),
      ],
      lastAgentId: 'c2',
    });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '@' } });
    expect(screen.getByRole('listbox', { name: tt('ui.hub.mention_aria') })).toBeTruthy();
    fireEvent.keyDown(box, { key: 'Escape' });
    expect(screen.queryByRole('listbox', { name: tt('ui.hub.mention_aria') })).toBeNull();
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c2', '@'));
    expect((box as HTMLTextAreaElement).value).toBe('');
  });

  it('@ 补全键盘切换时高亮项 scrollIntoView 跟随（保持可见）', () => {
    useAppStore.setState({
      chats: [
        chat({ ID: 'c1', ToolID: 'claude', Title: '甲' }),
        chat({ ID: 'c2', ToolID: 'cursor', Title: '乙' }),
      ],
    });
    const spy = vi.fn();
    const proto = HTMLElement.prototype as unknown as { scrollIntoView?: unknown };
    const prev = proto.scrollIntoView;
    proto.scrollIntoView = spy;
    try {
      render(<AgentHubPanel />);
      const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
      fireEvent.change(box, { target: { value: '@' } });
      fireEvent.keyDown(box, { key: 'ArrowDown' });
      expect(spy).toHaveBeenCalledWith({ block: 'nearest' });
      // 高亮的是第二项：scrollIntoView 的 this 是该 li
      const list = screen.getByRole('listbox', { name: tt('ui.hub.mention_aria') });
      const second = within(list).getAllByRole('option')[1];
      expect(spy.mock.contexts).toContain(second);
    } finally {
      proto.scrollIntoView = prev;
    }
  });

  it('# 弹层键盘切换时高亮项 scrollIntoView 跟随（保持可见）', () => {
    const spy = vi.fn();
    const proto = HTMLElement.prototype as unknown as { scrollIntoView?: unknown };
    const prev = proto.scrollIntoView;
    proto.scrollIntoView = spy;
    try {
      render(<AgentHubPanel />);
      const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
      fireEvent.change(box, { target: { value: '#' } });
      fireEvent.keyDown(box, { key: 'ArrowDown' });
      expect(spy).toHaveBeenCalledWith({ block: 'nearest' });
      const list = screen.getByRole('listbox', { name: tt('ui.hub.cmd_aria') });
      const second = within(list).getAllByRole('option')[1];
      expect(spy.mock.contexts).toContain(second);
    } finally {
      proto.scrollIntoView = prev;
    }
  });

  it('无标题会话在面板兜底显示项目基名', () => {
    useAppStore.setState({
      chats: [chat({ ID: 'c1', Title: '', Workspace: 'D:\\ws\\gamma' })],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.session_bar_aria') }));
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

  it('新建 agent：模型下拉合并 kshell 注入模型并去重', async () => {
    api.getTools.mockResolvedValue([acpTool()]);
    api.getModelConfig.mockResolvedValue({
      Enabled: true,
      Preset: 'openai',
      OpenAIBaseURL: 'https://h/',
      AnthropicBaseURL: 'https://h/',
      Models: ['glm-5.3-flash', 'deepseek-v4.1-flash'],
      ActiveModel: 'glm-5.3-flash',
      APIKeySet: true,
    });
    useAppStore.setState({ workspaces: [workspaceA] });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.new_agent') }));
    const select = (await screen.findByLabelText(tt('ui.hub.field_model'))) as HTMLSelectElement;
    // 有模型时直接列模型（无空「默认」选项，既有行为）；无模型时才只剩「默认模型」
    await waitFor(() => expect(select.options).toHaveLength(2));
    const values = Array.from(select.options).map((o) => o.value);
    expect(values).toContain('glm-5.3-flash');
    expect(values).toContain('deepseek-v4.1-flash');
    expect(values[0]).toBe('glm-5.3-flash'); // 注入模型在前
    // 缓存模型与注入模型重复时去重：缓存 m1 与注入 glm 重名（用不同 key 模拟不重复）
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.cancel') }));
    saveToolModels('claude', ['glm-5.3-flash', 'cache-only']);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.new_agent') }));
    const refreshed = (await screen.findByLabelText(tt('ui.hub.field_model'))) as HTMLSelectElement;
    await waitFor(() => expect(refreshed.options).toHaveLength(3)); // 2 注入 + cache-only，glm 去重
    const merged = Array.from(refreshed.options).map((o) => o.value).filter(Boolean);
    expect(new Set(merged).size).toBe(merged.length);
    expect(merged[0]).toBe('glm-5.3-flash'); // 注入模型在前
    expect(merged).toContain('cache-only');
  });

  it('新建 agent：getModelConfig 失败时静默回退仅默认选项', async () => {
    api.getTools.mockResolvedValue([acpTool()]);
    api.getModelConfig.mockRejectedValue(new Error('no binding'));
    useAppStore.setState({ workspaces: [workspaceA] });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.new_agent') }));
    const select = (await screen.findByLabelText(tt('ui.hub.field_model'))) as HTMLSelectElement;
    await waitFor(() => expect(select.options).toHaveLength(1)); // 仅「默认模型」
    expect(select.value).toBe('');
  });

  it('新建 agent：单窗体含项目/工具/模型三字段，选模型创建后自动切换', async () => {
    api.getTools.mockResolvedValue([acpTool()]);
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
    useAppStore.setState({ workspaces: [workspaceA] });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.new_agent') }));
    // 三字段同屏
    expect(await screen.findByLabelText(tt('ui.hub.field_project'))).toBeTruthy();
    expect(screen.getByLabelText(tt('ui.hub.field_tool'))).toBeTruthy();
    expect(screen.getByLabelText(tt('ui.hub.field_model'))).toBeTruthy();
    // 无本地缓存时只有「默认模型」空选项
    const modelSelect = screen.getByLabelText(tt('ui.hub.field_model')) as HTMLSelectElement;
    expect(modelSelect.value).toBe('');
    expect(modelSelect.options).toHaveLength(1);
    // 缓存了模型列表后重开：模型下拉出现缓存选项（渲染期直接读缓存，重开即刷新）
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.cancel') }));
    saveToolModels('claude', ['m1', 'm2']);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.new_agent') }));
    const refreshed = (await screen.findByLabelText(tt('ui.hub.field_model'))) as HTMLSelectElement;
    await waitFor(() => expect(refreshed.options).toHaveLength(2));
    // 无 lastCreate.modelId 预选：modelId 状态仍为空串（建会话后不触发 SetModel）；
    // 注意有缓存时下拉无空选项，浏览器显示值会回落到第一项，属既有展示行为
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.create') }));
    await waitFor(() => expect(api.openWorkspaceACP).toHaveBeenCalledWith('D:\\ws\\a', 'claude'));
    await waitFor(() => expect(api.setChatModel).not.toHaveBeenCalled());
    expect(useAppStore.getState().chats.some((c) => c.ID === 'n1')).toBe(true);
  });

  it('新建 agent：选择非当前模型时创建后自动 SetModel', async () => {
    api.getTools.mockResolvedValue([acpTool()]);
    api.openWorkspaceACP.mockResolvedValue({
      Kind: 'chat',
      Chat: chat({ ID: 'n4', Workspace: 'D:\\ws\\a', ToolID: 'claude' }),
    });
    api.listChatModels.mockResolvedValue({
      Current: 'm1',
      Models: [
        { ID: 'm1', Name: 'Model One' },
        { ID: 'm2', Name: 'Model Two' },
      ],
    });
    saveToolModels('claude', ['m1', 'm2']);
    useAppStore.setState({ workspaces: [workspaceA] });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.new_agent') }));
    fireEvent.change(await screen.findByLabelText(tt('ui.hub.field_project')), { target: { value: 'D:\\ws\\a' } });
    fireEvent.change(screen.getByLabelText(tt('ui.hub.field_model')), { target: { value: 'm2' } });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.create') }));
    await waitFor(() => expect(api.setChatModel).toHaveBeenCalledWith('n4', 'm2'));
    expect(useAppStore.getState().chats.some((c) => c.ID === 'n4')).toBe(true);
  });

  it('新建 agent：打开时按 lastCreate 预填，创建成功后保存默认值与模型缓存', async () => {
    api.getTools.mockResolvedValue([acpTool(), { ...acpTool('codex'), Name: 'Codex' }]);
    api.openWorkspaceACP.mockResolvedValue({
      Kind: 'chat',
      Chat: chat({ ID: 'n5', Workspace: 'D:\\ws\\a', ToolID: 'claude' }),
    });
    api.listChatModels.mockResolvedValue({ Current: '', Models: [] });
    saveLastCreate({ wsPath: 'D:\\ws\\a', toolId: 'claude', modelId: 'm9' });
    useAppStore.setState({ workspaces: [workspaceA] });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.new_agent') }));
    // 打开即预填 lastCreate 的项目与工具
    await waitFor(() => {
      expect((screen.getByLabelText(tt('ui.hub.field_project')) as HTMLSelectElement).value).toBe('D:\\ws\\a');
      expect((screen.getByLabelText(tt('ui.hub.field_tool')) as HTMLSelectElement).value).toBe('claude');
    });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.create') }));
    await waitFor(() => expect(api.openWorkspaceACP).toHaveBeenCalledWith('D:\\ws\\a', 'claude'));
    // listChatModels 拉到的模型列表落缓存，供下次预填
    await waitFor(() => expect(useAppStore.getState().chats.some((c) => c.ID === 'n5')).toBe(true));
  });

  it('新建 agent：lastCreate 的项目已不在工作区列表时不预填', async () => {
    api.getTools.mockResolvedValue([acpTool()]);
    saveLastCreate({ wsPath: 'D:\\ws\\gone', toolId: 'claude', modelId: null });
    useAppStore.setState({ workspaces: [workspaceA] });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.new_agent') }));
    await waitFor(() => {
      expect((screen.getByLabelText(tt('ui.hub.field_project')) as HTMLSelectElement).value).toBe('D:\\ws\\a');
    });
  });

  it('默认设置：齿轮按钮打开对话框，选工具保存后写入 defaultCreate', async () => {
    api.getTools.mockResolvedValue([acpTool(), { ...acpTool('codex'), Name: 'Codex' }]);
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.default_settings') }));
    expect(await screen.findByText(tt('ui.hub.default_settings_title'))).toBeTruthy();
    fireEvent.change(screen.getByLabelText(tt('ui.hub.field_tool')), { target: { value: 'claude' } });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.save') }));
    expect(loadDefaultCreate()).toEqual({ toolId: 'claude', modelId: null });
    // 保存后对话框关闭
    expect(screen.queryByText(tt('ui.hub.default_settings_title'))).toBeNull();
  });

  it('默认设置：打开时按已存设置预填工具与模型', async () => {
    api.getTools.mockResolvedValue([acpTool()]);
    saveDefaultCreate({ toolId: 'claude', modelId: 'm1' });
    saveToolModels('claude', ['m1', 'm2']);
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.default_settings') }));
    await waitFor(() => {
      expect((screen.getByLabelText(tt('ui.hub.field_tool')) as HTMLSelectElement).value).toBe('claude');
      expect((screen.getByLabelText(tt('ui.hub.field_model')) as HTMLSelectElement).value).toBe('m1');
    });
  });

  it('默认设置：无已存设置时打开为空选择，工具未选前保存不可用', async () => {
    api.getTools.mockResolvedValue([acpTool()]);
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.default_settings') }));
    await waitFor(() => expect(screen.getByText(tt('ui.hub.default_settings_title'))).toBeTruthy());
    // 无已存设置时 toolId 状态为空（真实浏览器中 select 显示空白；jsdom 会归一化到首项，
    // 故以「保存按钮禁用」断言空选择语义而非 select.value）
    expect(screen.getByRole('button', { name: tt('ui.hub.save') })).toHaveAttribute('disabled');
    // 模型空值即「默认模型」空选项
    expect((screen.getByLabelText(tt('ui.hub.field_model')) as HTMLSelectElement).value).toBe('');
    // 选上工具后保存变为可用
    fireEvent.change(screen.getByLabelText(tt('ui.hub.field_tool')), { target: { value: 'claude' } });
    expect(screen.getByRole('button', { name: tt('ui.hub.save') })).not.toHaveAttribute('disabled');
  });

  it('busy 创建中：显示加载覆盖层与 loading 按钮，防重复点击', async () => {
    api.getTools.mockResolvedValue([acpTool()]);
    api.openWorkspaceACP.mockReturnValue(new Promise(() => {})); // 挂起，保持 busy
    useAppStore.setState({ workspaces: [workspaceA] });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.new_agent') }));
    fireEvent.change(await screen.findByLabelText(tt('ui.hub.field_project')), { target: { value: 'D:\\ws\\a' } });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.create') }));
    // busy 期间：spinner 文案出现，创建按钮 disabled 且变成 loading 文案
    expect(await screen.findByText(tt('ui.hub.creating'))).toBeTruthy();
    const busyBtn = screen.getByRole('button', { name: tt('ui.hub.creating') });
    expect(busyBtn).toHaveAttribute('disabled');
    expect(screen.queryByRole('button', { name: tt('ui.hub.create') })).toBeNull();
  });

  it('#easynew：有有效默认值时不弹窗直接创建，创建结束即清提示', async () => {
    api.getTools.mockResolvedValue([acpTool()]);
    // 挂起建会话：观察提示出现 → 创建结束（resolve）→ 提示立即清除（不等问题欢迎词）
    let resolveOpen: (v: unknown) => void = () => {};
    api.openWorkspaceACP.mockReturnValue(
      new Promise((res) => {
        resolveOpen = res;
      }),
    );
    api.listChatModels.mockResolvedValue({ Current: '', Models: [] });
    saveLastCreate({ wsPath: 'D:\\ws\\a', toolId: 'claude', modelId: null });
    useAppStore.setState({ workspaces: [workspaceA] });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#easynew' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    // 不弹窗，创建进行中显示提示
    expect(screen.queryByLabelText(tt('ui.hub.field_project'))).toBeNull();
    expect(await screen.findByText(tt('ui.hub.easynew_running'))).toBeTruthy();
    resolveOpen({ Kind: 'chat', Chat: chat({ ID: 'n6', Workspace: 'D:\\ws\\a', ToolID: 'claude' }) });
    // 创建结束提示立即清除（spinner 使命已结束，欢迎词随后由后端写入）
    await waitFor(() => expect(screen.queryByText(tt('ui.hub.easynew_running'))).toBeNull());
    await waitFor(() => expect(useAppStore.getState().chats.some((c) => c.ID === 'n6')).toBe(true));
  });

  it('#easynew：默认值失效时打开新建弹窗让用户补选', async () => {
    api.getTools.mockResolvedValue([acpTool()]);
    saveLastCreate({ wsPath: 'D:\\ws\\gone', toolId: 'gone-tool', modelId: null });
    useAppStore.setState({ workspaces: [workspaceA] });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#easynew' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    // 弹窗打开，不直接创建
    expect(await screen.findByLabelText(tt('ui.hub.field_project'))).toBeTruthy();
    expect(api.openWorkspaceACP).not.toHaveBeenCalled();
  });

  it('键入 # 展示客户端命令，并可按前缀过滤', () => {
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#' } });
    const list = screen.getByRole('listbox', { name: tt('ui.hub.cmd_aria') });
    expect(list).toHaveTextContent('#new');
    expect(list).toHaveTextContent('#stop');
    expect(list).toHaveTextContent('#model');
    expect(list).toHaveTextContent('#clear');
    expect(list).toHaveTextContent('#easynew');
    fireEvent.change(box, { target: { value: '#cl' } });
    const filtered = screen.getByRole('listbox', { name: tt('ui.hub.cmd_aria') });
    expect(filtered).toHaveTextContent('#clear');
    expect(filtered).not.toHaveTextContent('#new');
  });

  it('/ 补全不再混入客户端命令（/ 前缀只留给 agent 技能）', async () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '/' } });
    await waitFor(() => expect(api.listChatCommands).toHaveBeenCalledWith('c1'));
    // 无 agent 技能时 slash 模式无候选，补全面板不出现（client 命令已迁到 # 模式）
    expect(screen.queryByRole('listbox', { name: tt('ui.hub.cmd_aria') })).toBeNull();
  });

  it('选中 #clear：清空本地 Hub 消息流与 agent 通知，且不投递后端', async () => {
    useAppStore.setState({
      chats: [chat({ ID: 'c1' })],
      lastAgentId: 'c1',
      agentNotices: [{ id: 1, tool: 'codex', event: 'Stop', termKey: 'k', workspace: 'D:\\ws\\a', summary: '已完成' }],
      hubMessages: [{ id: 1, kind: 'agent', tool: 'codex', event: 'Stop', text: '旧消息', ts: 1 }],
    });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#clear' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByText('旧消息')).toBeNull());
    expect(useAppStore.getState().agentNotices).toHaveLength(0);
    expect(useAppStore.getState().hubMessages).toHaveLength(0);
  });

  it('agent 技能与客户端命令同名时不去重也不冲突（/ 只列 agent 技能）', async () => {
    api.listChatCommands.mockResolvedValue([{ Name: 'Clear', Description: 'agent clear' }]);
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '/' } });
    await waitFor(() => expect(api.listChatCommands).toHaveBeenCalledWith('c1'));
    // slash 模式只列 agent 技能：agent 的 Clear 原样出现（不再被客户端命令过滤）
    await waitFor(() => {
      const list = screen.getByRole('listbox', { name: tt('ui.hub.cmd_aria') });
      expect(within(list).getAllByText(/^\/clear$/i)).toHaveLength(1);
    });
  });

  it('选中 #stop：对目标 agent 调 cancelChat', async () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#stop' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.cancelChat).toHaveBeenCalledWith('c1'));
  });

  it('#stop 无目标时提示 need_target_drag', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: null });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#stop' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(api.cancelChat).not.toHaveBeenCalled();
    expect(useAppStore.getState().toasts.some((t) => t.title === tt('ui.hub.need_target_drag'))).toBe(true);
    // 无目标时不消费输入，草稿保留
    expect((box as HTMLTextAreaElement).value).toBe('#stop');
  });

  it('发送未知 # 命令：提示 unknown_cmd，保留草稿，不投递', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#nope' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
    expect(
      useAppStore
        .getState()
        .toasts.some((t) => t.title === tt('ui.hub.unknown_cmd').replace('{{cmd}}', '#nope')),
    ).toBe(true);
    expect((box as HTMLTextAreaElement).value).toBe('#nope');
  });

  it('发送带 @ 提及及行中 # 词：整体透传给 agent（仅 trim 后 # 开头才视为命令）', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '@Claude Code#c1 #nope rest' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', '@Claude Code#c1 #nope rest');
    expect(
      useAppStore
        .getState()
        .toasts.some((t) => t.title === tt('ui.hub.unknown_cmd').replace('{{cmd}}', '#nope')),
    ).toBe(false);
  });

  it('行中 # 词（颜色值）不拦截：正常投递给 agent', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '把按钮颜色改成 #333' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', '把按钮颜色改成 #333');
    expect(
      useAppStore
        .getState()
        .toasts.some((t) => t.title === tt('ui.hub.unknown_cmd').replace('{{cmd}}', '#333')),
    ).toBe(false);
  });

  it('发送 #stop（带尾空格）：单 token 已知命令直接执行 cancelChat，不投递', async () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#stop ' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.cancelChat).toHaveBeenCalledWith('c1'));
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
  });

  it('hash 补全大小写不敏感过滤（#ST 只匹配 stop）', () => {
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#ST' } });
    const list = screen.getByRole('listbox', { name: tt('ui.hub.cmd_aria') });
    expect(list).toHaveTextContent('#stop');
    expect(list).not.toHaveTextContent('#new');
  });

  it('hash 补全键盘导航：# → ArrowDown×2 选中 stop，Enter 执行', async () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#' } });
    fireEvent.keyDown(box, { key: 'ArrowDown' });
    fireEvent.keyDown(box, { key: 'ArrowDown' });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.cancelChat).toHaveBeenCalledWith('c1'));
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
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
    fireEvent.keyDown(box, { key: 'Enter' });
    expect((box as HTMLTextAreaElement).value).toBe('/commit ');
    fireEvent.change(box, { target: { value: '/commit fix bug' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', '/commit fix bug'));
  });

  it('/ 开头但不匹配任何 agent 技能：不拦截，原样发送给 agent', async () => {
    api.listChatCommands.mockResolvedValue([]);
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '/agent-only-cmd arg' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', '/agent-only-cmd arg'));
  });

  it('新建 agent：agent 无模型报告时直接登记', async () => {
    api.getTools.mockResolvedValue([acpTool()]);
    api.openWorkspaceACP.mockResolvedValue({
      Kind: 'chat',
      Chat: chat({ ID: 'n2', Workspace: 'D:\\ws\\a', ToolID: 'claude' }),
    });
    api.listChatModels.mockResolvedValue({ Current: '', Models: [] });
    useAppStore.setState({ workspaces: [workspaceA] });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.new_agent') }));
    fireEvent.change(await screen.findByLabelText(tt('ui.hub.field_project')), { target: { value: 'D:\\ws\\a' } });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.create') }));
    await waitFor(() => expect(useAppStore.getState().chats.some((c) => c.ID === 'n2')).toBe(true));
    expect(api.setChatModel).not.toHaveBeenCalled();
  });

  it('新建期间取消：会话仍登记，再次打开回第一步', async () => {
    api.getTools.mockResolvedValue([acpTool()]);
    // 让 openWorkspaceACP 挂起，模拟建会话期间的关闭
    let resolveOpen: (v: unknown) => void = () => {};
    api.openWorkspaceACP.mockReturnValue(
      new Promise((res) => {
        resolveOpen = res;
      }),
    );
    useAppStore.setState({ workspaces: [workspaceA] });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.new_agent') }));
    fireEvent.change(await screen.findByLabelText(tt('ui.hub.field_project')), { target: { value: 'D:\\ws\\a' } });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.create') }));
    // 关闭对话框（Cancel），随后在途的 openWorkspaceACP 才 resolve
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.cancel') }));
    resolveOpen({ Kind: 'chat', Chat: chat({ ID: 'n3', Workspace: 'D:\\ws\\a', ToolID: 'claude' }) });
    await waitFor(() => expect(useAppStore.getState().chats.some((c) => c.ID === 'n3')).toBe(true));
    // 再次打开应回到第一步（表单字段仍在）
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.new_agent') }));
    expect(await screen.findByLabelText(tt('ui.hub.field_project'))).toBeTruthy();
  });

  it('agent 卡片快捷回复常显：仅「继续」「换个说法」，无「好的」，且无需 hover', async () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1', Key: 'new:1', ToolID: 'claude' })] });
    api.listHubMessages.mockResolvedValue([
      { id: 2, kind: 'agent', tool: 'claude', event: 'Stop', chatId: 'c1', text: 'done', ts: 2 },
    ]);
    render(<AgentHubPanel />);
    expect(await screen.findByRole('button', { name: tt('ui.hub.quick_continue') })).toBeTruthy();
    expect(screen.getByRole('button', { name: tt('ui.hub.quick_rephrase') })).toBeTruthy();
    // quick_ok 死 key 已删，无法走 tt；按字面文案断言不存在「好的」按钮
    expect(screen.queryByRole('button', { name: /^(好的|Sounds good)$/ })).toBeNull();
    // 容器常显：不再依赖 group-hover（hidden/absolute 移除）
    const rephraseBtn = screen.getByRole('button', { name: tt('ui.hub.quick_rephrase') });
    const container = rephraseBtn.parentElement as HTMLElement;
    expect(container.className).toContain('flex');
    expect(container.className).not.toContain('hidden');
  });

  it('agent 卡片点「继续」：直接发送 quick_continue_text 到归因 agent', async () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1', Key: 'new:1', ToolID: 'claude' })] });
    api.listHubMessages.mockResolvedValue([
      { id: 2, kind: 'agent', tool: 'claude', event: 'Stop', chatId: 'c1', text: 'done', ts: 2 },
    ]);
    render(<AgentHubPanel />);
    const btn = await screen.findByRole('button', { name: tt('ui.hub.quick_continue') });
    fireEvent.click(btn);
    // 发送的是 quick_continue_text（给 agent 的实际文本），而非按钮显示文案 quick_continue
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', tt('ui.hub.quick_continue_text')));
    expect(useAppStore.getState().lastAgentId).toBe('c1');
  });

  it('agent 卡片点「换个说法」：@ 目标填入输入框并聚焦，不发送', async () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1', ToolID: 'claude', Title: '重构认证' })] });
    api.listHubMessages.mockResolvedValue([
      { id: 2, kind: 'agent', tool: 'claude', event: 'Stop', chatId: 'c1', text: 'done', ts: 2 },
    ]);
    render(<AgentHubPanel />);
    const btn = await screen.findByRole('button', { name: tt('ui.hub.quick_rephrase') });
    fireEvent.click(btn);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    expect((box as HTMLTextAreaElement).value).toBe('@Claude Code#c1 ');
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(box);
  });

  it('agent 卡片点「换个说法」：保留既有草稿，追加 @ 目标并聚焦', async () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1', ToolID: 'claude', Title: '重构认证' })] });
    api.listHubMessages.mockResolvedValue([
      { id: 2, kind: 'agent', tool: 'claude', event: 'Stop', chatId: 'c1', text: 'done', ts: 2 },
    ]);
    render(<AgentHubPanel />);
    const btn = await screen.findByRole('button', { name: tt('ui.hub.quick_rephrase') });
    // 先在输入框写好草稿，再点「换个说法」：原有内容必须保留
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '帮我看看' } });
    fireEvent.click(btn);
    expect((box as HTMLTextAreaElement).value).toBe('帮我看看 @Claude Code#c1 ');
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(box);
  });

  it('agent 卡片缺 chatId 时按 termKey 反查 chat ID', async () => {
    useAppStore.setState({ chats: [chat({ ID: 'c2', Key: 'new:7', ToolID: 'cursor' })] });
    api.listHubMessages.mockResolvedValue([
      { id: 3, kind: 'agent', tool: 'cursor', event: 'Stop', termKey: 'new:7', text: 'ok', ts: 3 },
    ]);
    render(<AgentHubPanel />);
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.hub.quick_continue') }));
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c2', tt('ui.hub.quick_continue_text')));
  });

  it('「继续」目标未激活时先激活再发送（termKey session: 前缀 resume）', async () => {
    // 记录的 chatId 不在 chats（会话已关闭/重启后未恢复）：走激活路径
    api.listHubMessages.mockResolvedValue([
      { id: 5, kind: 'agent', tool: 'claude', event: 'Stop', chatId: 'c-dead', termKey: 'session:s-9', text: 'ok', ts: 5 },
    ]);
    api.openSessionACP.mockResolvedValue({
      Kind: 'chat',
      Chat: chat({ ID: 'c-new', Key: 'session:s-9', Kind: 'session', SessionID: 's-9' }),
    });
    render(<AgentHubPanel />);
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.hub.quick_continue') }));
    await waitFor(() => expect(api.openSessionACP).toHaveBeenCalledWith('s-9'));
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c-new', tt('ui.hub.quick_continue_text')));
    // 激活后的会话登记进 store，后续消息可直达
    expect(useAppStore.getState().chats.some((c) => c.ID === 'c-new')).toBe(true);
  });

  it('「继续」目标未激活时优先用 sessionId 快照激活', async () => {
    api.listHubMessages.mockResolvedValue([
      { id: 6, kind: 'agent', tool: 'claude', event: 'Stop', chatId: 'c-dead', sessionId: 's-snap', text: 'ok', ts: 6 },
    ]);
    api.openSessionACP.mockResolvedValue({
      Kind: 'chat',
      Chat: chat({ ID: 'c-new2', Key: 'session:s-snap', Kind: 'session', SessionID: 's-snap' }),
    });
    render(<AgentHubPanel />);
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.hub.quick_continue') }));
    await waitFor(() => expect(api.openSessionACP).toHaveBeenCalledWith('s-snap'));
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c-new2', tt('ui.hub.quick_continue_text')));
  });

  it('「继续」无 sessionId/termKey 前缀时按 workspace+tool+标题匹配发现会话激活', async () => {
    // 存量记录（旧格式，无 sessionId；termKey 为 new:N 无磁盘会话 ID）
    api.listHubMessages.mockResolvedValue([
      { id: 7, kind: 'agent', tool: 'claude', event: 'Stop', chatId: 'c-dead', termKey: 'new:2', workspace: 'D:\\ws\\a', sessionTitle: '重构认证', text: 'ok', ts: 7 },
    ]);
    api.getSessions.mockResolvedValue([
      { ID: 's-legacy', ToolID: 'claude', Workspace: 'D:\\ws\\a', Title: '重构认证', CreatedAt: '', UpdatedAt: '', Messages: 3, Path: '' },
      { ID: 's-other', ToolID: 'claude', Workspace: 'D:\\ws\\a', Title: '别的会话', CreatedAt: '', UpdatedAt: '', Messages: 1, Path: '' },
    ]);
    api.openSessionACP.mockResolvedValue({
      Kind: 'chat',
      Chat: chat({ ID: 'c-legacy', Key: 'session:s-legacy', Kind: 'session', SessionID: 's-legacy' }),
    });
    render(<AgentHubPanel />);
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.hub.quick_continue') }));
    await waitFor(() => expect(api.openSessionACP).toHaveBeenCalledWith('s-legacy'));
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c-legacy', tt('ui.hub.quick_continue_text')));
  });

  it('「继续」目标未激活且无法恢复会话时提示 need_target 且不发送', async () => {
    api.listHubMessages.mockResolvedValue([
      { id: 8, kind: 'agent', tool: 'codex', event: 'Stop', text: 'x', ts: 8 },
    ]);
    render(<AgentHubPanel />);
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.hub.quick_continue') }));
    await waitFor(() =>
      expect(useAppStore.getState().toasts.some((t) => t.title === tt('ui.hub.need_target'))).toBe(true),
    );
    expect(api.openSessionACP).not.toHaveBeenCalled();
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
  });

  it('「继续」激活失败时报 find_activate_failed 且不发送', async () => {
    api.listHubMessages.mockResolvedValue([
      { id: 9, kind: 'agent', tool: 'claude', event: 'Stop', termKey: 'session:s-err', text: 'ok', ts: 9 },
    ]);
    api.openSessionACP.mockRejectedValue(new Error('boom'));
    render(<AgentHubPanel />);
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.hub.quick_continue') }));
    await waitFor(() =>
      expect(
        useAppStore
          .getState()
          .toasts.some((t) => t.title === tt('ui.hub.find_activate_failed').replace('{{err}}', 'boom')),
      ).toBe(true),
    );
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
  });

  it('快速回复无目标 agent 时提示 need_target 且不发送', async () => {
    api.listHubMessages.mockResolvedValue([
      { id: 4, kind: 'agent', tool: 'codex', event: 'Stop', text: 'x', ts: 4 },
    ]);
    render(<AgentHubPanel />);
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.hub.quick_rephrase') }));
    await waitFor(() =>
      expect(useAppStore.getState().toasts.some((t) => t.title === tt('ui.hub.need_target'))).toBe(true),
    );
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
  });

  it('queued 用户消息撤回按钮直接可见（无需 hover），点击后乐观标记已撤回', async () => {
    // 经 listHubMessages 回流（挂载 effect 会用拉取结果覆盖 store，直塞 state 会被清掉）
    api.listHubMessages.mockResolvedValue([
      { id: 7, kind: 'user', chatId: 'c1', text: '排队消息', status: 'queued', ts: 1 },
    ]);
    render(<AgentHubPanel />);
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.hub.recall') }));
    await waitFor(() => expect(api.recallHubMessage).toHaveBeenCalledWith(7));
    await waitFor(() => expect(useAppStore.getState().hubMessages[0].status).toBe('recalled'));
    expect(screen.getByText(tt('ui.hub.recalled'))).toBeTruthy();
  });

  it('recalled 用户气泡灰显并带已撤回标签，原文保留', () => {
    useAppStore.setState({
      hubMessages: [{ id: 7, kind: 'user', chatId: 'c1', text: '旧消息', status: 'recalled', ts: 1 }],
    });
    render(<AgentHubPanel />);
    expect(screen.getByText(tt('ui.hub.recalled'))).toBeTruthy();
    expect(screen.getByText('旧消息')).toBeTruthy();
  });

  it('ack wire key 经 translateBackend 翻译展示', async () => {
    api.listHubMessages.mockResolvedValue([
      { id: 3, kind: 'agent', tool: 'claude', event: 'ui.hub.ack_queued', chatId: 'c1', text: 'ui.hub.ack_queued', ts: 3 },
    ]);
    render(<AgentHubPanel />);
    expect(await screen.findByText(tt('ui.hub.ack_queued'))).toBeTruthy();
  });

  // ---- easynew 默认设置接入 ----

  it('#easynew：默认设置的工具/模型优先于 lastCreate，项目取选中项目', async () => {
    api.getTools.mockResolvedValue([acpTool(), { ...acpTool('codex'), Name: 'Codex' }]);
    api.openWorkspaceACP.mockResolvedValue({
      Kind: 'chat',
      Chat: chat({ ID: 'n7', Workspace: 'D:\\ws\\a', ToolID: 'codex' }),
    });
    api.listChatModels.mockResolvedValue({ Current: '', Models: [] });
    saveDefaultCreate({ toolId: 'codex', modelId: 'm2' });
    saveLastCreate({ wsPath: 'D:\\ws\\a', toolId: 'claude', modelId: 'm1' });
    useAppStore.setState({ workspaces: [workspaceA], selectedProject: 'D:\\ws\\a' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#easynew' } });
    fireEvent.keyDown(box, { key: 'Enter' });

    await waitFor(() => expect(api.openWorkspaceACP).toHaveBeenCalledWith('D:\\ws\\a', 'codex'));
    // 默认设置的模型在创建后生效
    await waitFor(() => expect(api.setChatModel).toHaveBeenCalledWith('n7', 'm2'));
  });

  it('#easynew：无选中项目时回落 lastCreate.wsPath', async () => {
    api.getTools.mockResolvedValue([acpTool()]);
    api.openWorkspaceACP.mockResolvedValue({
      Kind: 'chat',
      Chat: chat({ ID: 'n8', Workspace: 'D:\\ws\\a', ToolID: 'claude' }),
    });
    api.listChatModels.mockResolvedValue({ Current: '', Models: [] });
    saveLastCreate({ wsPath: 'D:\\ws\\a', toolId: 'claude', modelId: null });
    useAppStore.setState({ workspaces: [workspaceA] });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#easynew' } });
    fireEvent.keyDown(box, { key: 'Enter' });

    await waitFor(() => expect(api.openWorkspaceACP).toHaveBeenCalledWith('D:\\ws\\a', 'claude'));
  });

  it('#easynew：默认设置工具不可用时提示回落并沿用 lastCreate 工具', async () => {
    api.getTools.mockResolvedValue([acpTool()]); // 只有 claude 可用，默认设置指向 gone-tool
    api.openWorkspaceACP.mockResolvedValue({
      Kind: 'chat',
      Chat: chat({ ID: 'n9', Workspace: 'D:\\ws\\a', ToolID: 'claude' }),
    });
    api.listChatModels.mockResolvedValue({ Current: '', Models: [] });
    saveDefaultCreate({ toolId: 'gone-tool', modelId: null });
    saveLastCreate({ wsPath: 'D:\\ws\\a', toolId: 'claude', modelId: null });
    useAppStore.setState({ workspaces: [workspaceA], selectedProject: 'D:\\ws\\a' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#easynew' } });
    fireEvent.keyDown(box, { key: 'Enter' });

    await waitFor(() => expect(api.openWorkspaceACP).toHaveBeenCalledWith('D:\\ws\\a', 'claude'));
    expect(
      useAppStore.getState().toasts.some((x) => x.title === tt('ui.hub.default_tool_unavailable')),
    ).toBe(true);
  });

  // ---- 面板级需求拖放派发 ----

  // 构造含 REQUIREMENT_MIME 的 dataTransfer（types 只读数组，jsdom 需手工模拟）
  function requirementDt(payload: object) {
    return {
      types: [REQUIREMENT_MIME],
      data: { [REQUIREMENT_MIME]: JSON.stringify(payload) } as Record<string, string>,
      getData(k: string) {
        return this.data[k] ?? '';
      },
      dropEffect: '',
      preventDefault: vi.fn(),
    };
  }

  it('面板 drop 需求条目：调 dispatchRequirement(id, text, project, 默认设置) 并提示成功', async () => {
    saveDefaultCreate({ toolId: 'claude', modelId: 'm1' });
    render(<AgentHubPanel />);
    const panel = screen.getByTestId('hub-panel');
    fireEvent.dragOver(panel, { dataTransfer: requirementDt({ id: 'r1', text: '需求文本', project: 'D:\\ws\\a' }) });
    // dragOver 校验 MIME 后允许 drop 且面板高亮
    expect(panel.className).toContain('ring-2');
    fireEvent.drop(panel, { dataTransfer: requirementDt({ id: 'r1', text: '需求文本', project: 'D:\\ws\\a' }) });

    await waitFor(() =>
      expect(dispatchRequirement).toHaveBeenCalledWith('r1', '需求文本', 'D:\\ws\\a', { toolId: 'claude', modelId: 'm1' }),
    );
    await waitFor(() =>
      expect(
        useAppStore.getState().toasts.some((x) => x.title.includes(tt('ui.requirements.dispatch_ok').replace('{{name}}', 'a'))),
      ).toBe(true),
    );
    // drop 后高亮取消
    expect(panel.className).not.toContain('ring-2');
  });

  it('面板 drop 需求条目：无默认设置时 dispatchRequirement 收到 undefined', async () => {
    render(<AgentHubPanel />);
    const panel = screen.getByTestId('hub-panel');
    fireEvent.drop(panel, { dataTransfer: requirementDt({ id: 'r2', text: '文本', project: 'D:\\ws\\a' }) });

    await waitFor(() =>
      expect(dispatchRequirement).toHaveBeenCalledWith('r2', '文本', 'D:\\ws\\a', undefined),
    );
  });

  it('面板 drop 需求条目缺 project：提示派发失败且不调 dispatchRequirement', async () => {
    render(<AgentHubPanel />);
    const panel = screen.getByTestId('hub-panel');
    fireEvent.drop(panel, { dataTransfer: requirementDt({ id: 'r3', text: '无主文本' }) });

    expect(dispatchRequirement).not.toHaveBeenCalled();
    // 失败提示的 err 插值为「补选项目」提示文案
    const expected = tt('ui.requirements.dispatch_failed').replace(
      '{{err}}',
      tt('ui.requirements.no_project_drag_hint'),
    );
    await waitFor(() =>
      expect(useAppStore.getState().toasts.some((x) => x.title === expected)).toBe(true),
    );
  });

  it('面板 drop 派发失败：提示 dispatch_failed', async () => {
    dispatchRequirement.mockRejectedValue(new Error('boom'));
    render(<AgentHubPanel />);
    const panel = screen.getByTestId('hub-panel');
    fireEvent.drop(panel, { dataTransfer: requirementDt({ id: 'r4', text: '文本', project: 'D:\\ws\\a' }) });

    await waitFor(() =>
      expect(
        useAppStore.getState().toasts.some((x) =>
          x.title.includes(tt('ui.requirements.dispatch_failed').replace('{{err}}', 'boom')),
        ),
      ).toBe(true),
    );
  });

  it('面板 drop 在途守卫命中：dispatchRequirement 返回 busy 时提示派发中、不提示成功', async () => {
    // 守卫本体在 lib 单测覆盖；这里断言面板消费 busy 结果的呈现
    dispatchRequirement.mockResolvedValue('busy');
    render(<AgentHubPanel />);
    const panel = screen.getByTestId('hub-panel');
    fireEvent.drop(panel, { dataTransfer: requirementDt({ id: 'r5', text: '文本', project: 'D:\\ws\\a' }) });

    await waitFor(() =>
      expect(
        useAppStore.getState().toasts.some((x) => x.title.includes(tt('ui.requirements.dispatch_busy'))),
      ).toBe(true),
    );
    expect(
      useAppStore.getState().toasts.some((x) => x.title.includes(tt('ui.requirements.dispatch_ok'))),
    ).toBe(false);
  });

  it('面板 drop 同一需求连拖两次：两次 drop 都交给 dispatchRequirement（lib 层守卫兜底互斥）', async () => {
    // 第一次挂起制造在途窗口，期间第二次 drop 到达
    let release!: () => void;
    dispatchRequirement.mockImplementation(() => {
      if (dispatchRequirement.mock.calls.length === 1) {
        return new Promise<'ok'>((resolve) => {
          release = () => resolve('ok');
        });
      }
      return Promise.resolve('busy');
    });
    render(<AgentHubPanel />);
    const panel = screen.getByTestId('hub-panel');
    const dt = requirementDt({ id: 'r6', text: '文本', project: 'D:\\ws\\a' });
    fireEvent.drop(panel, { dataTransfer: dt });
    fireEvent.drop(panel, { dataTransfer: dt });

    await waitFor(() => expect(dispatchRequirement).toHaveBeenCalledTimes(2));
    expect(dispatchRequirement).toHaveBeenNthCalledWith(1, 'r6', '文本', 'D:\\ws\\a', undefined);
    expect(dispatchRequirement).toHaveBeenNthCalledWith(2, 'r6', '文本', 'D:\\ws\\a', undefined);
    // 第二次返回 busy：提示「派发中」而非成功
    await waitFor(() =>
      expect(
        useAppStore.getState().toasts.some((x) => x.title.includes(tt('ui.requirements.dispatch_busy'))),
      ).toBe(true),
    );
    release();
  });

  it('面板 dragOver 非 REQUIREMENT_MIME 拖拽不高亮不允许 drop', () => {
    render(<AgentHubPanel />);
    const panel = screen.getByTestId('hub-panel');
    const dt = { types: ['text/plain'], preventDefault: vi.fn(), dropEffect: '' };
    fireEvent.dragOver(panel, { dataTransfer: dt });
    expect(dt.preventDefault).not.toHaveBeenCalled();
    expect(panel.className).not.toContain('ring-2');
  });

  it('输入框 drop 需求 MIME：不粘进草稿（两套拖放隔离），text/plain 仍正常追加', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    // 需求拖拽：草稿不变
    const reqDt = requirementDt({ id: 'r1', text: '需求文本', project: 'D:\\ws\\a' });
    reqDt.data['text/plain'] = '需求文本'; // 拖拽源同时带 text/plain
    fireEvent.drop(box, { dataTransfer: reqDt });
    expect((box as HTMLTextAreaElement).value).toBe('');
    // 会话标签拖拽：正常追加
    const plainDt = {
      types: ['text/plain'],
      data: { 'text/plain': '@Claude Code#c1' } as Record<string, string>,
      getData(k: string) {
        return this.data[k] ?? '';
      },
    };
    fireEvent.drop(box, { dataTransfer: plainDt });
    expect((box as HTMLTextAreaElement).value).toBe('@Claude Code#c1');
  });

  // ---- 默认设置对话框：清空模型回默认 ----

  it('默认设置：模型下拉恒有头部空选项，可把已设模型清回默认模型', async () => {
    api.getTools.mockResolvedValue([acpTool()]);
    saveDefaultCreate({ toolId: 'claude', modelId: 'm1' });
    saveToolModels('claude', ['m1', 'm2']);
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.default_settings') }));
    const select = (await screen.findByLabelText(tt('ui.hub.field_model'))) as HTMLSelectElement;
    // 头部空选项 + 两个缓存模型
    await waitFor(() => expect(select.options).toHaveLength(3));
    expect(select.options[0].value).toBe('');
    expect(select.options[0].text).toBe(tt('ui.hub.default_model'));
    expect(select.value).toBe('m1');
    // 清回默认模型并保存
    fireEvent.change(select, { target: { value: '' } });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.save') }));
    expect(loadDefaultCreate()).toEqual({ toolId: 'claude', modelId: null });
  });
});

describe('AgentHubPanel #find', () => {
  it('#find 出现在 hash 补全候选', () => {
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#fi' } });
    const list = screen.getByRole('listbox', { name: tt('ui.hub.cmd_aria') });
    expect(list).toHaveTextContent('#find');
    expect(list).toHaveTextContent(tt('ui.hub.cmd_find_desc'));
  });

  it('裸 #find 回车：打开查找对话框（getSessions 被调），草稿清空，不投递', async () => {
    api.getSessions.mockResolvedValue([]);
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#find' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.getSessions).toHaveBeenCalled());
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
    expect((box as HTMLTextAreaElement).value).toBe('');
    // 对话框标题出现即视为打开
    expect(screen.getByText(tt('ui.hub.find_title'))).toBeTruthy();
  });

  it('#find claude 带参数发送：对话框打开且过滤框初值 claude，草稿清空，不透传不报错', async () => {
    api.getSessions.mockResolvedValue([]);
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#find claude' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.getSessions).toHaveBeenCalled());
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
    expect(
      useAppStore.getState().toasts.some((t) => t.title === tt('ui.hub.unknown_cmd').replace('{{cmd}}', '#find')),
    ).toBe(false);
    expect((box as HTMLTextAreaElement).value).toBe('');
    expect(screen.getByPlaceholderText(tt('ui.hub.find_placeholder'))).toHaveValue('claude');
  });

  it('过滤：输入关键词后列表只剩匹配行；无匹配显示空态', async () => {
    api.getSessions.mockResolvedValue([
      {
        ID: 's1',
        ToolID: 'claude',
        Workspace: 'D:\\ws\\alpha',
        Title: '重构认证',
        CreatedAt: '2026-10-16T10:00:00Z',
        UpdatedAt: new Date().toISOString(),
        Messages: 3,
        Path: '',
      },
      {
        ID: 's2',
        ToolID: 'codex',
        Workspace: 'D:\\ws\\beta',
        Title: '写文档',
        CreatedAt: '2026-10-16T09:00:00Z',
        UpdatedAt: '2026-10-16T09:00:00Z',
        Messages: 1,
        Path: '',
      },
    ]);
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#find' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(screen.getByText('重构认证')).toBeTruthy());
    expect(screen.getByText('写文档')).toBeTruthy();
    // 关键词 alpha 只剩项目 alpha 的会话
    const input = screen.getByPlaceholderText(tt('ui.hub.find_placeholder'));
    fireEvent.change(input, { target: { value: 'alpha' } });
    await waitFor(() => expect(screen.queryByText('写文档')).toBeNull());
    expect(screen.getByText('重构认证')).toBeTruthy();
    // 无匹配时空态文案
    fireEvent.change(input, { target: { value: 'zzz没有' } });
    await waitFor(() => expect(screen.getByText(tt('ui.hub.find_empty'))).toBeTruthy());
  });

  it('选中行：openSessionACP 被调、registerChat 生效、draft 追加 @label、对话框关闭', async () => {
    api.getSessions.mockResolvedValue([
      {
        ID: 's1',
        ToolID: 'claude',
        Workspace: 'D:\\ws\\alpha',
        Title: '重构认证',
        CreatedAt: '2026-10-16T10:00:00Z',
        UpdatedAt: new Date().toISOString(),
        Messages: 3,
        Path: '',
      },
    ]);
    api.openSessionACP.mockResolvedValue({
      Kind: 'chat',
      Chat: {
        ID: 'c77',
        Key: 'k77',
        Kind: 'session',
        SessionID: 's1',
        Workspace: 'D:\\ws\\alpha',
        Title: '重构认证',
        ToolID: 'claude',
        Status: 'ready',
        ExitCode: 0,
        Error: '',
        Prompted: true,
      },
    });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#find' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(screen.getByText('重构认证')).toBeTruthy());
    fireEvent.click(screen.getByText('重构认证'));
    await waitFor(() => expect(api.openSessionACP).toHaveBeenCalledWith('s1'));
    // registerChat 生效：upsertChat 进 store + lastAgentId 指向新 chat
    expect(useAppStore.getState().chats.some((c) => c.ID === 'c77')).toBe(true);
    expect(useAppStore.getState().lastAgentId).toBe('c77');
    // draft 追加 @label（label 由 buildMentionOptions 生成：工具展示名#chatID）
    expect((box as HTMLTextAreaElement).value).toBe('@Claude Code#c77 ');
    // 对话框关闭（标题不再渲染）
    await waitFor(() => expect(screen.queryByText(tt('ui.hub.find_title'))).toBeNull());
  });

  it('openSessionACP reject：notify 错误 ui.hub.find_activate_failed，对话框保持', async () => {
    api.getSessions.mockResolvedValue([
      {
        ID: 's1',
        ToolID: 'claude',
        Workspace: 'D:\\ws\\alpha',
        Title: '重构认证',
        CreatedAt: '2026-10-16T10:00:00Z',
        UpdatedAt: new Date().toISOString(),
        Messages: 3,
        Path: '',
      },
    ]);
    api.openSessionACP.mockRejectedValue(new Error('boom'));
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#find' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(screen.getByText('重构认证')).toBeTruthy());
    fireEvent.click(screen.getByText('重构认证'));
    await waitFor(() =>
      expect(
        useAppStore
          .getState()
          .toasts.some((t) => t.title === tt('ui.hub.find_activate_failed').replace('{{err}}', 'boom')),
      ).toBe(true),
    );
    // 对话框不关，草稿不追加
    expect(screen.getByText(tt('ui.hub.find_title'))).toBeTruthy();
    expect((box as HTMLTextAreaElement).value).toBe('');
  });
});
