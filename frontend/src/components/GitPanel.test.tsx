import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import GitPanel from './GitPanel';
import type { GitSCMSnapshot } from '../lib/api';
import { useAppStore } from '../state/store';
import { tt } from '../test/i18n';

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
  Remotes: ['origin', 'gitcode'],
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
  mocks.gitPull.mockResolvedValue(undefined);
  mocks.gitPush.mockResolvedValue(undefined);
});

describe('GitPanel', () => {
  it('可见时加载变更分组，提交说明为单行', async () => {
    render(<GitPanel wsPath="D:\\proj" visible onOpenDiff={() => {}} />);
    expect(await screen.findByText('staged.go')).toBeInTheDocument();
    expect(screen.getByText('dirty.go')).toBeInTheDocument();
    const msg = screen.getByLabelText(tt('ui.git.commit_message_aria'));
    expect(msg.tagName).toBe('INPUT');
    expect(screen.getByRole('button', { name: tt('ui.git.primary_action_aria') })).toBeDisabled();
    fireEvent.change(msg, { target: { value: 'msg' } });
    expect(screen.getByRole('button', { name: tt('ui.git.primary_action_aria') })).toBeEnabled();
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
    expect(screen.getByRole('button', { name: tt('ui.git.primary_action_aria') })).toBeDisabled();
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

  it('单击 commit message 打开 commit diff 预览', async () => {
    const onOpenDiff = vi.fn();
    render(<GitPanel wsPath="D:\\proj" visible onOpenDiff={onOpenDiff} />);
    fireEvent.click(await screen.findByRole('button', { name: /init/ }));
    expect(onOpenDiff).toHaveBeenCalledWith({
      kind: 'commit',
      repoRel: '',
      hash: 'abc1234deadbeef',
      preview: true,
    });
  });

  it('丢弃取消时不调 API', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false);
    render(<GitPanel wsPath="D:\\proj" visible onOpenDiff={() => {}} />);
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.git.discard_aria').replace('{{path}}', 'dirty.go') }));
    expect(mocks.gitDiscard).not.toHaveBeenCalled();
  });

  it('有未提交时主按钮为提交，干净时为同步', async () => {
    render(<GitPanel wsPath="D:\\proj" visible onOpenDiff={() => {}} />);
    expect(await screen.findByRole('button', { name: tt('ui.git.primary_action_aria') })).toHaveTextContent(tt('ui.git.commit'));
    expect(screen.getByRole('button', { name: tt('ui.git.more_commit_aria') })).toBeInTheDocument();
    cleanup();
    mocks.gitSCM.mockResolvedValue(snap({ Entries: [], Ahead: 1, Behind: 0, HasUpstream: true }));
    render(<GitPanel wsPath="D:\\proj" visible onOpenDiff={() => {}} />);
    expect(await screen.findByRole('button', { name: tt('ui.git.primary_action_aria') })).toHaveTextContent(tt('ui.git.sync'));
  });

  it('切换分支筛选会按 ref/all 重新拉取日志并记住选择', async () => {
    const ws = 'D:/proj';
    render(<GitPanel wsPath={ws} visible onOpenDiff={() => {}} />);
    await screen.findByText('init');
    expect(mocks.gitLog).toHaveBeenCalledWith(ws, '', 'current', '', 200);

    fireEvent.change(screen.getByLabelText(tt('ui.git.log_filter_aria')), { target: { value: 'topic' } });
    await waitFor(() => {
      expect(mocks.gitLog).toHaveBeenCalledWith(ws, '', 'ref', 'topic', 200);
    });
    expect(localStorage.getItem(`kshell-git-log-sel:${ws}\0`)).toBe('topic');

    fireEvent.change(screen.getByLabelText(tt('ui.git.log_filter_aria')), { target: { value: 'all' } });
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
    const sel = await screen.findByLabelText(tt('ui.git.select_repo'));
    expect(sel.className).toMatch(/w-full/);
    expect(sel.className).toMatch(/min-w-0/);
  });

  it('提交图显示日志并提供 Fetch all', async () => {
    render(<GitPanel wsPath="D:\\proj" visible onOpenDiff={() => {}} />);
    expect(await screen.findByText('init')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Fetch all' })).toBeInTheDocument();
    expect(screen.getByLabelText(tt('ui.git.log_filter_aria'))).toBeInTheDocument();
  });

  it('同步源选择器：显示生效源，切换后按新源重新加载并记忆', async () => {
    const ws = 'D:\\proj';
    render(<GitPanel wsPath={ws} visible onOpenDiff={() => {}} />);
    const sel = await screen.findByLabelText(tt('ui.git.sync_source_aria'));
    expect(sel).toHaveValue('origin');
    expect(mocks.gitSCM).toHaveBeenCalledWith(ws, '', '');

    mocks.gitSCM.mockResolvedValue(snap({ Remotes: ['origin', 'gitcode'], SyncRemote: 'gitcode' }));
    fireEvent.change(sel, { target: { value: 'gitcode' } });
    await waitFor(() => {
      expect(mocks.gitSCM).toHaveBeenCalledWith(ws, '', 'gitcode');
    });
    expect(localStorage.getItem(`kshell-git-sync-remote:${ws}\0`)).toBe('gitcode');
    expect(await screen.findByLabelText(tt('ui.git.sync_source_aria'))).toHaveValue('gitcode');
  });

  it('同步按钮按生效同步源拉取推送', async () => {
    const ws = 'D:\\proj';
    mocks.gitSCM.mockResolvedValue(
      snap({ Entries: [], Ahead: 1, Behind: 1, HasUpstream: true, SyncRemote: 'gitcode' }),
    );
    render(<GitPanel wsPath={ws} visible onOpenDiff={() => {}} />);
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.git.primary_action_aria') }));
    await waitFor(() => {
      expect(mocks.gitPull).toHaveBeenCalledWith(ws, '', 'gitcode');
      expect(mocks.gitPush).toHaveBeenCalledWith(ws, '', 'gitcode');
    });
  });

  it('同步进行中主按钮显示转圈图标', async () => {
    const ws = 'D:\\proj';
    let resolvePush!: () => void;
    mocks.gitSCM.mockResolvedValue(
      snap({ Entries: [], Ahead: 1, Behind: 0, HasUpstream: true, SyncRemote: 'origin' }),
    );
    mocks.gitPush.mockImplementation(
      () =>
        new Promise<void>((r) => {
          resolvePush = r;
        }),
    );
    render(<GitPanel wsPath={ws} visible onOpenDiff={() => {}} />);
    const btn = await screen.findByRole('button', { name: tt('ui.git.primary_action_aria') });
    fireEvent.click(btn);
    await waitFor(() => {
      expect(btn.querySelector('.animate-spin')).toBeTruthy();
    });
    await act(async () => {
      resolvePush();
    });
    await waitFor(() => {
      expect(btn.querySelector('.animate-spin')).toBeNull();
    });
  });

  it('无 remote 时不渲染同步源选择器', async () => {
    mocks.gitSCM.mockResolvedValue(snap({ Remotes: null, SyncRemote: '' }));
    render(<GitPanel wsPath="D:\\proj" visible onOpenDiff={() => {}} />);
    await screen.findByText('dirty.go');
    expect(screen.queryByLabelText(tt('ui.git.sync_source_aria'))).not.toBeInTheDocument();
  });

  it('Git 操作菜单的拉取/推送按生效同步源调用', async () => {
    const ws = 'D:\\proj';
    mocks.gitSCM.mockResolvedValue(snap({ SyncRemote: 'gitcode' }));
    render(<GitPanel wsPath={ws} visible onOpenDiff={() => {}} />);
    await screen.findByText('dirty.go');

    // Radix 菜单由 pointerdown 展开；子菜单项文本带 ▸ 后缀，用正则匹配
    const trigger = screen.getByRole('button', { name: tt('ui.git.actions_aria') });
    fireEvent.pointerDown(trigger, { button: 0 });
    fireEvent.click(trigger);
    const sub = await screen.findByRole('menuitem', { name: new RegExp(tt('ui.git.pull')) });
    fireEvent.click(sub);
    fireEvent.pointerMove(sub);
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Pull' }));
    await waitFor(() => {
      expect(mocks.gitPull).toHaveBeenCalledWith(ws, '', 'gitcode');
    });
    // 等 run() 收尾解除 busy，否则禁用态触发器展不开菜单
    await waitFor(() => expect(trigger).toBeEnabled());

    // 菜单内 Fetch 是独立调用点，单独断言（与底部工具栏 Fetch 各改各的）
    fireEvent.pointerDown(trigger, { button: 0 });
    fireEvent.click(trigger);
    const subF = await screen.findByRole('menuitem', { name: new RegExp(tt('ui.git.pull')) });
    fireEvent.click(subF);
    fireEvent.pointerMove(subF);
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Fetch' }));
    await waitFor(() => {
      expect(mocks.gitFetch).toHaveBeenCalledWith(ws, '', 'gitcode');
    });
    await waitFor(() => expect(trigger).toBeEnabled());

    fireEvent.pointerDown(trigger, { button: 0 });
    fireEvent.click(trigger);
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Push' }));
    await waitFor(() => {
      expect(mocks.gitPush).toHaveBeenCalledWith(ws, '', 'gitcode');
    });
  });

  it('底部 Fetch 按生效同步源调用', async () => {
    const ws = 'D:\\proj';
    mocks.gitSCM.mockResolvedValue(snap({ SyncRemote: 'gitcode' }));
    render(<GitPanel wsPath={ws} visible onOpenDiff={() => {}} />);
    await screen.findByText('init');
    fireEvent.click(screen.getByRole('button', { name: 'Fetch' }));
    await waitFor(() => {
      expect(mocks.gitFetch).toHaveBeenCalledWith(ws, '', 'gitcode');
    });
  });

  it('提交并推送按生效同步源推送', async () => {
    const ws = 'D:\\proj';
    mocks.gitSCM.mockResolvedValue(snap({ SyncRemote: 'gitcode' }));
    render(<GitPanel wsPath={ws} visible onOpenDiff={() => {}} />);
    fireEvent.change(await screen.findByLabelText(tt('ui.git.commit_message_aria')), { target: { value: 'msg' } });
    const more = screen.getByRole('button', { name: tt('ui.git.more_commit_aria') });
    fireEvent.pointerDown(more, { button: 0 });
    fireEvent.click(more);
    fireEvent.click(await screen.findByRole('menuitem', { name: tt('ui.git.commit_and_push') }));
    await waitFor(() => {
      expect(mocks.gitCommit).toHaveBeenCalledWith(ws, '', 'msg');
      expect(mocks.gitPush).toHaveBeenCalledWith(ws, '', 'gitcode');
    });
  });

  it('快速连续切换同步源时丢弃过期响应，显示与记忆保持一致', async () => {
    const ws = 'D:\\proj';
    const key = `kshell-git-sync-remote:${ws}\0`;
    mocks.gitSCM.mockResolvedValue(snap({ SyncRemote: 'origin' }));
    render(<GitPanel wsPath={ws} visible onOpenDiff={() => {}} />);
    const sel = await screen.findByLabelText(tt('ui.git.sync_source_aria'));
    expect(sel).toHaveValue('origin');

    // 挂起后续两次请求，人工控制返回顺序以复现竞态
    const pending: Array<(v: GitSCMSnapshot) => void> = [];
    mocks.gitSCM.mockImplementation(() => new Promise((res) => pending.push(res)));
    fireEvent.change(sel, { target: { value: 'gitcode' } });
    fireEvent.change(sel, { target: { value: 'origin' } });
    await waitFor(() => {
      expect(mocks.gitSCM).toHaveBeenCalledWith(ws, '', 'gitcode');
      expect(mocks.gitSCM).toHaveBeenCalledWith(ws, '', 'origin');
    });
    expect(pending).toHaveLength(2);
    expect(localStorage.getItem(key)).toBe('origin');

    // 后发的 origin 响应先回，先发的 gitcode 响应后到（过期）
    await act(async () => {
      pending[1](snap({ SyncRemote: 'origin' }));
    });
    await act(async () => {
      pending[0](snap({ SyncRemote: 'gitcode' }));
    });

    expect(await screen.findByLabelText(tt('ui.git.sync_source_aria'))).toHaveValue('origin');
    expect(localStorage.getItem(key)).toBe('origin');
  });
});
