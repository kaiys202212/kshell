// agenthubDefaults：新建 agent 默认值与模型列表缓存的 localStorage 读写测试。
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  loadDefaultCreate,
  loadLastCreate,
  loadToolModels,
  saveDefaultCreate,
  saveLastCreate,
  saveToolModels,
} from './agenthubDefaults';

afterEach(() => {
  localStorage.clear();
});

describe('agenthubDefaults', () => {
  it('saveLastCreate 后 loadLastCreate 往返一致', () => {
    const v = { wsPath: 'D:\\ws\\a', toolId: 'claude', modelId: 'm2' };
    saveLastCreate(v);
    expect(loadLastCreate()).toEqual(v);
  });

  it('modelId 为 null 时往返仍为 null', () => {
    saveLastCreate({ wsPath: 'D:\\ws\\a', toolId: 'claude', modelId: null });
    expect(loadLastCreate()).toEqual({ wsPath: 'D:\\ws\\a', toolId: 'claude', modelId: null });
  });

  it('lastCreate 键损坏 JSON 时返回 null', () => {
    localStorage.setItem('kshell.agenthub.lastCreate', '{bad json');
    expect(loadLastCreate()).toBeNull();
  });

  it('lastCreate 结构缺字段时返回 null', () => {
    localStorage.setItem('kshell.agenthub.lastCreate', JSON.stringify({ wsPath: 'D:\\ws\\a' }));
    expect(loadLastCreate()).toBeNull();
  });

  it('清空后 loadLastCreate 返回 null', () => {
    saveLastCreate({ wsPath: 'D:\\ws\\a', toolId: 'claude', modelId: 'm2' });
    localStorage.clear();
    expect(loadLastCreate()).toBeNull();
  });

  it('saveToolModels 后 loadToolModels 往返一致', () => {
    saveToolModels('claude', ['m1', 'm2']);
    expect(loadToolModels('claude')).toEqual(['m1', 'm2']);
  });

  it('工具未缓存时 loadToolModels 返回空数组', () => {
    expect(loadToolModels('codex')).toEqual([]);
  });

  it('模型列表键损坏 JSON 或非数组时返回空数组', () => {
    localStorage.setItem('kshell.agenthub.models.claude', '{bad');
    expect(loadToolModels('claude')).toEqual([]);
    localStorage.setItem('kshell.agenthub.models.claude', JSON.stringify('not-array'));
    expect(loadToolModels('claude')).toEqual([]);
  });

  it('模型列表里的非字符串项被过滤', () => {
    localStorage.setItem('kshell.agenthub.models.claude', JSON.stringify(['m1', 3, null]));
    expect(loadToolModels('claude')).toEqual(['m1']);
  });

  it('清空后 loadToolModels 返回空数组', () => {
    saveToolModels('claude', ['m1']);
    localStorage.clear();
    expect(loadToolModels('claude')).toEqual([]);
  });

  it('saveDefaultCreate 后 loadDefaultCreate 往返一致', () => {
    const v = { toolId: 'claude', modelId: 'm2' };
    saveDefaultCreate(v);
    expect(loadDefaultCreate()).toEqual(v);
  });

  it('defaultCreate 的 modelId 为 null 时往返仍为 null', () => {
    saveDefaultCreate({ toolId: 'codex', modelId: null });
    expect(loadDefaultCreate()).toEqual({ toolId: 'codex', modelId: null });
  });

  it('defaultCreate 键损坏 JSON 时返回 null', () => {
    localStorage.setItem('kshell.agenthub.defaultCreate', '{bad json');
    expect(loadDefaultCreate()).toBeNull();
  });

  it('defaultCreate 结构缺 toolId 时返回 null', () => {
    localStorage.setItem('kshell.agenthub.defaultCreate', JSON.stringify({ modelId: 'm1' }));
    expect(loadDefaultCreate()).toBeNull();
  });

  it('清空后 loadDefaultCreate 返回 null', () => {
    saveDefaultCreate({ toolId: 'claude', modelId: 'm2' });
    localStorage.clear();
    expect(loadDefaultCreate()).toBeNull();
  });

  it('defaultCreate 的 modelId 为非字符串时回落 null', () => {
    localStorage.setItem('kshell.agenthub.defaultCreate', JSON.stringify({ toolId: 'claude', modelId: 42 }));
    expect(loadDefaultCreate()).toEqual({ toolId: 'claude', modelId: null });
  });

  it('saveDefaultCreate 在 localStorage 写入异常时静默不抛错', () => {
    const spy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('quota');
    });
    expect(() => saveDefaultCreate({ toolId: 'claude', modelId: null })).not.toThrow();
    spy.mockRestore();
  });
});
