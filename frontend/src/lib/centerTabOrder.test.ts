import { afterEach, describe, expect, it } from 'vitest';
import {
  computeOverflowHidden,
  loadCenterTabOrder,
  mergeCenterTabOrder,
  reorderCenterTabs,
  saveCenterTabOrder,
} from './centerTabOrder';

describe('mergeCenterTabOrder', () => {
  it('保留已保存顺序，新 ID 按 live 顺序追加', () => {
    expect(mergeCenterTabOrder(['b', 'a'], ['a', 'b', 'c'])).toEqual(['b', 'a', 'c']);
  });

  it('丢掉已关闭的 ID', () => {
    expect(mergeCenterTabOrder(['a', 'gone', 'b'], ['b', 'a'])).toEqual(['a', 'b']);
  });

  it('无保存时等于 liveIDs', () => {
    expect(mergeCenterTabOrder([], ['x', 'y'])).toEqual(['x', 'y']);
  });
});

describe('reorderCenterTabs', () => {
  it('把 from 插到 to 的位置', () => {
    expect(reorderCenterTabs(['a', 'b', 'c'], 'c', 'a')).toEqual(['c', 'a', 'b']);
    expect(reorderCenterTabs(['a', 'b', 'c'], 'a', 'c')).toEqual(['b', 'c', 'a']);
  });

  it('未知 id 或相同 id 不改序', () => {
    expect(reorderCenterTabs(['a', 'b'], 'a', 'a')).toEqual(['a', 'b']);
    expect(reorderCenterTabs(['a', 'b'], 'z', 'a')).toEqual(['a', 'b']);
  });
});

describe('computeOverflowHidden', () => {
  it('总宽未超 available 时不隐藏', () => {
    expect(computeOverflowHidden(['a', 'b'], [40, 40], 'a', 100, 28)).toEqual([]);
  });

  it('超宽时从右侧藏非激活项，并预留更多按钮宽度', () => {
    // 三页签各 50，可用 100 → 需更多钮后 avail=72 → 只能放激活 a(50) + 部分，藏 b、c
    expect(computeOverflowHidden(['a', 'b', 'c'], [50, 50, 50], 'a', 100, 28)).toEqual(['b', 'c']);
  });

  it('激活在末尾时从左侧藏', () => {
    expect(computeOverflowHidden(['a', 'b', 'c'], [50, 50, 50], 'c', 100, 28)).toEqual(['a', 'b']);
  });
});

describe('load/saveCenterTabOrder', () => {
  afterEach(() => {
    localStorage.removeItem('kshell.centerTabOrder');
  });

  it('按工作区路径读写', () => {
    saveCenterTabOrder('D:\\ws', ['t1', 'c1']);
    expect(loadCenterTabOrder('D:\\ws')).toEqual(['t1', 'c1']);
    expect(loadCenterTabOrder('D:\\other')).toEqual([]);
  });

  it('损坏 JSON 时返回空数组', () => {
    localStorage.setItem('kshell.centerTabOrder', '{not-json');
    expect(loadCenterTabOrder('D:\\ws')).toEqual([]);
  });
});
