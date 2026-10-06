// 右栏 Git SCM 面板（对齐 VS Code Source Control + Graph）。
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent, PointerEvent as ReactPointerEvent } from 'react';
import {
  ArrowDownUp,
  Check,
  ChevronDown,
  CloudDownload,
  Download,
  Ellipsis,
} from 'lucide-react';
import {
  gitCheckout,
  gitCommit,
  gitCommitStat,
  gitCreateBranch,
  gitDiscard,
  gitFetch,
  gitFetchAll,
  gitLog,
  gitPull,
  gitPush,
  gitRefs,
  gitSCM,
  gitStage,
  gitStashApply,
  gitStashDrop,
  gitStashPop,
  gitStashPush,
  gitUnstage,
} from '../lib/api';
import type { GitDiffSide, GitLogCommit, GitRef, GitSCMEntry, GitSCMSnapshot } from '../lib/api';
import { refreshGitStatus } from '../lib/git';
import { loadLogSel, resolveLogFilter, saveLogSel } from '../lib/gitLogSel';
import type { GraphCommit } from '../lib/gitGraph';
import { cn } from '../lib/cn';
import { PANE_HEADER } from '../lib/ui';
import { useAppStore } from '../state/store';
import { Button } from './ui/button';
import { EmptyState } from './ui/empty-state';
import { Input } from './ui/input';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from './ui/dropdown-menu';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip';
import { GitChangeTree } from './GitChangeTree';
import { GitLogGraph } from './GitLogGraph';

export type GitDiffSpec = {
  repoRel: string;
  path: string;
  side: GitDiffSide;
  preview: boolean;
};

function toGraph(commits: GitLogCommit[]): GraphCommit[] {
  return commits.map((c) => ({
    hash: c.Hash,
    parents: c.Parents ?? [],
    subject: c.Subject,
    author: c.Author,
    date: c.Date,
    decorations: c.Decorations ?? [],
  }));
}

/** 原生 select 长路径前省略：rtl 显示 + option 仍 ltr。 */
const selectEllipsis =
  'w-full min-w-0 max-w-full truncate rounded-md border border-border bg-card px-1 text-left [direction:rtl] [text-align:left] [&_option]:[direction:ltr]';

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
  const [refs, setRefs] = useState<GitRef[]>([]);
  const [commits, setCommits] = useState<GitLogCommit[]>([]);
  const [logSel, setLogSel] = useState(() => loadLogSel(wsPath, ''));
  const [picked, setPicked] = useState('');
  const [split, setSplit] = useState(0.55);
  const splitRef = useRef<HTMLDivElement>(null);
  const logReq = useRef(0);

  const { mode: logMode, ref: logRef } = resolveLogFilter(logSel);

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

  const loadLog = useCallback(async () => {
    const seq = ++logReq.current;
    try {
      const [r, logs] = await Promise.all([
        gitRefs(wsPath, repoRel),
        gitLog(wsPath, repoRel, logMode, logRef, 200),
      ]);
      if (seq !== logReq.current) return;
      setRefs(r ?? []);
      setCommits(logs ?? []);
    } catch {
      if (seq !== logReq.current) return;
      setCommits([]);
    }
  }, [wsPath, repoRel, logMode, logRef]);

  useEffect(() => {
    setLogSel(loadLogSel(wsPath, repoRel));
  }, [wsPath, repoRel]);

  useEffect(() => {
    if (visible) void load();
  }, [visible, load]);

  useEffect(() => {
    if (visible) void loadLog();
  }, [visible, loadLog]);

  const setLogSelPersist = (sel: string) => {
    setLogSel(sel);
    saveLogSel(wsPath, repoRel, sel);
  };

  const run = async (fn: () => Promise<void>, ok?: string) => {
    setBusy(true);
    try {
      await fn();
      if (ok) notify(ok, 'success');
      await refreshGitStatus(wsPath);
      await load();
      await loadLog();
    } catch (e) {
      notify(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setBusy(false);
    }
  };

  const staged = useMemo(() => (snap?.Entries ?? []).filter((e) => e.Staged), [snap]);
  const working = useMemo(
    () => (snap?.Entries ?? []).filter((e) => e.Unstaged || e.Untracked),
    [snap],
  );
  const repos = snap?.Repos ?? [];
  const canSync = Boolean(snap?.HasUpstream && ((snap.Ahead ?? 0) > 0 || (snap.Behind ?? 0) > 0));
  const dirty = staged.length + working.length > 0;
  const canCommit = Boolean(msg.trim() && staged.length > 0);
  const loadStat = useCallback(
    (hash: string) => gitCommitStat(wsPath, repoRel, hash),
    [wsPath, repoRel],
  );

  const doCommit = () => {
    const m = msg.trim();
    if (!m || staged.length === 0) return;
    setMsg('');
    void run(() => gitCommit(wsPath, repoRel, m), '已提交');
  };

  const openEntry = (e: GitSCMEntry, side: GitDiffSide, preview: boolean) => {
    onOpenDiff({ repoRel, path: e.Path, side, preview });
  };

  const onSplitPointer = (ev: ReactPointerEvent<HTMLDivElement>) => {
    const el = splitRef.current;
    if (!el) return;
    ev.preventDefault();
    const move = (e: PointerEvent) => {
      const rect = el.getBoundingClientRect();
      const y = (e.clientY - rect.top) / rect.height;
      setSplit(Math.min(0.8, Math.max(0.22, y)));
    };
    const up = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
  };

  const sync = () =>
    run(async () => {
      if ((snap?.Behind ?? 0) > 0) await gitPull(wsPath, repoRel);
      if ((snap?.Ahead ?? 0) > 0) await gitPush(wsPath, repoRel);
    }, '已同步');

  const onMsgKey = (e: ReactKeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      doCommit();
    }
  };

  if (!visible && !snap) {
    return <div className="hidden" />;
  }

  if (snap && !snap.IsRepo && repos.length === 0) {
    return <EmptyState title="不是 git 仓库" />;
  }

  const localRefs = refs.filter((r) => r.Kind === 'local');
  const remoteRefs = refs.filter((r) => r.Kind === 'remote');
  const branchLabel = snap?.Branch || 'master';

  return (
    <TooltipProvider delayDuration={300}>
      <div ref={splitRef} className="flex h-full min-h-0 flex-col overflow-hidden text-xs">
        {repos.length > 1 && (
          <select
            className={cn(selectEllipsis, 'mb-1 h-7 shrink-0')}
            aria-label="选择仓库"
            title={repoRel ? repoRel : '工作区根'}
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

        <div className="flex shrink-0 items-center gap-1 px-0.5 pb-1">
          <span className="min-w-0 truncate font-medium" title={snap?.Branch}>
            {snap?.Branch || '—'}
          </span>
          {snap?.HasUpstream && (
            <span className="text-muted-foreground">
              ↑{snap.Ahead} ↓{snap.Behind}
            </span>
          )}
          <div className="ml-auto flex items-center gap-0.5">
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button size="icon" variant="ghost" aria-label="Git 操作" disabled={busy}>
                  <Ellipsis className="h-3.5 w-3.5" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuSub>
                  <DropdownMenuSubTrigger>拉取</DropdownMenuSubTrigger>
                  <DropdownMenuSubContent>
                    <DropdownMenuItem onSelect={() => void run(() => gitPull(wsPath, repoRel), '已拉取')}>
                      Pull
                    </DropdownMenuItem>
                    <DropdownMenuItem onSelect={() => void run(() => gitFetch(wsPath, repoRel), '已 Fetch')}>
                      Fetch
                    </DropdownMenuItem>
                    <DropdownMenuItem onSelect={() => void run(() => gitFetchAll(wsPath, repoRel), '已 Fetch all')}>
                      Fetch all
                    </DropdownMenuItem>
                  </DropdownMenuSubContent>
                </DropdownMenuSub>
                <DropdownMenuItem onSelect={() => void run(() => gitPush(wsPath, repoRel), '已推送')}>Push</DropdownMenuItem>
                <DropdownMenuItem
                  disabled={!msg.trim() || staged.length === 0}
                  onSelect={() => {
                    const m = msg.trim();
                    setMsg('');
                    void run(() => gitCommit(wsPath, repoRel, m), '已提交');
                  }}
                >
                  提交
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuSub>
                  <DropdownMenuSubTrigger>检出分支</DropdownMenuSubTrigger>
                  <DropdownMenuSubContent className="max-h-64">
                    <DropdownMenuLabel>本地</DropdownMenuLabel>
                    {localRefs.map((r) => (
                      <DropdownMenuItem key={r.Name} onSelect={() => void run(() => gitCheckout(wsPath, repoRel, r.Name))}>
                        {r.Name}
                        {r.Current ? ' ·' : ''}
                      </DropdownMenuItem>
                    ))}
                    <DropdownMenuLabel>远端</DropdownMenuLabel>
                    {remoteRefs.map((r) => (
                      <DropdownMenuItem key={r.Name} onSelect={() => void run(() => gitCheckout(wsPath, repoRel, r.Name))}>
                        {r.Name}
                      </DropdownMenuItem>
                    ))}
                    <DropdownMenuSeparator />
                    <DropdownMenuItem
                      onSelect={() => {
                        const name = window.prompt('新分支名');
                        if (!name?.trim()) return;
                        void run(() => gitCreateBranch(wsPath, repoRel, name.trim()));
                      }}
                    >
                      新建分支…
                    </DropdownMenuItem>
                  </DropdownMenuSubContent>
                </DropdownMenuSub>
                <DropdownMenuSub>
                  <DropdownMenuSubTrigger>Stash</DropdownMenuSubTrigger>
                  <DropdownMenuSubContent>
                    <DropdownMenuItem onSelect={() => void run(() => gitStashPush(wsPath, repoRel, ''))}>Stash All</DropdownMenuItem>
                    <DropdownMenuItem
                      disabled={(snap?.Stashes ?? []).length === 0}
                      onSelect={() => void run(() => gitStashPop(wsPath, repoRel, 0))}
                    >
                      Pop 最新
                    </DropdownMenuItem>
                  </DropdownMenuSubContent>
                </DropdownMenuSub>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>

        <div className="flex min-h-0 flex-col" style={{ height: `${split * 100}%` }}>
          <Input
            size="sm"
            className="mb-1 w-full shrink-0"
            placeholder={`消息 (Ctrl+Enter 在 '${branchLabel}' 提交)`}
            value={msg}
            onChange={(e) => setMsg(e.target.value)}
            onKeyDown={onMsgKey}
            aria-label="提交说明"
          />
          <div className="mb-1 flex w-full shrink-0">
            <Button
              size="sm"
              className="h-8 min-w-0 flex-1 rounded-r-none"
              disabled={busy || (dirty ? !canCommit : !canSync)}
              aria-label="主 Git 操作"
              onClick={() => {
                if (dirty) doCommit();
                else void sync();
              }}
            >
              {dirty ? (
                <>
                  <Check className="h-3.5 w-3.5" />
                  提交
                </>
              ) : (
                <>
                  <ArrowDownUp className="h-3.5 w-3.5" />
                  同步
                </>
              )}
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  size="sm"
                  className="h-8 w-8 shrink-0 rounded-l-none border-l border-primary-foreground/25 px-0"
                  disabled={busy}
                  aria-label="更多提交操作"
                >
                  <ChevronDown className="h-3.5 w-3.5" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem disabled={!canCommit} onSelect={() => doCommit()}>
                  提交
                </DropdownMenuItem>
                <DropdownMenuItem
                  disabled={!canCommit}
                  onSelect={() => {
                    const m = msg.trim();
                    if (!m) return;
                    setMsg('');
                    void run(async () => {
                      await gitCommit(wsPath, repoRel, m);
                      await gitPush(wsPath, repoRel);
                    }, '已提交并推送');
                  }}
                >
                  提交并推送
                </DropdownMenuItem>
                <DropdownMenuItem disabled={!canSync} onSelect={() => void sync()}>
                  同步
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
          <div className="min-h-0 flex-1 overflow-auto">
            <GitChangeTree
              title="已暂存"
              entries={staged}
              side="staged"
              onOpen={openEntry}
              onStage={null}
              onUnstage={(paths) => void run(() => gitUnstage(wsPath, repoRel, paths))}
              onDiscard={null}
            />
            <GitChangeTree
              title="更改"
              entries={working}
              side="working"
              onOpen={openEntry}
              onStage={(paths) => void run(() => gitStage(wsPath, repoRel, paths))}
              onUnstage={null}
              onDiscard={(paths, label) => {
                if (!window.confirm(`丢弃 ${label} 的改动？`)) return;
                void run(() => gitDiscard(wsPath, repoRel, paths));
              }}
            />
            <div className="mt-1">
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
        </div>

        <div
          role="separator"
          aria-orientation="horizontal"
          aria-label="调整 Git 上下分栏"
          className="h-1.5 shrink-0 cursor-ns-resize bg-border/80 hover:bg-primary/40"
          onPointerDown={onSplitPointer}
        />

        <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
          <div className="flex shrink-0 items-center gap-1 py-1">
            <select
              className={cn(selectEllipsis, 'h-7 flex-1')}
              aria-label="提交图分支筛选"
              value={logSel}
              onChange={(e) => setLogSelPersist(e.target.value)}
            >
              <option value="current">当前分支</option>
              <option value="all">全部</option>
              <optgroup label="本地">
                {localRefs.map((r) => (
                  <option key={`l:${r.Name}`} value={r.Name}>
                    {r.Name}
                  </option>
                ))}
              </optgroup>
              <optgroup label="远端">
                {remoteRefs.map((r) => (
                  <option key={`r:${r.Name}`} value={r.Name}>
                    {r.Name}
                  </option>
                ))}
              </optgroup>
            </select>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button size="icon" variant="ghost" disabled={busy} aria-label="Fetch" onClick={() => void run(() => gitFetch(wsPath, repoRel))}>
                  <Download className="h-3.5 w-3.5" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Fetch</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  size="icon"
                  variant="ghost"
                  disabled={busy}
                  aria-label="Fetch all"
                  onClick={() => void run(() => gitFetchAll(wsPath, repoRel))}
                >
                  <CloudDownload className="h-3.5 w-3.5" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Fetch all</TooltipContent>
            </Tooltip>
          </div>
          <GitLogGraph commits={toGraph(commits)} selected={picked} onSelect={setPicked} loadStat={loadStat} />
        </div>
      </div>
    </TooltipProvider>
  );
}
