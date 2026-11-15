// agenthubDefaults：新建 agent 默认值与模型列表缓存的 localStorage 读写测试。
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  isClaudeDefaultModel,
  lastModelForCreate,
  loadDefaultCreate,
  loadLastCreate,
  loadPerToolLastModel,
  loadToolModelOptions,
  loadToolModels,
  mergeHubModelOptions,
  normalizeCreateModel,
  rememberChatModels,
  rememberLastModel,
  rememberPerToolLastModel,
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

    it('按工具记忆双写：跨工具互补覆盖', () => {
      rememberLastModel('opencode', 'D:\\ws\\a', 'glm-5.3-flash');
      rememberLastModel('claude', 'D:\\ws\\b', 'claude-m');
      // lastCreate 被后写的 claude 覆盖，但 opencode 的按工具记忆仍在
      expect(loadPerToolLastModel('opencode')).toEqual({ modelId: 'glm-5.3-flash' });
      expect(loadPerToolLastModel('claude')).toEqual({ modelId: 'claude-m' });
      expect(loadPerToolLastModel('codex')).toBeNull();
    });

    it('按工具记忆 modelId 空串时不写', () => {
      rememberPerToolLastModel('opencode', 'glm-5.3-flash');
      rememberLastModel('opencode', 'D:\\ws\\a', '');
      expect(loadPerToolLastModel('opencode')).toEqual({ modelId: 'glm-5.3-flash' });
    });

    it('按工具记忆键损坏 JSON 时返回 null', () => {
      localStorage.setItem('kshell.agenthub.perToolLastModel', '{bad json');
      expect(loadPerToolLastModel('opencode')).toBeNull();
    });

    it('claude 的 default 不落记忆：清按工具条目且同工具 lastCreate 置 null', () => {
      rememberLastModel('claude', 'D:\\ws\\a', 'm1');
      rememberLastModel('claude', 'D:\\ws\\b', 'default');
      expect(loadPerToolLastModel('claude')).toBeNull();
      expect(loadLastCreate()).toEqual({ wsPath: 'D:\\ws\\a', toolId: 'claude', modelId: null });
    });

    it('claude 的 Default（大小写/空白）同样不落记忆', () => {
      rememberPerToolLastModel('claude', ' Default ');
      expect(loadPerToolLastModel('claude')).toBeNull();
    });

    it('非 claude 工具的 default 照常记忆', () => {
      rememberLastModel('codex', 'D:\\ws\\a', 'default');
      expect(loadPerToolLastModel('codex')).toEqual({ modelId: 'default' });
      expect(loadLastCreate()).toEqual({ wsPath: 'D:\\ws\\a', toolId: 'codex', modelId: 'default' });
    });
  });

  describe('lastModelForCreate', () => {
    it('显式默认设置模型优先', () => {
      rememberPerToolLastModel('opencode', 'glm-5.3-flash');
      expect(lastModelForCreate('explicit-m', 'opencode')).toBe('explicit-m');
    });

    it('默认设置为 null 时回落该工具记忆', () => {
      rememberPerToolLastModel('opencode', 'glm-5.3-flash');
      expect(lastModelForCreate(null, 'opencode')).toBe('glm-5.3-flash');
    });

    it('无按工具记忆时回落 lastCreate 同工具记录', () => {
      saveLastCreate({ wsPath: 'D:\\ws\\a', toolId: 'codex', modelId: 'last-m' });
      expect(lastModelForCreate(null, 'codex')).toBe('last-m');
      // lastCreate 工具不同则不借用别的工具的记忆
      expect(lastModelForCreate(null, 'gemini')).toBeNull();
    });

    it('全部缺失返回 null（用工具默认模型）', () => {
      expect(lastModelForCreate(null, 'none-tool')).toBeNull();
    });

    it('claude 的历史 default 一律过滤，按优先级继续回落', () => {
      saveDefaultCreate({ toolId: 'claude', modelId: 'default' });
      saveLastCreate({ wsPath: 'D:\\ws\\a', toolId: 'claude', modelId: 'default' });
      // 显式默认设置是 default → 跳过；按工具记忆无 → 跳过；lastCreate 也是 → null
      expect(lastModelForCreate(loadDefaultCreate()?.modelId, 'claude')).toBeNull();
      expect(lastModelForCreate(null, 'claude')).toBeNull();
    });

    it('非 claude 工具的 default 原样返回', () => {
      rememberPerToolLastModel('codex', 'default');
      expect(lastModelForCreate(null, 'codex')).toBe('default');
      // cursor 的 default[] 不是伪模型
      expect(lastModelForCreate('default[]', 'cursor')).toBe('default[]');
    });
  });

  describe('isClaudeDefaultModel / normalizeCreateModel', () => {
    it('只认 claude 的精确 default（trim + 大小写归一）', () => {
      expect(isClaudeDefaultModel('claude', 'default')).toBe(true);
      expect(isClaudeDefaultModel('claude', ' Default ')).toBe(true);
      expect(isClaudeDefaultModel('claude', 'default[]')).toBe(false);
      expect(isClaudeDefaultModel('cursor', 'default')).toBe(false);
      expect(isClaudeDefaultModel('claude', 'glm-5.3-flash')).toBe(false);
      expect(isClaudeDefaultModel('claude', null)).toBe(false);
    });

    it('创建模型归一：claude default → null，其余原样', () => {
      expect(normalizeCreateModel('claude', 'default')).toBeNull();
      expect(normalizeCreateModel('claude', '')).toBeNull();
      expect(normalizeCreateModel('claude', 'glm-5.3-flash')).toBe('glm-5.3-flash');
      expect(normalizeCreateModel('cursor', 'default[]')).toBe('default[]');
      expect(normalizeCreateModel('codex', 'default')).toBe('default');
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
