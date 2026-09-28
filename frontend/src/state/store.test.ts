// store 增量测试：setBasket 重建篮子镜像、notify/dismissToast 轻量提示队列。
import { beforeEach, describe, expect, it } from 'vitest';
import { SETTINGS_TAB_ID, useAppStore } from './store';

beforeEach(() => {
  useAppStore.setState({ basket: [], toasts: [] });
});

describe('store', () => {
  it('setBasket 用 Go 侧返回的路径列表整体重建镜像', () => {
    useAppStore.setState({ basket: ['D:\\old.md'] });
    useAppStore.getState().setBasket(['D:\\a.md', 'D:\\b.md', 'D:\\c.md']);
    expect(useAppStore.getState().basket).toEqual(['D:\\a.md', 'D:\\b.md', 'D:\\c.md']);

    // 空列表也要能清空镜像（Go 侧篮子确实为空）
    useAppStore.getState().setBasket([]);
    expect(useAppStore.getState().basket).toEqual([]);
  });

  it('notify 追加提示（不截断），语气默认 info，id 互不相同', () => {
    useAppStore.getState().notify('第一条');
    useAppStore.getState().notify('第二条');

    const toasts = useAppStore.getState().toasts;
    expect(toasts).toHaveLength(2);
    expect(toasts[0]).toMatchObject({ title: '第一条', tone: 'info' });
    expect(toasts[1]).toMatchObject({ title: '第二条', tone: 'info' });
    expect(new Set(toasts.map((t) => t.id)).size).toBe(2);
  });

  it('notify 可指定语气（success/error）', () => {
    useAppStore.getState().notify('已完成', 'success');
    useAppStore.getState().notify('失败了', 'error');

    const toasts = useAppStore.getState().toasts;
    expect(toasts[0].tone).toBe('success');
    expect(toasts[1].tone).toBe('error');
  });

  it('dismissToast 移除指定提示且不影响其他；移除不存在的 id 是安全空操作', () => {
    useAppStore.getState().notify('第一条');
    useAppStore.getState().notify('第二条');
    const firstId = useAppStore.getState().toasts[0].id;

    useAppStore.getState().dismissToast(firstId);
    const toasts = useAppStore.getState().toasts;
    expect(toasts).toHaveLength(1);
    expect(toasts[0].title).toBe('第二条');

    useAppStore.getState().dismissToast(999_999);
    expect(useAppStore.getState().toasts).toHaveLength(1);
  });

  it('SETTINGS_TAB_ID 是保留的页签标识，不与工作区路径冲突', () => {
    expect(SETTINGS_TAB_ID).toBe('kshell:settings');
  });
});
