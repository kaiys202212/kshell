// 文件树（工作区页签右栏「文件」页签）：递归组件 + 目录懒加载。
// Go 侧 ListFiles 只填充当前层子项（Children 字段无意义），
// 展开目录时必须再次调 listFiles(wsPath, 子目录相对路径)。
// relPath 统一用 / 拼接：Go 侧 filepath.Clean 会归一化为平台分隔符。
// 交互：点目录展开/收起、点文件回调 onOpenFile、铅笔按钮行内重命名；
// 顶部搜索框先过滤已加载节点，防抖后走 Go 递归搜索出平铺结果。
// git 状态：文件名右侧小色标（数据来自 store.gitStatus[wsPath]，lib/git.ts 负责刷新）。
import { useEffect, useState } from 'react';
import { listFiles, renameEntry, searchFiles } from '../lib/api';
import type { FileNode, SearchHit } from '../lib/api';
import { cn } from '../lib/cn';
import { refreshGitStatus } from '../lib/git';
import { useAppStore } from '../state/store';
import { EmptyState } from './ui/empty-state';
import { Input } from './ui/input';
import { Skeleton } from './ui/skeleton';

// 树条目：node 为 Go 返回的节点，relPath 是相对工作区的 / 分隔路径（ListFiles 用），
// children 为 null 表示子层尚未加载（懒加载）。
// error 只用于「子目录加载失败」的行内提示（根层失败由组件级 error 整树呈现）。
interface TreeItem {
  node: FileNode;
  relPath: string;
  children: TreeItem[] | null;
  expanded: boolean;
  error?: string;
}

// 手写内联 SVG（不引图标库）：chevron / folder / folder-open / file / 铅笔 / 刷新
function ChevronIcon({ open }: { open: boolean }) {
  return (
    <svg
      viewBox="0 0 16 16"
      className={cn('h-3.5 w-3.5 transition-transform', open && 'rotate-90')}
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      aria-hidden="true"
    >
      <path strokeLinecap="round" strokeLinejoin="round" d="M6 4l4 4-4 4" />
    </svg>
  );
}

function FolderIcon({ open }: { open: boolean }) {
  return (
    <svg
      viewBox="0 0 16 16"
      className="h-3.5 w-3.5 shrink-0 text-primary/70"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.2"
      aria-hidden="true"
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

function FileIcon() {
  return (
    <svg
      viewBox="0 0 16 16"
      className="h-3.5 w-3.5 shrink-0 text-muted-foreground"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.2"
      aria-hidden="true"
    >
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        d="M4 2.5h5l3 3v8a.5.5 0 0 1-.5.5h-7a.5.5 0 0 1-.5-.5v-10.5a.5.5 0 0 1 .5-.5ZM9 2.5V6h3.5"
      />
    </svg>
  );
}

function PencilIcon() {
  return (
    <svg
      viewBox="0 0 16 16"
      className="h-3.5 w-3.5"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.2"
      aria-hidden="true"
    >
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        d="M11.1 2.2a1.41 1.41 0 0 1 2 2l-7.4 7.4-2.9.9.9-2.9 7.4-7.4Z"
      />
    </svg>
  );
}

function RefreshIcon() {
  return (
    <svg
      viewBox="0 0 16 16"
      className="h-3.5 w-3.5"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      aria-hidden="true"
    >
      <path strokeLinecap="round" strokeLinejoin="round" d="M13.5 8a5.5 5.5 0 1 1-1.62-3.9M13.5 1.5v3h-3" />
    </svg>
  );
}

// git 状态小色标（S1：改用语义状态色，label/title 与既有测试一致）
const GIT_BADGES: Record<string, { label: string; cls: string; title: string }> = {
  modified: { label: 'M', cls: 'text-warning', title: '已修改' },
  added: { label: 'A', cls: 'text-success', title: '新增（已暂存）' },
  deleted: { label: 'D', cls: 'text-danger', title: '已删除' },
  renamed: { label: 'R', cls: 'text-info', title: '重命名' },
  untracked: { label: 'U', cls: 'text-success', title: '未跟踪' },
  conflicted: { label: '!', cls: 'text-danger', title: '合并冲突' },
};

function GitBadge({ code }: { code: string }) {
  const b = GIT_BADGES[code];
  if (!b) return null;
  return (
    <span
      className={cn('shrink-0 font-mono text-[11px] font-bold', b.cls)}
      title={`git：${b.title}`}
      aria-label={`git ${b.title}`}
    >
      {b.label}
    </span>
  );
}

function toItems(nodes: FileNode[], parentRel: string): TreeItem[] {
  return nodes.map((n) => ({
    node: n,
    relPath: parentRel ? `${parentRel}/${n.Name}` : n.Name,
    children: null,
    expanded: false,
  }));
}

// 按 relPath 在嵌套结构中定位并更新（不可变更新，递归下钻）
function updateItems(items: TreeItem[], relPath: string, fn: (t: TreeItem) => TreeItem): TreeItem[] {
  return items.map((it) => {
    if (it.relPath === relPath) return fn(it);
    return it.children ? { ...it, children: updateItems(it.children, relPath, fn) } : it;
  });
}

// 按名字子串过滤已加载的树（搜索防抖期间的即时反馈；未加载子层搜不到，
// 由后端递归搜索兜底——结果到达后整树被平铺结果列表替换）
function filterItems(items: TreeItem[], q: string): TreeItem[] {
  const out: TreeItem[] = [];
  for (const it of items) {
    const hit = it.node.Name.toLowerCase().includes(q);
    if (it.node.IsDir) {
      const kids = it.children ? filterItems(it.children, q) : null;
      if (hit || (kids && kids.length > 0)) {
        out.push({ ...it, children: kids, expanded: true, error: undefined });
      }
    } else if (hit) {
      out.push(it);
    }
  }
  return out;
}

interface RowProps {
  item: TreeItem;
  depth: number;
  gitMap?: Record<string, string>; // git 状态映射（键为 '/' 分隔 relPath），行内按自身 relPath 查
  onDirToggle(item: TreeItem): void;
  onOpenFile(path: string): void;
  onRename(relPath: string, newName: string): void;
}

function TreeRow({ item, depth, gitMap, onDirToggle, onOpenFile, onRename }: RowProps) {
  const { node } = item;
  const gitCode = node.IsDir ? undefined : gitMap?.[item.relPath];
  const [renaming, setRenaming] = useState(false);
  const [draft, setDraft] = useState(node.Name);

  const commitRename = () => {
    setRenaming(false);
    const name = draft.trim();
    if (!name || name === node.Name) return;
    onRename(item.relPath, name);
  };

  return (
    <li
      role="treeitem"
      aria-expanded={node.IsDir ? item.expanded : undefined}
    >
      <div
        className="group flex h-6 items-center gap-0.5 rounded pr-1 transition-colors hover:bg-muted"
        style={{ paddingLeft: depth * 14 }}
      >
        {renaming ? (
          <Input
            autoFocus
            className="h-5 min-w-0 flex-1 px-1 py-0 font-mono"
            value={draft}
            aria-label={`重命名 ${node.Name}`}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                commitRename();
              } else if (e.key === 'Escape') {
                e.preventDefault();
                setRenaming(false);
              }
            }}
            onBlur={commitRename}
          />
        ) : (
          <>
            <button
              className="flex min-w-0 flex-1 items-center gap-1 rounded px-1 py-0.5 text-left"
              title={node.Path}
              onClick={() => (node.IsDir ? onDirToggle(item) : onOpenFile(node.Path))}
            >
              <span className="flex h-4 w-4 shrink-0 items-center justify-center text-muted-foreground">
                {node.IsDir && <ChevronIcon open={item.expanded} />}
              </span>
              {node.IsDir ? <FolderIcon open={item.expanded} /> : <FileIcon />}
              <span
                className={cn(
                  'min-w-0 truncate font-mono text-xs',
                  node.IsDir ? 'font-medium' : 'text-foreground/90',
                )}
              >
                {node.Name}
              </span>
              {!node.IsDir && gitCode && <GitBadge code={gitCode} />}
            </button>
            <button
              className="shrink-0 rounded p-0.5 text-muted-foreground opacity-0 transition-opacity hover:text-primary group-hover:opacity-100"
              aria-label={`重命名 ${node.Name}`}
              title="重命名"
              onClick={() => {
                setDraft(node.Name);
                setRenaming(true);
              }}
            >
              <PencilIcon />
            </button>
          </>
        )}
      </div>
      {node.IsDir && item.expanded && item.error && (
        <div
          className="m-0.5 flex items-center gap-1.5 text-xs"
          style={{ paddingLeft: (depth + 1) * 14 }}
        >
          <span className="truncate text-destructive">{item.error}</span>
          <button
            className="shrink-0 rounded border border-border px-1.5 py-0.5 text-xs transition-colors hover:bg-muted"
            aria-label={`重试加载 ${node.Name}`}
            onClick={() => onDirToggle(item)}
          >
            重试
          </button>
        </div>
      )}
      {node.IsDir && item.expanded && item.children && (
        <ul role="group" className="m-0 list-none p-0">
          {item.children.map((c) => (
            <TreeRow
              key={c.node.Path}
              item={c}
              depth={depth + 1}
              gitMap={gitMap}
              onDirToggle={onDirToggle}
              onOpenFile={onOpenFile}
              onRename={onRename}
            />
          ))}
        </ul>
      )}
    </li>
  );
}

export default function FileTree({
  wsPath,
  onOpenFile,
}: {
  wsPath: string;
  onOpenFile(path: string): void;
}) {
  const [items, setItems] = useState<TreeItem[] | null>(null);
  const [error, setError] = useState('');
  const [query, setQuery] = useState('');
  const [hits, setHits] = useState<SearchHit[] | null>(null);
  const [searching, setSearching] = useState(false);
  const gitMap = useAppStore((s) => s.gitStatus[wsPath]);

  useEffect(() => {
    let cancelled = false;
    setItems(null);
    setError('');
    setQuery('');
    setHits(null);
    listFiles(wsPath, '')
      .then((nodes) => {
        if (!cancelled) setItems(toItems(nodes, ''));
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    // git 状态随树一起加载；失败静默（lib/git.ts 处理）
    void refreshGitStatus(wsPath);
    return () => {
      cancelled = true;
    };
  }, [wsPath]);

  // 搜索防抖：停顿 200ms 后走 Go 递归搜索（未加载的深层目录也能命中）。
  // 换查询即丢弃旧结果，防抖期间显示本地过滤视图，避免「旧结果 + 搜索中」并存。
  useEffect(() => {
    const q = query.trim();
    setHits(null);
    if (!q) {
      setSearching(false);
      return;
    }
    let cancelled = false;
    setSearching(true);
    const timer = setTimeout(() => {
      searchFiles(wsPath, q)
        .then((r) => {
          if (!cancelled) setHits(r ?? []);
        })
        .catch(() => {
          if (!cancelled) setHits([]);
        })
        .finally(() => {
          if (!cancelled) setSearching(false);
        });
    }, 200);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [query, wsPath]);

  // 目录展开/收起：首次展开才调 ListFiles（懒加载），之后读本地缓存。
  // 加载失败只记到该目录行内（error 字段），不动整树；重试复用同一入口。
  const handleDirToggle = (item: TreeItem) => {
    if (item.children) {
      setItems((cur) =>
        cur && updateItems(cur, item.relPath, (it) => ({ ...it, expanded: !it.expanded })),
      );
      return;
    }
    listFiles(wsPath, item.relPath)
      .then((nodes) => {
        setItems((cur) =>
          cur &&
          updateItems(cur, item.relPath, (it) => ({
            ...it,
            children: toItems(nodes, it.relPath),
            expanded: true,
            error: undefined,
          })),
        );
      })
      .catch((e: unknown) => {
        const msg = e instanceof Error ? e.message : String(e);
        setItems((cur) =>
          cur && updateItems(cur, item.relPath, (it) => ({ ...it, error: msg, expanded: true })),
        );
      });
  };

  // 搜索结果里的目录：逐层加载并展开，然后清空搜索回到树视图定位
  const openDirFromSearch = async (relPath: string) => {
    let parent = '';
    for (const seg of relPath.split('/')) {
      const r = parent ? `${parent}/${seg}` : seg;
      try {
        const nodes = await listFiles(wsPath, r);
        setItems(
          (cur) =>
            cur &&
            updateItems(cur, r, (it) => ({
              ...it,
              children: toItems(nodes, r),
              expanded: true,
              error: undefined,
            })),
        );
      } catch {
        break;
      }
      parent = r;
    }
    setQuery('');
    setHits(null);
  };

  // 重命名：Go 侧已作废树缓存，这里重建前端树镜像并刷新 git 状态
  const handleRename = (relPath: string, newName: string) => {
    renameEntry(wsPath, relPath, newName)
      .then(() => {
        useAppStore.getState().notify(`已重命名为 ${newName}`, 'success');
        listFiles(wsPath, '')
          .then((nodes) => setItems(toItems(nodes, '')))
          .catch(() => {
            /* 根层刷新失败保持原树，下次展开会重新拉取 */
          });
        void refreshGitStatus(wsPath);
      })
      .catch((e: unknown) => {
        useAppStore.getState().notify(e instanceof Error ? e.message : String(e), 'error');
      });
  };

  if (error) {
    return <p className="text-sm text-destructive">{error}</p>;
  }
  if (items === null) {
    // 骨架屏：5 行占位，带递进缩进模拟树形结构
    return (
      <div className="flex flex-col gap-1.5" aria-label="工作区文件树加载中">
        {[0, 1, 2, 3, 4].map((i) => (
          <Skeleton
            key={i}
            className="h-6"
            style={{ marginLeft: i * 14, width: `${72 - i * 8}%` }}
          />
        ))}
      </div>
    );
  }

  const q = query.trim();
  const showResults = q.length > 0 && hits !== null;
  const dirOf = (rel: string) => {
    const i = rel.lastIndexOf('/');
    return i < 0 ? '' : rel.slice(0, i);
  };

  return (
    <div className="flex min-h-0 flex-col">
      {/* 搜索框 + git 刷新 */}
      <div className="mb-1.5 flex items-center gap-1">
        <Input
          size="sm"
          className="min-w-0 flex-1 bg-background"
          placeholder="搜索文件…"
          value={query}
          aria-label="搜索文件"
          onChange={(e) => setQuery(e.target.value)}
        />
        <button
          className="shrink-0 rounded p-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          aria-label="刷新 git 状态"
          title="刷新 git 状态"
          onClick={() => void refreshGitStatus(wsPath)}
        >
          <RefreshIcon />
        </button>
      </div>

      {showResults ? (
        <div className="min-h-0 flex-1 overflow-auto">
          <p className="mb-1 px-1 text-xs text-muted-foreground" aria-live="polite">
            {searching ? '搜索中…' : `共 ${hits.length} 项`}
          </p>
          <ul role="listbox" aria-label="搜索结果" className="m-0 list-none p-0 font-mono text-xs">
            {hits.length === 0 ? (
              <EmptyState title={`没有匹配「${q}」的文件`} />
            ) : (
              hits.map((h) => (
                <li key={h.Path}>
                  <button
                    className="flex w-full items-baseline gap-2 rounded px-1 py-1 text-left transition-colors hover:bg-muted"
                    onClick={() => (h.IsDir ? void openDirFromSearch(h.RelPath) : onOpenFile(h.Path))}
                    title={h.Path}
                  >
                    <span className="shrink-0 truncate text-foreground/90">{h.Name}</span>
                    {dirOf(h.RelPath) && (
                      <span className="min-w-0 truncate text-xs text-muted-foreground">
                        {dirOf(h.RelPath)}
                      </span>
                    )}
                  </button>
                </li>
              ))
            )}
          </ul>
        </div>
      ) : q.length > 0 ? (
        // 防抖等待期：先用本地已加载节点即时过滤，结果到达后切平铺列表
        (() => {
          const filtered = filterItems(items, q.toLowerCase());
          return (
            <ul role="tree" aria-label="工作区文件树（过滤中）" className="m-0 list-none p-0 text-sm">
              {filtered.length === 0 ? (
                <p className="px-1 text-xs text-muted-foreground">
                  已加载节点中没有匹配，正在全量搜索…
                </p>
              ) : (
                filtered.map((it) => (
                  <TreeRow
                    key={it.node.Path}
                    item={it}
                    depth={0}
                    gitMap={gitMap}
                    onDirToggle={handleDirToggle}
                    onOpenFile={onOpenFile}
                    onRename={handleRename}
                  />
                ))
              )}
            </ul>
          );
        })()
      ) : (
        <ul role="tree" aria-label="工作区文件树" className="m-0 list-none p-0 text-sm">
          {items.length === 0 ? (
            <EmptyState title="没有可显示的文件" />
          ) : (
            items.map((it) => (
              <TreeRow
                key={it.node.Path}
                item={it}
                depth={0}
                gitMap={gitMap}
                onDirToggle={handleDirToggle}
                onOpenFile={onOpenFile}
                onRename={handleRename}
              />
            ))
          )}
        </ul>
      )}
    </div>
  );
}
