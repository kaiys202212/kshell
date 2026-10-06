import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import GitPanel from './GitPanel';
import type { GitSCMSnapshot } from '../lib/api';
import { useAppStore } from '../state/store';

const mocks = vi.hoisted(() => ({
  gitSCM: vi.fn(),
  gitStage: vi.fn().mockResolvedValue(undefined),
  gitUnstage: vi.fn().mockResolvedValue(undefined),
  gitDiscard: vi.fn().mockResolvedValue(undefined),
  gitCommit: vi.fn().mockResolvedValue(undefined),
  gitBranches: vi.fn().mockResolvedValue(['main']),
  gitCheckout: vi.fn().mockResolvedValue(undefined),
  gitCreateBranch: vi.fn().mockResolvedValue(undefined),
  gitFetch: vi.fn().mockResolvedValue(undefined),
  gitPull: vi.fn().mockResolvedValue(undefined),
  gitPush: vi.fn().mockResolvedValue(undefined),
  gitStashPush: vi.fn().mockResolvedValue(undefined),
  gitStashPop: vi.fn().mockResolvedValue(undefined),
  gitStashApply: vi.fn().mockResolvedValue(undefined),
  gitStashDrop: vi.fn().mockResolvedValue(undefined),
  gitLog: vi.fn().mockResolvedValue([
    {
      Hash: 'abc1234deadbeef',
      Parents: [],
      Author: 't',
      Email: 't@t',
      Date: '2026-01-01T00:00:00Z',
      Subject: 'init',
      Decorations: ['main'],
    },
  ]),
  gitRefs: vi.fn().mockResolvedValue([
    { Name: 'main', Short: 'main', Kind: 'local', Current: true },
    { Name: 'topic', Short: 'topic', Kind: 'local', Current: false },
    { Name: 'origin/main', Short: 'origin/main', Kind: 'remote', Current: false },
  ]),
  gitFetchAll: vi.fn().mockResolvedValue(undefined),
  gitCommitStat: vi.fn().mockResolvedValue({ Files: 19, Insertions: 1867, Deletions: 202 }),
}));
vi.mock('../lib/api', () => mocks);
vi.mock('../lib/git', () => ({ refreshGitStatus: vi.fn().mockResolvedValue(undefined) }));

const snap = (over: Partial<GitSCMSnapshot> = {}): GitSCMSnapshot => ({
  IsRepo: true,
  RepoRel: '',
  Branch: 'main',
  Remotes: ['origin'],
  SyncRemote: 'origin',
  HasUpstream: true,
  Ahead: 1,
  Behind: 0,
  Repos: [{ Rel: '', Path: 'D:\\proj', Branch: 'main' }],
  Entries: [
    { Path: 'staged.go', X: 'M', Y: ' ', Staged: true, Unstaged: false, Untracked: false, Conflicted: false },
    { Path: 'dirty.go', X: ' ', Y: 'M', Staged: false, Unstaged: true, Untracked: false, Conflicted: false },
  ],
  Stashes: [],
  ...over,
});

afterEach(cleanup);

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  localStorage.clear();
  useAppStore.setState({ toasts: [] });
  mocks.gitSCM.mockResolvedValue(snap());
});

describe('GitPanel', () => {
  it('可见时加载变更分组，提交说明为单行', async () => {
    render(<GitPanel wsPath="D:\\proj" visible onOpenDiff={() => {}} />);
    expect(await screen.findByText('staged.go')).toBeInTheDocument();
    expect(screen.getByText('dirty.go')).toBeInTheDocument();
    const msg = screen.getByLabelText('提交说明');
    expect(msg.tagName).toBe('INPUT');
    expect(screen.getByRole('button', { name: '主 Git 操作' })).toBeDisabled();
    fireEvent.change(msg, { target: { value: 'msg' } });
    expect(screen.getByRole('button', { name: '主 Git 操作' })).toBeEnabled();
    expect(screen.queryByRole('button', { name: '同步到远程' })).not.toBeInTheDocument();
  });

  it('无 staged 时提交禁用', async () => {
    mocks.gitSCM.mockResolvedValue(
      snap({
        Entries: [
          { Path: 'dirty.go', X: ' ', Y: 'M', Staged: false, Unstaged: true, Untracked: false, Conflicted: false },
        ],
      }),
    );
    render(<GitPanel wsPath="D:\\proj" visible onOpenDiff={() => {}} />);
    expect(await screen.findByText('dirty.go')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '主 Git 操作' })).toBeDisabled();
  });

  it('单击 Changes 行打开 working diff', async () => {
    const onOpenDiff = vi.fn();
    render(<GitPanel wsPath="D:\\proj" visible onOpenDiff={onOpenDiff} />);
    fireEvent.click(await screen.findByText('dirty.go'));
    expect(onOpenDiff).toHaveBeenCalledWith({
      repoRel: '',
      path: 'dirty.go',
      side: 'working',
      preview: true,
    });
  });

  it('丢弃取消时不调 API', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false);
    render(<GitPanel wsPath="D:\\proj" visible onOpenDiff={() => {}} />);
    fireEvent.click(await screen.findByRole('button', { name: '丢弃 dirty.go' }));
    expect(mocks.gitDiscard).not.toHaveBeenCalled();
  });

  it('有未提交时主按钮为提交，干净时为同步', async () => {
    render(<GitPanel wsPath="D:\\proj" visible onOpenDiff={() => {}} />);
    expect(await screen.findByRole('button', { name: '主 Git 操作' })).toHaveTextContent('提交');
    expect(screen.getByRole('button', { name: '更多提交操作' })).toBeInTheDocument();
    cleanup();
    mocks.gitSCM.mockResolvedValue(snap({ Entries: [], Ahead: 1, Behind: 0, HasUpstream: true }));
    render(<GitPanel wsPath="D:\\proj" visible onOpenDiff={() => {}} />);
    expect(await screen.findByRole('button', { name: '主 Git 操作' })).toHaveTextContent('同步');
  });

  it('切换分支筛选会按 ref/all 重新拉取日志并记住选择', async () => {
    const ws = 'D:/proj';
    render(<GitPanel wsPath={ws} visible onOpenDiff={() => {}} />);
    await screen.findByText('init');
    expect(mocks.gitLog).toHaveBeenCalledWith(ws, '', 'current', '', 200);

    fireEvent.change(screen.getByLabelText('提交图分支筛选'), { target: { value: 'topic' } });
    await waitFor(() => {
      expect(mocks.gitLog).toHaveBeenCalledWith(ws, '', 'ref', 'topic', 200);
    });
    expect(localStorage.getItem(`kshell-git-log-sel:${ws}\0`)).toBe('topic');

    fireEvent.change(screen.getByLabelText('提交图分支筛选'), { target: { value: 'all' } });
    await waitFor(() => {
      expect(mocks.gitLog).toHaveBeenCalledWith(ws, '', 'all', '', 200);
    });
  });

  it('多仓库时选择器有满宽类', async () => {
    mocks.gitSCM.mockResolvedValue(
      snap({
        Repos: [
          { Rel: '', Path: 'D:/proj', Branch: 'main' },
          { Rel: 'nested/very/long/path/name', Path: 'D:/proj/nested', Branch: 'dev' },
        ],
      }),
    );
    render(<GitPanel wsPath="D:/proj" visible onOpenDiff={() => {}} />);
    const sel = await screen.findByLabelText('选择仓库');
    expect(sel.className).toMatch(/w-full/);
    expect(sel.className).toMatch(/min-w-0/);
  });

  it('提交图显示日志并提供 Fetch all', async () => {
    render(<GitPanel wsPath="D:\\proj" visible onOpenDiff={() => {}} />);
    expect(await screen.findByText('init')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Fetch all' })).toBeInTheDocument();
    expect(screen.getByLabelText('提交图分支筛选')).toBeInTheDocument();
  });
});
