// SessionList 组件测试：排序 / 工作区过滤 / 关键词过滤 / 内嵌终端入口（恢复·切换）/
// 标题清洗显示 / 事件刷新。
// api 层整体打桩（vi.mock），事件回调通过桩捕获后手动触发。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import SessionList from './SessionList';
import type { ChatInfo, ChatPermissionRequest, Session, TerminalInfo } from '../lib/api';
import { useAppStore } from '../state/store';
import { tt } from '../test/i18n';

// 会话消息数走复数 key：按 en 复数规则取 one/other 再填 {{count}}
const msgs = (n: number) =>
  tt(`ui.session_list.messages_${n === 1 ? 'one' : 'other'}`).replace('{{count}}', String(n));

const mocks = vi.hoisted(() => ({
  getSessions: vi.fn(),
  getRemoteSessions: vi.fn(),
  scanRemoteSessions: vi.fn(),
  batchArchiveSessions: vi.fn(),
  batchRestoreSessions: vi.fn(),
  isSSHWorkspaceRef: (p: string) => typeof p === 'string' && p.startsWith('ssh://'),
  onScanDone: vi.fn(),
}));
vi.mock('../lib/api', () => mocks);

const onSelectRow = vi.fn();
const onActivate = vi.fn();

const minutesAgo = (m: number) => new Date(Date.now() - m * 60_000).toISOString();

// 三个会话属于 D:\proj-a（Workspace 大小写混排验证不敏感匹配），一个属于其他工作区
const sessions: Session[] = [
  { ID: 's1', ToolID: 'codebuddy', Workspace: 'd:\\proj-a', Title: '修复上传白名单', CreatedAt: minutesAgo(30), UpdatedAt: minutesAgo(10), Messages: 12, Path: 'p1' },
  { ID: 's2', ToolID: 'claude', Workspace: 'D:\\Proj-A', Title: '重构登录页', CreatedAt: minutesAgo(200), UpdatedAt: minutesAgo(120), Messages: 30, Path: 'p2' },
  { ID: 's3', ToolID: 'gemini', Workspace: 'd:\\other', Title: '其他工作区会话', CreatedAt: minutesAgo(5), UpdatedAt: minutesAgo(5), Messages: 3, Path: 'p3' },
  { ID: 's4', ToolID: 'codex', Workspace: 'D:\\proj-a', Title: '清理构建缓存', CreatedAt: minutesAgo(60), UpdatedAt: minutesAgo(2), Messages: 5, Path: 'p4' },
];

let scanDoneCb: (payload: unknown) => void = () => {};

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  mocks.onScanDone.mockImplementation((cb: typeof scanDoneCb) => {
    scanDoneCb = cb;
    return () => {};
  });
  mocks.getSessions.mockResolvedValue(sessions);
  mocks.getRemoteSessions.mockResolvedValue([]);
  mocks.scanRemoteSessions.mockResolvedValue([]);
  useAppStore.setState({
    windowStatus: {},
    terminals: [],
    chats: [],
    chatPermissions: {},
    terminalBusy: {},
    archivedIDs: [],
    toasts: [],
    unreachableWorkspaces: {},
  });
});

const renderList = (selectedSessionID: string | null = null) =>
  render(
    <SessionList
      workspacePath={'D:\\proj-a'}
      selectedSessionID={selectedSessionID}
      onSelectRow={onSelectRow}
      onActivate={onActivate}
    />,
  );

// 按标题找会话行（li 元素）
async function findRow(title: string): Promise<HTMLElement> {
  const text = await screen.findByText(title);
  const row = text.closest('li');
  if (!row) throw new Error(`找不到会话行: ${title}`);
  return row;
}

// 内嵌终端镜像（store.terminals）：Status running 表示会话正在中心区运行
function terminal(over: Partial<TerminalInfo> = {}): TerminalInfo {
  return {
    ID: 't1',
    Kind: 'session',
    SessionID: 's1',
    Workspace: 'd:\\proj-a',
    Title: '修复上传白名单',
    ToolID: 'codebuddy',
    Status: 'running',
    ExitCode: 0,
    Cols: 120,
    Rows: 30,
    ...over,
  };
}

function chat(over: Partial<ChatInfo> = {}): ChatInfo {
  return {
    ID: 'c1',
    Kind: 'session',
    SessionID: 's1',
    Workspace: 'd:\\proj-a',
    Title: '修复上传白名单',
    ToolID: 'codebuddy',
    Status: 'running',
    ExitCode: 0,
    Error: '',
    ...over,
  };
}

const samplePermission: ChatPermissionRequest = {
  RequestID: 'r1',
  SessionID: 's1',
  ToolCall: { ToolCallID: 'tc1' },
  Options: [{ OptionID: 'allow', Name: '允许', Kind: 'allow_once' }],
};

describe('SessionList', () => {
  it('只显示当前工作区的会话（路径大小写不敏感），按 updatedAt 降序排列', async () => {
    renderList();

    await findRow('清理构建缓存');
    const titles = screen
      .getAllByRole('listitem')
      .map((li) => li.querySelector('[data-testid="session-title"]')?.textContent);
    expect(titles).toEqual(['清理构建缓存', '修复上传白名单', '重构登录页']);
    expect(screen.queryByText('其他工作区会话')).not.toBeInTheDocument();

    // 工具徽标与消息数（chip 行与会话行都有 CodeBuddy 徽标，断言放宽为「至少出现」）
    expect(screen.getAllByText('CodeBuddy').length).toBeGreaterThan(0);
    expect(screen.getByText(msgs(12))).toBeInTheDocument();
  });

  it('消息数单数 n=1 走复数 _one 分支（"1 message"）', async () => {
    mocks.getSessions.mockResolvedValue([
      { ID: 's1', ToolID: 'claude', Workspace: 'D:\\proj-a', Title: '只有一条消息', CreatedAt: minutesAgo(5), UpdatedAt: minutesAgo(2), Messages: 1, Path: 'p1' },
    ]);
    renderList();
    const row = await findRow('只有一条消息');
    expect(within(row).getByText(msgs(1))).toBeInTheDocument();
  });

  it('opencode 的前斜杠 cwd（D:/proj-a）与反斜杠写法视为同一工作区，会话与 chip 都要出现', async () => {
    mocks.getSessions.mockResolvedValue([
      ...sessions,
      {
        ID: 's5',
        ToolID: 'opencode',
        Workspace: 'D:/proj-a', // opencode 的 SQLite 里存的就是这种写法
        Title: 'opencode 会话',
        CreatedAt: minutesAgo(1),
        UpdatedAt: minutesAgo(1),
        Messages: 0,
        Path: 'db',
      },
    ]);
    renderList();

    expect(await findRow('opencode 会话')).toBeInTheDocument();
    const chips = screen.getByLabelText(tt('ui.session_list.filter_aria'));
    expect(within(chips).getByText('OpenCode')).toBeInTheDocument();
  });

  it('工具 chip 行按当前工作区会话去重展示，其他工作区的工具不出现（扁平下划线式）', async () => {
    renderList();
    await findRow('清理构建缓存');

    const chips = screen.getByLabelText(tt('ui.session_list.filter_aria'));
    expect(within(chips).getByText('CodeBuddy')).toBeInTheDocument();
    expect(within(chips).getByText('Claude')).toBeInTheDocument();
    expect(within(chips).getByText('Codex')).toBeInTheDocument();
    expect(within(chips).queryByText('Gemini')).not.toBeInTheDocument(); // 属于其他工作区

    // 扁平化：统一 rounded-sm，不用圆角胶囊；选中态用主色底 + 主色文字
    const all = within(chips).getByRole('button', { name: tt('ui.session_list.all') });
    expect(all).toHaveClass('rounded-sm');
    expect(all).toHaveClass('bg-primary/10');
    expect(all).toHaveClass('text-primary');
    const codex = within(chips).getByRole('button', { name: 'Codex' });
    expect(codex).not.toHaveClass('bg-primary/10');
    expect(codex.className).not.toContain('rounded-full');
  });

  it('点击工具 chip 筛选该工具会话，与文字搜索 AND 叠加，「全部」恢复', async () => {
    renderList();
    await findRow('清理构建缓存');

    fireEvent.click(screen.getByRole('button', { name: 'Codex' }));
    expect(screen.getByText('清理构建缓存')).toBeInTheDocument();
    expect(screen.queryByText('修复上传白名单')).not.toBeInTheDocument();
    expect(screen.queryByText('重构登录页')).not.toBeInTheDocument();

    // AND 叠加：Codex + 关键词「白名单」→ 无匹配（关键词命中的是 CodeBuddy 会话）
    fireEvent.change(screen.getByLabelText(tt('ui.session_list.search_aria')), { target: { value: '白名单' } });
    expect(screen.getByText(tt('ui.session_list.no_match'))).toBeInTheDocument();

    // 点「全部」并清空关键词恢复
    fireEvent.click(screen.getByRole('button', { name: tt('ui.session_list.all') }));
    fireEvent.change(screen.getByLabelText(tt('ui.session_list.search_aria')), { target: { value: '' } });
    expect(screen.getByText('修复上传白名单')).toBeInTheDocument();
    expect(screen.getByText('清理构建缓存')).toBeInTheDocument();
  });

  it('过滤词匹配标题 / 工具名', async () => {
    renderList();
    await findRow('清理构建缓存');

    const input = screen.getByLabelText(tt('ui.session_list.search_aria'));

    // 按标题过滤
    fireEvent.change(input, { target: { value: '白名单' } });
    expect(screen.getByText('修复上传白名单')).toBeInTheDocument();
    expect(screen.queryByText('清理构建缓存')).not.toBeInTheDocument();
    expect(screen.queryByText('重构登录页')).not.toBeInTheDocument();

    // 按工具名过滤：code 同时命中 codex 与 codebuddy
    fireEvent.change(input, { target: { value: 'code' } });
    expect(screen.getByText('清理构建缓存')).toBeInTheDocument();
    expect(screen.getByText('修复上传白名单')).toBeInTheDocument();
    expect(screen.queryByText('重构登录页')).not.toBeInTheDocument();

    // 无匹配时显示空态
    fireEvent.change(input, { target: { value: '不存在' } });
    expect(screen.getByText(tt('ui.session_list.no_match'))).toBeInTheDocument();
  });

  it('点击激活图标把会话交给 onActivate，点行交给 onSelectRow', async () => {
    renderList();
    const row = await findRow('修复上传白名单');

    fireEvent.click(within(row).getByRole('button', { name: tt('ui.session_list.activate') }));
    expect(onActivate).toHaveBeenCalledWith(expect.objectContaining({ ID: 's1' }));
    expect(onSelectRow).not.toHaveBeenCalled();

    fireEvent.click(row);
    expect(onSelectRow).toHaveBeenCalledWith(expect.objectContaining({ ID: 's1' }));
    expect(row).not.toHaveClass('bg-primary/8');
  });

  it('已恢复会话无激活按钮、有已恢复图标；仅 selectedSessionID 走高亮；点行走 onSelectRow', async () => {
    useAppStore.setState({ terminals: [terminal()], terminalBusy: { t1: true } });
    renderList('s1');
    const row = await findRow('修复上传白名单');

    expect(within(row).queryByRole('button', { name: tt('ui.session_list.activate') })).toBeNull();
    expect(within(row).queryByRole('button', { name: tt('ui.session_list.restore') })).toBeNull();
    expect(within(row).getByLabelText(tt('ui.session_list.restored'))).toBeInTheDocument();
    expect(within(row).getByLabelText(tt('ui.agent_activity.running'))).toBeInTheDocument();
    expect(row).toHaveClass('bg-primary/8');

    fireEvent.click(row);
    expect(onSelectRow).toHaveBeenCalledWith(expect.objectContaining({ ID: 's1' }));
  });

  it('终端 running 且无 busy 时列表行为「等待用户」', async () => {
    useAppStore.setState({ terminals: [terminal()], terminalBusy: {} });
    renderList();
    const row = await findRow('修复上传白名单');
    expect(within(row).getByLabelText(tt('ui.agent_activity.waiting'))).toBeInTheDocument();
    expect(within(row).queryByLabelText(tt('ui.agent_activity.running'))).toBeNull();
  });

  it('内嵌终端已退出时视为未激活（激活图标，无已恢复）', async () => {
    useAppStore.setState({
      terminals: [terminal({ Status: 'exited', ExitCode: 0 })],
    });
    renderList();
    const row = await findRow('修复上传白名单');

    expect(within(row).getByRole('button', { name: tt('ui.session_list.activate') })).toBeInTheDocument();
    expect(within(row).queryByLabelText(tt('ui.session_list.restored'))).toBeNull();
    expect(within(row).queryByText('✓')).toBeNull();
    expect(within(row).queryByLabelText(tt('ui.agent_activity.running'))).toBeNull();
    expect(within(row).queryByLabelText(tt('ui.agent_activity.waiting'))).toBeNull();
  });

  it('该会话已有打开中的 ACP 聊天时标已恢复；未选中不高亮', async () => {
    useAppStore.setState({
      chats: [chat({ Status: 'ready' })],
    });
    renderList();
    const row = await findRow('修复上传白名单');

    expect(within(row).queryByRole('button', { name: tt('ui.session_list.activate') })).toBeNull();
    expect(within(row).getByLabelText(tt('ui.session_list.restored'))).toBeInTheDocument();
    expect(within(row).getByLabelText(tt('ui.agent_activity.waiting'))).toBeInTheDocument();
    expect(row).not.toHaveClass('bg-primary/8');
  });

  it('标题剥掉 XML 包装标签后渲染；清洗后为空显示「(无标题)」（完整原文走悬停浮动卡片）', async () => {
    const dirty: Session[] = [
      { ID: 'd1', ToolID: 'claude', Workspace: 'D:\\proj-a', Title: '<system-reminder>今天日期 2026-09-28</system-reminder> 帮我把登录页报错文案改一下', CreatedAt: minutesAgo(9), UpdatedAt: minutesAgo(9), Messages: 2, Path: 'x1' },
      { ID: 'd2', ToolID: 'claude', Workspace: 'D:\\proj-a', Title: '<local-command-caveat>Caveat: 头部被截断的机器内容', CreatedAt: minutesAgo(8), UpdatedAt: minutesAgo(8), Messages: 2, Path: 'x2' },
    ];
    mocks.getSessions.mockResolvedValue(dirty);
    renderList();

    const row = await findRow('帮我把登录页报错文案改一下');
    // 行内单行截断展示清洗后的标题
    expect(row.querySelector('[data-testid="session-title"]')?.textContent).toBe('帮我把登录页报错文案改一下');
    expect(screen.queryByText(/local-command-caveat/)).not.toBeInTheDocument();

    const empty = await screen.findByText(tt('ui.session_list.untitled'));
    expect(empty).toHaveClass('italic');
    expect(empty.closest('li')).toBeInTheDocument();
  });

  it('滚动隔离：会话列表包在独立滚动容器中，搜索框与过滤 chips 不在其内', async () => {
    renderList();
    const row = await findRow('清理构建缓存');
    const ul = row.closest('ul');
    if (!ul) throw new Error('找不到会话列表 ul');
    // 列表自身撑满剩余高度并在内部滚动（父级无高度约束不影响类名断言）
    const scroller = ul.parentElement;
    expect(scroller).toHaveClass('min-h-0', 'flex-1', 'overflow-y-auto');
    // 搜索框与工具 chips 行留在滚动容器外，不随列表滚动
    const chips = screen.getByLabelText(tt('ui.session_list.filter_aria'));
    expect(scroller!.contains(chips)).toBe(false);
  });

  it('收到 scan:done 事件后重调 GetSessions 刷新', async () => {
    mocks.getSessions.mockResolvedValueOnce([]).mockResolvedValueOnce(sessions);
    renderList();
    expect(screen.queryByText('修复上传白名单')).not.toBeInTheDocument();

    await act(async () => {
      scanDoneCb({});
    });

    expect(await screen.findByText('修复上传白名单')).toBeInTheDocument();
    expect(mocks.getSessions).toHaveBeenCalledTimes(2);
  });

  it('GetSessions 失败不崩溃，scan:done 后恢复刷新', async () => {
    mocks.getSessions.mockRejectedValueOnce(new Error('绑定异常')).mockResolvedValueOnce(sessions);
    renderList();
    expect(screen.queryByText('修复上传白名单')).not.toBeInTheDocument();

    await act(async () => {
      scanDoneCb({});
    });

    expect(await screen.findByText('修复上传白名单')).toBeInTheDocument();
  });

  it('空态区分「扫描中」与「确实没有」', async () => {
    mocks.getSessions.mockResolvedValue([]);

    // 扫描未完成（idle/scanning）：显示骨架屏占位而非空态文案
    useAppStore.setState({ scanState: 'scanning' });
    const { unmount, container } = renderList();
    expect(container.querySelectorAll('.animate-pulse').length).toBeGreaterThan(0);
    expect(screen.queryByText(tt('ui.session_list.empty'))).not.toBeInTheDocument();
    unmount();

    // 扫描已完成、该工作区确实没有会话
    useAppStore.setState({ scanState: 'done' });
    renderList();
    expect(await screen.findByText(tt('ui.session_list.empty'))).toBeInTheDocument();
  });

  it('onScanDone 回调把 scanState 置 done（幂等，消除死角）', async () => {
    mocks.getSessions.mockResolvedValue([]);
    useAppStore.setState({ scanState: 'scanning' });
    renderList();

    await act(async () => {
      scanDoneCb({});
    });
    expect(useAppStore.getState().scanState).toBe('done');

    // 重复收到事件保持 done（幂等）
    await act(async () => {
      scanDoneCb({});
    });
    expect(useAppStore.getState().scanState).toBe('done');
  });
});

describe('SessionList ACP/TUI 标记', () => {
  it('cursor + ACPOnly 会话行显示 ACP 徽标', async () => {
    mocks.getSessions.mockResolvedValue([
      { ID: 'cs1', ToolID: 'cursor', Workspace: 'D:\\proj-a', Title: 'ACP 对话', CreatedAt: minutesAgo(9), UpdatedAt: minutesAgo(9), Messages: 2, Path: 'pc1', ACPOnly: true },
    ] satisfies Session[]);
    renderList();
    const row = await findRow('ACP 对话');
    expect(within(row).getByTestId('session-mode-badge')).toHaveTextContent('ACP');
  });

  it('cursor 非 ACPOnly 显示 TUI；其他工具与 live 行不标', async () => {
    mocks.getSessions.mockResolvedValue([
      { ID: 'cs2', ToolID: 'cursor', Workspace: 'D:\\proj-a', Title: 'TUI 对话', CreatedAt: minutesAgo(9), UpdatedAt: minutesAgo(9), Messages: 2, Path: 'pc2', ACPOnly: false },
      { ID: 'cs3', ToolID: 'claude', Workspace: 'D:\\proj-a', Title: '普通对话', CreatedAt: minutesAgo(8), UpdatedAt: minutesAgo(8), Messages: 2, Path: 'pc3', ACPOnly: false },
    ] satisfies Session[]);
    useAppStore.setState({
      terminals: [terminal({ ID: 'lt1', SessionID: '', Kind: 'session', ToolID: 'cursor', Title: '新开会话', Prompted: true })],
    });
    renderList();
    const tuiRow = await findRow('TUI 对话');
    expect(within(tuiRow).getByTestId('session-mode-badge')).toHaveTextContent('TUI');
    const plainRow = await findRow('普通对话');
    expect(within(plainRow).queryByTestId('session-mode-badge')).toBeNull();
    // live 行（Path live: 前缀）不带 ACPOnly，不得误标成 TUI
    const liveRow = await findRow('新开会话');
    expect(within(liveRow).queryByTestId('session-mode-badge')).toBeNull();
  });
});

describe('SessionList agent 活动图标', () => {
  it('打开 chat running 时列表行有「执行中」', async () => {
    useAppStore.setState({ chats: [chat({ Status: 'running' })] });
    renderList();
    const row = await findRow('修复上传白名单');
    expect(within(row).getByLabelText(tt('ui.agent_activity.running'))).toBeInTheDocument();
    expect(within(row).queryByText('✓')).toBeNull();
  });

  it('有 permission 时列表行有「待用户确认」', async () => {
    useAppStore.setState({
      chats: [chat({ Status: 'running' })],
      chatPermissions: { c1: samplePermission },
    });
    renderList();
    const row = await findRow('修复上传白名单');
    expect(within(row).getByLabelText(tt('ui.agent_activity.awaiting'))).toBeInTheDocument();
    expect(within(row).queryByLabelText(tt('ui.agent_activity.running'))).toBeNull();
  });

  it('ready chat 时列表行有「等待用户」', async () => {
    useAppStore.setState({
      chats: [chat({ Status: 'ready' })],
    });
    renderList();
    const row = await findRow('修复上传白名单');
    expect(within(row).getByLabelText(tt('ui.agent_activity.waiting'))).toBeInTheDocument();
  });

  it('running chat 时列表行有「执行中」', async () => {
    useAppStore.setState({ chats: [chat({ Status: 'running' })] });
    renderList();
    const row = await findRow('修复上传白名单');
    expect(within(row).getByLabelText(tt('ui.agent_activity.running'))).toBeInTheDocument();
    expect(within(row).queryByLabelText(tt('ui.agent_activity.waiting'))).toBeNull();
  });

  it('终端 exited 时列表行无活动图标', async () => {
    useAppStore.setState({
      terminals: [terminal({ Status: 'exited', ExitCode: 0 })],
    });
    renderList();
    const row = await findRow('修复上传白名单');
    expect(within(row).queryByLabelText(tt('ui.agent_activity.waiting'))).toBeNull();
    expect(within(row).queryByLabelText(tt('ui.agent_activity.running'))).toBeNull();
    expect(within(row).getByRole('button', { name: tt('ui.session_list.activate') })).toBeInTheDocument();
  });

  it('用户已发送且磁盘还没有时，列表立刻出现该会话', async () => {
    useAppStore.setState({
      terminals: [terminal({
        ID: 't9',
        Kind: 'new',
        SessionID: '',
        Title: '修复登录空指针',
        Prompted: true,
        Workspace: 'D:\\proj-a',
        ToolID: 'claude',
      })],
    });
    renderList();
    const row = await findRow('修复登录空指针');
    fireEvent.click(row);
    expect(onSelectRow).toHaveBeenCalledWith(expect.objectContaining({
      Title: '修复登录空指针',
      Path: 'live:t9',
    }));
  });

  it('已绑定到磁盘会话的不再重复插入', async () => {
    useAppStore.setState({
      terminals: [terminal({ SessionID: 's1', Prompted: true, Title: '不应重复' })],
    });
    renderList();
    expect(await screen.findByText('修复上传白名单')).toBeInTheDocument();
    expect(screen.queryByText('不应重复')).not.toBeInTheDocument();
  });

  it('未勾选归档时隐藏已归档会话', async () => {
    useAppStore.setState({ archivedIDs: ['s1'] });
    renderList();
    expect(await screen.findByText('重构登录页')).toBeInTheDocument();
    expect(screen.queryByText('修复上传白名单')).not.toBeInTheDocument();
  });

  it('开发列表磁盘会话可行上点归档图标，不触发行点击', async () => {
    const onArchive = vi.fn();
    render(
      <SessionList
        workspacePath={'D:\\proj-a'}
        onSelectRow={onSelectRow}
        onActivate={onActivate}
        onArchive={onArchive}
      />,
    );
    const row = await findRow('修复上传白名单');
    fireEvent.click(within(row).getByRole('button', { name: tt('ui.session_list.archive') }));
    expect(onArchive).toHaveBeenCalledWith(expect.objectContaining({ ID: 's1' }));
    expect(onSelectRow).not.toHaveBeenCalled();
  });

  it('尚未落盘的 live 行没有归档按钮', async () => {
    useAppStore.setState({
      terminals: [terminal({
        ID: 't9',
        Kind: 'new',
        SessionID: '',
        Title: '修复登录空指针',
        Prompted: true,
        Workspace: 'D:\\proj-a',
        ToolID: 'claude',
      })],
    });
    renderList();
    const row = await findRow('修复登录空指针');
    expect(within(row).queryByRole('button', { name: tt('ui.session_list.archive') })).not.toBeInTheDocument();
  });

  it('勾选归档后只显示已归档会话，点击还原', async () => {
    const onRestore = vi.fn();
    useAppStore.setState({ archivedIDs: ['s2'] });
    render(
      <SessionList
        workspacePath={'D:\\proj-a'}
        showArchived
        onRestore={onRestore}
      />,
    );
    const row = await findRow('重构登录页');
    expect(within(row).getByRole('img', { name: tt('ui.session_list.archived') })).toBeInTheDocument();
    expect(screen.queryByText('修复上传白名单')).not.toBeInTheDocument();
    fireEvent.click(within(row).getByRole('button', { name: tt('ui.session_list.restore') }));
    expect(onRestore).toHaveBeenCalledWith(expect.objectContaining({ ID: 's2' }));
    expect(within(row).queryByRole('button', { name: tt('ui.session_list.archive') })).not.toBeInTheDocument();
  });

  it('ssh 工作区走 ScanRemoteSessions，不调本地 GetSessions', async () => {
    const ref = 'ssh://c1/home/u/proj';
    const remote: Session[] = [
      {
        ID: 'r1',
        ToolID: 'claude',
        Workspace: ref,
        Title: '远端会话',
        CreatedAt: minutesAgo(3),
        UpdatedAt: minutesAgo(1),
        Messages: 2,
        Path: '/home/u/.claude/projects/x/r1.jsonl',
      },
    ];
    mocks.scanRemoteSessions.mockResolvedValue(remote);
    render(
      <SessionList workspacePath={ref} onSelectRow={onSelectRow} onActivate={onActivate} />,
    );
    expect(await screen.findByText('远端会话')).toBeInTheDocument();
    expect(mocks.scanRemoteSessions).toHaveBeenCalledWith(ref);
    expect(mocks.getSessions).not.toHaveBeenCalled();
    expect(useAppStore.getState().unreachableWorkspaces[ref]).toBeUndefined();
  });

  it('ssh 远端扫描与缓存均失败时 toast、标不可达并展示空态文案', async () => {
    const ref = 'ssh://c1/home/u/down';
    mocks.scanRemoteSessions.mockRejectedValueOnce(new Error('err.ssh.exec_failed|timeout'));
    mocks.getRemoteSessions.mockRejectedValueOnce(new Error('err.ssh.exec_failed|timeout'));
    render(
      <SessionList workspacePath={ref} onSelectRow={onSelectRow} onActivate={onActivate} />,
    );
    expect(await screen.findByText(tt('ui.session_list.remote_unreachable'))).toBeInTheDocument();
    expect(useAppStore.getState().unreachableWorkspaces[ref]).toBe(true);
    expect(
      useAppStore.getState().toasts.some(
        (t) => t.tone === 'error' && t.title.includes(tt('ui.session_list.remote_scan_failed').split('{{')[0]),
      ),
    ).toBe(true);
  });

  it('ssh 扫描失败且 getRemoteSessions 返回空数组时仍标不可达', async () => {
    const ref = 'ssh://c1/home/u/empty-cache';
    mocks.scanRemoteSessions.mockRejectedValueOnce(new Error('err.ssh.exec_failed|timeout'));
    mocks.getRemoteSessions.mockResolvedValueOnce([]);
    render(
      <SessionList workspacePath={ref} onSelectRow={onSelectRow} onActivate={onActivate} />,
    );
    expect(await screen.findByText(tt('ui.session_list.remote_unreachable'))).toBeInTheDocument();
    expect(useAppStore.getState().unreachableWorkspaces[ref]).toBe(true);
    expect(
      useAppStore.getState().toasts.some(
        (t) => t.tone === 'error' && t.title.includes(tt('ui.session_list.remote_scan_failed').split('{{')[0]),
      ),
    ).toBe(true);
  });

  it('ssh 扫描失败但有缓存时展示列表、toast，且不 clearUnreachable', async () => {
    const ref = 'ssh://c1/home/u/cached';
    const remote: Session[] = [
      {
        ID: 'r-cache',
        ToolID: 'claude',
        Workspace: ref,
        Title: '缓存会话',
        CreatedAt: minutesAgo(3),
        UpdatedAt: minutesAgo(1),
        Messages: 2,
        Path: '/home/u/.claude/projects/x/r-cache.jsonl',
      },
    ];
    useAppStore.setState({ unreachableWorkspaces: { [ref]: true } });
    mocks.scanRemoteSessions.mockRejectedValueOnce(new Error('err.ssh.exec_failed|timeout'));
    mocks.getRemoteSessions.mockResolvedValueOnce(remote);
    render(
      <SessionList workspacePath={ref} onSelectRow={onSelectRow} onActivate={onActivate} />,
    );
    expect(await screen.findByText('缓存会话')).toBeInTheDocument();
    expect(useAppStore.getState().unreachableWorkspaces[ref]).toBe(true);
    expect(
      useAppStore.getState().toasts.some(
        (t) => t.tone === 'error' && t.title.includes(tt('ui.session_list.remote_scan_failed').split('{{')[0]),
      ),
    ).toBe(true);
  });
});

describe('SessionList 批量多选', () => {
  // tt 不做插值，测试里手动替换占位符（沿用 msgs 的做法）
  const fmt = (key: string, params: Record<string, string | number>) =>
    Object.entries(params).reduce(
      (acc, [k, v]) => acc.split(`{{${k}}}`).join(String(v)),
      tt(key),
    );

  const liveTerm = () =>
    terminal({
      ID: 't9',
      Kind: 'new',
      SessionID: '',
      Title: '修复登录空指针',
      Prompted: true,
      Workspace: 'D:\\proj-a',
      ToolID: 'claude',
    });

  // 等列表渲染完成后点「多选」进入选择模式（按钮可访问名来自 aria-label）
  const enterSelectMode = async () => {
    await findRow('清理构建缓存');
    fireEvent.click(screen.getByRole('button', { name: tt('ui.session_list.select_mode_aria') }));
    return screen.getByRole('checkbox', { name: tt('ui.session_list.select_all') });
  };

  const rowCheckbox = (row: HTMLElement) => within(row).getByRole('checkbox');

  it('进入多选模式：显示全选行与每行复选框，live 行复选框禁用', async () => {
    useAppStore.setState({ terminals: [liveTerm()] });
    renderList();
    const selectAll = await enterSelectMode();

    // 全选行 + 计数文案（live 行计入总数但不参与勾选）
    expect(selectAll).toBeInTheDocument();
    expect(
      screen.getByText(fmt('ui.session_list.selected_count', { selected: 0, total: 4 })),
    ).toBeInTheDocument();

    // 磁盘行复选框可用，live 行复选框禁用
    for (const title of ['修复上传白名单', '重构登录页', '清理构建缓存']) {
      expect(rowCheckbox(await findRow(title))).toBeEnabled();
    }
    expect(rowCheckbox(await findRow('修复登录空指针'))).toBeDisabled();

    // 选择模式下磁盘行点击 = 切换勾选，不触发 onSelectRow
    const row = await findRow('修复上传白名单');
    fireEvent.click(row);
    expect(rowCheckbox(row)).toBeChecked();
    expect(onSelectRow).not.toHaveBeenCalled();
    // live 行点击仍走 onSelectRow
    fireEvent.click(await findRow('修复登录空指针'));
    expect(onSelectRow).toHaveBeenCalledWith(expect.objectContaining({ Title: '修复登录空指针' }));
  });

  it('全选：可见磁盘会话全部勾选；部分勾选时全选框呈半选态；再点取消全选', async () => {
    renderList();
    const selectAll = await enterSelectMode();

    // 勾选一项 → 半选态
    fireEvent.click(rowCheckbox(await findRow('修复上传白名单')));
    expect(rowCheckbox(await findRow('修复上传白名单'))).toBeChecked();
    expect((selectAll as HTMLInputElement).indeterminate).toBe(true);
    expect(
      screen.getByText(fmt('ui.session_list.selected_count', { selected: 1, total: 3 })),
    ).toBeInTheDocument();

    // 点全选 → 三个磁盘会话全部勾选，半选态消失
    fireEvent.click(selectAll);
    for (const title of ['修复上传白名单', '重构登录页', '清理构建缓存']) {
      expect(rowCheckbox(await findRow(title))).toBeChecked();
    }
    expect((selectAll as HTMLInputElement).indeterminate).toBe(false);
    expect(selectAll).toBeChecked();
    expect(
      screen.getByText(fmt('ui.session_list.selected_count', { selected: 3, total: 3 })),
    ).toBeInTheDocument();

    // 再点全选 → 清空
    fireEvent.click(selectAll);
    for (const title of ['修复上传白名单', '重构登录页', '清理构建缓存']) {
      expect(rowCheckbox(await findRow(title))).not.toBeChecked();
    }
    expect(selectAll).not.toBeChecked();
  });

  it('勾选 2 项 → 归档所选 → 确认 → BatchArchiveSessions 以正确 ids 调用，archivedIDs 更新并退出选择模式', async () => {
    // 故意返回 1（≠ ids.length 2）：验证通知计数用 API 实际变更数而非提交数
    mocks.batchArchiveSessions.mockResolvedValue(1);
    renderList();
    await enterSelectMode();

    fireEvent.click(rowCheckbox(await findRow('修复上传白名单')));
    fireEvent.click(rowCheckbox(await findRow('清理构建缓存')));

    fireEvent.click(screen.getByRole('button', { name: tt('ui.session_list.batch_archive') }));
    expect(await screen.findByText(tt('ui.session_list.batch_archive_confirm_title'))).toBeInTheDocument();
    expect(
      screen.getByText(fmt('ui.session_list.batch_archive_confirm_body', { count: 2 })),
    ).toBeInTheDocument();

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.session_list.batch_confirm_ok') }));
    });

    expect(mocks.batchArchiveSessions).toHaveBeenCalledTimes(1);
    expect(mocks.batchArchiveSessions).toHaveBeenCalledWith(['s1', 's4']);
    expect(useAppStore.getState().archivedIDs).toEqual(expect.arrayContaining(['s1', 's4']));
    expect(useAppStore.getState().toasts.some((toast) =>
      toast.tone === 'success' &&
      toast.title === fmt('ui.session_list.batch_done', { count: 1 }),
    )).toBe(true);
    // 成功后清空选择并退出选择模式
    expect(screen.queryByRole('button', { name: tt('ui.session_list.batch_archive') })).not.toBeInTheDocument();
    expect(screen.queryByRole('checkbox', { name: tt('ui.session_list.select_all') })).not.toBeInTheDocument();
  });

  it('live 行存在时全选磁盘会话：全选框为 checked 且非半选态', async () => {
    useAppStore.setState({ terminals: [liveTerm()] });
    renderList();
    const selectAll = await enterSelectMode();

    fireEvent.click(selectAll);
    // 3 个磁盘会话全部勾选；live 行不参与分母，不出现「半选但点击全取消」的矛盾态
    for (const title of ['修复上传白名单', '重构登录页', '清理构建缓存']) {
      expect(rowCheckbox(await findRow(title))).toBeChecked();
    }
    expect((selectAll as HTMLInputElement).indeterminate).toBe(false);
    expect(selectAll).toBeChecked();
  });

  it('提交前与当前可见磁盘会话取交集：过滤后不可见的残留勾选不提交', async () => {
    mocks.batchArchiveSessions.mockResolvedValue(1);
    renderList();
    const selectAll = await enterSelectMode();

    fireEvent.click(selectAll);
    // 过滤后只剩「清理构建缓存」(s4) 可见，其余勾选为不可见残留
    fireEvent.change(screen.getByLabelText(tt('ui.session_list.search_aria')), {
      target: { value: '构建' },
    });
    expect(screen.getByText('清理构建缓存')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: tt('ui.session_list.batch_archive') }));
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.session_list.batch_confirm_ok') }));
    });

    expect(mocks.batchArchiveSessions).toHaveBeenCalledTimes(1);
    expect(mocks.batchArchiveSessions).toHaveBeenCalledWith(['s4']);
  });

  it('showArchived 下对称：确认后走 BatchRestoreSessions，archivedIDs 移除所选', async () => {
    mocks.batchRestoreSessions.mockResolvedValue(1);
    useAppStore.setState({ archivedIDs: ['s2'] });
    render(<SessionList workspacePath={'D:\\proj-a'} showArchived onRestore={vi.fn()} />);
    await findRow('重构登录页');
    fireEvent.click(screen.getByRole('button', { name: tt('ui.session_list.select_mode_aria') }));

    fireEvent.click(rowCheckbox(await findRow('重构登录页')));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.session_list.batch_restore') }));
    expect(await screen.findByText(tt('ui.session_list.batch_restore_confirm_title'))).toBeInTheDocument();
    expect(
      screen.getByText(fmt('ui.session_list.batch_restore_confirm_body', { count: 1 })),
    ).toBeInTheDocument();

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.session_list.batch_confirm_ok') }));
    });

    expect(mocks.batchRestoreSessions).toHaveBeenCalledWith(['s2']);
    expect(useAppStore.getState().archivedIDs).toEqual([]);
    expect(screen.queryByRole('button', { name: tt('ui.session_list.batch_restore') })).not.toBeInTheDocument();
  });

  it('API reject → 通知 batch_failed，选择保留、仍处于选择模式', async () => {
    mocks.batchArchiveSessions.mockRejectedValue(new Error('err.db.write_failed'));
    renderList();
    const selectAll = await enterSelectMode();

    fireEvent.click(rowCheckbox(await findRow('修复上传白名单')));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.session_list.batch_archive') }));
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.session_list.batch_confirm_ok') }));
    });

    expect(
      useAppStore.getState().toasts.some((toast) =>
        toast.tone === 'error' &&
        toast.title.includes(tt('ui.session_list.batch_failed').split('{{')[0]),
      ),
    ).toBe(true);
    // 弹窗关闭但选择与模式保留
    expect(screen.queryByText(tt('ui.session_list.batch_archive_confirm_title'))).not.toBeInTheDocument();
    expect(rowCheckbox(await findRow('修复上传白名单'))).toBeChecked();
    expect(screen.getByRole('button', { name: tt('ui.session_list.batch_archive') })).toBeInTheDocument();
    expect(
      screen.getByText(fmt('ui.session_list.selected_count', { selected: 1, total: 3 })),
    ).toBeInTheDocument();
    expect((selectAll as HTMLInputElement)).toBeInTheDocument();
  });

  it('取消按钮清空选择并退出选择模式；空列表时底部操作栏仍渲染、多选入口禁用', async () => {
    renderList();
    await enterSelectMode();

    fireEvent.click(rowCheckbox(await findRow('重构登录页')));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.session_list.batch_cancel') }));
    expect(screen.queryByRole('checkbox', { name: tt('ui.session_list.select_all') })).not.toBeInTheDocument();

    await enterSelectMode();
    // 过滤到无匹配 → 多选入口禁用；底部操作栏按 selectMode 渲染，主按钮因未勾选禁用
    fireEvent.change(screen.getByLabelText(tt('ui.session_list.search_aria')), {
      target: { value: '不存在' },
    });
    const toggle = screen.getByRole('button', { name: tt('ui.session_list.select_mode_aria') });
    expect(toggle).toBeDisabled();
    expect(screen.getByRole('button', { name: tt('ui.session_list.batch_archive') })).toBeDisabled();
    expect(screen.getByRole('button', { name: tt('ui.session_list.batch_cancel') })).toBeInTheDocument();
  });
});

