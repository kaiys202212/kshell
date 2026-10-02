// 首页工作区卡片测试：按最后活动时间倒序（零值排最后，再按名称）/ 卡片信息（会话数、
// 相对时间、git 徽标、工具分布折叠）/ 点击整卡打开页签 / 重新扫描的扫描状态机。
// api 层整体打桩（vi.mock），与 App.test.tsx 同一套模式。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import Home from './Home';
import type { Workspace } from '../lib/api';
import { useAppStore } from '../state/store';

const mocks = vi.hoisted(() => ({
  getWorkspaces: vi.fn(),
  scanSessions: vi.fn(),
  onScanDone: vi.fn(),
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
  useAppStore.setState({
    workspaces: [],
    openTabs: [],
    activeTabId: null,
    scanState: 'idle',
    terminals: [],
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

    expect(within(fresh).getByText('4 个会话')).toBeInTheDocument();
    expect(within(fresh).getByText('最后活动 5 分钟前')).toBeInTheDocument();
    expect(within(fresh).queryByText('git')).toBeNull();

    const gitOnly = card('D:\\git-only');
    expect(within(gitOnly).getByText('git')).toBeInTheDocument();
    expect(within(gitOnly).getByText('0 个会话')).toBeInTheDocument();
    expect(within(gitOnly).getByText('未使用过')).toBeInTheDocument();
    expect(within(gitOnly).queryByText(/最后活动/)).toBeNull();
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
    expect(screen.getByRole('button', { name: '扫描中…' })).toBeDisabled();
    expect(screen.queryByRole('button', { name: '重新扫描' })).toBeNull();

    await act(async () => {
      scanDoneCb({});
    });
    expect(useAppStore.getState().scanState).toBe('done');
    expect(screen.queryByRole('button', { name: '扫描中…' })).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: '重新扫描' }));
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
    expect(screen.queryByText('未发现工作区')).toBeNull();

    await act(async () => {
      scanDoneCb({});
    });
    expect(await screen.findByText('未发现工作区')).toBeInTheDocument();
    expect(screen.getByText('0 个会话')).toBeInTheDocument();
  });
});
