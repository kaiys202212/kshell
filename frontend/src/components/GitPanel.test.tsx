import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import GitPanel from './GitPanel';
import type { GitLogCommit, GitRef, GitSCMSnapshot } from '../lib/api';
import { useAppStore } from '../state/store';
import { tt } from '../test/i18n';

type IOCallback = IntersectionObserverCallback;
let ioCallback: IOCallback | null = null;

function triggerNearEnd() {
  expect(ioCallback).toBeTruthy();
  const entry = { isIntersecting: true } as IntersectionObserverEntry;
  ioCallback!([entry], {} as IntersectionObserver);
}

function fakeCommit(i: number): GitLogCommit {
  const hash = i.toString(16).padStart(40, '0');
  return {
    Hash: hash,
    Parents: [],
    Author: 't',
    Email: 't@t',
    Date: '2026-01-01T00:00:00Z',
    Subject: `c${i}`,
    Decorations: [],
  };
}

const DEFAULT_LOG: GitLogCommit[] = [
  {
    Hash: 'abc1234deadbeef',
    Parents: [],
    Author: 't',
    Email: 't@t',
    Date: '2026-01-01T00:00:00Z',
    Subject: 'init',
    Decorations: ['main'],
  },
];

const DEFAULT_REFS: GitRef[] = [
  { Name: 'main', Short: 'main', Kind: 'local', Current: true },
  { Name: 'topic', Short: 'topic', Kind: 'local', Current: false },
  { Name: 'origin/main', Short: 'origin/main', Kind: 'remote', Current: false },
];

const mocks = vi.hoisted(() => ({
  gitSCM: vi.fn(),
  gitStage: vi.fn().mockResolvedValue(undefined),
  gitUnstage: vi.fn().mockResolvedValue(undefined),
  gitDiscard: vi.fn().mockResolvedValue(undefined),
  gitCommit: vi.fn().mockResolvedValue(undefined),
  generateCommitMessage: vi.fn().mockResolvedValue('feat: 自动生成'),
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
  gitLog: vi.fn(),
  gitRefs: vi.fn(),
  gitFetchAll: vi.fn().mockResolvedValue(undefined),
  gitCommitStat: vi.fn().mockResolvedValue({ Files: 19, Insertions: 1867, Deletions: 202 }),
  onFilesChanged: vi.fn<(cb: (path: string) => void) => () => void>(() => () => {}),
  onGitChanged: vi.fn<(cb: (path: string) => void) => () => void>(() => () => {}),
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

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  localStorage.clear();
  useAppStore.setState({ toasts: [] });
  // clearAllMocks 只清调用记录，不清 mockImplementation/mockRejectedValue；用例各自改写过的
  // 实现会漏到后续用例。统一 reset 后重设全部默认值，避免顺序依赖。
  for (const m of Object.values(mocks)) m.mockReset();
  mocks.gitStage.mockResolvedValue(undefined);
  mocks.gitUnstage.mockResolvedValue(undefined);
  mocks.gitDiscard.mockResolvedValue(undefined);
  mocks.gitCommit.mockResolvedValue(undefined);
  mocks.generateCommitMessage.mockResolvedValue('feat: 自动生成');
  mocks.gitBranches.mockResolvedValue(['main']);
  mocks.gitCheckout.mockResolvedValue(undefined);
  mocks.gitCreateBranch.mockResolvedValue(undefined);
  mocks.gitFetch.mockResolvedValue(undefined);
  mocks.gitPull.mockResolvedValue(undefined);
  mocks.gitPush.mockResolvedValue(undefined);
  mocks.gitStashPush.mockResolvedValue(undefined);
  mocks.gitStashPop.mockResolvedValue(undefined);
  mocks.gitStashApply.mockResolvedValue(undefined);
  mocks.gitStashDrop.mockResolvedValue(undefined);
  mocks.gitLog.mockResolvedValue(DEFAULT_LOG);
  mocks.gitRefs.mockResolvedValue(DEFAULT_REFS);
  mocks.gitFetchAll.mockResolvedValue(undefined);
  mocks.gitCommitStat.mockResolvedValue({ Files: 19, Insertions: 1867, Deletions: 202 });
  mocks.onFilesChanged.mockReturnValue(() => {});
  mocks.onGitChanged.mockReturnValue(() => {});
  mocks.gitSCM.mockResolvedValue(snap());
  ioCallback = null;
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      constructor(cb: IOCallback) {
        ioCallback = cb;
      }
      observe() {}
      disconnect() {}
      unobserve() {}
      takeRecords() {
        return [];
      }
      root = null;
      rootMargin = '';
      thresholds = [];
    },
  );
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

  it('AI 生成：无 staged 时按钮禁用', async () => {
    mocks.gitSCM.mockResolvedValue(
      snap({
        Entries: [
          { Path: 'dirty.go', X: ' ', Y: 'M', Staged: false, Unstaged: true, Untracked: false, Conflicted: false },
        ],
      }),
    );
    render(<GitPanel wsPath="D:/proj" visible onOpenDiff={() => {}} />);
    expect(await screen.findByText('dirty.go')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: tt('ui.git.ai_generate_aria') })).toBeDisabled();
  });

  it('AI 生成：生成中禁用提交，成功后回填提交说明', async () => {
    let resolveGen!: (v: string) => void;
    mocks.generateCommitMessage.mockReturnValue(
      new Promise<string>((resolve) => {
        resolveGen = resolve;
      }),
    );
    render(<GitPanel wsPath="D:/proj" visible onOpenDiff={() => {}} />);
    expect(await screen.findByText('staged.go')).toBeInTheDocument();
    const input = screen.getByLabelText(tt('ui.git.commit_message_aria'));
    fireEvent.change(input, { target: { value: 'wip' } });
    const primary = screen.getByRole('button', { name: tt('ui.git.primary_action_aria') });
    expect(primary).toBeEnabled();

    fireEvent.click(screen.getByRole('button', { name: tt('ui.git.ai_generate_aria') }));
    await waitFor(() =>
      expect(mocks.generateCommitMessage).toHaveBeenCalledWith(
        'D:/proj',
        '',
        expect.stringMatching(/^(zh|en)$/),
      ),
    );
    expect(primary).toBeDisabled();

    resolveGen('feat: 自动生成');
    await waitFor(() => expect(input).toHaveValue('feat: 自动生成'));
    await waitFor(() => expect(primary).toBeEnabled());
  });

  it('AI 生成：失败提示错误且保留已有输入', async () => {
    mocks.generateCommitMessage.mockRejectedValue(new Error('err.git.ai_model_not_ready'));
    render(<GitPanel wsPath="D:/proj" visible onOpenDiff={() => {}} />);
    expect(await screen.findByText('staged.go')).toBeInTheDocument();
    const input = screen.getByLabelText(tt('ui.git.commit_message_aria'));
    fireEvent.change(input, { target: { value: 'wip' } });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.git.ai_generate_aria') }));
    await waitFor(() => {
      expect(
        useAppStore
          .getState()
          .toasts.some((x) => x.tone === 'error' && x.title === tt('err.git.ai_model_not_ready')),
      ).toBe(true);
    });
    expect(input).toHaveValue('wip');
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
    expect(mocks.gitLog).toHaveBeenCalledWith(ws, '', 'current', '', 100, 0);

    fireEvent.change(screen.getByLabelText(tt('ui.git.log_filter_aria')), { target: { value: 'topic' } });
    await waitFor(() => {
      expect(mocks.gitLog).toHaveBeenCalledWith(ws, '', 'ref', 'topic', 100, 0);
    });
    expect(localStorage.getItem(`kshell-git-log-sel:${ws}\0`)).toBe('topic');

    fireEvent.change(screen.getByLabelText(tt('ui.git.log_filter_aria')), { target: { value: 'all' } });
    await waitFor(() => {
      expect(mocks.gitLog).toHaveBeenCalledWith(ws, '', 'all', '', 100, 0);
    });
  });

  it('触底加载下一页，不足一页后不再请求', async () => {
    const ws = 'D:/proj';
    const page1 = Array.from({ length: 100 }, (_, i) => fakeCommit(i));
    const page2 = [fakeCommit(100), fakeCommit(101)];
    mocks.gitLog.mockResolvedValueOnce(page1).mockResolvedValueOnce(page2);

    render(<GitPanel wsPath={ws} visible onOpenDiff={() => {}} />);
    await screen.findByText('c0');
    expect(mocks.gitLog).toHaveBeenCalledWith(ws, '', 'current', '', 100, 0);

    await act(async () => {
      triggerNearEnd();
    });
    await waitFor(() => {
      expect(mocks.gitLog).toHaveBeenCalledWith(ws, '', 'current', '', 100, 100);
    });
    expect(await screen.findByText('c100')).toBeInTheDocument();

    const callsAfterPage2 = mocks.gitLog.mock.calls.length;
    await act(async () => {
      triggerNearEnd();
    });
    expect(mocks.gitLog.mock.calls.length).toBe(callsAfterPage2);
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
    expect(screen.getByRole('button', { name: tt('ui.git.fetch_all') })).toBeInTheDocument();
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

  it('Fetch and Sync：先取远端，再按 fetch 后的差异拉取推送', async () => {
    const ws = 'D:\\proj';
    let fetched = false;
    mocks.gitFetch.mockImplementation(() => {
      fetched = true;
      return Promise.resolve();
    });
    // fetch 前快照只有领先（按钮可用），fetch 后同时落后 → 需要先拉再推。
    mocks.gitSCM.mockImplementation(() =>
      Promise.resolve(
        snap({
          Entries: [],
          Ahead: 1,
          Behind: fetched ? 1 : 0,
          HasUpstream: true,
          SyncRemote: 'gitcode',
        }),
      ),
    );
    render(<GitPanel wsPath={ws} visible onOpenDiff={() => {}} />);
    const btn = await screen.findByRole('button', { name: tt('ui.git.primary_action_aria') });
    await waitFor(() => expect(btn).toBeEnabled());
    fireEvent.click(btn);
    await waitFor(() => {
      expect(mocks.gitFetch).toHaveBeenCalledWith(ws, '', 'gitcode');
      expect(mocks.gitPull).toHaveBeenCalledWith(ws, '', 'gitcode');
      expect(mocks.gitPush).toHaveBeenCalledWith(ws, '', 'gitcode');
    });
    const order = (fn: { mock: { invocationCallOrder: number[] } }) => fn.mock.invocationCallOrder[0];
    expect(order(mocks.gitFetch)).toBeLessThan(order(mocks.gitPull));
    expect(order(mocks.gitPull)).toBeLessThan(order(mocks.gitPush));
  });

  it('Fetch and Sync：fetch 后已无差异则不再拉取推送', async () => {
    const ws = 'D:\\proj';
    let fetched = false;
    mocks.gitFetch.mockImplementation(() => {
      fetched = true;
      return Promise.resolve();
    });
    mocks.gitSCM.mockImplementation(() =>
      Promise.resolve(
        snap({
          Entries: [],
          Ahead: fetched ? 0 : 1,
          Behind: 0,
          HasUpstream: true,
          SyncRemote: 'gitcode',
        }),
      ),
    );
    render(<GitPanel wsPath={ws} visible onOpenDiff={() => {}} />);
    const btn = await screen.findByRole('button', { name: tt('ui.git.primary_action_aria') });
    await waitFor(() => expect(btn).toBeEnabled());
    fireEvent.click(btn);
    // 必须等整条链路收尾（成功 toast）再断言未调用，否则只覆盖「与 fetch 同 tick」的误调用，
    // 延迟发生的误拉/误推会漏掉。
    await waitFor(() => {
      expect(useAppStore.getState().toasts.some((x) => x.title === tt('ui.git.synced'))).toBe(true);
    });
    expect(mocks.gitFetch).toHaveBeenCalledWith(ws, '', 'gitcode');
    expect(mocks.gitPull).not.toHaveBeenCalled();
    expect(mocks.gitPush).not.toHaveBeenCalled();
  });

  it('Fetch and Sync：pull 后按新快照推送（合并提交产生新的领先）', async () => {
    const ws = 'D:\\proj';
    let fetched = false;
    let pulled = false;
    mocks.gitFetch.mockImplementation(() => {
      fetched = true;
      return Promise.resolve();
    });
    mocks.gitPull.mockImplementation(() => {
      pulled = true;
      return Promise.resolve();
    });
    // fetch 前只有领先；fetch 后落后（无领先）；pull 合并后重新产生领先 → 必须 push。
    mocks.gitSCM.mockImplementation(() =>
      Promise.resolve(
        snap({
          Entries: [],
          Ahead: pulled ? 1 : fetched ? 0 : 1,
          Behind: fetched && !pulled ? 1 : 0,
          HasUpstream: true,
          SyncRemote: 'gitcode',
        }),
      ),
    );
    render(<GitPanel wsPath={ws} visible onOpenDiff={() => {}} />);
    const btn = await screen.findByRole('button', { name: tt('ui.git.primary_action_aria') });
    await waitFor(() => expect(btn).toBeEnabled());
    fireEvent.click(btn);
    await waitFor(() => expect(mocks.gitPush).toHaveBeenCalledWith(ws, '', 'gitcode'));
    expect(mocks.gitPull).toHaveBeenCalledWith(ws, '', 'gitcode');
  });

  it('Fetch and Sync：fetch 失败时报错并停止后续操作', async () => {
    const ws = 'D:\\proj';
    mocks.gitFetch.mockRejectedValue(new Error('boom'));
    mocks.gitSCM.mockResolvedValue(
      snap({ Entries: [], Ahead: 1, Behind: 0, HasUpstream: true, SyncRemote: 'gitcode' }),
    );
    render(<GitPanel wsPath={ws} visible onOpenDiff={() => {}} />);
    const btn = await screen.findByRole('button', { name: tt('ui.git.primary_action_aria') });
    await waitFor(() => expect(btn).toBeEnabled());
    fireEvent.click(btn);
    await waitFor(() => {
      expect(
        useAppStore
          .getState()
          .toasts.some((x) => x.tone === 'error' && x.title === 'boom'),
      ).toBe(true);
    });
    expect(mocks.gitPull).not.toHaveBeenCalled();
    expect(mocks.gitPush).not.toHaveBeenCalled();
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

  it('提交进行中主按钮显示转圈图标', async () => {
    const ws = 'D:\\proj';
    let resolveCommit!: () => void;
    mocks.gitCommit.mockImplementation(
      () =>
        new Promise<void>((r) => {
          resolveCommit = r;
        }),
    );
    render(<GitPanel wsPath={ws} visible onOpenDiff={() => {}} />);
    const msg = await screen.findByLabelText(tt('ui.git.commit_message_aria'));
    fireEvent.change(msg, { target: { value: 'msg' } });
    const btn = screen.getByRole('button', { name: tt('ui.git.primary_action_aria') });
    fireEvent.click(btn);
    await waitFor(() => {
      expect(btn.querySelector('.animate-spin')).toBeTruthy();
    });
    await act(async () => {
      resolveCommit();
    });
    await waitFor(() => {
      expect(btn.querySelector('.animate-spin')).toBeNull();
    });
  });

  it('提交并推送进行中主按钮显示转圈图标', async () => {
    const ws = 'D:\\proj';
    let resolvePush!: () => void;
    mocks.gitCommit.mockResolvedValue(undefined);
    mocks.gitPush.mockImplementation(
      () =>
        new Promise<void>((r) => {
          resolvePush = r;
        }),
    );
    render(<GitPanel wsPath={ws} visible onOpenDiff={() => {}} />);
    fireEvent.change(await screen.findByLabelText(tt('ui.git.commit_message_aria')), {
      target: { value: 'msg' },
    });
    const btn = screen.getByRole('button', { name: tt('ui.git.primary_action_aria') });
    const more = screen.getByRole('button', { name: tt('ui.git.more_commit_aria') });
    fireEvent.pointerDown(more, { button: 0 });
    fireEvent.click(more);
    fireEvent.click(await screen.findByRole('menuitem', { name: tt('ui.git.commit_and_push') }));
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
    fireEvent.click(await screen.findByRole('menuitem', { name: tt('ui.git.pull') }));
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
    fireEvent.click(await screen.findByRole('menuitem', { name: tt('ui.git.fetch') }));
    await waitFor(() => {
      expect(mocks.gitFetch).toHaveBeenCalledWith(ws, '', 'gitcode');
    });
    await waitFor(() => expect(trigger).toBeEnabled());

    fireEvent.pointerDown(trigger, { button: 0 });
    fireEvent.click(trigger);
    fireEvent.click(await screen.findByRole('menuitem', { name: tt('ui.git.push') }));
    await waitFor(() => {
      expect(mocks.gitPush).toHaveBeenCalledWith(ws, '', 'gitcode');
    });
  });

  it('底部 Fetch 按生效同步源调用', async () => {
    const ws = 'D:\\proj';
    mocks.gitSCM.mockResolvedValue(snap({ SyncRemote: 'gitcode' }));
    render(<GitPanel wsPath={ws} visible onOpenDiff={() => {}} />);
    await screen.findByText('init');
    fireEvent.click(screen.getByRole('button', { name: tt('ui.git.fetch') }));
    await waitFor(() => {
      expect(mocks.gitFetch).toHaveBeenCalledWith(ws, '', 'gitcode');
    });
  });

  it('修改区刷新按钮重新拉取 git 状态', async () => {
    const ws = 'D:\\proj';
    render(<GitPanel wsPath={ws} visible onOpenDiff={() => {}} />);
    await screen.findByText('dirty.go');
    const calls = mocks.gitSCM.mock.calls.length;
    fireEvent.click(screen.getByRole('button', { name: tt('ui.git.refresh_scm') }));
    await waitFor(() => {
      expect(mocks.gitSCM.mock.calls.length).toBeGreaterThan(calls);
    });
    expect(mocks.gitSCM).toHaveBeenLastCalledWith(ws, '', '');
  });

  it('日志区刷新按钮重新拉取提交日志', async () => {
    const ws = 'D:\\proj';
    render(<GitPanel wsPath={ws} visible onOpenDiff={() => {}} />);
    await screen.findByText('init');
    const calls = mocks.gitLog.mock.calls.length;
    fireEvent.click(screen.getByRole('button', { name: tt('ui.git.refresh_log') }));
    await waitFor(() => {
      expect(mocks.gitLog.mock.calls.length).toBeGreaterThan(calls);
    });
    expect(mocks.gitLog).toHaveBeenLastCalledWith(ws, '', 'current', '', 100, 0);
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

  it('提交日志加载失败时弹出错误提示', async () => {
    mocks.gitLog.mockRejectedValue(new Error('boom'));
    render(<GitPanel wsPath="D:/proj" visible onOpenDiff={() => {}} />);
    await waitFor(() => {
      expect(
        useAppStore
          .getState()
          .toasts.some(
            (x) => x.tone === 'error' && x.title === tt('ui.git.log_read_failed').replace('{{err}}', 'boom'),
          ),
      ).toBe(true);
    });
  });

  it('可见时主动订阅 git:changed（刷状态+日志）与 files:changed（刷状态）', async () => {
    const gitCbs: Array<(path: string) => void> = [];
    const fileCbs: Array<(path: string) => void> = [];
    const gitOff = vi.fn();
    const fileOff = vi.fn();
    mocks.onGitChanged.mockImplementation((cb) => {
      gitCbs.push(cb);
      return gitOff;
    });
    mocks.onFilesChanged.mockImplementation((cb) => {
      fileCbs.push(cb);
      return fileOff;
    });
    const { unmount } = render(<GitPanel wsPath={'D:\\proj'} visible onOpenDiff={() => {}} />);
    await screen.findByText('staged.go');
    expect(gitCbs).toHaveLength(1);
    expect(fileCbs).toHaveLength(1);

    // 其它工作区的路径不触发请求
    const mismatchScm = mocks.gitSCM.mock.calls.length;
    await act(async () => {
      gitCbs.forEach((cb) => cb('D:\\other'));
      fileCbs.forEach((cb) => cb('D:/other'));
    });
    expect(mocks.gitSCM.mock.calls.length).toBe(mismatchScm);

    const scmBefore = mocks.gitSCM.mock.calls.length;
    const logBefore = mocks.gitLog.mock.calls.length;
    act(() => gitCbs.forEach((cb) => cb('D:\\proj')));
    await waitFor(() => {
      expect(mocks.gitSCM.mock.calls.length).toBeGreaterThan(scmBefore);
      expect(mocks.gitLog.mock.calls.length).toBeGreaterThan(logBefore);
    });

    // 工作区文件变更只影响 staged/working 列表，不重拉提交历史
    const scmAfter = mocks.gitSCM.mock.calls.length;
    const logAfter = mocks.gitLog.mock.calls.length;
    act(() => fileCbs.forEach((cb) => cb('D:\\proj')));
    await waitFor(() => expect(mocks.gitSCM.mock.calls.length).toBeGreaterThan(scmAfter));
    expect(mocks.gitLog.mock.calls.length).toBe(logAfter);

    // 卸载时退订，避免隐藏后的泄漏
    unmount();
    expect(gitOff).toHaveBeenCalled();
    expect(fileOff).toHaveBeenCalled();
  });

  it('不可见时不订阅变更事件', () => {
    render(<GitPanel wsPath="D:\\proj" visible={false} onOpenDiff={() => {}} />);
    expect(mocks.onGitChanged).not.toHaveBeenCalled();
    expect(mocks.onFilesChanged).not.toHaveBeenCalled();
    expect(mocks.gitSCM).not.toHaveBeenCalled();
  });
});
