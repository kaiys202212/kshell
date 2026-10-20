// AgentHubPanel：跨项目汇总运行中的 ACP agent，@ 指定目标发消息、跳转定位、新建 agent、粘贴。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, createEvent, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ChatInfo, TerminalInfo, ToolInfo } from '../lib/api';
import { useAppStore } from '../state/store';
import '../test/i18n';
import { tt } from '../test/i18n';
import { loadDefaultCreate, loadLastCreate, loadToolModelOptions, saveDefaultCreate, saveLastCreate, saveToolModels } from '../lib/agenthubDefaults';
import { REQUIREMENT_MIME } from './RequirementList';
import AgentHubPanel from './AgentHubPanel';

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
  // 默认给可回复工具集：既有快捷按钮/激活用例假定 claude/cursor/codex 均可回复；
  // 只读语义用例自行覆盖 getTools（返回空列表或非 ACP 工具）。
  api.getTools.mockResolvedValue([
    acpTool(),
    { ...acpTool('cursor'), Name: 'Cursor' },
    { ...acpTool('codex'), Name: 'Codex' },
  ]);
  api.onToolsUpdated.mockImplementation(() => () => {});
  api.getModelConfig.mockResolvedValue({
    Enabled: false,
    Platforms: [],
    DefaultPlatform: '',
    ActiveModel: '',
  });
  api.readClipboardPaste.mockResolvedValue({ Text: '', Path: '' });
  api.listChatCommands.mockResolvedValue([]);
  api.listChatModels.mockResolvedValue({ Current: '', Models: [] });
  api.setChatModel.mockResolvedValue(undefined);
  api.cancelChat.mockResolvedValue(undefined);
  api.closeChat.mockResolvedValue(undefined);
  // 回显 store 当前 hubMessages：挂载 effect 会用 listHubMessages() 覆盖 setState
  // 预置值，回显让「预置 + act 刷新（等 tools 加载）」的用例不被清空。
  api.listHubMessages.mockImplementation(async () => useAppStore.getState().hubMessages);
  api.listHubArchive.mockResolvedValue([]);
  api.loadHubArchive.mockResolvedValue([]);
  api.clearHubMessages.mockResolvedValue(undefined);
  api.recallHubMessage.mockResolvedValue(undefined);
  api.hubCommand.mockResolvedValue([]);
  api.hubPickTarget.mockResolvedValue({ Replies: [], Pending: false });
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
    terminals: [],
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

// 终端在册条目工厂：Key 为终端 manager 的 key（session:<id> / new:<n>）
const term = (over: Partial<TerminalInfo> & Pick<TerminalInfo, 'Key'>): TerminalInfo => ({
  ID: 't1',
  Kind: 'session',
  SessionID: '',
  Workspace: 'D:\\ws\\a',
  Title: 'x',
  ToolID: 'claude',
  Status: 'running',
  ExitCode: 0,
  Cols: 80,
  Rows: 24,
  ...over,
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
  it('无会话时 chips 区不渲染空文案（2026-10-20 反馈：留空不提示）', () => {
    useAppStore.setState({ chats: [] });
    render(<AgentHubPanel />);
    expect(screen.getByTestId('hub-session-chips')).toHaveTextContent('');
    expect(screen.queryByText(tt('ui.hub.empty'))).toBeNull();
  });

  it('会话 chips 位于输入区上方（与 textarea 同容器）', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const chips = screen.getByTestId('hub-session-chips');
    const inputArea = chips.closest('[data-testid="hub-input-area"]');
    expect(inputArea).not.toBeNull();
    // 结构意图：chips 区紧邻输入框上方，与 textarea 同属输入区容器
    expect(inputArea?.querySelector('textarea')).not.toBeNull();
  });

  it('有会话时 chips 常驻平铺渲染：无需展开即可见每枚 chip（状态点+标题+工具图标）', () => {
    useAppStore.setState({
      chats: [
        chat({ ID: 'c1', Title: '重构认证', ToolID: 'claude', Status: 'ready' }),
        chat({ ID: 'c2', Title: '乙', ToolID: 'cursor', Status: 'running', Workspace: 'D:\\ws\\b' }),
      ],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    const chips = screen.getByTestId('hub-session-chips');
    // 每个会话一枚 chip，常驻可见（无展开前置步骤）
    expect(within(chips).getByText('重构认证')).toBeTruthy();
    expect(within(chips).getByText('乙')).toBeTruthy();
    // 标题旁用工具图标，悬停仍能看到工具名
    expect(chips.querySelector('[data-tool="claude"]')).toBeTruthy();
    expect(chips.querySelector('[data-tool="cursor"]')).toBeTruthy();
    expect(within(chips).getByTitle('Claude Code')).toBeTruthy();
    expect(within(chips).getByTitle('Cursor')).toBeTruthy();
    // 状态点存在（aria-hidden 装饰，用样式类断言运行中主色点）
    expect(chips.querySelectorAll('.bg-primary').length).toBeGreaterThan(0);
  });

  it('点击 chip 主体：草稿保留式追加 @ 标签并聚焦输入框', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder')) as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: '帮我看看' } });
    const chips = screen.getByTestId('hub-session-chips');
    // chip 主体按钮（aria-current 标注高亮态，名称含标题+工具名）
    const main = within(chips).getByRole('button', { name: /重构认证/ });
    fireEvent.click(main);
    // 尾随空格便于继续输入（与 quickReply rephrase 同语义）
    expect(box.value).toBe('帮我看看 @Claude Code#重构认证 ');
    expect(document.activeElement).toBe(box);
  });

  it('默认接收目标 chip 高亮：最后可归因已激活会话 aria-current=true；草稿含 @ 提及时提及目标高亮', async () => {
    useAppStore.setState({
      chats: [
        chat({ ID: 'c1', Title: '甲', ToolID: 'claude' }),
        chat({ ID: 'c2', Title: '乙', ToolID: 'cursor', Workspace: 'D:\\ws\\b' }),
      ],
      hubMessages: [{ id: 1, kind: 'agent', chatId: 'c1', tool: 'claude', event: 'Stop', text: 'hi', ts: 1 }],
      lastAgentId: 'c2',
    });
    render(<AgentHubPanel />);
    await act(async () => {});
    const chips = screen.getByTestId('hub-session-chips');
    const chipOf = (id: string) =>
      within(chips).getByRole('button', { name: new RegExp(id === 'c1' ? '甲' : '乙') });
    // 无 @：最后可归因 c1 高亮（不用 lastAgentId=c2）
    expect(chipOf('c1')).toHaveAttribute('aria-current', 'true');
    expect(chipOf('c2')).not.toHaveAttribute('aria-current');
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '@Cursor#c2 看看' } });
    expect(chipOf('c2')).toHaveAttribute('aria-current', 'true');
    expect(chipOf('c1')).not.toHaveAttribute('aria-current');
  });

  it('chip 主体带 mention_session 悬停提示（title），点击即 @ 无独立图标按钮', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const main = within(screen.getByTestId('hub-session-chips')).getByRole('button', {
      name: /重构认证/,
    });
    expect(main).toHaveAttribute('title', tt('ui.hub.mention_session'));
  });

  it('点空闲会话的 ×：该 chip 从 chips 区消失；忙碌 chip 无 ×', async () => {
    useAppStore.setState({
      chats: [chat({ ID: 'c1' }), chat({ ID: 'c2', Title: '乙', Status: 'running' })],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    // chips 常驻：无需展开
    const chips = screen.getByTestId('hub-session-chips');
    // 仅空闲的 c1 有关闭按钮（c2 running 无）
    const closes = screen.getAllByRole('button', { name: tt('ui.hub.close_session') });
    expect(closes).toHaveLength(1);
    fireEvent.click(closes[0]);
    // 断言结果而非实现：c1 chip 消失，c2 仍在，chips 区常驻
    await waitFor(() => expect(within(chips).queryByText('重构认证')).toBeNull());
    expect(within(chips).getByText('乙')).toBeTruthy();
    // 后端 Manager.Close 已被调用（幂等），不再调 cancelChat（那只会取消当前轮）
    await waitFor(() => expect(api.closeChat).toHaveBeenCalledWith('c1'));
    expect(api.cancelChat).not.toHaveBeenCalled();
  });

  it('关闭会话失败时提示 close_session_failed 且 chip 不消失', async () => {
    api.closeChat.mockRejectedValue(new Error('boom'));
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.close_session') }));
    await waitFor(() =>
      expect(
        useAppStore
          .getState()
          .toasts.some((t) => t.title === tt('ui.hub.close_session_failed').replace('{{err}}', 'boom')),
      ).toBe(true),
    );
    // 失败不摘除：chip 仍在 chips 区，可重试
    expect(screen.getByTestId('hub-session-chips')).toHaveTextContent('重构认证');
  });

  it('chip 可拖拽：dragStart 写入 @ 标签到 dataTransfer', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const chips = screen.getByTestId('hub-session-chips');
    // chip 容器 div：含标题文本的可拖拽节点
    const chip = within(chips).getByText('重构认证').closest('div[draggable="true"]')!;
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
    fireEvent.dragStart(chip, { dataTransfer: dt });
    expect(dt.getData('text/plain')).toBe('@Claude Code#重构认证');
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

  it('chip 主体点击后草稿追加 @ 标签（原 @ 图标行为并入主体），归档展开不影响 chips', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const chips = screen.getByTestId('hub-session-chips');
    fireEvent.click(within(chips).getByRole('button', { name: /重构认证/ }));
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder')) as HTMLTextAreaElement;
    expect(box.value).toBe('@Claude Code#重构认证 ');
    // 归档展开不影响 chips
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.history') }));
    expect(screen.getByTestId('hub-session-chips')).toBeTruthy();
  });

  it('chip 点跳转图标：打开工作区页签并按 chat.Key 定位会话对话页', () => {
    const openTab = vi.spyOn(useAppStore.getState(), 'openTab');
    const focus = vi.spyOn(useAppStore.getState(), 'requestFocusTerm');
    useAppStore.setState({
      chats: [chat({ ID: 'c1', Key: 'new:1', Workspace: 'D:\\ws\\a' })],
      workspaces: [workspaceA],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.jump_session') }));
    expect(openTab).toHaveBeenCalledWith(workspaceA);
    expect(focus).toHaveBeenCalledWith('new:1');
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
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.jump_session') }));
    expect(openTab).toHaveBeenCalledWith(expect.objectContaining({ Path: 'D:\\ws\\x', Name: 'x' }));
    expect(focus).not.toHaveBeenCalled();
  });

  it('@ 指定目标发送：投递到该 agent 并记录 lastAgentId', async () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1', ToolID: 'claude', Title: '重构认证' })] });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '@Claude Code#c1 帮我看下' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', '帮我看下'));
    expect(useAppStore.getState().lastAgentId).toBe('c1');
    expect((box as HTMLTextAreaElement).value).toBe('');
  });

  it('未 @ 且无最后交互：调用 hubPickTarget 列出会话、清空草稿且不发送', async () => {
    api.hubPickTarget.mockResolvedValue({
      Replies: [{ Text: '未精确匹配到会话——回复编号' }],
      Pending: true,
    });
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: null });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '随便看看' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.hubPickTarget).toHaveBeenCalledWith('随便看看'));
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
    await waitFor(() => expect((box as HTMLTextAreaElement).value).toBe(''));
    await waitFor(() =>
      expect(useAppStore.getState().hubMessages.some((m) => m.id < 0 && m.text === '未精确匹配到会话——回复编号')).toBe(true),
    );
  });

  it('待选列表未能建立（Pending=false）：保留草稿，避免用户正文丢失', async () => {
    api.hubPickTarget.mockResolvedValue({ Replies: [{ Text: '请先完成当前选择' }], Pending: false });
    useAppStore.setState({ chats: [], lastAgentId: null });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '别弄丢我' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.hubPickTarget).toHaveBeenCalledWith('别弄丢我'));
    await waitFor(() =>
      expect(useAppStore.getState().hubMessages.some((m) => m.id < 0 && m.text === '请先完成当前选择')).toBe(true),
    );
    expect((box as HTMLTextAreaElement).value).toBe('别弄丢我');
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
  });

  it('未 @ 但最后可归因为已激活会话：直发该会话', async () => {
    useAppStore.setState({
      chats: [chat({ ID: 'c9' })],
      hubMessages: [{ id: 1, kind: 'user', chatId: 'c9', text: '先前', ts: 1 }],
      lastAgentId: null,
    });
    render(<AgentHubPanel />);
    await act(async () => {});
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '继续' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c9', '继续'));
  });

  it('新命令别名 #l：透传 HubCommand、清空输入框且不投给 agent', async () => {
    api.hubCommand.mockResolvedValue([{ Text: '会话列表' }]);
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#l' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.hubCommand).toHaveBeenCalledWith('#l'));
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
    expect((box as HTMLTextAreaElement).value).toBe('');
    // 回复文本作为系统提示渲染进 hub 消息流
    await waitFor(() =>
      expect(useAppStore.getState().hubMessages.some((m) => m.id < 0 && m.text === '会话列表')).toBe(true),
    );
  });

  it('带参数新命令 #f claude：完整原文透传', async () => {
    api.hubCommand.mockResolvedValue([]);
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#f claude' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.hubCommand).toHaveBeenCalledWith('#f claude'));
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
  });

  it('纯数字输入 3：同样透传 HubCommand（编号切换/菜单选择）', async () => {
    api.hubCommand.mockResolvedValue([{ Text: '已切换' }]);
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '3' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.hubCommand).toHaveBeenCalledWith('3'));
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
  });

  it('旧命令 #new 优先命中，不走 HubCommand', () => {
    useAppStore.setState({ chats: [], workspaces: [workspaceA], selectedProject: 'D:\\ws\\a' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#new' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    // 旧路径命中：打开新建 agent 对话框（输入清空），绝不透传 HubCommand
    expect(api.hubCommand).not.toHaveBeenCalled();
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
    expect((box as HTMLTextAreaElement).value).toBe('');
    expect(screen.getByRole('dialog')).toBeTruthy();
  });

  it('未 @ 时回落到 hub 消息里最近一次 agent 回复的会话，而非 store.lastAgentId', async () => {
    useAppStore.setState({
      chats: [
        chat({ ID: 'c1', ToolID: 'claude', Title: '甲' }),
        chat({ ID: 'c2', ToolID: 'claude', Title: '乙', Workspace: 'D:\\ws\\b' }),
      ],
      hubMessages: [
        { id: 1, kind: 'user', chatId: 'c1', workspace: 'D:\\ws\\a', text: '给甲的', ts: 1 },
        { id: 2, kind: 'agent', chatId: 'c2', tool: 'claude', event: 'Stop', workspace: 'D:\\ws\\b', text: '乙的回复', ts: 2 },
      ],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    await act(async () => {});
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
    await act(async () => {});
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
    await act(async () => {});
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '继续' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c2', '继续'));
  });

  it('hub 消息只有 workspace 归因：不再模糊猜同项目会话，改为列会话待选', async () => {
    api.hubPickTarget.mockResolvedValue({
      Replies: [{ Text: '未精确匹配到会话——回复编号' }],
      Pending: true,
    });
    useAppStore.setState({
      chats: [
        chat({ ID: 'c1', ToolID: 'claude', Title: '甲' }),
        chat({ ID: 'c2', ToolID: 'claude', Title: '乙', Workspace: 'D:\\ws\\b' }),
      ],
      // 消息 workspace 小写 + 正斜杠：旧逻辑会归并到乙，现要求精确归因，不得猜。
      hubMessages: [{ id: 1, kind: 'agent', tool: 'claude', event: 'Stop', workspace: 'd:/ws/b', text: '乙的回复', ts: 1 }],
      lastAgentId: null,
    });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '继续' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.hubPickTarget).toHaveBeenCalledWith('继续'));
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
  });

  it('hub 消息不可投递时不回落 store.lastAgentId，改为列会话待选', async () => {
    api.hubPickTarget.mockResolvedValue({
      Replies: [{ Text: '未精确匹配到会话——回复编号' }],
      Pending: true,
    });
    // 工具表覆盖：codex 无 ACP → replyState=tool，最后可归因但不可投递
    api.getTools.mockResolvedValue([acpTool(), { ID: 'codex', Name: 'Codex', Installed: true }]);
    useAppStore.setState({
      chats: [chat({ ID: 'c1', ToolID: 'claude', Title: '甲' })],
      hubMessages: [
        {
          id: 1,
          kind: 'agent',
          tool: 'codex',
          event: 'Stop',
          workspace: 'D:\\ws\\gone',
          sessionId: 'sess-gone',
          text: '旧回复',
          ts: 1,
        },
      ],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    await act(async () => {});
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '继续' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.hubPickTarget).toHaveBeenCalledWith('继续'));
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
  });

  it('无 hub 归因且仅有无效 lastAgentId：列会话待选', async () => {
    api.hubPickTarget.mockResolvedValue({
      Replies: [{ Text: '未精确匹配到会话——回复编号' }],
      Pending: true,
    });
    useAppStore.setState({
      chats: [chat({ ID: 'c1', ToolID: 'claude', Title: '甲' })],
      hubMessages: [],
      lastAgentId: 'c-gone',
    });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '继续' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.hubPickTarget).toHaveBeenCalledWith('继续'));
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
  });

  it('最新 agent 回复未激活时走待选，不自动激活、不误投同 workspace 会话', async () => {
    api.hubPickTarget.mockResolvedValue({
      Replies: [{ Text: '未精确匹配到会话——回复编号\n\n1. [a] claude 甲\n2. en — 快速新建' }],
      Pending: true,
    });
    useAppStore.setState({
      chats: [chat({ ID: 'c1', ToolID: 'claude', Title: '甲' })],
      hubMessages: [
        {
          id: 1,
          kind: 'agent',
          tool: 'claude',
          event: 'Stop',
          workspace: 'D:\\ws\\a',
          sessionId: 'sess-9',
          sessionTitle: 'strategy',
          text: 'strategy 的回复',
          ts: 2,
        },
      ],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    await act(async () => {});
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '继续' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.hubPickTarget).toHaveBeenCalledWith('继续'));
    expect(api.openSessionACP).not.toHaveBeenCalled();
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
  });

  it('最新 agent 仅带 session: termKey 未激活时同样走待选', async () => {
    api.hubPickTarget.mockResolvedValue({
      Replies: [{ Text: '未精确匹配到会话——回复编号' }],
      Pending: true,
    });
    useAppStore.setState({
      chats: [chat({ ID: 'c1', ToolID: 'claude', Title: '甲' })],
      hubMessages: [
        {
          id: 1,
          kind: 'agent',
          tool: 'claude',
          event: 'Stop',
          workspace: 'D:\\ws\\a',
          termKey: 'session:sess-8',
          text: '回复',
          ts: 1,
        },
      ],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    await act(async () => {});
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '继续' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.hubPickTarget).toHaveBeenCalledWith('继续'));
    expect(api.openSessionACP).not.toHaveBeenCalled();
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
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

  it('拉取失败不阻塞：无会话时 chips 区不渲染空文案', async () => {
    api.listHubMessages.mockRejectedValue(new Error('boom'));
    render(<AgentHubPanel />);
    expect(screen.queryByText(tt('ui.hub.empty'))).toBeNull();
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

  it('粘贴拦截原生默认行为（防原生插入与 handlePaste 双写）', () => {
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    const evt = createEvent.paste(box);
    const spy = vi.spyOn(evt, 'preventDefault');
    fireEvent(box, evt);
    expect(spy).toHaveBeenCalled();
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
    expect(list).toHaveTextContent('@Cursor#乙');
    // 第二行展示项目 · 会话标题
    expect(list).toHaveTextContent('a · 乙');
    // 候选为 role=option 的 li（与 # 弹层结构一致），mousedown 选中
    const option = within(list).getByText('@Cursor#乙').closest('li')!;
    fireEvent.mouseDown(option);
    expect((box as HTMLTextAreaElement).value).toBe('@Cursor#乙 ');
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
    expect((box as HTMLTextAreaElement).value).toBe('@Claude Code#甲 ');
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
    expect((box as HTMLTextAreaElement).value).toBe('@Cursor#乙 ');
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
    expect((box as HTMLTextAreaElement).value).toBe('@Claude Code#甲 ');
  });

  it('@ 补全 Esc 关闭后，Enter 恢复发送语义（回落最后可归因会话）', async () => {
    useAppStore.setState({
      chats: [
        chat({ ID: 'c1', ToolID: 'claude', Title: '甲' }),
        chat({ ID: 'c2', ToolID: 'cursor', Title: '乙' }),
      ],
      hubMessages: [{ id: 1, kind: 'user', chatId: 'c2', text: '先前', ts: 1 }],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    await act(async () => {});
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

  it('无标题会话在 chip 兜底显示项目基名', () => {
    useAppStore.setState({
      chats: [chat({ ID: 'c1', Title: '', Workspace: 'D:\\ws\\gamma' })],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    // chip 主体兜底显示项目基名（chips 常驻，无需展开）
    expect(screen.getByTestId('hub-session-chips')).toHaveTextContent('gamma');
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
    await waitFor(() => expect(api.openWorkspaceACP).toHaveBeenCalledWith('D:\\ws\\a', 'claude', null));
    await waitFor(() => expect(useAppStore.getState().chats.some((c) => c.ID === 'n1')).toBe(true));
    expect(useAppStore.getState().lastAgentId).toBe('n1');
  });

  it('新建 agent：模型下拉合并 kshell 注入模型并去重', async () => {
    api.getTools.mockResolvedValue([acpTool()]);
    api.getModelConfig.mockResolvedValue({
      Enabled: true,
      Platforms: [
        {
          ID: 'p1',
          Name: '智谱',
          Preset: 'openai',
          OpenAIBaseURL: 'https://h/',
          AnthropicBaseURL: 'https://h/',
          Models: ['glm-5.3-flash', 'deepseek-v4.1-flash'],
          APIKeySet: true,
          IsDefault: true,
        },
      ],
      DefaultPlatform: 'p1',
      ActiveModel: 'glm-5.3-flash',
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
    expect(values[0]).toBe('glm-5.3-flash'); // 注入模型在前；value 仍为裸 ID
    // 展示名带平台前缀，不得把平台名写进 value
    expect(select.options[0].textContent).toBe('智谱: glm-5.3-flash');
    expect(select.options[1].textContent).toBe('智谱: deepseek-v4.1-flash');
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
    const glmOpt = Array.from(refreshed.options).find((o) => o.value === 'glm-5.3-flash');
    expect(glmOpt?.textContent).toBe('智谱: glm-5.3-flash');
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

  it('新建 agent：模型下拉文案用 Name，value 仍为 ID（Cursor Auto→default[]）', async () => {
    api.getTools.mockResolvedValue([{ ...acpTool('cursor'), Name: 'Cursor' }]);
    saveToolModels('cursor', [
      { id: 'default[]', name: 'Auto' },
      { id: 'grok-4.7[fast=true]', name: 'grok-4.7' },
    ]);
    useAppStore.setState({ workspaces: [workspaceA] });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.new_agent') }));
    await waitFor(() => {
      expect((screen.getByLabelText(tt('ui.hub.field_tool')) as HTMLSelectElement).value).toBe('cursor');
    });
    const select = screen.getByLabelText(tt('ui.hub.field_model')) as HTMLSelectElement;
    const auto = Array.from(select.options).find((o) => o.value === 'default[]');
    expect(auto).toBeTruthy();
    expect(auto!.textContent).toBe('Auto');
    const grok = Array.from(select.options).find((o) => o.value === 'grok-4.7[fast=true]');
    expect(grok!.textContent).toBe('grok-4.7');
  });

  it('新建 agent：创建成功后按 listChatModels 的 Name 落缓存', async () => {
    api.getTools.mockResolvedValue([{ ...acpTool('cursor'), Name: 'Cursor' }]);
    api.openWorkspaceACP.mockResolvedValue({
      Kind: 'chat',
      Chat: chat({ ID: 'n-auto', Workspace: 'D:\\ws\\a', ToolID: 'cursor' }),
    });
    api.listChatModels.mockResolvedValue({
      Current: 'default[]',
      Models: [
        { ID: 'default[]', Name: 'Auto' },
        { ID: 'm2', Name: 'Two' },
      ],
    });
    useAppStore.setState({ workspaces: [workspaceA] });
    render(<AgentHubPanel />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.new_agent') }));
    await waitFor(() => {
      expect((screen.getByLabelText(tt('ui.hub.field_tool')) as HTMLSelectElement).value).toBe('cursor');
    });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.create') }));
    await waitFor(() => expect(api.listChatModels).toHaveBeenCalledWith('n-auto'));
    await waitFor(() =>
      expect(loadToolModelOptions('cursor')).toEqual([
        { id: 'default[]', name: 'Auto' },
        { id: 'm2', name: 'Two' },
      ]),
    );
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
    await waitFor(() => expect(api.openWorkspaceACP).toHaveBeenCalledWith('D:\\ws\\a', 'claude', null));
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
    await waitFor(() => expect(api.openWorkspaceACP).toHaveBeenCalledWith('D:\\ws\\a', 'claude', null));
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
    useAppStore.setState({
      chats: [chat({ ID: 'c1' })],
      hubMessages: [{ id: 1, kind: 'user', chatId: 'c1', text: '先前', ts: 1 }],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    await act(async () => {});
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '/' } });
    await waitFor(() => expect(api.listChatCommands).toHaveBeenCalledWith('c1'));
    // 无 agent 技能时 slash 模式无候选，补全面板不出现（client 命令已迁到 # 模式）
    expect(screen.queryByRole('listbox', { name: tt('ui.hub.cmd_aria') })).toBeNull();
  });

  it('选中 #clear：清空本地 Hub 消息流与 agent 通知，且不投递后端', async () => {
    // 显式空列表：挂载 effect 的 listHubMessages 不能回显预置消息，否则清空后被再次写回
    api.listHubMessages.mockResolvedValue([]);
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
    useAppStore.setState({
      chats: [chat({ ID: 'c1' })],
      hubMessages: [{ id: 1, kind: 'user', chatId: 'c1', text: '先前', ts: 1 }],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    await act(async () => {});
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
    useAppStore.setState({
      chats: [chat({ ID: 'c1' })],
      hubMessages: [{ id: 1, kind: 'user', chatId: 'c1', text: '先前', ts: 1 }],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    await act(async () => {});
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

  it('正文中间的 @ 提及剥离标签，行中 # 词仍原样投递', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '看下 @Claude Code#c1 #nope rest' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', '看下 #nope rest');
    expect(
      useAppStore
        .getState()
        .toasts.some((t) => t.title === tt('ui.hub.unknown_cmd').replace('{{cmd}}', '#nope')),
    ).toBe(false);
  });

  it('行中 # 词（颜色值）不拦截：正常投递给 agent', async () => {
    useAppStore.setState({
      chats: [chat({ ID: 'c1' })],
      hubMessages: [{ id: 1, kind: 'user', chatId: 'c1', text: '先前', ts: 1 }],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    await act(async () => {});
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '把按钮颜色改成 #333' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', '把按钮颜色改成 #333'));
    expect(
      useAppStore
        .getState()
        .toasts.some((t) => t.title === tt('ui.hub.unknown_cmd').replace('{{cmd}}', '#333')),
    ).toBe(false);
  });

  it('空 hub 仅有 lastAgentId：不直发，走待选', async () => {
    api.hubPickTarget.mockResolvedValue({
      Replies: [{ Text: '未精确匹配到会话——回复编号' }],
      Pending: true,
    });
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], hubMessages: [], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '继续' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.hubPickTarget).toHaveBeenCalledWith('继续'));
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
  });

  it('最后消息未激活时无默认接收者：不出现待激活 chip 高亮', async () => {
    const msgs = [
      { id: 1, kind: 'agent', tool: 'claude', event: 'Stop', workspace: 'D:\\ws\\a', sessionId: 'sess-9', sessionTitle: 'strategy', text: '回复', ts: 1 },
    ];
    api.listHubMessages.mockResolvedValue(msgs);
    useAppStore.setState({
      chats: [chat({ ID: 'c1', ToolID: 'claude', Title: '甲' })],
      hubMessages: msgs,
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    await act(async () => {});
    expect(screen.queryByTitle(tt('ui.hub.pending_chip_aria'))).toBeNull();
    expect(screen.getByText('甲').closest('[aria-current="true"]')).toBeNull();
  });

  it('pending 目标已激活（会话重新打开）后待激活 chip 消失', async () => {
    api.listHubMessages.mockResolvedValue([
      { id: 1, kind: 'agent', tool: 'claude', event: 'Stop', chatId: 'c1', workspace: 'D:\\ws\\a', sessionId: 'sess-9', sessionTitle: '甲', text: '回复', ts: 1 },
    ]);
    useAppStore.setState({
      chats: [chat({ ID: 'c1', ToolID: 'claude', Title: '甲' })],
      hubMessages: [
        { id: 1, kind: 'agent', tool: 'claude', event: 'Stop', chatId: 'c1', workspace: 'D:\\ws\\a', sessionId: 'sess-9', sessionTitle: '甲', text: '回复', ts: 1 },
      ],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    // 最新消息精确命中已激活 c1：currentTargetId='c1'，无待激活 chip
    await screen.findByText('甲');
    expect(screen.queryByText(new RegExp(tt('ui.hub.pending_chip')))).toBeNull();
  });

  it('发送 #stop（带尾空格）：对最后可归因已激活会话 cancelChat，不投递', async () => {
    useAppStore.setState({
      chats: [chat({ ID: 'c1' })],
      hubMessages: [{ id: 1, kind: 'user', chatId: 'c1', text: '先前', ts: 1 }],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    await act(async () => {});
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#stop ' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.cancelChat).toHaveBeenCalledWith('c1'));
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
  });

  it('#stop 在无默认目标（未激活最后消息）时提示无目标且不激活', async () => {
    useAppStore.setState({
      chats: [chat({ ID: 'c1', ToolID: 'claude', Title: '甲' })],
      hubMessages: [
        { id: 1, kind: 'agent', tool: 'claude', event: 'Stop', workspace: 'D:\\ws\\a', sessionId: 'sess-9', sessionTitle: 'strategy', text: '回复', ts: 1 },
      ],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    await act(async () => {});
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#stop ' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(api.openSessionACP).not.toHaveBeenCalled();
    expect(api.cancelChat).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(useAppStore.getState().toasts.some((x) => x.title === tt('ui.hub.need_target_drag'))).toBe(true),
    );
    expect(api.cancelChat).not.toHaveBeenCalled();
  });

  it('#model 在无默认目标（未激活最后消息）时提示 need_target_drag', async () => {
    useAppStore.setState({
      chats: [chat({ ID: 'c1', ToolID: 'claude', Title: '甲' })],
      hubMessages: [
        { id: 1, kind: 'agent', tool: 'claude', event: 'Stop', workspace: 'D:\\ws\\a', sessionId: 'sess-9', sessionTitle: 'strategy', text: '回复', ts: 1 },
      ],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    await act(async () => {});
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#model ' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(api.openSessionACP).not.toHaveBeenCalled();
    expect(api.listChatModels).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(useAppStore.getState().toasts.some((x) => x.title === tt('ui.hub.need_target_drag'))).toBe(true),
    );
  });

  it('#model 菜单切换成功后把所切模型记为该工具的上次模型（lastCreate 回写）', async () => {
    api.listChatModels.mockResolvedValue({
      Current: 'm1',
      Models: [
        { ID: 'm1', Name: 'Model One' },
        { ID: 'm2', Name: 'Model Two' },
      ],
    });
    saveLastCreate({ wsPath: 'D:\\ws\\a', toolId: 'claude', modelId: 'm1' });
    useAppStore.setState({
      chats: [chat({ ID: 'c1', ToolID: 'claude', Workspace: 'D:\\ws\\a' })],
      hubMessages: [{ id: 1, kind: 'user', chatId: 'c1', text: '先前', ts: 1 }],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    await act(async () => {});
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#model' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.listChatModels).toHaveBeenCalledWith('c1'));
    // 模型菜单出现，点选 Model Two
    const menu = await screen.findByRole('list', { name: tt('ui.hub.model_label') });
    fireEvent.click(within(menu).getByRole('button', { name: 'Model Two' }));
    await waitFor(() => expect(api.setChatModel).toHaveBeenCalledWith('c1', 'm2'));
    // 成功后回写 lastCreate：同工具原地更新 modelId，wsPath 保留
    await waitFor(() =>
      expect(loadLastCreate()).toEqual({ wsPath: 'D:\\ws\\a', toolId: 'claude', modelId: 'm2' }),
    );
  });

  it('#model 菜单切换失败时不回写 lastCreate', async () => {
    api.listChatModels.mockResolvedValue({
      Current: 'm1',
      Models: [
        { ID: 'm1', Name: 'Model One' },
        { ID: 'm2', Name: 'Model Two' },
      ],
    });
    api.setChatModel.mockRejectedValueOnce(new Error('model boom'));
    saveLastCreate({ wsPath: 'D:\\ws\\a', toolId: 'claude', modelId: 'm1' });
    useAppStore.setState({
      chats: [chat({ ID: 'c1', ToolID: 'claude', Workspace: 'D:\\ws\\a' })],
      hubMessages: [{ id: 1, kind: 'user', chatId: 'c1', text: '先前', ts: 1 }],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    await act(async () => {});
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#model' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    const menu = await screen.findByRole('list', { name: tt('ui.hub.model_label') });
    fireEvent.click(within(menu).getByRole('button', { name: 'Model Two' }));
    await waitFor(() => expect(api.setChatModel).toHaveBeenCalledWith('c1', 'm2'));
    expect(loadLastCreate()).toEqual({ wsPath: 'D:\\ws\\a', toolId: 'claude', modelId: 'm1' });
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
    useAppStore.setState({
      chats: [chat({ ID: 'c1' })],
      hubMessages: [{ id: 1, kind: 'user', chatId: 'c1', text: '先前', ts: 1 }],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    await act(async () => {});
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
    useAppStore.setState({
      chats: [chat({ ID: 'c1' })],
      hubMessages: [{ id: 1, kind: 'user', chatId: 'c1', text: '先前', ts: 1 }],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    await act(async () => {});
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

  it('@ 目标后输入 / 仍弹技能补全：选中只替换尾部词，发送时去掉 @ 前缀', async () => {
    api.listChatCommands.mockResolvedValue([{ Name: 'commit', Description: 'Commit changes' }]);
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder')) as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: '@Claude Code#重构认证 /' } });
    await waitFor(() =>
      expect(screen.getByRole('listbox', { name: tt('ui.hub.cmd_aria') })).toHaveTextContent('/commit'),
    );
    fireEvent.keyDown(box, { key: 'Enter' });
    // @ 目标保留，仅尾部 / 词替换成技能
    expect(box.value).toBe('@Claude Code#重构认证 /commit ');
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', '/commit'));
  });

  it('@ 目标的普通消息：投递正文不含路由标签', async () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '@Claude Code#重构认证 帮我修复' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', '帮我修复'));
  });

  it('@ 目标后的 # 命令仍按客户端命令执行（标签不参与命令探测）', async () => {
    api.hubCommand.mockResolvedValue([]);
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '@Claude Code#重构认证 #l' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.hubCommand).toHaveBeenCalledWith('#l'));
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
  });

  it('@ 目标后的纯数字按正文投递，不当菜单编号命令', async () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '@Claude Code#重构认证 3' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', '3'));
    expect(api.hubCommand).not.toHaveBeenCalled();
  });

  it('只有 @ 标签时不投递：提示并保留草稿', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder')) as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: '@Claude Code#重构认证 ' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
    expect(
      useAppStore.getState().toasts.some((x) => x.title === tt('ui.hub.empty_message')),
    ).toBe(true);
    expect(box.value).toBe('@Claude Code#重构认证 ');
  });

  it('历史用户消息气泡不展示 @ 路由标签', () => {
    useAppStore.setState({
      chats: [chat({ ID: 'c1' })],
      hubMessages: [{ id: 1, kind: 'user', chatId: 'c1', text: '@Claude Code#c1 旧消息', ts: 1 }],
    });
    render(<AgentHubPanel />);
    expect(screen.getByText('旧消息')).toBeTruthy();
    expect(screen.queryByText(/@Claude Code#/)).toBeNull();
  });

  it('/ 前有普通文字时不弹技能补全（"/" 不在正文首字符，技能不会执行）', async () => {
    api.listChatCommands.mockResolvedValue([{ Name: 'commit', Description: 'Commit changes' }]);
    useAppStore.setState({ chats: [chat({ ID: 'c1' })], lastAgentId: 'c1' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '看看 /' } });
    await waitFor(() => expect(api.listChatCommands).not.toHaveBeenCalled());
    expect(screen.queryByRole('listbox', { name: tt('ui.hub.cmd_aria') })).toBeNull();
  });

  it('/ 开头但不匹配任何 agent 技能：不拦截，原样发送给 agent', async () => {
    api.listChatCommands.mockResolvedValue([]);
    useAppStore.setState({
      chats: [chat({ ID: 'c1' })],
      hubMessages: [{ id: 1, kind: 'user', chatId: 'c1', text: '先前', ts: 1 }],
      lastAgentId: 'c1',
    });
    render(<AgentHubPanel />);
    await act(async () => {});
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
    expect((box as HTMLTextAreaElement).value).toBe('@Claude Code#重构认证 ');
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
    expect((box as HTMLTextAreaElement).value).toBe('帮我看看 @Claude Code#重构认证 ');
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

  it('非 ACP 工具的 agent 气泡：只读徽标 + 隐藏快捷按钮（规则1）', async () => {
    // codex 无 ACP 探测 → replyState 'tool'：永久只读
    api.getTools.mockResolvedValue([{ ...acpTool('codex'), Name: 'Codex', ACP: undefined }]);
    useAppStore.setState({ chats: [chat({ ID: 'c1', ToolID: 'codex' })] });
    api.listHubMessages.mockResolvedValue([
      { id: 2, kind: 'agent', tool: 'codex', event: 'Stop', chatId: 'c1', text: 'done', ts: 2 },
    ]);
    render(<AgentHubPanel />);
    await screen.findByText('done');
    expect(screen.getAllByText(tt('ui.hub.readonly_badge'))).toHaveLength(1);
    expect(screen.queryByRole('button', { name: tt('ui.hub.quick_continue') })).toBeNull();
    expect(screen.queryByRole('button', { name: tt('ui.hub.quick_rephrase') })).toBeNull();
  });

  it('终端激活态的 agent 气泡：只读徽标 + 隐藏快捷按钮（规则2）', async () => {
    useAppStore.setState({
      chats: [chat({ ID: 'c1', Key: 'session:s1', ToolID: 'claude' })],
      terminals: [term({ Key: 'session:s1' })],
    });
    api.listHubMessages.mockResolvedValue([
      { id: 2, kind: 'agent', tool: 'claude', event: 'Stop', chatId: 'c1', termKey: 'session:s1', text: 'done', ts: 2 },
    ]);
    render(<AgentHubPanel />);
    await screen.findByText('done');
    expect(screen.getAllByText(tt('ui.hub.readonly_badge'))).toHaveLength(1);
    expect(screen.queryByRole('button', { name: tt('ui.hub.quick_continue') })).toBeNull();
    expect(screen.queryByRole('button', { name: tt('ui.hub.quick_rephrase') })).toBeNull();
  });

  it('终端在册 + 消息仅 sessionId（termKey 为 new:N）：只读，不调 openSessionACP', async () => {
    // 历史 ACP 欢迎词常带 new:N；扫描回填 sessionId 后用户又以 TUI 打开同会话
    useAppStore.setState({ terminals: [term({ Key: 'session:s1' })] });
    api.listHubMessages.mockResolvedValue([
      {
        id: 2,
        kind: 'agent',
        tool: 'claude',
        event: 'Stop',
        chatId: 'c-dead',
        termKey: 'new:3',
        sessionId: 's1',
        text: 'done',
        ts: 2,
      },
    ]);
    render(<AgentHubPanel />);
    await screen.findByText('done');
    expect(screen.getAllByText(tt('ui.hub.readonly_badge'))).toHaveLength(1);
    expect(screen.queryByRole('button', { name: tt('ui.hub.quick_continue') })).toBeNull();
    expect(api.openSessionACP).not.toHaveBeenCalled();
  });

  it('离线 ACP 气泡不受只读影响：终端激活条带徽标，离线条按钮照常（规则3）', async () => {
    useAppStore.setState({ terminals: [term({ Key: 'session:s1' })] });
    api.listHubMessages.mockResolvedValue([
      { id: 1, kind: 'agent', tool: 'claude', event: 'Stop', termKey: 'session:s1', text: 'term', ts: 1 },
      { id: 2, kind: 'agent', tool: 'claude', event: 'Stop', termKey: 'session:s-off', text: 'off', ts: 2 },
    ]);
    render(<AgentHubPanel />);
    await screen.findByText('off');
    // 仅终端激活那条带徽标；离线那条快捷按钮照常
    expect(screen.getAllByText(tt('ui.hub.readonly_badge'))).toHaveLength(1);
    expect(screen.getAllByRole('button', { name: tt('ui.hub.quick_continue') })).toHaveLength(1);
    expect(screen.getAllByRole('button', { name: tt('ui.hub.quick_rephrase') })).toHaveLength(1);
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

  it('「换个说法」目标未激活时先激活会话再填 @ 新标签并聚焦，不发送', async () => {
    // 记录的 chatId 不在 chats（会话已关闭/重启后未恢复）：走激活路径
    api.listHubMessages.mockResolvedValue([
      { id: 10, kind: 'agent', tool: 'claude', event: 'Stop', chatId: 'c-dead', termKey: 'session:s-r1', text: 'ok', ts: 10 },
    ]);
    api.openSessionACP.mockResolvedValue({
      Kind: 'chat',
      Chat: chat({ ID: 'c-r1', Key: 'session:s-r1', Kind: 'session', SessionID: 's-r1' }),
    });
    render(<AgentHubPanel />);
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.hub.quick_rephrase') }));
    await waitFor(() => expect(api.openSessionACP).toHaveBeenCalledWith('s-r1'));
    // 激活后新 chat 的 @ 标签填入输入框并聚焦（既有草稿保留式追加），不自动发送
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    await waitFor(() => expect((box as HTMLTextAreaElement).value).toBe('@Claude Code#重构认证 '));
    expect(document.activeElement).toBe(box);
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
    // 激活后的会话登记进 store，后续消息可直达
    expect(useAppStore.getState().chats.some((c) => c.ID === 'c-r1')).toBe(true);
  });

  it('「换个说法」激活成功但 options 暂无新 chat 时按会话标题构造 @ 标签', async () => {
    api.listHubMessages.mockResolvedValue([
      { id: 11, kind: 'agent', tool: 'cursor', event: 'Stop', chatId: 'c-dead', sessionId: 's-r2', text: 'ok', ts: 11 },
    ]);
    api.openSessionACP.mockResolvedValue({
      Kind: 'chat',
      Chat: chat({ ID: 'c-r2', Key: 'session:s-r2', Kind: 'session', SessionID: 's-r2', ToolID: 'cursor' }),
    });
    render(<AgentHubPanel />);
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.hub.quick_rephrase') }));
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    await waitFor(() => expect((box as HTMLTextAreaElement).value).toBe('@Cursor#重构认证 '));
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
  });

  it('「换个说法」目标未激活且激活链路解析不到会话时提示 need_target 且不填入', async () => {
    // 无 sessionId / termKey 前缀 / workspace+标题：三级解析全落空
    api.listHubMessages.mockResolvedValue([
      { id: 12, kind: 'agent', tool: 'codex', event: 'Stop', text: 'x', ts: 12 },
    ]);
    render(<AgentHubPanel />);
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.hub.quick_rephrase') }));
    await waitFor(() =>
      expect(useAppStore.getState().toasts.some((t) => t.title === tt('ui.hub.need_target'))).toBe(true),
    );
    expect(api.openSessionACP).not.toHaveBeenCalled();
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    expect((box as HTMLTextAreaElement).value).toBe('');
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

    await waitFor(() => expect(api.openWorkspaceACP).toHaveBeenCalledWith('D:\\ws\\a', 'codex', 'm2'));
    // 默认设置的模型在创建后生效
    await waitFor(() => expect(api.setChatModel).toHaveBeenCalledWith('n7', 'm2'));
  });

  it('#easynew：默认设置未显式选模型时回落 lastCreate.modelId（上次模型）', async () => {
    api.getTools.mockResolvedValue([acpTool()]);
    api.openWorkspaceACP.mockResolvedValue({
      Kind: 'chat',
      Chat: chat({ ID: 'n10', Workspace: 'D:\\ws\\a', ToolID: 'claude' }),
    });
    api.listChatModels.mockResolvedValue({ Current: '', Models: [] });
    saveDefaultCreate({ toolId: 'claude', modelId: null });
    saveLastCreate({ wsPath: 'D:\\ws\\a', toolId: 'claude', modelId: 'm9' });
    useAppStore.setState({ workspaces: [workspaceA], selectedProject: 'D:\\ws\\a' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#easynew' } });
    fireEvent.keyDown(box, { key: 'Enter' });

    await waitFor(() => expect(api.openWorkspaceACP).toHaveBeenCalledWith('D:\\ws\\a', 'claude', 'm9'));
    // 默认设置只固定工具；模型跟随上次创建记录
    await waitFor(() => expect(api.setChatModel).toHaveBeenCalledWith('n10', 'm9'));
  });

  it('#easynew：listChatModels 失败仍 setChatModel，且 open 传入 modelId', async () => {
    api.getTools.mockResolvedValue([acpTool()]);
    api.openWorkspaceACP.mockResolvedValue({
      Kind: 'chat',
      Chat: chat({ ID: 'n11', Workspace: 'D:\\ws\\a', ToolID: 'claude' }),
    });
    api.listChatModels.mockRejectedValue(new Error('not ready'));
    saveLastCreate({ wsPath: 'D:\\ws\\a', toolId: 'claude', modelId: 'deepseek-v4.1-flash' });
    useAppStore.setState({ workspaces: [workspaceA], selectedProject: 'D:\\ws\\a' });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder'));
    fireEvent.change(box, { target: { value: '#easynew' } });
    fireEvent.keyDown(box, { key: 'Enter' });

    await waitFor(() =>
      expect(api.openWorkspaceACP).toHaveBeenCalledWith('D:\\ws\\a', 'claude', 'deepseek-v4.1-flash'),
    );
    await waitFor(() => expect(api.setChatModel).toHaveBeenCalledWith('n11', 'deepseek-v4.1-flash'));
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

    await waitFor(() => expect(api.openWorkspaceACP).toHaveBeenCalledWith('D:\\ws\\a', 'claude', null));
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

    await waitFor(() => expect(api.openWorkspaceACP).toHaveBeenCalledWith('D:\\ws\\a', 'claude', null));
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
    // draft 追加 @ 标签（工具展示名#会话标题，不再写 chat id）
    expect((box as HTMLTextAreaElement).value).toBe('@Claude Code#重构认证 ');
    // 对话框关闭（标题不再渲染）
    await waitFor(() => expect(screen.queryByText(tt('ui.hub.find_title'))).toBeNull());
  });

  it('同名会话经 #find 激活：写入带项目消歧的 @ 标签（与全量补全候选一致）', async () => {
    useAppStore.setState({
      chats: [chat({ ID: 'c0', Title: '重构认证', Workspace: 'D:\\ws\\a' })],
      lastAgentId: 'c0',
    });
    api.getSessions.mockResolvedValue([
      {
        ID: 's1',
        ToolID: 'claude',
        Workspace: 'D:\\ws\\alpha',
        Title: '重构认证',
        CreatedAt: new Date().toISOString(),
        UpdatedAt: new Date().toISOString(),
        Messages: 3,
        Path: '',
      },
    ]);
    api.openSessionACP.mockResolvedValue({
      Kind: 'chat',
      Chat: chat({ ID: 'c77', Key: 'k77', Kind: 'session', SessionID: 's1', Workspace: 'D:\\ws\\alpha' }),
    });
    render(<AgentHubPanel />);
    const box = screen.getByPlaceholderText(tt('ui.hub.input_placeholder')) as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: '#find' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    const dialog = await screen.findByRole('dialog');
    fireEvent.click(within(dialog).getByText('重构认证'));
    await waitFor(() => expect(api.openSessionACP).toHaveBeenCalledWith('s1'));
    // 两个同名 claude 会话：插入标签须带项目名，且能被 resolveMentionTarget 命中
    expect(box.value).toBe('@Claude Code#重构认证 · alpha ');
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

  it('路由模式切换：写 localStorage 并写穿后端', async () => {
    render(<AgentHubPanel />);
    const sel = screen.getByTestId('hub-route-mode') as HTMLSelectElement;
    expect(sel.value).toBe('auto');
    fireEvent.change(sel, { target: { value: 'agent' } });
    expect(sel.value).toBe('agent');
    expect(localStorage.getItem('kshell.agenthub.routeMode')).toBe('agent');
    await waitFor(() => expect(api.saveHubRouteMode).toHaveBeenCalledWith('agent'));
  });

  it('tool_confirm 消息渲染为确认卡：确认点击调 confirmHubTool', async () => {
    api.confirmHubTool.mockResolvedValue(undefined);
    useAppStore.setState({
      hubMessages: [
        {
          id: 1,
          kind: 'user',
          chatId: 'c1',
          text: '帮我新建需求',
          ts: 1,
        },
        {
          id: 2,
          kind: 'agent',
          chatId: 'c1',
          event: 'tool_confirm',
          text: '新建需求（项目 D:\\ws-a）：新增需求\n{"pending":"p9","tool":"create_requirement"}',
          ts: 2,
        },
      ],
    });
    render(<AgentHubPanel />);
    expect(screen.getByText('新建需求（项目 D:\\ws-a）：新增需求')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('hub-confirm-approve'));
    await waitFor(() => expect(api.confirmHubTool).toHaveBeenCalledWith('c1', 'p9', true));
  });
});
