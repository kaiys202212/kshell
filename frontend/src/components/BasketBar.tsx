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
      notify('篮子操作失败，请稍后重试');
    }
  };

  return (
    <div className="basket-bar">
      <span className="basket-title">
        上下文篮（{basket.length}/{maxBasket}）
      </span>
      {basket.length === 0 ? (
        <span className="basket-empty">未选择文件</span>
      ) : (
        <ul className="basket-items">
          {basket.map((p) => (
            <li key={p} className="basket-item">
              <span className="basket-name" title={p}>
                {basename(p)}
              </span>
              <button
                className="basket-remove"
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
