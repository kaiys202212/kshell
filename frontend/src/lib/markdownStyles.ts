// markdown 气泡共享样式：ChatView / AgentHubPanel 的聊天正文与 MarkdownPreview
// 共用一套 [&_xxx] 元素映射，保证全应用 markdown 渲染观感一致。
// 这里只含「内容元素」类；容器类（滚动/内边距）由调用方自带。
// 清单源自 MarkdownPreview 原有 bodyClass，气泡场景去掉容器专属项。
export const markdownBubbleClass = [
  'text-sm text-foreground',
  '[&_h1]:mb-2 [&_h1]:text-xl [&_h1]:font-semibold',
  '[&_h2]:mb-2 [&_h2]:text-lg [&_h2]:font-semibold',
  '[&_h3]:mb-1.5 [&_h3]:text-base [&_h3]:font-semibold',
  '[&_p]:mb-2 [&_ul]:mb-2 [&_ul]:list-disc [&_ul]:pl-5',
  '[&_ol]:mb-2 [&_ol]:list-decimal [&_ol]:pl-5',
  '[&_code]:rounded [&_code]:bg-muted [&_code]:px-1 [&_code]:font-mono [&_code]:text-xs',
  '[&_pre]:mb-2 [&_pre]:overflow-auto [&_pre]:rounded [&_pre]:bg-muted [&_pre]:p-2',
  '[&_pre]:whitespace-pre-wrap',
  '[&_a]:text-primary [&_a]:underline',
  '[&_blockquote]:border-l-2 [&_blockquote]:border-border [&_blockquote]:pl-3 [&_blockquote]:text-muted-foreground',
  '[&_table]:mb-2 [&_table]:w-full [&_table]:border-collapse',
  '[&_th]:border [&_th]:border-border [&_th]:px-2 [&_th]:py-1 [&_th]:text-left',
  '[&_td]:border [&_td]:border-border [&_td]:px-2 [&_td]:py-1',
  '[&_mark]:rounded-[2px] [&_mark]:bg-primary/30 [&_mark]:text-inherit',
].join(' ');
