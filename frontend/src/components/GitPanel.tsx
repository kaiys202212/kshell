// 右栏 Git SCM 面板（参考 VS Code Source Control）。
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  gitBranches,
  gitCheckout,
  gitCommit,
  gitCreateBranch,
  gitDiscard,
  gitFetch,
  gitPull,
  gitPush,
  gitSCM,
  gitStage,
  gitStashApply,
  gitStashDrop,
  gitStashPop,
  gitStashPush,
  gitUnstage,
} from '../lib/api';
import type { GitDiffSide, GitSCMEntry, GitSCMSnapshot } from '../lib/api';
import { refreshGitStatus } from '../lib/git';
import { cn } from '../lib/cn';
import { PANE_HEADER } from '../lib/ui';
import { useAppStore } from '../state/store';
import { Button } from './ui/button';
import { EmptyState } from './ui/empty-state';
import { Input } from './ui/input';

export type GitDiffSpec = {
  repoRel: string;
  path: string;
  side: GitDiffSide;
  preview: boolean;
};

export default function GitPanel({
  wsPath,
  visible,
  onOpenDiff,
}: {
  wsPath: string;
  visible: boolean;
  onOpenDiff: (spec: GitDiffSpec) => void;
}) {
  const notify = useAppStore((s) => s.notify);
  const [snap, setSnap] = useState<GitSCMSnapshot | null>(null);
  const [repoRel, setRepoRel] = useState('');
  const [msg, setMsg] = useState('');
  const [busy, setBusy] = useState(false);
  const [branchOpen, setBranchOpen] = useState(false);
  const [branches, setBranches] = useState<string[]>([]);
  const [newBranch, setNewBranch] = useState('');

  const load = useCallback(async () => {
    try {
      const s = await gitSCM(wsPath, repoRel);
      setSnap(s);
      if (!repoRel && s.Repos && s.Repos.length > 0 && !s.IsRepo) {
        const first = s.Repos[0];
        if (first.Rel) setRepoRel(first.Rel);
      }
    } catch (e) {
      notify(`读取 git 失败：${e instanceof Error ? e.message : String(e)}`, 'error');
    }
  }, [wsPath, repoRel, notify]);

  useEffect(() => {
    if (visible) void load();
  }, [visible, load]);

  const run = async (fn: () => Promise<void>, ok?: string) => {
    setBusy(true);
    try {
      await fn();
      if (ok) notify(ok, 'success');
      await refreshGitStatus(wsPath);
      await load();
    } catch (e) {
      notify(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setBusy(false);
    }
  };

  const staged = useMemo(() => (snap?.Entries ?? []).filter((e) => e.Staged), [snap]);
  const changes = useMemo(
    () => (snap?.Entries ?? []).filter((e) => e.Unstaged && !e.Untracked),
    [snap],
  );
  const untracked = useMemo(() => (snap?.Entries ?? []).filter((e) => e.Untracked), [snap]);
  const repos = snap?.Repos ?? [];

  const openEntry = (e: GitSCMEntry, side: GitDiffSide, preview: boolean) => {
    onOpenDiff({ repoRel, path: e.Path, side, preview });
  };

  if (!visible && !snap) {
    return <div className="hidden" />;
  }

  if (snap && !snap.IsRepo && repos.length === 0) {
    return <EmptyState title="不是 git 仓库" />;
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-2 text-xs">
      {repos.length > 1 && (
        <select
          className="h-7 rounded-md border border-border bg-card px-1"
          aria-label="选择仓库"
          value={repoRel}
          onChange={(e) => setRepoRel(e.target.value)}
        >
          {repos.map((r) => (
            <option key={r.Rel || '__root'} value={r.Rel}>
              {r.Rel ? r.Rel : '工作区根'} ({r.Branch || '?'})
            </option>
          ))}
        </select>
      )}

      <div className="flex flex-wrap items-center gap-1">
        <button
          type="button"
          className="rounded-md px-1.5 py-0.5 hover:bg-muted"
          onClick={() => {
            setBranchOpen((v) => !v);
            void gitBranches(wsPath, repoRel).then(setBranches).catch(() => setBranches([]));
          }}
        >
          {snap?.Branch || '—'}
        </button>
        {snap?.HasUpstream && (
          <span className="text-muted-foreground">
            ↑{snap.Ahead} ↓{snap.Behind}
          </span>
        )}
        <Button size="sm" variant="ghost" disabled={busy} onClick={() => void run(() => gitFetch(wsPath, repoRel))}>
          Fetch
        </Button>
        <Button size="sm" variant="ghost" disabled={busy} onClick={() => void run(() => gitPull(wsPath, repoRel))}>
          Pull
        </Button>
        <Button size="sm" variant="ghost" disabled={busy} onClick={() => void run(() => gitPush(wsPath, repoRel))}>
          Push
        </Button>
      </div>

      {branchOpen && (
        <div className="space-y-1 rounded-md border border-border p-1.5">
          {branches.map((b) => (
            <button
              key={b}
              type="button"
              className={cn('block w-full rounded px-1 py-0.5 text-left hover:bg-muted', b === snap?.Branch && 'bg-muted')}
              onClick={() => {
                setBranchOpen(false);
                void run(() => gitCheckout(wsPath, repoRel, b));
              }}
            >
              {b}
            </button>
          ))}
          <div className="flex gap-1">
            <Input
              value={newBranch}
              onChange={(e) => setNewBranch(e.target.value)}
              placeholder="新分支名"
              className="h-7 text-xs"
            />
            <Button
              size="sm"
              disabled={!newBranch.trim() || busy}
              onClick={() => {
                const name = newBranch.trim();
                setNewBranch('');
                setBranchOpen(false);
                void run(() => gitCreateBranch(wsPath, repoRel, name));
              }}
            >
              新建
            </Button>
          </div>
        </div>
      )}

      <textarea
        className="min-h-16 resize-y rounded-md border border-border bg-card p-1.5"
        placeholder="提交说明"
        value={msg}
        onChange={(e) => setMsg(e.target.value)}
        aria-label="提交说明"
      />
      <Button
        size="sm"
        disabled={busy || !msg.trim() || staged.length === 0}
        onClick={() => {
          const m = msg.trim();
          setMsg('');
          void run(() => gitCommit(wsPath, repoRel, m), '已提交');
        }}
      >
        提交
      </Button>

      <Section
        title={`已暂存 (${staged.length})`}
        entries={staged}
        side="staged"
        onOpen={openEntry}
        onStage={null}
        onUnstage={(e) => void run(() => gitUnstage(wsPath, repoRel, [e.Path]))}
        onDiscard={null}
      />
      <Section
        title={`更改 (${changes.length})`}
        entries={changes}
        side="working"
        onOpen={openEntry}
        onStage={(e) => void run(() => gitStage(wsPath, repoRel, [e.Path]))}
        onUnstage={null}
        onDiscard={(e) => {
          if (!window.confirm(`丢弃 ${e.Path} 的工作区改动？`)) return;
          void run(() => gitDiscard(wsPath, repoRel, [e.Path]));
        }}
      />
      <Section
        title={`未跟踪 (${untracked.length})`}
        entries={untracked}
        side="working"
        onOpen={openEntry}
        onStage={(e) => void run(() => gitStage(wsPath, repoRel, [e.Path]))}
        onUnstage={null}
        onDiscard={(e) => {
          if (!window.confirm(`删除未跟踪文件 ${e.Path}？`)) return;
          void run(() => gitDiscard(wsPath, repoRel, [e.Path]));
        }}
      />

      <div>
        <div className={cn(PANE_HEADER, 'mb-1 flex items-center justify-between')}>
          <span>Stash ({(snap?.Stashes ?? []).length})</span>
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => void run(() => gitStashPush(wsPath, repoRel, ''))}>
            Stash
          </Button>
        </div>
        {(snap?.Stashes ?? []).map((st) => (
          <div key={st.Index} className="flex items-center gap-1 py-0.5">
            <span className="min-w-0 flex-1 truncate" title={st.Message}>
              {st.Message}
            </span>
            <Button size="sm" variant="ghost" onClick={() => void run(() => gitStashPop(wsPath, repoRel, st.Index))}>
              Pop
            </Button>
            <Button size="sm" variant="ghost" onClick={() => void run(() => gitStashApply(wsPath, repoRel, st.Index))}>
              Apply
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => {
                if (!window.confirm(`删除 stash：${st.Message}？`)) return;
                void run(() => gitStashDrop(wsPath, repoRel, st.Index));
              }}
            >
              Drop
            </Button>
          </div>
        ))}
      </div>
    </div>
  );
}

function Section({
  title,
  entries,
  side,
  onOpen,
  onStage,
  onUnstage,
  onDiscard,
}: {
  title: string;
  entries: GitSCMEntry[];
  side: GitDiffSide;
  onOpen: (e: GitSCMEntry, side: GitDiffSide, preview: boolean) => void;
  onStage: ((e: GitSCMEntry) => void) | null;
  onUnstage: ((e: GitSCMEntry) => void) | null;
  onDiscard: ((e: GitSCMEntry) => void) | null;
}) {
  return (
    <div className="min-h-0">
      <div className={PANE_HEADER}>{title}</div>
      {entries.map((e) => (
        <div
          key={`${side}:${e.Path}`}
          className="group flex items-center gap-1 rounded py-0.5 hover:bg-muted/50"
        >
          <button
            type="button"
            className="min-w-0 flex-1 truncate text-left"
            onClick={() => onOpen(e, side, true)}
            onDoubleClick={() => onOpen(e, side, false)}
          >
            {e.Path}
          </button>
          {onStage && (
            <Button size="sm" variant="ghost" aria-label={`暂存 ${e.Path}`} onClick={() => onStage(e)}>
              +
            </Button>
          )}
          {onUnstage && (
            <Button size="sm" variant="ghost" aria-label={`取消暂存 ${e.Path}`} onClick={() => onUnstage(e)}>
              −
            </Button>
          )}
          {onDiscard && (
            <Button size="sm" variant="ghost" aria-label={`丢弃 ${e.Path}`} onClick={() => onDiscard(e)}>
              丢弃
            </Button>
          )}
        </div>
      ))}
    </div>
  );
}
