// store 增量测试：notify/dismissToast 轻量提示队列、
// openTabs/activeTabId 经 persist 中间件落 localStorage（kshell-tabs）。
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { ChatInfo, TerminalInfo } from '../lib/api';
import { SETTINGS_TAB_ID, useAppStore } from './store';

beforeEach(() => {
  // persist 会在每次 setState 后写 localStorage，用例间必须清干净避免互相污染
  localStorage.clear();
  useAppStore.setState({
    toasts: [],
    openTabs: [],
    activeTabId: null,
    terminals: [],
    layout: { left: 288, right: 300 },
    terminalBusy: {},
    agentNotices: [],
    focusTermKey: null,
    notifySound: true,
    unreachableWorkspaces: {},
  });
});

describe('store', () => {
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

  it('markUnreachable / clearUnreachable 维护不可达工作区集合且不持久化', () => {
    useAppStore.getState().markUnreachable('ssh://c1/a');
    useAppStore.getState().markUnreachable('ssh://c1/b');
    expect(useAppStore.getState().unreachableWorkspaces).toEqual({
      'ssh://c1/a': true,
      'ssh://c1/b': true,
    });
    useAppStore.getState().clearUnreachable('ssh://c1/a');
    expect(useAppStore.getState().unreachableWorkspaces).toEqual({ 'ssh://c1/b': true });

    const parsed = JSON.parse(localStorage.getItem('kshell-tabs')!) as {
      state: Record<string, unknown>;
    };
    expect(parsed.state).not.toHaveProperty('unreachableWorkspaces');
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

  it('partialize 只持久化页签与提示音开关，toast/终端等内存态不入 localStorage', () => {
    useAppStore.setState({
      openTabs: [{ id: 'D:\\proj-a', name: 'proj-a' }],
      activeTabId: null,
    });
    useAppStore.getState().notify('不要被持久化');

    const parsed = JSON.parse(localStorage.getItem('kshell-tabs')!) as {
      state: Record<string, unknown>;
    };
    expect(Object.keys(parsed.state)).toEqual(
      expect.arrayContaining(['openTabs', 'activeTabId', 'layout', 'notifySound']),
    );
    expect(parsed.state).not.toHaveProperty('toasts');
    expect(parsed.state).not.toHaveProperty('terminals');
    expect(parsed.state).not.toHaveProperty('activityCompleted');
    expect(parsed.state).not.toHaveProperty('terminalBusy');
    // 语言配置来自 Go 绑定层，刷新即重取，不入 localStorage
    expect(parsed.state).not.toHaveProperty('language');
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

  it('setChatItems 用真实最大值更新 chatSeq，空数组重置为 0', () => {
    useAppStore.setState({ chatItems: {}, chatSeq: {} });

    // 非末尾项才是最大 Seq（upsert 可能造成），必须取 max 而非末项
    useAppStore.getState().setChatItems('x', [
      { key: 'a', type: 'assistant', seq: 5 },
      { key: 'b', type: 'tool', seq: 2 },
    ]);
    expect(useAppStore.getState().chatSeq.x).toBe(5);

    useAppStore.getState().setChatItems('x', []);
    expect(useAppStore.getState().chatSeq.x).toBe(0);
  });

  it('三栏宽度：setLayout 持久化且 clamp 到 [200,720]', () => {
    useAppStore.getState().setLayout({ left: 9999, right: 10 });
    expect(useAppStore.getState().layout).toEqual({ left: 720, right: 200 });

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

  it('applyChat：已退出的聊天收到 turn_done/error 不再复活为 ready', () => {
    const exited: ChatInfo = {
      ID: 'c1',
      Kind: 'new',
      SessionID: 's1',
      Workspace: 'D:\\proj-a',
      Title: 't',
      ToolID: 'claude',
      Status: 'exited',
      ExitCode: 3,
      Error: '',
    };
    useAppStore.setState({ chats: [exited], chatItems: {}, chatSeq: {} });

    useAppStore.getState().applyChat('c1', { Seq: 1, Type: 'turn_done' });
    expect(useAppStore.getState().chats[0].Status).toBe('exited');

    useAppStore.getState().applyChat('c1', { Seq: 2, Type: 'error', Text: 'boom' });
    expect(useAppStore.getState().chats[0].Status).toBe('exited');
  });

  it('setTerminalBusy 置位与清除；removeTerminal 去掉键', () => {
    useAppStore.getState().setTerminalBusy('t1', true);
    expect(useAppStore.getState().terminalBusy.t1).toBe(true);
    useAppStore.getState().setTerminalBusy('t1', false);
    expect(useAppStore.getState().terminalBusy.t1).toBeUndefined();
    useAppStore.getState().setTerminalBusy('t1', true);
    useAppStore.getState().removeTerminal('t1');
    expect(useAppStore.getState().terminalBusy.t1).toBeUndefined();
  });

  it('turn_done 将 Status 置 ready（不再写 activityCompleted）', () => {
    const chat = {
      ID: 'c1',
      Kind: 'new',
      SessionID: '',
      Workspace: 'D:\\w',
      Title: 't',
      ToolID: 'claude',
      Status: 'running',
      ExitCode: 0,
      Error: '',
    };
    useAppStore.setState({ chats: [chat], chatSeq: {}, chatItems: {} });
    useAppStore.getState().applyChat('c1', {
      Seq: 1,
      Type: 'turn_done',
      StopReason: 'end_turn',
    });
    expect(useAppStore.getState().chats[0].Status).toBe('ready');
    expect(useAppStore.getState()).not.toHaveProperty('activityCompleted');
  });

  it('agent 通知：入队追加、dismiss 移除、clear 清空', () => {
    useAppStore.getState().pushAgentNotice({
      tool: 'claude', event: 'Stop', termKey: 'session:s1', workspace: 'D:\\w', summary: '完成',
    });
    useAppStore.getState().pushAgentNotice({
      tool: 'codex', event: 'Notification', termKey: 'new:1', workspace: 'D:\\w', summary: '等确认',
    });

    const notices = useAppStore.getState().agentNotices;
    expect(notices).toHaveLength(2);
    expect(notices[0]).toMatchObject({ tool: 'claude', event: 'Stop', termKey: 'session:s1' });
    expect(new Set(notices.map((n) => n.id)).size).toBe(2);

    useAppStore.getState().dismissAgentNotice(notices[0].id);
    expect(useAppStore.getState().agentNotices.map((n) => n.tool)).toEqual(['codex']);

    useAppStore.getState().clearAgentNotices();
    expect(useAppStore.getState().agentNotices).toHaveLength(0);
  });

  it('agent 通知队列有上限保护（窗口隐藏期间不无限堆积）', () => {
    for (let i = 0; i < 12; i++) {
      useAppStore.getState().pushAgentNotice({
        tool: 'claude', event: 'Stop', termKey: 'k' + i, workspace: 'D:\\w', summary: String(i),
      });
    }
    const notices = useAppStore.getState().agentNotices;
    expect(notices.length).toBeLessThanOrEqual(8);
    // 保留最新的一条
    expect(notices[notices.length - 1].termKey).toBe('k11');
  });

  it('notifySound 初始默认值为开（不依赖 beforeEach 预置）', async () => {
    // beforeEach 已把 notifySound 置 true，只有重置模块图才能看到 store.ts 的原始默认值
    localStorage.clear();
    vi.resetModules();
    const freshStore = await import('./store');
    expect(freshStore.useAppStore.getState().notifySound).toBe(true);
  });

  it('setNotifySound 切换并持久化到 kshell-tabs', () => {
    useAppStore.getState().setNotifySound(false);
    expect(useAppStore.getState().notifySound).toBe(false);
    const parsed = JSON.parse(localStorage.getItem('kshell-tabs')!) as {
      state: Record<string, unknown>;
    };
    expect(parsed.state.notifySound).toBe(false);
  });

  it('agent 通知不入 localStorage（与页签持久化分离）', () => {
    useAppStore.setState({ openTabs: [{ id: 'D:\\proj-a', name: 'proj-a' }] });
    useAppStore.getState().pushAgentNotice({
      tool: 'claude', event: 'Stop', termKey: 'k', workspace: 'D:\\w', summary: '完成',
    });

    const parsed = JSON.parse(localStorage.getItem('kshell-tabs')!) as {
      state: Record<string, unknown>;
    };
    expect(parsed.state).not.toHaveProperty('agentNotices');
    expect(parsed.state).not.toHaveProperty('focusTermKey');
  });

  it('focusTermKey：请求带自增 seq，clear 只清掉对应那次请求', () => {
    useAppStore.getState().requestFocusTerm('session:s1');
    const first = useAppStore.getState().focusTermKey;
    expect(first).toEqual({ termKey: 'session:s1', seq: 1 });

    // 消费期间来了新的请求：旧 seq 的 clear 不得误删新请求
    useAppStore.getState().requestFocusTerm('new:2');
    useAppStore.getState().clearFocusTerm(first!.seq);
    expect(useAppStore.getState().focusTermKey).toEqual({ termKey: 'new:2', seq: 2 });

    useAppStore.getState().clearFocusTerm(2);
    expect(useAppStore.getState().focusTermKey).toBeNull();
  });
});
