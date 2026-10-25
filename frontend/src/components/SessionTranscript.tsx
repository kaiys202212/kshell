// 未激活会话的只读 Markdown 预览：顶部激活按钮 + MarkdownPreview。
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getSessionPreview } from '../lib/api';
import { backendError } from '../lib/errors';
import { decodePercentText } from '../lib/displayText';
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
  const { t } = useTranslation();
  const [markdown, setMarkdown] = useState('');
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
      })
      .catch((e: unknown) => {
        if (cancelled) return;
        setError(backendError(e));
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
        <p className="min-w-0 flex-1 truncate text-sm font-medium">{title || t('ui.transcript.preview_title')}</p>
        <Button size="sm" onClick={onActivate}>
          {t('ui.transcript.activate')}
        </Button>
      </div>
      {/* flex 列布局：预览占满剩余高度可滚动。预览是 h-full 块级元素时会被
          overflow-hidden 裁掉底部，滚动条滚不到最底部。 */}
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
        {loading ? (
          <div className="flex flex-col gap-2 p-3">
            <Skeleton className="h-6 w-1/2" />
            <Skeleton className="h-24 w-full" />
          </div>
        ) : error ? (
          <EmptyState title={t('ui.transcript.load_failed')} hint={error} />
        ) : (
          <div className="min-h-0 flex-1">
            {/* 展示层还原百分号编码文件名；后端预览原文不动 */}
            <MarkdownPreview markdown={decodePercentText(markdown)} workspaceRoot={workspaceRoot} />
          </div>
        )}
      </div>
    </div>
  );
}
