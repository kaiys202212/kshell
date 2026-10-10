// listConnections 的 wire 边界防御测试：后端 nil 切片经 Wails 序列化成 JSON null，
// 这里必须兜底成空数组，避免调用方对 null 调数组方法抛错（「查看全部」黑屏诱因）。
import { afterEach, describe, expect, it, vi } from 'vitest';
import { listConnections } from './api';

function stubBackend(list: unknown): void {
  (window as unknown as { go: unknown }).go = {
    desktop: { App: { ListConnections: vi.fn(async () => list) } },
  };
}

afterEach(() => {
  delete (window as unknown as { go?: unknown }).go;
  vi.restoreAllMocks();
});

describe('listConnections wire 防御', () => {
  it('后端返回 null 时兜底为空数组', async () => {
    stubBackend(null);
    await expect(listConnections('')).resolves.toEqual([]);
  });

  it('后端正常返回数组时原样透传', async () => {
    const list = [{ ID: 'c1' }];
    stubBackend(list);
    await expect(listConnections('ws')).resolves.toBe(list);
  });
});
