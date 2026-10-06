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
    { Name: 'origin/main', Short: 'origin/main', Kind: 'remote', Current: false },
  ]),
  gitFetchAll: vi.fn().mockResolvedValue(undefined),
}));
vi.mock('../lib/api', () => mocks);
vi.mock('../lib/git', () => ({ refreshGitStatus: vi.fn().mockResolvedValue(undefined) }));

const snap = (over: Partial<GitSCMSnapshot> = {}): GitSCMSnapshot => ({
  IsRepo: true,
  RepoRel: '',
  Branch: 'main',
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
  useAppStore.setState({ toasts: [] });
  mocks.gitSCM.mockResolvedValue(snap());
});

describe('GitPanel', () => {
  it('可见时加载变更分组', async () => {
    render(<GitPanel wsPath="D:\\proj" visible onOpenDiff={() => {}} />);
    expect(await screen.findByText('staged.go')).toBeInTheDocument();
    expect(screen.getByText('dirty.go')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '提交' })).toBeDisabled();
    fireEvent.change(screen.getByLabelText('提交说明'), { target: { value: 'msg' } });
    expect(screen.getByRole('button', { name: '提交' })).toBeEnabled();
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
    expect(screen.getByRole('button', { name: '提交' })).toBeDisabled();
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

  it('ahead 时可同步', async () => {
    render(<GitPanel wsPath="D:\\proj" visible onOpenDiff={() => {}} />);
    expect(await screen.findByRole('button', { name: '同步' })).toBeEnabled();
  });

  it('无上游时同步禁用', async () => {
    mocks.gitSCM.mockResolvedValue(snap({ HasUpstream: false, Ahead: 0, Behind: 0 }));
    render(<GitPanel wsPath="D:\\proj" visible onOpenDiff={() => {}} />);
    expect(await screen.findByRole('button', { name: '同步' })).toBeDisabled();
  });

  it('提交图显示日志并提供 Fetch all', async () => {
    render(<GitPanel wsPath="D:\\proj" visible onOpenDiff={() => {}} />);
    expect(await screen.findByText('init')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Fetch all' })).toBeInTheDocument();
    expect(screen.getByLabelText('提交图分支筛选')).toBeInTheDocument();
  });
});
