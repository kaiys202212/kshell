// SessionList 组件测试：排序 / 工作区过滤 / 关键词过滤 / 恢复与聚焦 / 事件还原。
// api 层整体打桩（vi.mock），事件回调通过桩捕获后手动触发。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import SessionList from './SessionList';
import type { Session } from '../lib/api';
import { useAppStore } from '../state/store';

const mocks = vi.hoisted(() => ({
  getSessions: vi.fn(),
  resumeSession: vi.fn(),
  focusSession: vi.fn(),
  onScanDone: vi.fn(),
  onWindowClosed: vi.fn(),
}));
vi.mock('../lib/api', () => mocks);

const minutesAgo = (m: number) => new Date(Date.now() - m * 60_000).toISOString();

// 三个会话属于 D:\proj-a（Workspace 大小写混排验证不敏感匹配），一个属于其他工作区
const sessions: Session[] = [
  { ID: 's1', ToolID: 'codebuddy', Workspace: 'd:\\proj-a', Title: '修复上传白名单', CreatedAt: minutesAgo(30), UpdatedAt: minutesAgo(10), Messages: 12, Path: 'p1' },
  { ID: 's2', ToolID: 'claude', Workspace: 'D:\\Proj-A', Title: '重构登录页', CreatedAt: minutesAgo(200), UpdatedAt: minutesAgo(120), Messages: 30, Path: 'p2' },
  { ID: 's3', ToolID: 'gemini', Workspace: 'd:\\other', Title: '其他工作区会话', CreatedAt: minutesAgo(5), UpdatedAt: minutesAgo(5), Messages: 3, Path: 'p3' },
  { ID: 's4', ToolID: 'codex', Workspace: 'D:\\proj-a', Title: '清理构建缓存', CreatedAt: minutesAgo(60), UpdatedAt: minutesAgo(2), Messages: 5, Path: 'p4' },
];

let scanDoneCb: (payload: unknown) => void = () => {};
let closedCb: (title: string) => void = () => {};

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  mocks.onScanDone.mockImplementation((cb: typeof scanDoneCb) => {
    scanDoneCb = cb;
    return () => {};
  });
  mocks.onWindowClosed.mockImplementation((cb: typeof closedCb) => {
    closedCb = cb;
    return () => {};
  });
  mocks.getSessions.mockResolvedValue(sessions);
  mocks.resumeSession.mockResolvedValue(undefined);
  mocks.focusSession.mockResolvedValue(true);
  useAppStore.setState({ windowStatus: {} });
});

// 按标题找会话行（li 元素）
async function findRow(title: string): Promise<HTMLElement> {
  const text = await screen.findByText(title);
  const row = text.closest('li');
  if (!row) throw new Error(`找不到会话行: ${title}`);
  return row;
}

describe('SessionList', () => {
  it('只显示当前工作区的会话（路径大小写不敏感），按 updatedAt 降序排列', async () => {
    render(<SessionList workspacePath={'D:\\proj-a'} />);

    await findRow('清理构建缓存');
    const titles = screen
      .getAllByRole('listitem')
      .map((li) => li.querySelector('.session-title')?.textContent);
    expect(titles).toEqual(['清理构建缓存', '修复上传白名单', '重构登录页']);
    expect(screen.queryByText('其他工作区会话')).not.toBeInTheDocument();

    // 工具徽标与消息数
    expect(screen.getByText('CodeBuddy')).toBeInTheDocument();
    expect(screen.getByText('12 条')).toBeInTheDocument();
  });

  it('过滤词匹配标题 / 工具名', async () => {
    render(<SessionList workspacePath={'D:\\proj-a'} />);
    await findRow('清理构建缓存');

    const input = screen.getByLabelText('过滤会话');

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
    expect(screen.getByText('没有匹配的会话')).toBeInTheDocument();
  });

  it('点击「恢复」调用 ResumeSession，并把行状态置 open', async () => {
    render(<SessionList workspacePath={'D:\\proj-a'} />);
    const row = await findRow('修复上传白名单');

    await act(async () => {
      fireEvent.click(within(row).getByRole('button', { name: '恢复' }));
    });

    expect(mocks.resumeSession).toHaveBeenCalledWith('s1');
    expect(useAppStore.getState().windowStatus['kshell · 修复上传白名单']).toBe(true);
    expect(row).toHaveClass('session-item--open');
  });

  it('收到 window:closed 事件后还原 open 状态', async () => {
    render(<SessionList workspacePath={'D:\\proj-a'} />);
    const row = await findRow('修复上传白名单');

    await act(async () => {
      fireEvent.click(within(row).getByRole('button', { name: '恢复' }));
    });
    expect(row).toHaveClass('session-item--open');

    await act(async () => {
      closedCb('kshell · 修复上传白名单');
    });
    expect(row).not.toHaveClass('session-item--open');
    expect(useAppStore.getState().windowStatus['kshell · 修复上传白名单']).toBe(false);
  });

  it('已 open 的会话点击「恢复」走 FocusSession 而非 ResumeSession', async () => {
    useAppStore.setState({ windowStatus: { 'kshell · 重构登录页': true } });
    render(<SessionList workspacePath={'D:\\proj-a'} />);
    const row = await findRow('重构登录页');

    await act(async () => {
      fireEvent.click(within(row).getByRole('button', { name: '恢复' }));
    });

    expect(mocks.focusSession).toHaveBeenCalledWith('s2');
    expect(mocks.resumeSession).not.toHaveBeenCalled();
  });

  it('FocusSession 返回 false（窗口实际已关）时还原状态', async () => {
    mocks.focusSession.mockResolvedValue(false);
    useAppStore.setState({ windowStatus: { 'kshell · 重构登录页': true } });
    render(<SessionList workspacePath={'D:\\proj-a'} />);
    const row = await findRow('重构登录页');

    await act(async () => {
      fireEvent.click(within(row).getByRole('button', { name: '恢复' }));
    });

    expect(mocks.focusSession).toHaveBeenCalledWith('s2');
    expect(useAppStore.getState().windowStatus['kshell · 重构登录页']).toBe(false);
    expect(row).not.toHaveClass('session-item--open');
  });

  it('收到 scan:done 事件后重调 GetSessions 刷新', async () => {
    mocks.getSessions.mockResolvedValueOnce([]).mockResolvedValueOnce(sessions);
    render(<SessionList workspacePath={'D:\\proj-a'} />);
    expect(screen.queryByText('修复上传白名单')).not.toBeInTheDocument();

    await act(async () => {
      scanDoneCb({});
    });

    expect(await screen.findByText('修复上传白名单')).toBeInTheDocument();
    expect(mocks.getSessions).toHaveBeenCalledTimes(2);
  });

  it('GetSessions 失败不崩溃，scan:done 后恢复刷新', async () => {
    mocks.getSessions.mockRejectedValueOnce(new Error('绑定异常')).mockResolvedValueOnce(sessions);
    render(<SessionList workspacePath={'D:\\proj-a'} />);
    expect(screen.queryByText('修复上传白名单')).not.toBeInTheDocument();

    await act(async () => {
      scanDoneCb({});
    });

    expect(await screen.findByText('修复上传白名单')).toBeInTheDocument();
  });

  it('标题互为前缀的两个会话，关其一不影响另一（严格相等匹配）', async () => {
    const prefixSessions: Session[] = [
      { ID: 'p1', ToolID: 'codex', Workspace: 'D:\\proj-a', Title: '任务A', CreatedAt: minutesAgo(10), UpdatedAt: minutesAgo(10), Messages: 1, Path: 'q1' },
      { ID: 'p2', ToolID: 'codex', Workspace: 'D:\\proj-a', Title: '任务A续', CreatedAt: minutesAgo(9), UpdatedAt: minutesAgo(9), Messages: 2, Path: 'q2' },
    ];
    mocks.getSessions.mockResolvedValue(prefixSessions);
    useAppStore.setState({
      windowStatus: { 'kshell · 任务A': true, 'kshell · 任务A续': true },
    });
    render(<SessionList workspacePath={'D:\\proj-a'} />);

    const rowA = await findRow('任务A');
    const rowA2 = await findRow('任务A续');
    expect(rowA).toHaveClass('session-item--open');
    expect(rowA2).toHaveClass('session-item--open');

    // "kshell · 任务A" 是 "kshell · 任务A续" 的前缀：旧 startsWith 匹配会误伤后者
    await act(async () => {
      closedCb('kshell · 任务A');
    });
    expect(rowA).not.toHaveClass('session-item--open');
    expect(rowA2).toHaveClass('session-item--open');
    const { windowStatus } = useAppStore.getState();
    expect(windowStatus['kshell · 任务A']).toBe(false);
    expect(windowStatus['kshell · 任务A续']).toBe(true);
  });

  it('空态区分「扫描中」与「确实没有」', async () => {
    mocks.getSessions.mockResolvedValue([]);

    // 扫描未完成（idle/scanning）
    useAppStore.setState({ scanState: 'scanning' });
    const { unmount } = render(<SessionList workspacePath={'D:\\proj-a'} />);
    expect(await screen.findByText('暂无会话，正在扫描……')).toBeInTheDocument();
    unmount();

    // 扫描已完成、该工作区确实没有会话
    useAppStore.setState({ scanState: 'done' });
    render(<SessionList workspacePath={'D:\\proj-a'} />);
    expect(await screen.findByText('该工作区暂无会话')).toBeInTheDocument();
  });

  it('onScanDone 回调把 scanState 置 done（幂等，消除死角）', async () => {
    mocks.getSessions.mockResolvedValue([]);
    useAppStore.setState({ scanState: 'scanning' });
    render(<SessionList workspacePath={'D:\\proj-a'} />);

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
