// 篮子开关的共用封装：FileTree 与 Preview 共用同一套「切换 + 同步 + 提示」逻辑，
// 保证两条入口对 Go 侧篮子状态的行为一致。
import { toggleBasket } from './api';
import { useAppStore } from '../state/store';

// 加入/移出篮子：以 Go ToggleBasket 的返回值为准同步 store，避免双份状态漂移。
// 篮满（返回 false 且原本不在篮中）或调用失败时用轻量提示告知，不打断浏览。
export async function toggleAndSync(path: string): Promise<void> {
  const wasIn = useAppStore.getState().basket.includes(path);
  try {
    const inBasket = await toggleBasket(path);
    useAppStore.getState().syncBasket(path, inBasket);
    if (!inBasket && !wasIn) {
      // 20 与 Go 侧 maxBasket 一致（见 BasketBar 同款注释），仅用于提示文案
      useAppStore.getState().notify('篮子已满（20 个文件），请先移出部分文件再加入');
    }
  } catch {
    useAppStore.getState().notify('篮子操作失败，请稍后重试', 'error');
  }
}
