import { describe, expect, it } from 'vitest';
import {
  activateTab,
  closeTab,
  emptyFileTabs,
  fileTabLabel,
  openPinned,
  openPreview,
  pinTab,
} from './fileTabs';

describe('fileTabLabel', () => {
  it('取路径最后一段', () => {
    expect(fileTabLabel('D:\\proj\\src\\main.ts')).toBe('main.ts');
    expect(fileTabLabel('/tmp/a.md')).toBe('a.md');
  });
});

describe('openPreview', () => {
  it('空态新建预览页签', () => {
    const s = openPreview(emptyFileTabs(), 'a.ts');
    expect(s.tabs).toEqual([{ path: 'a.ts', preview: true }]);
    expect(s.activePath).toBe('a.ts');
  });

  it('单击另一文件替换预览页签', () => {
    const s = openPreview(openPreview(emptyFileTabs(), 'a.ts'), 'b.ts');
    expect(s.tabs).toEqual([{ path: 'b.ts', preview: true }]);
    expect(s.activePath).toBe('b.ts');
  });

  it('已打开的固定页签只激活、不新建', () => {
    const pinned = openPinned(emptyFileTabs(), 'a.ts');
    const s = openPreview(pinned, 'a.ts');
    expect(s.tabs).toEqual([{ path: 'a.ts', preview: false }]);
    expect(s.activePath).toBe('a.ts');
  });

  it('不挤掉已固定页签', () => {
    const s = openPreview(openPinned(emptyFileTabs(), 'a.ts'), 'b.ts');
    expect(s.tabs).toEqual([
      { path: 'a.ts', preview: false },
      { path: 'b.ts', preview: true },
    ]);
  });
});

describe('openPinned / pinTab', () => {
  it('双击未打开文件新开固定页签', () => {
    const s = openPinned(emptyFileTabs(), 'a.ts');
    expect(s.tabs).toEqual([{ path: 'a.ts', preview: false }]);
  });

  it('双击预览页签将其固定', () => {
    const s = openPinned(openPreview(emptyFileTabs(), 'a.ts'), 'a.ts');
    expect(s.tabs).toEqual([{ path: 'a.ts', preview: false }]);
  });

  it('pinTab 与双击相同', () => {
    expect(pinTab(openPreview(emptyFileTabs(), 'a.ts'), 'a.ts').tabs[0].preview).toBe(false);
  });
});

describe('closeTab / activateTab', () => {
  it('关闭当前页签激活右侧邻居', () => {
    let s = openPinned(emptyFileTabs(), 'a.ts');
    s = openPinned(s, 'b.ts');
    s = openPinned(s, 'c.ts');
    s = activateTab(s, 'b.ts');
    s = closeTab(s, 'b.ts');
    expect(s.tabs.map((t) => t.path)).toEqual(['a.ts', 'c.ts']);
    expect(s.activePath).toBe('c.ts');
  });

  it('关掉最后一个页签后 active 为空', () => {
    const s = closeTab(openPreview(emptyFileTabs(), 'a.ts'), 'a.ts');
    expect(s.tabs).toEqual([]);
    expect(s.activePath).toBeNull();
  });
});
