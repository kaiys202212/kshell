// 中栏文件页签内的 git diff（按 hunk 暂存/丢弃）。
import { useCallback, useEffect, useState } from 'react';
import {
  gitDiff,
  gitDiscard,
  gitDiscardHunk,
  gitStage,
  gitStageHunk,
  gitUnstage,
  gitUnstageHunk,
} from '../lib/api';
import type { GitDiffSide } from '../lib/api';
import { hunkPatch, parseDiffHunks } from '../lib/gitDiff';
import { refreshGitStatus } from '../lib/git';
import { cn } from '../lib/cn';
import { useAppStore } from '../state/store';
import { Button } from './ui/button';

export default function GitDiffView({
  wsPath,
  repoRel,
  path,
  side,
}: {
  wsPath: string;
  repoRel: string;
  path: string;
  side: GitDiffSide;
}) {
  const notify = useAppStore((s) => s.notify);
  const [text, setText] = useState('');
  const [binary, setBinary] = useState(false);
  const [err, setErr] = useState('');

  const load = useCallback(async () => {
    try {
      const d = await gitDiff(wsPath, repoRel, path, side);
      setBinary(!!d.Binary);
      setText(d.Text ?? '');
      setErr('');
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  }, [wsPath, repoRel, path, side]);

  useEffect(() => {
    void load();
  }, [load]);

  const run = async (fn: () => Promise<void>) => {
    try {
      await fn();
      await refreshGitStatus(wsPath);
      await load();
    } catch (e) {
      notify(e instanceof Error ? e.message : String(e), 'error');
    }
  };

  const hunks = parseDiffHunks(text);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 flex-wrap items-center gap-1 border-b border-border pb-2">
        <span className="mr-auto truncate font-mono text-xs">{path}</span>
        {side === 'working' && (
          <Button size="sm" onClick={() => void run(() => gitStage(wsPath, repoRel, [path]))}>
            暂存文件
          </Button>
        )}
        {side === 'staged' && (
          <Button size="sm" variant="secondary" onClick={() => void run(() => gitUnstage(wsPath, repoRel, [path]))}>
            取消暂存
          </Button>
        )}
        {side === 'working' && (
          <Button
            size="sm"
            variant="destructive"
            onClick={() => {
              if (!window.confirm(`丢弃 ${path} 的工作区改动？`)) return;
              void run(() => gitDiscard(wsPath, repoRel, [path]));
            }}
          >
            丢弃文件
          </Button>
        )}
      </div>
      {err && <p className="p-2 text-sm text-destructive">{err}</p>}
      {binary && <p className="p-2 text-sm text-muted-foreground">二进制文件无法预览 diff</p>}
      <div className="min-h-0 flex-1 overflow-auto font-mono text-[11px] leading-5">
        {hunks.map((h, i) => (
          <div key={i} className="mb-3 border-b border-border pb-2">
            <div className="sticky top-0 z-[1] flex gap-1 bg-card py-1">
              {side === 'working' && (
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => void run(() => gitStageHunk(wsPath, repoRel, path, side, hunkPatch(h)))}
                >
                  暂存此块
                </Button>
              )}
              {side === 'staged' && (
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => void run(() => gitUnstageHunk(wsPath, repoRel, path, side, hunkPatch(h)))}
                >
                  取消暂存此块
                </Button>
              )}
              {side === 'working' && (
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    if (!window.confirm('丢弃此 hunk？')) return;
                    void run(() => gitDiscardHunk(wsPath, repoRel, path, side, hunkPatch(h)));
                  }}
                >
                  丢弃此块
                </Button>
              )}
            </div>
            <pre className="whitespace-pre-wrap">
              {h.body.split('\n').map((line, j) => (
                <div
                  key={j}
                  className={cn(
                    line.startsWith('+') && !line.startsWith('+++') && 'bg-emerald-500/15',
                    line.startsWith('-') && !line.startsWith('---') && 'bg-destructive/15',
                    line.startsWith('@@') && 'text-muted-foreground',
                  )}
                >
                  {line}
                </div>
              ))}
            </pre>
          </div>
        ))}
      </div>
    </div>
  );
}
