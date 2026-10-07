// 首页工作区卡片测试：按最后活动时间倒序（零值排最后，再按名称）/ 卡片信息（会话数、
// 相对时间、git 徽标、工具分布折叠）/ 点击整卡打开页签 / 重新扫描的扫描状态机 /
// 项目表操作（新建、删除、回收站还原、projects:changed 刷新）。
// api 层整体打桩（vi.mock），与 App.test.tsx 同一套模式。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import Home from './Home';
import type { Workspace } from '../lib/api';
import { useAppStore } from '../state/store';
import { tt } from '../test/i18n';

// 会话数走复数 key（en 规则 one/other），相对时间走 time.* 命名插值
const count = (n: number) =>
  tt(`ui.home.session_count_${n === 1 ? 'one' : 'other'}`).replace('{{count}}', String(n));
const lastActive = (rel: string) => tt('ui.home.last_active').replace('{{rel}}', rel);
const minutesAgoText = (n: number) => tt('time.minutes_ago').replace('{{n}}', String(n));

const mocks = vi.hoisted(() => ({
  getWorkspaces: vi.fn(),
  scanSessions: vi.fn(),
  onScanDone: vi.fn(),
  createProject: vi.fn(),
  hideProject: vi.fn(),
  restoreProject: vi.fn(),
  getDeletedProjects: vi.fn(),
  onProjectsChanged: vi.fn(),
}));
vi.mock('../lib/api', () => mocks);

const minutesAgo = (m: number) => new Date(Date.now() - m * 60_000).toISOString();

// Go 侧零值时间：git 扫描发现但从未开过会话的工作区
const ZERO_TIME = '0001-01-01T00:00:00Z';

function ws(over: Partial<Workspace> & Pick<Workspace, 'Path' | 'Name'>): Workspace {
  return { LastUsed: '', SessionCount: 0, ToolCounts: {}, Source: 'sessions', ...over };
}

// 覆盖：正常时间 / 零值时间（git 来源）/ 空字符串时间（旧缓存缺字段）
const workspaces: Workspace[] = [
  ws({
    Path: 'D:\\proj-old',
    Name: 'proj-old',
    LastUsed: minutesAgo(600),
    SessionCount: 2,
    ToolCounts: { claude: 2 },
  }),
  ws({ Path: 'D:\\aaa-zero', Name: 'aaa-zero', LastUsed: ZERO_TIME, Source: 'git' }),
  ws({
    Path: 'D:\\proj-new',
    Name: 'proj-new',
    LastUsed: minutesAgo(5),
    SessionCount: 4,
    ToolCounts: { codex: 4, claude: 3, gemini: 2, codebuddy: 1 },
  }),
  ws({ Path: 'D:\\git-only', Name: 'git-only', LastUsed: '', Source: 'git' }),
];

let scanDoneCb: (payload: unknown) => void = () => {};

// 整卡是一个 button，用 title=完整路径定位，避免与卡片内文案撞车
const card = (path: string) => screen.getByTitle(path);
// 卡片名称 span（truncate 单行截断）——用于断言排序
const cardOrder = () =>
  screen.getAllByRole('listitem').map((li) => li.querySelector('[data-testid="ws-name"]')?.textContent ?? '');

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  mocks.onScanDone.mockImplementation((cb: typeof scanDoneCb) => {
    scanDoneCb = cb;
    return () => {};
  });
  mocks.getWorkspaces.mockResolvedValue(workspaces);
  mocks.scanSessions.mockResolvedValue(undefined);
  mocks.onProjectsChanged.mockImplementation(() => () => {});
  mocks.createProject.mockResolvedValue('');
  mocks.hideProject.mockResolvedValue(undefined);
  mocks.restoreProject.mockResolvedValue(undefined);
  mocks.getDeletedProjects.mockResolvedValue([]);
  useAppStore.setState({
    workspaces: [],
    openTabs: [],
    activeTabId: null,
    scanState: 'idle',
    terminals: [],
    toasts: [],
  });
});

describe('Home', () => {
  it('按最后活动时间倒序渲染，零值/空值的时间排最后（再按名称）', async () => {
    render(<Home />);
    await screen.findByTitle('D:\\proj-new');

    // proj-new(5 分钟) → proj-old(10 小时) → aaa-zero/git-only（未使用过，按名称排）
    expect(cardOrder()).toEqual(['proj-new', 'proj-old', 'aaa-zero', 'git-only']);
  });

  it('卡片显示会话数与最后活动相对时间；git 徽标只在 git 来源出现', async () => {
    render(<Home />);
    const fresh = await screen.findByTitle('D:\\proj-new');

    expect(within(fresh).getByText(count(4))).toBeInTheDocument();
    expect(within(fresh).getByText(lastActive(minutesAgoText(5)))).toBeInTheDocument();
    expect(within(fresh).queryByText('git')).toBeNull();

    const gitOnly = card('D:\\git-only');
    expect(within(gitOnly).getByText('git')).toBeInTheDocument();
    expect(within(gitOnly).getByText(count(0))).toBeInTheDocument();
    expect(within(gitOnly).getByText(tt('ui.home.never_used'))).toBeInTheDocument();
    const lastActivePrefix = tt('ui.home.last_active').split('{{')[0];
    expect(within(gitOnly).queryByText((c) => c.includes(lastActivePrefix))).toBeNull();
  });

  it('点击整卡打开工作区页签（键盘可达：整卡是 button）', async () => {
    render(<Home />);
    const fresh = await screen.findByTitle('D:\\proj-new');
    expect(fresh.tagName).toBe('BUTTON');

    fireEvent.click(fresh);

    expect(useAppStore.getState().openTabs).toEqual([{ id: 'D:\\proj-new', name: 'proj-new' }]);
    expect(useAppStore.getState().activeTabId).toBe('D:\\proj-new');
  });

  it('扫描中「重新扫描」按钮禁用并显示「扫描中…」，scan:done 后恢复可再扫', async () => {
    render(<Home />);
    await screen.findByTitle('D:\\proj-new');

    expect(mocks.scanSessions).toHaveBeenCalledTimes(1);
    expect(useAppStore.getState().scanState).toBe('scanning');
    expect(screen.getByRole('button', { name: tt('ui.home.scanning') })).toBeDisabled();
    expect(screen.queryByRole('button', { name: tt('ui.home.rescan') })).toBeNull();

    await act(async () => {
      scanDoneCb({});
    });
    expect(useAppStore.getState().scanState).toBe('done');
    expect(screen.queryByRole('button', { name: tt('ui.home.scanning') })).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: tt('ui.home.rescan') }));
    expect(mocks.scanSessions).toHaveBeenCalledTimes(2);
    expect(useAppStore.getState().scanState).toBe('scanning');
  });

  it('工具分布最多 3 个，其余折叠成 +N', async () => {
    render(<Home />);
    const fresh = await screen.findByTitle('D:\\proj-new');

    expect(within(fresh).getByText('Codex')).toBeInTheDocument();
    expect(within(fresh).getByText('Claude')).toBeInTheDocument();
    expect(within(fresh).getByText('Gemini')).toBeInTheDocument();
    expect(within(fresh).queryByText('CodeBuddy')).toBeNull(); // 第 4 个被折叠
    expect(within(fresh).getByText('+1')).toBeInTheDocument();

    // 只有一个工具的工作区不出现 +N
    const old = card('D:\\proj-old');
    expect(within(old).getByText('Claude')).toBeInTheDocument();
    expect(within(old).queryByText(/^\+\d+$/)).toBeNull();
  });

  it('空态：扫描未完成用骨架屏占位，scan:done 后仍为空才显示「未发现工作区」', async () => {
    mocks.getWorkspaces.mockResolvedValue([]);
    useAppStore.setState({ scanState: 'idle' });
    const { container } = render(<Home />);

    expect(container.querySelectorAll('.animate-pulse').length).toBeGreaterThan(0);
    expect(screen.queryByText(tt('ui.home.empty'))).toBeNull();

    await act(async () => {
      scanDoneCb({});
    });
    expect(await screen.findByText(tt('ui.home.empty'))).toBeInTheDocument();
    expect(screen.getByText(count(0))).toBeInTheDocument();
  });

  it('新建项目：调绑定、提示并刷新列表', async () => {
    mocks.createProject.mockResolvedValue('D:\\new-proj');
    render(<Home />);
    await screen.findByTitle('D:\\proj-new');
    const before = mocks.getWorkspaces.mock.calls.length;

    fireEvent.click(screen.getByRole('button', { name: tt('ui.home.create_project') }));

    await waitFor(() => expect(mocks.createProject).toHaveBeenCalledTimes(1));
    expect(
      useAppStore.getState().toasts.some((t) => t.title === tt('ui.home.added').replace('{{name}}', 'new-proj')),
    ).toBe(true);
    await waitFor(() => expect(mocks.getWorkspaces.mock.calls.length).toBeGreaterThan(before));
  });

  it('新建项目取消（返回空串）：不提示、不刷新', async () => {
    mocks.createProject.mockResolvedValue('');
    render(<Home />);
    await screen.findByTitle('D:\\proj-new');
    const before = mocks.getWorkspaces.mock.calls.length;

    fireEvent.click(screen.getByRole('button', { name: tt('ui.home.create_project') }));

    await waitFor(() => expect(mocks.createProject).toHaveBeenCalledTimes(1));
    expect(useAppStore.getState().toasts).toHaveLength(0);
    expect(mocks.getWorkspaces.mock.calls.length).toBe(before);
  });

  it('删除项目：卡片删除按钮调绑定并提示可从回收站还原', async () => {
    render(<Home />);
    await screen.findByTitle('D:\\proj-new');

    fireEvent.click(screen.getByLabelText(tt('ui.home.delete_project').replace('{{name}}', 'proj-new')));

    await waitFor(() => expect(mocks.hideProject).toHaveBeenCalledWith('D:\\proj-new'));
    expect(
      useAppStore.getState().toasts.some((t) => t.title === tt('ui.home.deleted').replace('{{name}}', 'proj-new')),
    ).toBe(true);
  });

  it('回收站：列出已删除项目，点还原调绑定并提示', async () => {
    mocks.getDeletedProjects.mockResolvedValue([
      { path: 'D:\\gone', name: 'gone', at: minutesAgo(30), exists: true },
      { path: 'D:\\lost', name: 'lost', at: minutesAgo(5), exists: false },
    ]);
    render(<Home />);
    await screen.findByTitle('D:\\proj-new');

    const binButton = await screen.findByRole('button', {
      name: (n) => n.startsWith(tt('ui.home.recycle_bin')),
    });
    await waitFor(() => expect(binButton).toBeEnabled());
    expect(binButton.textContent).toContain('(2)');

    fireEvent.click(binButton);
    expect(await screen.findByText('D:\\gone')).toBeInTheDocument();
    expect(screen.getByText(tt('ui.home.dir_missing'))).toBeInTheDocument();

    // 「还原」按钮每行一个：点第一条（gone）
    fireEvent.click(screen.getAllByRole('button', { name: tt('ui.home.restore') })[0]);

    await waitFor(() => expect(mocks.restoreProject).toHaveBeenCalledWith('D:\\gone'));
    expect(useAppStore.getState().toasts.some((t) => t.title === tt('ui.home.restored').replace('{{name}}', 'gone'))).toBe(true);
  });

  it('projects:changed 事件：重拉工作区与回收站列表', async () => {
    let projectsCb: (() => void) | undefined;
    mocks.onProjectsChanged.mockImplementation((cb: () => void) => {
      projectsCb = cb;
      return () => {};
    });
    render(<Home />);
    await screen.findByTitle('D:\\proj-new');
    const wsCalls = mocks.getWorkspaces.mock.calls.length;
    const delCalls = mocks.getDeletedProjects.mock.calls.length;

    await act(async () => {
      projectsCb?.();
    });

    await waitFor(() => {
      expect(mocks.getWorkspaces.mock.calls.length).toBeGreaterThan(wsCalls);
      expect(mocks.getDeletedProjects.mock.calls.length).toBeGreaterThan(delCalls);
    });
  });
});
