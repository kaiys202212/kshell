// 文件树（工作区页签右栏「文件」页签）：递归组件 + 目录懒加载。
// Go 侧 ListFiles 只填充当前层子项（Children 字段无意义），
// 展开目录时必须再次调 listFiles(wsPath, 子目录相对路径)。
// relPath 统一用 / 拼接：Go 侧 filepath.Clean 会归一化为平台分隔符。
// 交互：点目录展开/收起、点文件回调 onOpenFile、Space 键或 ○ 按钮加入/移出篮子。
import { useEffect, useState } from 'react';
import { listFiles, toggleBasket } from '../lib/api';
import type { FileNode } from '../lib/api';
import { useAppStore } from '../state/store';

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

interface RowProps {
  item: TreeItem;
  depth: number;
  basket: string[];
  onDirToggle(item: TreeItem): void;
  onOpenFile(path: string): void;
  onBasketToggle(path: string): void;
}

function TreeRow({ item, depth, basket, onDirToggle, onOpenFile, onBasketToggle }: RowProps) {
  const { node } = item;
  const inBasket = basket.includes(node.Path);
  return (
    <li
      className="tree-item"
      role="treeitem"
      aria-expanded={node.IsDir ? item.expanded : undefined}
    >
      <div className="tree-row" style={{ paddingLeft: depth * 14 }}>
        <button
          className="tree-name"
          title={node.Path}
          onClick={() => (node.IsDir ? onDirToggle(item) : onOpenFile(node.Path))}
          onKeyDown={(e) => {
            // Space 加入/移出篮子；preventDefault 防止原生按钮把 Space 当点击（触发预览）
            if (e.key === ' ') {
              e.preventDefault();
              onBasketToggle(node.Path);
            }
          }}
        >
          <span className="tree-icon" aria-hidden="true">
            {node.IsDir ? (item.expanded ? '▾' : '▸') : ''}
          </span>
          <span className={node.IsDir ? 'tree-dir' : 'tree-file'}>{node.Name}</span>
        </button>
        <button
          className="tree-basket-btn"
          aria-label={`${inBasket ? '移出' : '加入'}篮子 ${node.Name}`}
          title={inBasket ? '移出上下文篮' : '加入上下文篮'}
          onClick={() => onBasketToggle(node.Path)}
        >
          {inBasket ? '●' : '○'}
        </button>
      </div>
      {node.IsDir && item.expanded && item.error && (
        <div className="tree-item-error" style={{ paddingLeft: (depth + 1) * 14 }}>
          <span className="tree-item-error-text">{item.error}</span>
          <button
            className="tree-item-retry"
            aria-label={`重试加载 ${node.Name}`}
            onClick={() => onDirToggle(item)}
          >
            重试
          </button>
        </div>
      )}
      {node.IsDir && item.expanded && item.children && (
        <ul className="tree-group" role="group">
          {item.children.map((c) => (
            <TreeRow
              key={c.node.Path}
              item={c}
              depth={depth + 1}
              basket={basket}
              onDirToggle={onDirToggle}
              onOpenFile={onOpenFile}
              onBasketToggle={onBasketToggle}
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
  const basket = useAppStore((s) => s.basket);
  const syncBasket = useAppStore((s) => s.syncBasket);
  const notify = useAppStore((s) => s.notify);

  useEffect(() => {
    let cancelled = false;
    setItems(null);
    setError('');
    listFiles(wsPath, '')
      .then((nodes) => {
        if (!cancelled) setItems(toItems(nodes, ''));
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [wsPath]);

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

  // 加入/移出篮子：以 Go ToggleBasket 的返回值为准同步 store，避免双份状态漂移。
  // 篮满（返回 false 且原本不在篮中）或调用失败时用轻量提示告知，不打断浏览。
  const handleBasketToggle = async (path: string) => {
    const wasIn = useAppStore.getState().basket.includes(path);
    try {
      const inBasket = await toggleBasket(path);
      syncBasket(path, inBasket);
      if (!inBasket && !wasIn) {
        // 20 与 Go 侧 maxBasket 一致（见 BasketBar 同款注释），仅用于提示文案
        notify('篮子已满（20 个文件），请先移出部分文件再加入');
      }
    } catch {
      notify('篮子操作失败，请稍后重试');
    }
  };

  if (error) {
    return <p className="tree-error">{error}</p>;
  }
  if (items === null) {
    return <p className="tree-status">加载中……</p>;
  }
  return (
    <ul className="file-tree" role="tree" aria-label="工作区文件树">
      {items.length === 0 ? (
        <p className="tree-status">没有可显示的文件</p>
      ) : (
        items.map((it) => (
          <TreeRow
            key={it.node.Path}
            item={it}
            depth={0}
            basket={basket}
            onDirToggle={handleDirToggle}
            onOpenFile={onOpenFile}
            onBasketToggle={(p) => void handleBasketToggle(p)}
          />
        ))
      )}
    </ul>
  );
}
