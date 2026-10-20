// agenthubDefaults：新建 agent 默认值与模型列表缓存的 localStorage 读写测试。
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  formatPlatformModelName,
  hubInjectedModelsForTool,
  loadDefaultCreate,
  loadLastCreate,
  loadToolModelOptions,
  loadToolModels,
  mergeHubModelOptions,
  rememberChatModels,
  rememberLastModel,
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

  it('saveToolModels 对象形态后 loadToolModelOptions 保留 name', () => {
    saveToolModels('cursor', [
      { id: 'default[]', name: 'Auto' },
      { id: 'm2', name: 'Two' },
    ]);
    expect(loadToolModelOptions('cursor')).toEqual([
      { id: 'default[]', name: 'Auto' },
      { id: 'm2', name: 'Two' },
    ]);
    expect(loadToolModels('cursor')).toEqual(['default[]', 'm2']);
  });

  it('旧版 string[] 缓存升格为 name=id', () => {
    localStorage.setItem('kshell.agenthub.models.cursor', JSON.stringify(['default[]', 'm2']));
    expect(loadToolModelOptions('cursor')).toEqual([
      { id: 'default[]', name: 'default[]' },
      { id: 'm2', name: 'm2' },
    ]);
  });

  it('工具未缓存时 loadToolModels 返回空数组', () => {
    expect(loadToolModels('codex')).toEqual([]);
    expect(loadToolModelOptions('codex')).toEqual([]);
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

  it('对象项缺 id 或 id 非字符串时过滤；缺 name 回落 id', () => {
    localStorage.setItem(
      'kshell.agenthub.models.claude',
      JSON.stringify([{ id: 'm1' }, { id: 3, name: 'x' }, { name: 'orphan' }, { id: 'm2', name: '' }]),
    );
    expect(loadToolModelOptions('claude')).toEqual([
      { id: 'm1', name: 'm1' },
      { id: 'm2', name: 'm2' },
    ]);
  });

  it('清空后 loadToolModels 返回空数组', () => {
    saveToolModels('claude', ['m1']);
    localStorage.clear();
    expect(loadToolModels('claude')).toEqual([]);
  });

  it('mergeHubModelOptions：注入在前、缓存其后、按 id 去重', () => {
    expect(
      mergeHubModelOptions(
        ['glm-5.3-flash', 'deepseek-v4.1-flash'],
        [
          { id: 'default[]', name: 'Auto' },
          { id: 'glm-5.3-flash', name: 'should-dedupe' },
        ],
      ),
    ).toEqual([
      { id: 'glm-5.3-flash', name: 'glm-5.3-flash' },
      { id: 'deepseek-v4.1-flash', name: 'deepseek-v4.1-flash' },
      { id: 'default[]', name: 'Auto' },
    ]);
  });

  it('mergeHubModelOptions：注入项可带独立展示名（平台前缀），id 仍为裸模型名', () => {
    expect(
      mergeHubModelOptions(
        [{ id: 'glm-x', name: '智谱: glm-x' }],
        [{ id: 'cache-only', name: 'Cache' }],
      ),
    ).toEqual([
      { id: 'glm-x', name: '智谱: glm-x' },
      { id: 'cache-only', name: 'Cache' },
    ]);
  });

  it('formatPlatformModelName：有平台名则加前缀，空名回落裸 id', () => {
    expect(formatPlatformModelName('智谱', 'glm-x')).toBe('智谱: glm-x');
    expect(formatPlatformModelName('  ', 'glm-x')).toBe('glm-x');
    expect(formatPlatformModelName('', 'glm-x')).toBe('glm-x');
  });

  it('hubInjectedModelsForTool：cursor 不展示平台注入模型', () => {
    expect(hubInjectedModelsForTool('cursor', ['glm-5.3-flash', 'cfg2'])).toEqual([]);
    expect(hubInjectedModelsForTool('claude', ['glm-5.3-flash'])).toEqual(['glm-5.3-flash']);
    expect(
      hubInjectedModelsForTool('cursor', [{ id: 'glm-x', name: '智谱: glm-x' }]),
    ).toEqual([]);
  });

  describe('rememberChatModels', () => {
    it('把视图模型回写缓存并保留 Name', () => {
      rememberChatModels('cursor', { Models: [{ ID: 'default[]', Name: 'Auto' }, { ID: 'm2', Name: 'Two' }] });
      expect(loadToolModelOptions('cursor')).toEqual([
        { id: 'default[]', name: 'Auto' },
        { id: 'm2', name: 'Two' },
      ]);
    });

    it('Name 缺失回落 ID', () => {
      rememberChatModels('cursor', { Models: [{ ID: 'm1' }] });
      expect(loadToolModelOptions('cursor')).toEqual([{ id: 'm1', name: 'm1' }]);
    });

    it('视图为 null 或 Models 空时不覆盖既有缓存', () => {
      saveToolModels('cursor', [{ id: 'keep', name: 'Keep' }]);
      rememberChatModels('cursor', null);
      rememberChatModels('cursor', { Models: [] });
      expect(loadToolModelOptions('cursor')).toEqual([{ id: 'keep', name: 'Keep' }]);
    });

    it('toolId 为空时忽略', () => {
      rememberChatModels('', { Models: [{ ID: 'm1', Name: 'M1' }] });
      expect(loadToolModelOptions('')).toEqual([]);
    });
  });

  describe('rememberLastModel', () => {
    it('lastCreate 同工具时只更新 modelId，保留原 wsPath', () => {
      saveLastCreate({ wsPath: 'D:\\ws\\a', toolId: 'claude', modelId: 'm1' });
      rememberLastModel('claude', 'D:\\ws\\b', 'm2');
      expect(loadLastCreate()).toEqual({ wsPath: 'D:\\ws\\a', toolId: 'claude', modelId: 'm2' });
    });

    it('lastCreate 工具不同时以传入 wsPath/toolId 新建记录', () => {
      saveLastCreate({ wsPath: 'D:\\ws\\a', toolId: 'claude', modelId: 'm1' });
      rememberLastModel('codex', 'D:\\ws\\b', 'm2');
      expect(loadLastCreate()).toEqual({ wsPath: 'D:\\ws\\b', toolId: 'codex', modelId: 'm2' });
    });

    it('无 lastCreate 记录时新建', () => {
      rememberLastModel('codex', 'D:\\ws\\b', 'm2');
      expect(loadLastCreate()).toEqual({ wsPath: 'D:\\ws\\b', toolId: 'codex', modelId: 'm2' });
    });

    it('modelId 空串时不写（语义为回落默认模型，不留记忆）', () => {
      saveLastCreate({ wsPath: 'D:\\ws\\a', toolId: 'claude', modelId: 'm1' });
      rememberLastModel('claude', 'D:\\ws\\a', '');
      expect(loadLastCreate()).toEqual({ wsPath: 'D:\\ws\\a', toolId: 'claude', modelId: 'm1' });
    });

    it('toolId 空串时不写（无法定位工具）', () => {
      rememberLastModel('', 'D:\\ws\\a', 'm1');
      expect(loadLastCreate()).toBeNull();
    });
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
