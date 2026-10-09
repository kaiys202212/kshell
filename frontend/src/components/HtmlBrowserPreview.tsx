// HTML 浏览器页签：顶层文档以 blob URL 装进 sandbox iframe（不经过 Wails 资产
// 服务器，无 runtime 注入、opaque origin），相对资源经 Go 侧受限通道
// /__kshell-file/（工作区根限定 + 扩展名白名单）加载。
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { readFileBytes } from '../lib/api';
import { backendError } from '../lib/errors';
import { Button } from './ui/button';

// base64url（无填充）编码 UTF-8 字符串，与 Go base64.RawURLEncoding 对齐
export function base64UrlUtf8(s: string): string {
  const bytes = new TextEncoder().encode(s);
  let bin = '';
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

// 目录绝对路径（兼容 Windows 分隔符）
export function dirName(abs: string): string {
  const i = Math.max(abs.lastIndexOf('\\'), abs.lastIndexOf('/'));
  return i > 0 ? abs.slice(0, i) : abs;
}

// 注入 <base>：有 <head> 插其后，否则前置
export function injectBase(html: string, baseHref: string): string {
  const tag = `<base href="${baseHref}">`;
  const m = html.match(/<head[^>]*>/i);
  if (m && m.index !== undefined) {
    const at = m.index + m[0].length;
    return html.slice(0, at) + tag + html.slice(at);
  }
  return tag + html;
}

export default function HtmlBrowserPreview({
  wsPath,
  path,
}: {
  wsPath: string;
  path: string;
}) {
  const { t } = useTranslation();
  const [blobUrl, setBlobUrl] = useState<string | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [reloadTick, setReloadTick] = useState(0);
  const urlRef = useRef<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError('');
    setBlobUrl(null);
    readFileBytes(wsPath, path)
      .then((data) => {
        if (cancelled) return;
        if (!data || !data.AbsPath) {
          setError(t('ui.files.binding_missing'));
          return;
        }
        const dir = dirName(data.AbsPath);
        const baseHref = `${window.location.origin}/__kshell-file/${base64UrlUtf8(dir)}/`;
        const text = new TextDecoder().decode(
          Uint8Array.from(atob(data.Base64), (c) => c.charCodeAt(0)),
        );
        const blob = new Blob([injectBase(text, baseHref)], { type: 'text/html' });
        const url = URL.createObjectURL(blob);
        if (urlRef.current) URL.revokeObjectURL(urlRef.current);
        urlRef.current = url;
        setBlobUrl(url);
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(backendError(e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
      if (urlRef.current) {
        URL.revokeObjectURL(urlRef.current);
        urlRef.current = null;
      }
    };
  }, [wsPath, path, reloadTick, t]);

  return (
    <div className="flex h-full min-h-0 flex-col text-sm">
      <div className="flex shrink-0 items-center gap-2 border-b border-border px-2 py-1">
        <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground" title={path}>
          {path}
        </span>
        <Button
          size="sm"
          variant="secondary"
          disabled={loading}
          onClick={() => setReloadTick((v) => v + 1)}
        >
          {t('ui.files.html_preview_reload')}
        </Button>
      </div>
      {error && <p className="p-2 text-sm text-destructive">{error}</p>}
      {!error && (
        <div className="min-h-0 flex-1">
          {blobUrl && (
            <iframe
              sandbox="allow-scripts"
              src={blobUrl}
              title={path}
              data-testid="html-browser-frame"
              className="h-full w-full border-0"
            />
          )}
        </div>
      )}
    </div>
  );
}
