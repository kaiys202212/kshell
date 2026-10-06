// Markdown 预览：react-markdown + remark-gfm，样式跟随主题文本色。
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
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
  return (
    <div
      data-kshell-workspace={workspaceRoot}
      data-kshell-source-file={sourceFile}
      onClickCapture={(e) => {
        handleAnchorClick(e.nativeEvent, { workspaceRoot, sourceFile });
      }}
      className={[
        'h-full overflow-auto p-3 text-sm text-foreground select-text',
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
      ].join(' ')}
    >
      <ReactMarkdown remarkPlugins={[remarkGfm]}>{markdown}</ReactMarkdown>
    </div>
  );
}
