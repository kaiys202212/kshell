// 上下文篮（工作区页签中间栏顶部）：展示 Go 侧篮子内容的镜像，支持移除。
// 移除走 Go 绑定并以返回值同步 store（见 store.syncBasket），保证与 Go 状态一致。
// 新建会话时 Go 侧自动把篮子内容作为初始提示注入，前端无需再传文件列表。
import { toggleBasket } from '../lib/api';
import { useAppStore } from '../state/store';

const maxBasket = 20; // 与 Go 侧 maxBasket 一致，仅用于展示计数

function basename(p: string): string {
  return p.split(/[\\/]/).pop() ?? p;
}

export default function BasketBar() {
  const basket = useAppStore((s) => s.basket);
  const syncBasket = useAppStore((s) => s.syncBasket);
  const notify = useAppStore((s) => s.notify);

  const remove = async (path: string) => {
    try {
      const inBasket = await toggleBasket(path);
      syncBasket(path, inBasket);
    } catch {
      // 绑定异常时保持原状态，用轻量提示告知
      notify('篮子操作失败，请稍后重试', 'error');
    }
  };

  return (
    <div className="mb-2 flex flex-wrap items-center gap-2 rounded-lg border border-border bg-card px-2.5 py-1.5">
      <span className="whitespace-nowrap text-xs text-muted-foreground">
        上下文篮（{basket.length}/{maxBasket}）
      </span>
      {basket.length === 0 ? (
        <span className="text-xs italic text-muted-foreground">未选择文件</span>
      ) : (
        <ul className="m-0 flex list-none flex-wrap items-center gap-1.5 p-0">
          {basket.map((p) => (
            <li
              key={p}
              className="flex items-center gap-1 rounded-full border border-border bg-secondary px-2 py-0.5 text-xs"
            >
              <span className="max-w-40 truncate" title={p}>
                {basename(p)}
              </span>
              <button
                className="rounded-full leading-none text-muted-foreground transition-colors hover:text-destructive"
                aria-label={`移出 ${p}`}
                onClick={() => void remove(p)}
              >
                ×
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
