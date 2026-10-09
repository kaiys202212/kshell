// HTML 浏览器页签：顶层文档以 blob URL 装进 sandbox iframe（不经过 Wails 资产
// 服务器，无 runtime 注入、opaque origin），相对资源经 Go 侧受限通道
// /__kshell-file/（工作区根限定 + 扩展名白名单）加载。
// iframe 内点击链接/提交表单会被注入脚本拦截并通知父窗口，用系统默认应用
// 打开本页（不走 BrowserOpenURL(file://)——Wails 校验会拒绝 file scheme）。
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { openInDefaultApp, readFileBytes } from '../lib/api';
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

// 注入 head：storage polyfill（opaque origin 下访问 localStorage/cookie 会抛
// SecurityError，连带页面脚本崩溃、按钮失去交互——用内存实现兜底）+ 导航拦截
// + <base>。脚本置于 head 最前，先于页面自身脚本执行。
const NAV_SCRIPT = `<script>(function(){
  function fake(){ var d={}; return {getItem:function(k){return Object.prototype.hasOwnProperty.call(d,k)?d[k]:null},setItem:function(k,v){d[k]=String(v)},removeItem:function(k){delete d[k]},clear:function(){d={}},key:function(i){return Object.keys(d)[i]||null},get length(){return Object.keys(d).length}}; }
  ['localStorage','sessionStorage'].forEach(function(name){
    try { window[name].getItem('__probe__'); } catch(e) {
      try { Object.defineProperty(window, name, { get: fake, configurable: true }); } catch(e2) {}
    }
  });
  try { document.cookie; } catch(e) {
    try { Object.defineProperty(document, 'cookie', { get: function(){return ''}, set: function(){}, configurable: true }); } catch(e2) {}
  }
  function openExternal(){ try { parent.postMessage({ kshell: 'open-external' }, '*'); } catch(e) {} }
  document.addEventListener('click', function(e){
    var el = e.target;
    while (el && el.nodeType === 1) {
      if (el.tagName === 'A' && el.getAttribute('href')) { e.preventDefault(); openExternal(); return; }
      el = el.parentNode;
    }
  }, true);
  document.addEventListener('submit', function(e){ e.preventDefault(); openExternal(); }, true);
})();</scr` + `ipt>`;

export function injectBase(html: string, baseHref: string): string {
  const head = `${NAV_SCRIPT}<base href="${baseHref}">`;
  // 前瞻防误配 <header>
  const m = html.match(/<head(?=[\s>])[^>]*>/i);
  if (m && m.index !== undefined) {
    const at = m.index + m[0].length;
    return html.slice(0, at) + head + html.slice(at);
  }
  const dt = html.match(/<!doctype[^>]*>/i);
  if (dt && dt.index !== undefined) {
    const at = dt.index + dt[0].length;
    return html.slice(0, at) + head + html.slice(at);
  }
  return head + html;
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
  // 绑定缺失单独记 flag：文案在渲染时翻译，避免 t 进入 load 依赖、切语言重拉文件
  const [missingBinding, setMissingBinding] = useState(false);
  const [loading, setLoading] = useState(false);
  const [reloadTick, setReloadTick] = useState(0);
  // 用 state 驱动「浏览器打开」可点态（ref 不触发重渲染，会导致按钮一直 disabled）
  const [ready, setReady] = useState(false);
  const urlRef = useRef<string | null>(null);

  const openLocal = () => {
    void openInDefaultApp(wsPath, path).catch(() => {
      /* 打开失败时静默：系统侧偶发，避免打断预览 */
    });
  };

  // iframe 内导航/表单被拦截后 postMessage 到此，改用系统默认应用打开本页
  useEffect(() => {
    const onMessage = (e: MessageEvent) => {
      if ((e.data as { kshell?: string } | null)?.kshell === 'open-external') {
        void openInDefaultApp(wsPath, path).catch(() => {});
      }
    };
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
  }, [wsPath, path]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError('');
    setMissingBinding(false);
    setBlobUrl(null);
    setReady(false);
    readFileBytes(wsPath, path)
      .then((data) => {
        if (cancelled) return;
        if (!data || !data.AbsPath) {
          setMissingBinding(true);
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
        setReady(true);
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
  }, [wsPath, path, reloadTick]);

  return (
    <div className="flex h-full min-h-0 flex-col text-sm">
      <div className="flex shrink-0 items-center gap-2 border-b border-border px-2 py-1">
        <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground" title={path}>
          {path}
        </span>
        <Button
          size="sm"
          variant="secondary"
          disabled={loading || !ready}
          onClick={openLocal}
        >
          {t('ui.files.html_preview_open_browser')}
        </Button>
        <Button
          size="sm"
          variant="secondary"
          disabled={loading}
          onClick={() => setReloadTick((v) => v + 1)}
        >
          {t('ui.files.html_preview_reload')}
        </Button>
      </div>
      {missingBinding && <p className="p-2 text-sm text-destructive">{t('ui.files.binding_missing')}</p>}
      {error && <p className="p-2 text-sm text-destructive">{error}</p>}
      {!error && !missingBinding && (
        <div className="min-h-0 flex-1">
          {blobUrl && (
            <iframe
              sandbox="allow-scripts allow-forms"
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
