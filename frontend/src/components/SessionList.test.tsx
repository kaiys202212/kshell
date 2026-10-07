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
  useAppStore.setState({
    windowStatus: {},
    terminals: [],
    chats: [],
    chatPermissions: {},
    terminalBusy: {},
    archivedIDs: [],
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
    expect(within(row).queryByRole('button', { name: '恢复' })).toBeNull();
    expect(within(row).queryByRole('button', { name: '切换' })).toBeNull();
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

  it('不再提供「在外部终端打开」入口（该路径会弹系统控制台黑窗）', async () => {
    renderList();
    const row = await findRow('修复上传白名单');

    expect(within(row).queryByRole('button', { name: '在外部终端打开' })).toBeNull();
    expect(within(row).getByRole('button', { name: tt('ui.session_list.activate') })).toBeInTheDocument();
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
});

