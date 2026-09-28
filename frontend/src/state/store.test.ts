// store 增量测试：setBasket 重建篮子镜像、notify/dismissToast 轻量提示队列、
// openTabs/activeTabId 经 persist 中间件落 localStorage（kshell-tabs）。
import { beforeEach, describe, expect, it } from 'vitest';
import type { TerminalInfo } from '../lib/api';
import { SETTINGS_TAB_ID, useAppStore } from './store';

beforeEach(() => {
  // persist 会在每次 setState 后写 localStorage，用例间必须清干净避免互相污染
  localStorage.clear();
  useAppStore.setState({
    basket: [],
    toasts: [],
    openTabs: [],
    activeTabId: null,
    terminals: [],
    layout: { left: 288, right: 300 },
  });
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

  it('页签状态持久化：setState 后写入 localStorage 的 kshell-tabs', () => {
    useAppStore.setState({
      openTabs: [{ id: 'D:\\proj-a', name: 'proj-a' }],
      activeTabId: 'D:\\proj-a',
    });

    const raw = localStorage.getItem('kshell-tabs');
    expect(raw).not.toBeNull();
    const parsed = JSON.parse(raw!) as { state: { openTabs: unknown; activeTabId: unknown } };
    expect(parsed.state.openTabs).toEqual([{ id: 'D:\\proj-a', name: 'proj-a' }]);
    expect(parsed.state.activeTabId).toBe('D:\\proj-a');
  });

  it('partialize 只持久化页签字段，篮子/toast 等内存态不入 localStorage', () => {
    useAppStore.setState({
      basket: ['D:\\a.md'],
      openTabs: [{ id: 'D:\\proj-a', name: 'proj-a' }],
      activeTabId: null,
    });
    useAppStore.getState().notify('不要被持久化');

    const parsed = JSON.parse(localStorage.getItem('kshell-tabs')!) as {
      state: Record<string, unknown>;
    };
    expect(Object.keys(parsed.state)).toEqual(
      expect.arrayContaining(['openTabs', 'activeTabId', 'layout']),
    );
    expect(parsed.state).not.toHaveProperty('basket');
    expect(parsed.state).not.toHaveProperty('toasts');
    expect(parsed.state).not.toHaveProperty('terminals');
  });

  it('终端镜像：upsert 新增/覆盖、markTerminalExited 改状态、remove/setTerminals 重建', () => {
    const t1: TerminalInfo = {
      ID: 't1',
      Kind: 'session',
      SessionID: 's1',
      Workspace: 'D:\\proj-a',
      Title: '修登录页',
      ToolID: 'claude',
      Status: 'running',
      ExitCode: 0,
      Cols: 80,
      Rows: 24,
    };
    useAppStore.getState().upsertTerminal(t1);
    expect(useAppStore.getState().terminals).toHaveLength(1);

    // 同 ID 再 upsert 是覆盖而不是追加
    useAppStore.getState().upsertTerminal({ ...t1, Title: '改标题' });
    expect(useAppStore.getState().terminals).toHaveLength(1);
    expect(useAppStore.getState().terminals[0].Title).toBe('改标题');

    useAppStore.getState().markTerminalExited('t1', 3);
    expect(useAppStore.getState().terminals[0]).toMatchObject({ Status: 'exited', ExitCode: 3 });
    // 未知 id 的退出事件不新增条目
    useAppStore.getState().markTerminalExited('nope', 1);
    expect(useAppStore.getState().terminals).toHaveLength(1);

    useAppStore.getState().removeTerminal('t1');
    expect(useAppStore.getState().terminals).toHaveLength(0);

    useAppStore.getState().setTerminals([t1, { ...t1, ID: 't2' }]);
    expect(useAppStore.getState().terminals.map((t) => t.ID)).toEqual(['t1', 't2']);
  });

  it('三栏宽度：setLayout 持久化且 clamp 到 [200,560]', () => {
    useAppStore.getState().setLayout({ left: 9999, right: 10 });
    expect(useAppStore.getState().layout).toEqual({ left: 560, right: 200 });

    useAppStore.getState().setLayout({ left: 320 });
    expect(useAppStore.getState().layout).toEqual({ left: 320, right: 200 });

    // 非法值（NaN/Infinity）回落到下限而不是写进 store
    useAppStore.getState().setLayout({ left: Number.NaN });
    expect(useAppStore.getState().layout.left).toBe(200);

    const parsed = JSON.parse(localStorage.getItem('kshell-tabs')!) as {
      state: { layout: { left: number; right: number } };
    };
    expect(parsed.state.layout).toEqual({ left: 200, right: 200 });
  });
});
