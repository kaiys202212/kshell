// Markdown 预览：react-markdown + remark-gfm，样式跟随主题文本色。
// 顶部检索工具条：渲染结果内高亮命中（rehypeHighlightSearch）、计数、上下跳转；
// 命中标记经 rehype 插件在 React 渲染路径内生成，不做 DOM 后处理。
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import type { Pluggable } from 'unified';
import { MD_SEARCH_LIMIT, rehypeHighlightSearch } from '../lib/rehypeHighlightSearch';
import { handleAnchorClick } from '../lib/openHref';

export interface MarkdownPreviewProps {
  markdown: string;
  workspaceRoot?: string;
  sourceFile?: string;
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

  const bodyClass = [
    'min-h-0 flex-1 overflow-auto p-3 text-sm text-foreground select-text',
    '[&_h1]:mb-2 [&_h1]:text-xl [&_h1]:font-semibold',
    '[&_h2]:mb-2 [&_h2]:text-lg [&_h2]:font-semibold',
    '[&_h3]:mb-1.5 [&_h3]:text-base [&_h3]:font-semibold',
    '[&_p]:mb-2 [&_ul]:mb-2 [&_ul]:list-disc [&_ul]:pl-5',
    '[&_ol]:mb-2 [&_ol]:list-decimal [&_ol]:pl-5',
    '[&_code]:rounded [&_code]:bg-muted [&_code]:px-1 [&_code]:font-mono [&_code]:text-xs',
    '[&_pre]:mb-2 [&_pre]:overflow-auto [&_pre]:rounded [&_pre]:bg-muted [&_pre]:p-2',
    '[&_a]:text-primary [&_a]:underline',
    '[&_blockquote]:border-l-2 [&_blockquote]:border-border [&_blockquote]:pl-3 [&_blockquote]:text-muted-foreground',
    '[&_table]:mb-2 [&_table]:w-full [&_table]:border-collapse',
    '[&_th]:border [&_th]:border-border [&_th]:px-2 [&_th]:py-1 [&_th]:text-left',
    '[&_td]:border [&_td]:border-border [&_td]:px-2 [&_td]:py-1',
    '[&_mark]:rounded-[2px] [&_mark]:bg-primary/30 [&_mark]:text-inherit',
  ].join(' ');

  return (
    <div
      data-kshell-workspace={workspaceRoot}
      data-kshell-source-file={sourceFile}
      onClickCapture={(e) => {
        handleAnchorClick(e.nativeEvent, { workspaceRoot, sourceFile });
      }}
      className="flex h-full min-h-0 flex-col text-sm text-foreground"
    >
      <div
        data-testid="md-search-bar"
        className="flex shrink-0 items-center justify-end gap-1 border-b border-border px-2 py-1"
      >
        {open ? (
          <>
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
          </>
        ) : (
          <button
            type="button"
            aria-label={t('ui.files.md_search_toggle')}
            title={t('ui.files.md_search_toggle')}
            onClick={() => setOpen(true)}
            className="flex h-6 w-6 items-center justify-center rounded-sm text-muted-foreground hover:bg-muted hover:text-foreground"
          >
            <svg viewBox="0 0 24 24" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="2">
              <circle cx="11" cy="11" r="7" />
              <path strokeLinecap="round" d="m20 20-3.5-3.5" />
            </svg>
          </button>
        )}
      </div>
      <div ref={scrollRef} className={bodyClass}>
        <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={rehypePlugins}>
          {markdown}
        </ReactMarkdown>
      </div>
    </div>
  );
}
