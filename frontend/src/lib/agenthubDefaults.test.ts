// agenthubDefaults：新建 agent 默认值与模型列表缓存的 localStorage 读写测试。
import { afterEach, describe, expect, it } from 'vitest';
import {
  loadLastCreate,
  loadToolModels,
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
});
