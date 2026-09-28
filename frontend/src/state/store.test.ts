// store 增量测试：setBasket 重建篮子镜像、notify 轻量提示自动消失。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { SETTINGS_TAB_ID, useAppStore } from './store';

afterEach(() => {
  vi.useRealTimers();
});

beforeEach(() => {
  useAppStore.setState({ basket: [], message: '' });
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

  it('notify 设置提示文本，3 秒后自动消失', () => {
    vi.useFakeTimers();
    useAppStore.getState().notify('篮子已满');
    expect(useAppStore.getState().message).toBe('篮子已满');

    vi.advanceTimersByTime(3000);
    expect(useAppStore.getState().message).toBe('');
  });

  it('连续 notify 只保留最后一条，且定时器不叠加', () => {
    vi.useFakeTimers();
    useAppStore.getState().notify('第一条');
    vi.advanceTimersByTime(2000);
    useAppStore.getState().notify('第二条');

    vi.advanceTimersByTime(2000); // 距第一条 4s，距第二条 2s
    expect(useAppStore.getState().message).toBe('第二条');

    vi.advanceTimersByTime(1000);
    expect(useAppStore.getState().message).toBe('');
  });

  it('SETTINGS_TAB_ID 是保留的页签标识，不与工作区路径冲突', () => {
    expect(SETTINGS_TAB_ID).toBe('kshell:settings');
  });
});
