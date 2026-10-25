// Markdown 预览：react-markdown + remark-gfm，样式跟随主题文本色。
// 顶部检索工具条：渲染结果内高亮命中（rehypeHighlightSearch）、计数、上下跳转；
// 命中标记经 rehype 插件在 React 渲染路径内生成，不做 DOM 后处理。
import { useEffect, useLayoutEffect, useMemo, useRef, useState, type ComponentProps } from 'react';
import { useTranslation } from 'react-i18next';
import ReactMarkdown, { defaultUrlTransform, type Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import type { Pluggable } from 'unified';
import { MD_SEARCH_LIMIT, rehypeHighlightSearch } from '../lib/rehypeHighlightSearch';
import { workspaceFileUrl } from '../lib/fileUrl';
import { classifyHref, handleAnchorClick } from '../lib/openHref';
import { markdownBubbleClass } from '../lib/markdownStyles';

export interface MarkdownPreviewProps {
  markdown: string;
  workspaceRoot?: string;
  sourceFile?: string;
}

// 默认 urlTransform 把 `D:/…` 盘符路径当未知协议清成空串;单字母"协议"实为
// Windows 盘符,放行后由 img 组件按工作区解析,安全性仍由 classifyHref 兜底。
function mdUrlTransform(url: string): string {
  const m = url.match(/^([a-zA-Z][a-zA-Z0-9+.-]*):/);
  if (m && m[1].length === 1) return url;
  return defaultUrlTransform(url);
}

export default function MarkdownPreview({
  markdown,
  workspaceRoot,
  sourceFile,
}: MarkdownPreviewProps) {
  const { t } = useTranslation();
  const [raw, setRaw] = useState('');
  const [query, setQuery] = useState('');
  const [hits, setHits] = useState(0);
  const [idx, setIdx] = useState(-1);
  // 检索工具条默认隐藏：Ctrl+F（可取消事件，本预览可见时优先消费）或右上搜索按钮呼出
  const [open, setOpen] = useState(false);
  const totalRef = useRef(0);
  const scrollRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  // Ctrl+F 呼出检索：捕获阶段先于 WorkspaceSearch（冒泡）收到；仅本预览可见时消费。
  // 可见性用 offsetParent 判断（hidden 父容器下为 null，jsdom 无布局为 undefined）——
  // 页签常挂载、非激活时不响应。
  useEffect(() => {
    const onFocusSearch = (e: Event) => {
      if (!scrollRef.current || scrollRef.current.offsetParent == null) return;
      e.preventDefault();
      setOpen(true);
    };
    window.addEventListener('kshell:focus-search', onFocusSearch, true);
    return () => window.removeEventListener('kshell:focus-search', onFocusSearch, true);
  }, []);

  // 打开时聚焦输入框
  useEffect(() => {
    if (open) inputRef.current?.focus();
  }, [open]);

  // 输入防抖 200ms 后生效，避免每个按键都重渲染整棵 markdown
  useEffect(() => {
    const id = setTimeout(() => setQuery(raw.trim()), 200);
    return () => clearTimeout(id);
  }, [raw]);

  // 注意元组形式：react-markdown 会把 rehypePlugins 里的函数当插件再以参数调用，
  // 必须传 [工厂, query, totalRef]，工厂内部才返回真正的 transformer
  const rehypePlugins = useMemo<Pluggable[]>(
    () => (query ? [[rehypeHighlightSearch, query, totalRef] as Pluggable] : []),
    [query],
  );

  // 图片 src:工作区内(相对 sourceFile 所在目录)改走 /__kshell-file/ 受限通道;
  // 外链、无工作区上下文、工作区外路径原样保留(与改动前行为一致)。
  const components = useMemo<Components>(() => {
    const opts = { workspaceRoot, sourceFile };
    return {
      img: ({ src, ...rest }: ComponentProps<'img'>) => {
        const action = classifyHref(src ?? '', opts);
        if (action.kind !== 'workspace') return <img src={src} {...rest} />;
        return <img src={workspaceFileUrl(action.path)} {...rest} />;
      },
    };
  }, [workspaceRoot, sourceFile]);

  // 渲染完成后读取插件写入的命中数（react-markdown 同步转换）
  useLayoutEffect(() => {
    setHits(totalRef.current);
    setIdx(totalRef.current > 0 ? 0 : -1);
  }, [markdown, query]);

  // 当前命中滚动到可视区中央
  useEffect(() => {
    if (idx < 0) return;
    scrollRef.current
      ?.querySelector(`[data-md-hit="${idx}"]`)
      ?.scrollIntoView?.({ block: 'center' });
  }, [idx, query, markdown]);

  const goto = (delta: number) => {
    if (hits <= 0) return;
    setIdx((i) => (i + delta + hits) % hits);
  };

  // 内容元素样式共享自 markdownStyles；容器类（滚动/内边距）是本预览专属。
  const bodyClass = `min-h-0 flex-1 overflow-auto p-3 select-text ${markdownBubbleClass}`;

  return (
    <div
      data-kshell-workspace={workspaceRoot}
      data-kshell-source-file={sourceFile}
      onClickCapture={(e) => {
        handleAnchorClick(e.nativeEvent, { workspaceRoot, sourceFile });
      }}
      className="flex h-full min-h-0 flex-col text-sm text-foreground"
    >
      {open && (
        <div
          data-testid="md-search-bar"
          className="flex shrink-0 items-center justify-end gap-1 border-b border-border px-2 py-1"
        >
          <input
            ref={inputRef}
            type="text"
            aria-label={t('ui.files.md_search_placeholder')}
            placeholder={t('ui.files.md_search_placeholder')}
            value={raw}
            onChange={(e) => setRaw(e.target.value)}
            className="h-7 w-44 rounded-[3px] border border-input bg-card px-2 text-xs outline-none placeholder:text-muted-foreground focus:border-primary/60"
          />
          {query && (
            <span className="text-xs text-muted-foreground" data-testid="md-search-count">
              {hits > MD_SEARCH_LIMIT ? `${MD_SEARCH_LIMIT}+` : hits}
            </span>
          )}
          <button
            type="button"
            aria-label={t('ui.files.md_search_prev')}
            disabled={hits <= 0}
            onClick={() => goto(-1)}
            className="flex h-6 w-6 items-center justify-center rounded-sm text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-40"
          >
            ↑
          </button>
          <button
            type="button"
            aria-label={t('ui.files.md_search_next')}
            disabled={hits <= 0}
            onClick={() => goto(1)}
            className="flex h-6 w-6 items-center justify-center rounded-sm text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-40"
          >
            ↓
          </button>
          <button
            type="button"
            aria-label={t('ui.files.md_search_close')}
            onClick={() => {
              setRaw('');
              setOpen(false);
            }}
            className="flex h-6 w-6 items-center justify-center rounded-sm text-muted-foreground hover:bg-muted hover:text-foreground"
          >
            ×
          </button>
        </div>
      )}
      <div ref={scrollRef} className={bodyClass}>
        <ReactMarkdown
          remarkPlugins={[remarkGfm]}
          rehypePlugins={rehypePlugins}
          urlTransform={mdUrlTransform}
          components={components}
        >
          {markdown}
        </ReactMarkdown>
      </div>
    </div>
  );
}
