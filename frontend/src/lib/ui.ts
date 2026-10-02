// 共享视觉常量（S1 C 视觉重塑）：把重复出现的页签/列表行/面板标题/等宽样式集中到一处。
// 抽取以「消除 3 处以上重复」为准——TitleBar、中心区页签、右栏子页签各写了一份 tab 样式。
export const TAB_BASE =
  'relative flex shrink-0 items-center gap-1.5 px-2.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground';
export const TAB_ACTIVE = 'font-medium text-foreground';
export const TAB_UNDERLINE = 'absolute inset-x-1.5 bottom-0 h-0.5 bg-primary';
export const LIST_ROW = 'rounded border border-border bg-card transition-colors hover:bg-muted/50';
export const LIST_ROW_ACTIVE = 'border-l-2 border-l-primary bg-primary/5';
export const PANE_HEADER = 'text-[11px] text-muted-foreground';
export const MONO = 'font-mono text-[11px] text-muted-foreground';
