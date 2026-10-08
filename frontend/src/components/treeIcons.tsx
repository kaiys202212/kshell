// 文件树 / Git 变更树共用的内联 SVG 图标（不引图标库）。
import { cn } from '../lib/cn';

export function ChevronIcon({ open }: { open: boolean }) {
  return (
    <svg
      viewBox="0 0 16 16"
      className={cn('h-3.5 w-3.5 transition-transform', open && 'rotate-90')}
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      aria-hidden="true"
      data-icon="chevron"
    >
      <path strokeLinecap="round" strokeLinejoin="round" d="M6 4l4 4-4 4" />
    </svg>
  );
}

export function FolderIcon({ open }: { open: boolean }) {
  return (
    <svg
      viewBox="0 0 16 16"
      className="h-3.5 w-3.5 shrink-0 text-primary/70"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.2"
      aria-hidden="true"
      data-icon={open ? 'folder-open' : 'folder'}
    >
      {open ? (
        <path
          strokeLinecap="round"
          strokeLinejoin="round"
          d="M1.5 5.5A1 1 0 0 1 2.5 4.5h3l1.2 1.2h5.8a1 1 0 0 1 1 1V7H5.2a1 1 0 0 0-.97.757L3 12.5H2.5a1 1 0 0 1-1-1v-6Zm2.3 7 1.1-4.1a.5.5 0 0 1 .48-.4h8.4a.5.5 0 0 1 .48.65L13.3 12a1 1 0 0 1-.96.72H3.8Z"
        />
      ) : (
        <path
          strokeLinecap="round"
          strokeLinejoin="round"
          d="M1.5 4.5a1 1 0 0 1 1-1h3l1.4 1.4h5.6a1 1 0 0 1 1 1v6.1a1 1 0 0 1-1 1h-10a1 1 0 0 1-1-1v-7.5Z"
        />
      )}
    </svg>
  );
}

export function FileIcon() {
  return (
    <svg
      viewBox="0 0 16 16"
      className="h-3.5 w-3.5 shrink-0 text-muted-foreground"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.2"
      aria-hidden="true"
      data-icon="file"
    >
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        d="M4 2.5h5l3 3v8a.5.5 0 0 1-.5.5h-7a.5.5 0 0 1-.5-.5v-10.5a.5.5 0 0 1 .5-.5ZM9 2.5V6h3.5"
      />
    </svg>
  );
}
