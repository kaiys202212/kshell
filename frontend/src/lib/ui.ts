// 共享视觉常量（S1 C 视觉重塑）：把重复出现的页签/列表行/面板标题/等宽样式集中到一处。
// 抽取以「消除 3 处以上重复」为准——TitleBar、中心区页签、右栏子页签各写了一份 tab 样式。
export const TAB_BASE =
  'relative flex shrink-0 items-center gap-1.5 px-2.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground';
export const TAB_ACTIVE = 'font-medium text-foreground';
export const TAB_UNDERLINE =
  'absolute bottom-0 left-1/2 h-0.5 w-[60%] -translate-x-1/2 rounded-full bg-primary';
// 页签 hover 预览条：同款短条但半透明，hover 才显现（active 页签不渲染）
export const TAB_UNDERLINE_PREVIEW =
  'absolute bottom-0 left-1/2 h-0.5 w-[60%] -translate-x-1/2 rounded-full bg-primary/40 opacity-0 transition-opacity group-hover:opacity-100';
export const LIST_ROW = 'rounded border border-border bg-card transition-colors hover:bg-muted/50';
export const LIST_ROW_ACTIVE = 'border-l-2 border-l-primary bg-primary/8';
export const PANE_HEADER = 'text-[11px] text-muted-foreground';
export const MONO = 'font-mono text-[11px] text-muted-foreground';
