// 文件预览（工作区页签中间栏）：只读展示，等宽字体。
// Go 侧 PreviewFile 返回的 Lines 已带 "NNNN │ " 行号前缀，前端直接渲染、不再加行号；
// Truncated 显示截断提示，Binary 只展示 Info 元信息。
import { useEffect, useState } from 'react';
import { previewFile } from '../lib/api';
import type { FilePreview } from '../lib/api';

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
    <div className="preview">
      <div className="preview-head">
        <span className="preview-path" title={path ?? ''}>
          {path ?? '未选择文件'}
        </span>
      </div>
      {!path && <p className="preview-status">从右侧文件树选择文件查看预览</p>}
      {loading && <p className="preview-status">加载中……</p>}
      {error && <p className="preview-error">{error}</p>}
      {!loading && !error && data?.Binary && (
        <p className="preview-status">{data.Info || '二进制文件，无法预览'}</p>
      )}
      {!loading && !error && data && !data.Binary && (
        <>
          {data.Info && <p className="preview-meta">{data.Info}</p>}
          {/* 截断行数 500 与 Go 侧 internal/workspace/preview.go 的预览行数上限耦合，改一处需同步 */}
          {data.Truncated && <p className="preview-truncated">内容已截断：仅显示前 500 行</p>}
          <pre className="preview-pre">{data.Lines.join('\n')}</pre>
        </>
      )}
    </div>
  );
}
