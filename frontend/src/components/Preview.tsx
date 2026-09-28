// 文件预览（工作区页签中间栏）：只读展示，等宽字体。
// Go 侧 PreviewFile 返回的 Lines 已带 "NNNN │ " 行号前缀，前端直接渲染、不再加行号；
// Truncated 显示截断提示，Binary 只展示 Info 元信息。
import { useEffect, useState } from 'react';
import { previewFile } from '../lib/api';
import type { FilePreview } from '../lib/api';
import { Badge } from './ui/badge';
import { EmptyState } from './ui/empty-state';
import { Skeleton } from './ui/skeleton';

export default function Preview({ wsPath, path }: { wsPath: string; path: string | null }) {
  const [data, setData] = useState<FilePreview | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!path) {
      setData(null);
      setError('');
      setLoading(false);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError('');
    setData(null);
    previewFile(wsPath, path)
      .then((p) => {
        if (cancelled) return;
        if (!p) {
          setError('未检测到 kshell 桌面端绑定，请在桌面端运行');
          return;
        }
        setData(p);
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [wsPath, path]);

  return (
    <div className="flex min-h-0 flex-col text-sm">
      <div className="sticky top-0 z-10 flex items-center border-b border-border bg-card py-2">
        <span
          className="truncate font-mono text-xs text-muted-foreground"
          title={path ?? ''}
        >
          {path ?? '未选择文件'}
        </span>
      </div>
      {!path && <EmptyState title="从右侧文件树选择文件查看预览" />}
      {loading && (
        <div className="mt-3 flex flex-col gap-2">
          {[0, 1, 2, 3, 4, 5].map((i) => (
            <Skeleton key={i} className="h-4" style={{ width: `${88 - (i % 3) * 18}%` }} />
          ))}
        </div>
      )}
      {error && <p className="text-sm text-destructive">{error}</p>}
      {!loading && !error && data?.Binary && (
        <p className="text-sm text-muted-foreground">{data.Info || '二进制文件，无法预览'}</p>
      )}
      {!loading && !error && data && !data.Binary && (
        <>
          {data.Info && <p className="mb-2 text-xs text-muted-foreground">{data.Info}</p>}
          {/* 截断行数 500 与 Go 侧 internal/workspace/preview.go 的预览行数上限耦合，改一处需同步 */}
          {data.Truncated && (
            <Badge variant="outline" className="mb-2 w-fit">
              内容已截断：仅显示前 500 行
            </Badge>
          )}
          <pre className="overflow-auto rounded-md border border-border bg-card p-3 font-mono text-[13px] leading-relaxed whitespace-pre">
            {data.Lines.join('\n')}
          </pre>
        </>
      )}
    </div>
  );
}
