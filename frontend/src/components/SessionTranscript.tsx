// 未激活会话的只读 Markdown 预览：顶部激活按钮 + MarkdownPreview。
import { useEffect, useState } from 'react';
import { getSessionPreview } from '../lib/api';
import MarkdownPreview from './MarkdownPreview';
import { Button } from './ui/button';
import { EmptyState } from './ui/empty-state';
import { Skeleton } from './ui/skeleton';

export default function SessionTranscript({
  sessionID,
  title,
  workspaceRoot,
  onActivate,
}: {
  sessionID: string;
  title: string;
  workspaceRoot?: string;
  onActivate: () => void;
}) {
  const [markdown, setMarkdown] = useState('');
  const [truncated, setTruncated] = useState(false);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError('');
    setMarkdown('');
    void getSessionPreview(sessionID)
      .then((p) => {
        if (cancelled) return;
        setMarkdown(p.Markdown ?? '');
        setTruncated(!!p.Truncated);
      })
      .catch((e: unknown) => {
        if (cancelled) return;
        setError(e instanceof Error ? e.message : String(e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [sessionID]);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b border-border px-3 py-2">
        <p className="min-w-0 flex-1 truncate text-sm font-medium">{title || '会话预览'}</p>
        <Button size="sm" onClick={onActivate}>
          激活
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-hidden">
        {loading ? (
          <div className="flex flex-col gap-2 p-3">
            <Skeleton className="h-6 w-1/2" />
            <Skeleton className="h-24 w-full" />
          </div>
        ) : error ? (
          <EmptyState title="无法加载会话" hint={error} />
        ) : (
          <>
            {truncated && (
              <p className="border-b border-border px-3 py-1 text-xs text-muted-foreground">内容已截断</p>
            )}
            <MarkdownPreview markdown={markdown} workspaceRoot={workspaceRoot} />
          </>
        )}
      </div>
    </div>
  );
}
