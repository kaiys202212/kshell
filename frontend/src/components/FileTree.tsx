// 文件树（工作区页签右栏「文件」页签）：递归组件 + 目录懒加载。
// Go 侧 ListFiles 只填充当前层子项（Children 字段无意义），
// 展开目录时必须再次调 listFiles(wsPath, 子目录相对路径)。
// relPath 统一用 / 拼接：Go 侧 filepath.Clean 会归一化为平台分隔符。
// 交互：点目录展开/收起、单击文件 onOpenFile（预览）、双击 onEditFile（固定编辑页签）、铅笔按钮行内重命名、
// 右键菜单（新建/删除/复制路径/触发重命名，删除带确认弹窗）；
// 树内拖拽：行可拖起（携带绝对/相对路径 MIME），目录行作 drop 目标移入；
// 顶部搜索框先过滤已加载节点，防抖后走 Go 递归搜索出平铺结果。
// git 状态：文件名右侧小色标（数据来自 store.gitStatus[wsPath]，lib/git.ts 负责刷新）。
import { useEffect, useRef, useState, type DragEvent, type MouseEvent } from 'react';
import { useTranslation } from 'react-i18next';
import * as DialogPrimitive from '@radix-ui/react-dialog';
import {
  createEntry,
  deleteEntry,
  listFiles,
  moveEntry,
  onFilesChanged,
  refreshFiles,
  renameEntry,
  revealInExplorer,
  searchFiles,
  startFileWatch,
  stopFileWatch,
} from '../lib/api';
import type { FileNode, SearchHit } from '../lib/api';
import { cn } from '../lib/cn';
import { DRAG_MIME, REL_MIME } from '../lib/dragPath';
import { backendError } from '../lib/errors';
import { isNestedGitParent, refreshGitStatus, resolveGitCode, subtreeDirty } from '../lib/git';
import { sameWorkspacePath } from '../lib/workspacePath';
import { useAppStore } from '../state/store';
import ContextMenu, { type MenuItem } from './ContextMenu';
import { Button } from './ui/button';
import { Dialog } from './ui/dialog';
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

// git 状态小色标（S1：改用语义状态色；label 静态，title 走 ui.files.git_status.*）
const GIT_BADGES: Record<string, { label: string; cls: string }> = {
  modified: { label: 'M', cls: 'text-warning' },
  added: { label: 'A', cls: 'text-success' },
  deleted: { label: 'D', cls: 'text-danger' },
  renamed: { label: 'R', cls: 'text-info' },
  untracked: { label: 'N', cls: 'text-success' },
  ignored: { label: 'I', cls: 'text-muted-foreground' },
  conflicted: { label: '!', cls: 'text-danger' },
};

function GitBadge({ code }: { code: string }) {
  const { t } = useTranslation();
  const b = GIT_BADGES[code];
  if (!b) return null;
  const title = t(`ui.files.git_status.${code}`);
  return (
    <span
      className={cn('shrink-0 font-mono text-[11px] font-bold', b.cls)}
      title={t('ui.files.git_badge_title', { title })}
      aria-label={t('ui.files.git_badge_aria', { title })}
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

function wsBaseName(wsPath: string): string {
  const n = wsPath.replace(/[\\/]+$/, '');
  const i = Math.max(n.lastIndexOf('\\'), n.lastIndexOf('/'));
  return i < 0 ? n : n.slice(i + 1) || n;
}

function wrapVirtualRoot(wsPath: string, children: TreeItem[], expanded: boolean): TreeItem {
  return {
    node: {
      Name: wsBaseName(wsPath),
      Path: wsPath,
      IsDir: true,
      Expanded: expanded,
      Loaded: true,
    },
    relPath: '',
    children,
    expanded,
  };
}

function asTree(wsPath: string, nodes: FileNode[], expanded = true): TreeItem[] {
  return [wrapVirtualRoot(wsPath, toItems(nodes, ''), expanded)];
}

// 按 relPath 在嵌套结构中定位并更新（不可变更新，递归下钻）
function updateItems(items: TreeItem[], relPath: string, fn: (t: TreeItem) => TreeItem): TreeItem[] {
  return items.map((it) => {
    if (it.relPath === relPath) return fn(it);
    return it.children ? { ...it, children: updateItems(it.children, relPath, fn) } : it;
  });
}

// 收集已展开目录的 relPath（深度优先），供 reloadTree 重建后恢复展开
function collectExpanded(items: TreeItem[]): string[] {
  const out: string[] = [];
  const walk = (list: TreeItem[]) => {
    for (const it of list) {
      if (it.expanded && it.node.IsDir) {
        out.push(it.relPath);
        if (it.children) walk(it.children);
      }
    }
  };
  walk(items);
  return out;
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

// 行内新建输入：渲染在目标目录子层首位（根目录在树列表顶部），
// 目录未加载 children 时直接渲染在目录行下方。Enter 提交；Esc/失焦取消；空名不提交。
// 右键菜单 pointerdown 后挂载输入时，同次点击的后续阶段会立刻 blur；短时忽略以免闪没。
const BLUR_GUARD_MS = 150;

function CreateRow({
  depth,
  isDir,
  onCommit,
  onCancel,
}: {
  depth: number;
  isDir: boolean;
  onCommit(name: string): void;
  onCancel(): void;
}) {
  const { t } = useTranslation();
  const [draft, setDraft] = useState('');
  const ignoreBlurRef = useRef(true);
  useEffect(() => {
    ignoreBlurRef.current = true;
    const tmr = window.setTimeout(() => {
      ignoreBlurRef.current = false;
    }, BLUR_GUARD_MS);
    return () => window.clearTimeout(tmr);
  }, []);
  const commit = () => {
    const name = draft.trim();
    if (!name) return; // 空名不提交（失焦兜底取消）
    onCommit(name);
  };
  return (
    <li>
      <div className="flex h-6 items-center" style={{ paddingLeft: depth * 14 }}>
        <Input
          autoFocus
          className="h-5 min-w-0 flex-1 px-1 py-0 font-mono"
          placeholder={isDir ? t('ui.files.folder_name_placeholder') : t('ui.files.file_name_placeholder')}
          aria-label={isDir ? t('ui.files.new_folder_aria') : t('ui.files.new_file_aria')}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              commit();
            } else if (e.key === 'Escape') {
              e.preventDefault();
              onCancel();
            }
          }}
          onBlur={() => {
            if (ignoreBlurRef.current) return;
            onCancel();
          }}
        />
      </div>
    </li>
  );
}

interface RowProps {
  item: TreeItem;
  depth: number;
  gitMap?: Record<string, string>; // git 状态映射（键为 '/' 分隔 relPath），行内按自身 relPath 查
  rootBranch?: string;
  dirBranches?: Record<string, string>;
  renamingPath: string | null; // 受控行内重命名：renamingPath === item.relPath 时该行进入编辑
  creating: { dirRel: string; isDir: boolean } | null; // 行内新建输入的渲染位置
  onRenameStart(relPath: string): void;
  onRenameEnd(): void;
  onDirToggle(item: TreeItem): void;
  onOpenFile(path: string): void;
  onEditFile(path: string): void;
  onRename(relPath: string, newName: string): void;
  onCreateCommit(name: string): void;
  onCancelCreate(): void;
  onRowContextMenu(e: MouseEvent, item: TreeItem): void;
  onMoveInto(srcRel: string, dstDirRel: string): void;
}

function TreeRow({
  item,
  depth,
  gitMap,
  rootBranch,
  dirBranches,
  renamingPath,
  creating,
  onRenameStart,
  onRenameEnd,
  onDirToggle,
  onOpenFile,
  onEditFile,
  onRename,
  onCreateCommit,
  onCancelCreate,
  onRowContextMenu,
  onMoveInto,
}: RowProps) {
  const { t } = useTranslation();
  const { node } = item;
  const gitCode = resolveGitCode(gitMap, item.relPath, node.IsDir, dirBranches);
  const nestedParent = node.IsDir && isNestedGitParent(dirBranches, item.relPath);
  const branch = node.IsDir ? (item.relPath === '' ? rootBranch : dirBranches?.[item.relPath]) : undefined;
  // 中间层用 ⊞ 表达特殊未跟踪容器，不再叠 N
  const showGitBadge = !!gitCode && !(nestedParent && gitCode === 'untracked');
  const dirtyFolder = node.IsDir && !gitCode && subtreeDirty(gitMap, item.relPath, dirBranches);
  const isVirtualRoot = item.relPath === '';
  const renaming = renamingPath === item.relPath;
  const [draft, setDraft] = useState(node.Name);
  const [dropActive, setDropActive] = useState(false);
  const ignoreRenameBlurRef = useRef(false);

  // 受控重命名：进入编辑态时重置草稿为当前名（状态在父层，草稿留本行）；
  // 挂载后短时忽略 blur，避免右键菜单 pointerdown 同次点击立刻提交/收起。
  useEffect(() => {
    if (!renaming) return;
    setDraft(node.Name);
    ignoreRenameBlurRef.current = true;
    const tmr = window.setTimeout(() => {
      ignoreRenameBlurRef.current = false;
    }, BLUR_GUARD_MS);
    return () => window.clearTimeout(tmr);
  }, [renaming, node.Name]);

  const commitRename = () => {
    const name = draft.trim();
    onRenameEnd();
    if (!name || name === node.Name) return;
    onRename(item.relPath, name);
  };

  const rowContextMenu = (e: MouseEvent) => {
    // preventDefault 掉浏览器默认菜单；stopPropagation 防止空白处 handler 覆盖为 item=null
    e.preventDefault();
    e.stopPropagation();
    onRowContextMenu(e, item);
  };

  // 目录行拖放接收：types 含 DRAG_MIME 才视为本应用的拖拽（外部文件拖入走 Wails 全窗口回调）
  const rowDragOver = (e: DragEvent<HTMLDivElement>) => {
    if (!e.dataTransfer.types.includes(DRAG_MIME)) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    setDropActive(true);
  };
  const rowDrop = (e: DragEvent<HTMLDivElement>) => {
    setDropActive(false);
    if (!e.dataTransfer.types.includes(DRAG_MIME)) return;
    e.preventDefault();
    e.stopPropagation(); // 防冒泡到空白右键等容器 handler
    const srcRel = e.dataTransfer.getData(REL_MIME);
    if (srcRel) onMoveInto(srcRel, item.relPath);
  };

  return (
    <li
      role="treeitem"
      aria-expanded={node.IsDir ? item.expanded : undefined}
    >
      <div
        className={cn(
          'group flex h-6 items-center gap-0.5 rounded pr-1 transition-colors hover:bg-muted',
          dropActive && 'ring-1 ring-primary',
        )}
        style={{ paddingLeft: depth * 14 }}
        onContextMenu={rowContextMenu}
        draggable={!renaming && !isVirtualRoot} // 行内重命名编辑中不允许拖
        onDragStart={(e) => {
          e.dataTransfer.setData(DRAG_MIME, node.Path);
          e.dataTransfer.setData(REL_MIME, item.relPath);
          e.dataTransfer.effectAllowed = 'move';
        }}
        onDragOver={node.IsDir ? rowDragOver : undefined}
        onDragLeave={node.IsDir ? () => setDropActive(false) : undefined}
        onDrop={node.IsDir ? rowDrop : undefined}
      >
        {renaming ? (
          <Input
            autoFocus
            className="h-5 min-w-0 flex-1 px-1 py-0 font-mono"
            value={draft}
            aria-label={t('ui.files.rename_aria', { name: node.Name })}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                commitRename();
              } else if (e.key === 'Escape') {
                e.preventDefault();
                onRenameEnd();
              }
            }}
            onBlur={() => {
              if (ignoreRenameBlurRef.current) return;
              commitRename();
            }}
          />
        ) : (
          <>
            <button
              className="flex min-w-0 flex-1 items-center gap-1 rounded px-1 py-0.5 text-left"
              title={node.Path}
              onClick={() => (node.IsDir ? onDirToggle(item) : onOpenFile(node.Path))}
              onDoubleClick={(e) => {
                if (node.IsDir) return;
                e.preventDefault();
                onEditFile(node.Path);
              }}
            >
              <span className="flex h-4 w-4 shrink-0 items-center justify-center text-muted-foreground">
                {node.IsDir && <ChevronIcon open={item.expanded} />}
              </span>
              {node.IsDir ? <FolderIcon open={item.expanded} /> : <FileIcon />}
              <span
                className={cn(
                  'min-w-0 truncate font-mono text-xs',
                  node.IsDir ? 'font-medium' : 'text-foreground/90',
                  gitCode === 'ignored' && 'opacity-60 text-muted-foreground',
                )}
              >
                {node.Name}
              </span>
              {branch ? (
                <span className="shrink-0 truncate font-mono text-[11px] text-muted-foreground" title={t('ui.files.git_branch_title', { branch })}>
                  ({branch})
                </span>
              ) : null}
              {nestedParent ? (
                <span
                  className="shrink-0 font-mono text-[11px] text-muted-foreground"
                  title={t('ui.files.nested_git')}
                  aria-label={t('ui.files.nested_git')}
                >
                  ⊞
                </span>
              ) : null}
              {showGitBadge && <GitBadge code={gitCode!} />}
              {dirtyFolder && (
                <span
                  className="shrink-0 text-warning"
                  title={t('ui.files.uncommitted_change')}
                  aria-label={t('ui.files.uncommitted_change')}
                >
                  •
                </span>
              )}
            </button>
            {!isVirtualRoot && (
            <button
              className="shrink-0 rounded p-0.5 text-muted-foreground opacity-0 transition-opacity hover:text-primary group-hover:opacity-100"
              aria-label={t('ui.files.rename_aria', { name: node.Name })}
              title={t('ui.files.rename')}
              onClick={() => onRenameStart(item.relPath)}
            >
              <PencilIcon />
            </button>
            )}
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
            aria-label={t('ui.files.retry_load_aria', { name: node.Name })}
            onClick={() => onDirToggle(item)}
          >
            {t('ui.files.retry')}
          </button>
        </div>
      )}
      {node.IsDir && item.expanded && item.children && (
        <ul role="group" className="m-0 list-none p-0">
          {creating && creating.dirRel === item.relPath && (
            <CreateRow
              depth={depth + 1}
              isDir={creating.isDir}
              onCommit={onCreateCommit}
              onCancel={onCancelCreate}
            />
          )}
          {item.children.map((c) => (
            <TreeRow
              key={c.node.Path}
              item={c}
              depth={depth + 1}
              gitMap={gitMap}
              rootBranch={rootBranch}
              dirBranches={dirBranches}
              renamingPath={renamingPath}
              creating={creating}
              onRenameStart={onRenameStart}
              onRenameEnd={onRenameEnd}
              onDirToggle={onDirToggle}
              onOpenFile={onOpenFile}
              onEditFile={onEditFile}
              onRename={onRename}
              onCreateCommit={onCreateCommit}
              onCancelCreate={onCancelCreate}
              onRowContextMenu={onRowContextMenu}
              onMoveInto={onMoveInto}
            />
          ))}
        </ul>
      )}
      {/* 目录未加载/未展开 children：新建输入直接渲染在目录行下方。
          取舍：未实现「先展开目录再显示输入」，避免展开动作与新建意图耦合；
          外包 <ul role="group"> 保证 <li> 只出现在 <ul> 内（消除 React 嵌套告警） */}
      {node.IsDir && creating && creating.dirRel === item.relPath && !(item.expanded && item.children) && (
        <ul role="group" className="m-0 list-none p-0">
          <CreateRow
            depth={depth + 1}
            isDir={creating.isDir}
            onCommit={onCreateCommit}
            onCancel={onCancelCreate}
          />
        </ul>
      )}
    </li>
  );
}

export default function FileTree({
  wsPath,
  onOpenFile,
  onEditFile,
}: {
  wsPath: string;
  onOpenFile(path: string): void;
  onEditFile?(path: string): void;
}) {
  const { t } = useTranslation();
  const editFile = onEditFile ?? onOpenFile;
  const [items, setItems] = useState<TreeItem[] | null>(null);
  const [error, setError] = useState('');
  const [query, setQuery] = useState('');
  const [hits, setHits] = useState<SearchHit[] | null>(null);
  const [searching, setSearching] = useState(false);
  // 右键菜单：item 为 null 表示空白处（根目录）
  const [menu, setMenu] = useState<{ x: number; y: number; item: TreeItem | null } | null>(null);
  const [creating, setCreating] = useState<{ dirRel: string; isDir: boolean } | null>(null);
  const [deleting, setDeleting] = useState<TreeItem | null>(null);
  const [renamingPath, setRenamingPath] = useState<string | null>(null);
  // 显示全部（含忽略文件）；不持久化，切换由下方 effect 走 reloadTree 重拉已展开路径
  const [showAll, setShowAll] = useState(false);
  const gitMap = useAppStore((s) => s.gitStatus[wsPath]);
  const rootBranch = useAppStore((s) => s.gitBranch[wsPath]);
  const dirBranches = useAppStore((s) => s.gitDirBranches[wsPath]);

  // 避免 reloadTree / files:changed 回调读到过期的 items / showAll
  const itemsRef = useRef(items);
  itemsRef.current = items;
  const showAllRef = useRef(showAll);
  showAllRef.current = showAll;
  // wsPath 变更 / 卸载时递增，进行中的 reloadTree 不得再 setItems
  const reloadGenRef = useRef(0);
  const prevShowAllRef = useRef(showAll);

  // 作废 Go 缓存 → 重拉根层 → 按原展开路径逐层恢复 → 刷 git
  const reloadTree = async () => {
    const gen = reloadGenRef.current;
    const path = wsPath;
    const expanded = itemsRef.current ? collectExpanded(itemsRef.current) : [];
    const show = showAllRef.current;
    try {
      await refreshFiles(path);
    } catch {
      if (gen !== reloadGenRef.current) return;
      /* refresh 失败保持原树，与根层 list 失败一致 */
      return;
    }
    if (gen !== reloadGenRef.current) return;
    try {
      const nodes = await listFiles(path, '', show);
      const rootExpanded = itemsRef.current?.[0]?.relPath === '' ? itemsRef.current[0].expanded : true;
      let next = asTree(path, nodes, rootExpanded);
      for (const rel of expanded) {
        if (!rel) continue;
        if (gen !== reloadGenRef.current) return;
        try {
          const kids = await listFiles(path, rel, show);
          next = updateItems(next, rel, (it) => ({
            ...it,
            children: toItems(kids, rel),
            expanded: true,
            error: undefined,
          }));
        } catch {
          /* 单层失败忽略，其它层继续恢复 */
        }
      }
      if (gen !== reloadGenRef.current) return;
      setItems(next);
    } catch {
      /* 根层失败保持原树 */
    }
    if (gen !== reloadGenRef.current) return;
    void refreshGitStatus(path);
  };
  const reloadTreeRef = useRef(reloadTree);
  reloadTreeRef.current = reloadTree;

  // 监视生命周期仅随 wsPath；showAll 切换不重载 watch（单独 effect 走 reloadTree）
  useEffect(() => {
    void startFileWatch(wsPath).catch((e: unknown) => {
      useAppStore
        .getState()
        .notify(e instanceof Error ? backendError(e) : t('ui.files.watch_failed'), 'error');
    });
    const unsub = onFilesChanged((path) => {
      if (sameWorkspacePath(path, wsPath)) void reloadTreeRef.current();
    });
    return () => {
      reloadGenRef.current += 1;
      unsub();
      stopFileWatch(wsPath);
    };
  }, [wsPath]);

  useEffect(() => {
    reloadGenRef.current += 1;
    const gen = reloadGenRef.current;
    let cancelled = false;
    setItems(null);
    setError('');
    setQuery('');
    setHits(null);
    setMenu(null);
    setCreating(null);
    setDeleting(null);
    setRenamingPath(null);
    listFiles(wsPath, '', showAllRef.current)
      .then((nodes) => {
        if (!cancelled && gen === reloadGenRef.current) setItems(asTree(wsPath, nodes, true));
      })
      .catch((e: unknown) => {
        if (!cancelled && gen === reloadGenRef.current) {
          setError(backendError(e));
        }
      });
    void refreshGitStatus(wsPath);
    return () => {
      cancelled = true;
      reloadGenRef.current += 1;
    };
  }, [wsPath]);

  useEffect(() => {
    if (prevShowAllRef.current === showAll) return;
    prevShowAllRef.current = showAll;
    reloadGenRef.current += 1;
    void reloadTreeRef.current();
    return () => {
      reloadGenRef.current += 1;
    };
  }, [showAll, wsPath]);

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
      searchFiles(wsPath, q, showAll)
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
  }, [query, wsPath, showAll]);

  // 目录展开/收起：首次展开才调 ListFiles（懒加载），之后读本地缓存。
  // 加载失败只记到该目录行内（error 字段），不动整树；重试复用同一入口。
  const handleDirToggle = (item: TreeItem) => {
    if (item.children || item.relPath === '') {
      setItems((cur) =>
        cur && updateItems(cur, item.relPath, (it) => ({ ...it, expanded: !it.expanded })),
      );
      return;
    }
    listFiles(wsPath, item.relPath, showAll)
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
        const msg = backendError(e);
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
        const nodes = await listFiles(wsPath, r, showAll);
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

  // 根层重建树镜像（Go 侧改动后已作废缓存）：rename/create/delete 共用
  const refreshRoot = () => {
    listFiles(wsPath, '', showAll)
      .then((nodes) =>
        setItems((cur) => asTree(wsPath, nodes, cur?.[0]?.relPath === '' ? cur[0].expanded : true)),
      )
      .catch(() => {
        /* 根层刷新失败保持原树，下次展开会重新拉取 */
      });
  };

  // 重命名：Go 侧已作废树缓存，这里重建前端树镜像并刷新 git 状态
  const handleRename = (relPath: string, newName: string) => {
    renameEntry(wsPath, relPath, newName)
      .then(() => {
        useAppStore.getState().notify(t('ui.files.renamed', { name: newName }), 'success');
        refreshRoot();
        void refreshGitStatus(wsPath);
      })
      .catch((e: unknown) => {
        useAppStore.getState().notify(backendError(e), 'error');
      });
  };

  // 新建：Go 侧已作废树缓存，成功后刷新树镜像 + git 状态
  const handleCreateCommit = (name: string) => {
    if (!creating) return;
    const { dirRel, isDir } = creating;
    createEntry(wsPath, dirRel, name, isDir)
      .then(() => {
        useAppStore.getState().notify(t('ui.files.created', { name }), 'success');
        setCreating(null);
        refreshRoot();
        void refreshGitStatus(wsPath);
      })
      .catch((e: unknown) => {
        // 失败保留输入行：用户可改名重试（Esc/失焦仍可取消），不强行收起
        useAppStore.getState().notify(backendError(e), 'error');
      });
  };

  // 删除确认后的执行：os.RemoveAll 永久删除，成功后刷新树镜像 + git 状态
  const handleDeleteConfirm = () => {
    if (!deleting) return;
    const { relPath, node } = deleting;
    deleteEntry(wsPath, relPath)
      .then(() => {
        useAppStore.getState().notify(t('ui.files.deleted', { name: node.Name }), 'success');
        setDeleting(null);
        refreshRoot();
        void refreshGitStatus(wsPath);
      })
      .catch((e: unknown) => {
        useAppStore.getState().notify(backendError(e), 'error');
        setDeleting(null);
      });
  };

  const handleCopyPath = (item: TreeItem) => {
    navigator.clipboard
      .writeText(item.node.Path)
      .then(() => useAppStore.getState().notify(t('ui.files.path_copied'), 'success'))
      .catch(() => useAppStore.getState().notify(t('ui.files.copy_failed'), 'error'));
  };

  // 树内拖拽移动：移入自身/子孙目录直接忽略（后端 MoveEntry 也会再校验）
  const handleMoveInto = (srcRel: string, dstDirRel: string) => {
    if (srcRel === dstDirRel || dstDirRel.startsWith(`${srcRel}/`)) return;
    moveEntry(wsPath, srcRel, dstDirRel)
      .then(() => {
        useAppStore.getState().notify(t('ui.files.moved', { dst: dstDirRel || t('ui.files.root_dir') }), 'success');
        refreshRoot();
        void refreshGitStatus(wsPath);
      })
      .catch((e: unknown) => {
        useAppStore.getState().notify(backendError(e), 'error');
      });
  };

  const handleRowContextMenu = (e: MouseEvent, item: TreeItem | null) => {
    setMenu({ x: e.clientX, y: e.clientY, item });
  };

  if (error) {
    return <p className="text-sm text-destructive">{error}</p>;
  }
  if (items === null) {
    // 骨架屏：5 行占位，带递进缩进模拟树形结构
    return (
      <div className="flex flex-col gap-1.5" aria-label={t('ui.files.tree_loading_aria')}>
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

  // 右键菜单项：按目标（目录行/文件行/空白处）组装
  const menuItems: MenuItem[] = (() => {
    const startCreate = (dirRel: string, isDir: boolean) => {
      setCreating({ dirRel, isDir });
      setMenu(null);
    };
    const startRename = (it: TreeItem) => {
      setRenamingPath(it.relPath);
      setMenu(null);
    };
    const startDelete = (it: TreeItem) => {
      setDeleting(it);
      setMenu(null);
    };
    const copyPath = (it: TreeItem) => {
      handleCopyPath(it);
      setMenu(null);
    };
    const reveal = (it: TreeItem) => {
      revealInExplorer(wsPath, it.node.Path)
        .then(() => setMenu(null))
        .catch((e: unknown) => {
          useAppStore.getState().notify(backendError(e), 'error');
          setMenu(null);
        });
    };
    const it = menu?.item;
    if (!it) {
      // 空白处：根目录新建
      return [
        { label: t('ui.files.menu_new_file'), onSelect: () => startCreate('', false) },
        { label: t('ui.files.menu_new_folder'), onSelect: () => startCreate('', true) },
      ];
    }
    if (it.relPath === '') {
      return [
        { label: t('ui.files.menu_new_file'), onSelect: () => startCreate('', false) },
        { label: t('ui.files.menu_new_folder'), onSelect: () => startCreate('', true) },
        { label: t('ui.files.copy_path'), onSelect: () => copyPath(it) },
        { label: t('ui.files.reveal'), onSelect: () => reveal(it) },
      ];
    }
    if (it.node.IsDir) {
      return [
        { label: t('ui.files.menu_new_file'), onSelect: () => startCreate(it.relPath, false) },
        { label: t('ui.files.menu_new_folder'), onSelect: () => startCreate(it.relPath, true) },
        { label: t('ui.files.rename'), onSelect: () => startRename(it) },
        { label: t('ui.files.delete'), danger: true, onSelect: () => startDelete(it) },
        { label: t('ui.files.copy_path'), onSelect: () => copyPath(it) },
        { label: t('ui.files.reveal'), onSelect: () => reveal(it) },
      ];
    }
    return [
      { label: t('ui.files.rename'), onSelect: () => startRename(it) },
      { label: t('ui.files.delete'), danger: true, onSelect: () => startDelete(it) },
      { label: t('ui.files.copy_path'), onSelect: () => copyPath(it) },
      { label: t('ui.files.reveal'), onSelect: () => reveal(it) },
    ];
  })();

  const rowProps = {
    gitMap,
    rootBranch,
    dirBranches,
    renamingPath,
    creating,
    onRenameStart: setRenamingPath,
    onRenameEnd: () => setRenamingPath(null),
    onDirToggle: handleDirToggle,
    onOpenFile,
    onEditFile: editFile,
    onRename: handleRename,
    onCreateCommit: handleCreateCommit,
    onCancelCreate: () => setCreating(null),
    onRowContextMenu: handleRowContextMenu,
    onMoveInto: handleMoveInto,
  };
  const blankContextMenu = (e: MouseEvent) => {
    e.preventDefault();
    setMenu({ x: e.clientX, y: e.clientY, item: null });
  };

  return (
    // h-full 配合父级 min-h-0 flex-1 撑满右栏：空白右键 handler 挂在根 div，
    // 才能覆盖到列表下方空白（行内 handler 已 stopPropagation，不会冲突）
    <div className="flex h-full min-h-0 flex-col" onContextMenu={blankContextMenu}>
      {/* 搜索框 + 显示全部 + 刷新 */}
      <div className="mb-1.5 flex items-center gap-1">
        <Input
          size="sm"
          className="min-w-0 flex-1 bg-background"
          placeholder={t('ui.files.search_placeholder')}
          value={query}
          aria-label={t('ui.files.search_aria')}
          onChange={(e) => setQuery(e.target.value)}
        />
        <label className="flex shrink-0 cursor-pointer items-center gap-1 text-xs text-muted-foreground">
          <input
            type="checkbox"
            aria-label={t('ui.files.show_all_aria')}
            checked={showAll}
            onChange={(e) => setShowAll(e.target.checked)}
          />
          {t('ui.files.show_all')}
        </label>
        <button
          className="shrink-0 rounded p-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          aria-label={t('ui.files.refresh')}
          title={t('ui.files.refresh')}
          onClick={() => void reloadTree()}
        >
          <RefreshIcon />
        </button>
      </div>

      {showResults ? (
        <div className="min-h-0 flex-1 overflow-auto">
          <p className="mb-1 px-1 text-xs text-muted-foreground" aria-live="polite">
            {searching ? t('ui.files.searching') : t('ui.files.search_count', { n: hits.length })}
          </p>
          <ul role="listbox" aria-label={t('ui.files.search_results_aria')} className="m-0 list-none p-0 font-mono text-xs">
            {hits.length === 0 ? (
              <EmptyState title={t('ui.files.search_empty', { q })} />
            ) : (
              hits.map((h) => (
                <li key={h.Path}>
                  <button
                    className="flex w-full items-baseline gap-2 rounded px-1 py-1 text-left transition-colors hover:bg-muted"
                    onClick={() => (h.IsDir ? void openDirFromSearch(h.RelPath) : onOpenFile(h.Path))}
                    onDoubleClick={(e) => {
                      if (h.IsDir) return;
                      e.preventDefault();
                      editFile(h.Path);
                    }}
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
            <ul role="tree" aria-label={t('ui.files.tree_filtering_aria')} className="m-0 list-none p-0 text-sm">
              {filtered.length === 0 ? (
                <p className="px-1 text-xs text-muted-foreground">
                  {t('ui.files.filter_no_match')}
                </p>
              ) : (
                filtered.map((it) => (
                  <TreeRow key={it.node.Path} item={it} depth={0} {...rowProps} />
                ))
              )}
            </ul>
          );
        })()
      ) : (
        <ul role="tree" aria-label={t('ui.files.tree_aria')} className="m-0 list-none p-0 text-sm">
          {items.map((it) => <TreeRow key={it.node.Path} item={it} depth={0} {...rowProps} />)}
        </ul>
      )}

      {menu && (
        <ContextMenu x={menu.x} y={menu.y} items={menuItems} onClose={() => setMenu(null)} />
      )}

      {deleting && (
        <Dialog open onOpenChange={(o) => { if (!o) setDeleting(null); }} className="w-80">
          <DialogPrimitive.Title className="mb-1 text-sm font-medium">{t('ui.files.delete_confirm_title')}</DialogPrimitive.Title>
          <p className="mb-3 text-xs text-muted-foreground">
            {t('ui.files.delete_confirm_body', { name: deleting.node.Name })}
          </p>
          <div className="flex justify-end gap-2">
            <Button variant="outline" size="sm" onClick={() => setDeleting(null)}>
              {t('ui.files.cancel')}
            </Button>
            <Button variant="destructive" size="sm" onClick={handleDeleteConfirm}>
              {t('ui.files.delete')}
            </Button>
          </div>
        </Dialog>
      )}
    </div>
  );
}
