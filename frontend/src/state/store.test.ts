// store 增量测试：notify/dismissToast 轻量提示队列、
// openTabs/activeTabId 经 persist 中间件落 localStorage（kshell-tabs）。
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { ChatInfo, TerminalInfo } from '../lib/api';
import { SETTINGS_TAB_ID, dedupeTabs, useAppStore } from './store';

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
    lastAgentId: null,
    selectedProject: null,
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

  it('迟到/重排的 turn_done（时间线已有更新的 user 消息）不得把 running 打回 ready', () => {
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
    // 第一轮收尾（此时用户已乐观发第二轮：新一轮 user 消息 Seq=4 比该 turn_done 的 Seq=3 新）
    useAppStore.getState().applyChat('c1', { Seq: 1, Type: 'assistant', MessageID: 'm1', Text: 'a' });
    useAppStore.getState().applyChat('c1', { Seq: 4, Type: 'user', MessageID: 'u2', Text: '第二轮' });
    // 上一轮的迟到 turn_done 到达：时间线里已有更新的 user 消息，说明新一轮在跑，
    // 这个 turn_done 属于上一轮，不得打断
    useAppStore.getState().applyChat('c1', { Seq: 3, Type: 'turn_done' });
    expect(useAppStore.getState().chats[0].Status).toBe('running');
  });

  it('当前轮的正常 turn_done（无更新的 user 消息）照常收尾为 ready', () => {
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
    useAppStore.getState().applyChat('c1', { Seq: 1, Type: 'assistant', MessageID: 'm1', Text: 'a' });
    useAppStore.getState().applyChat('c1', { Seq: 2, Type: 'turn_done' });
    expect(useAppStore.getState().chats[0].Status).toBe('ready');
  });

  it('turn_done 之后 agent 继续输出（thought/assistant/tool/plan）把状态拉回 running', () => {
    // codebuddy 等 ACP 实现会在任务真正完成前提前返回 prompt 响应（turn_done 已到、
    // 状态已回 ready），随后继续推 session/update 流水。显示层必须跟随 agent 实际活动。
    const chat = {
      ID: 'c1',
      Kind: 'new',
      SessionID: '',
      Workspace: 'D:\\w',
      Title: 't',
      ToolID: 'codebuddy',
      Status: 'ready',
      ExitCode: 0,
      Error: '',
    };
    useAppStore.setState({ chats: [chat], chatSeq: {}, chatItems: {} });
    let seq = 1;
    for (const type of ['tool', 'thought', 'assistant', 'plan'] as const) {
      useAppStore.getState().applyChat('c1', { Seq: seq++, Type: type, MessageID: 'm1', Text: 'x' });
      expect(useAppStore.getState().chats[0].Status).toBe('running');
      // 回到 ready 再测下一种类型
      useAppStore.setState({
        chats: useAppStore.getState().chats.map((c) => (c.ID === 'c1' ? { ...c, Status: 'ready' } : c)),
      });
    }
  });

  it('commands/user 更新不代表 agent 活动，不把 ready 拉回 running', () => {
    const chat = {
      ID: 'c1',
      Kind: 'new',
      SessionID: '',
      Workspace: 'D:\\w',
      Title: 't',
      ToolID: 'codebuddy',
      Status: 'ready',
      ExitCode: 0,
      Error: '',
    };
    useAppStore.setState({ chats: [chat], chatSeq: {}, chatItems: {} });
    useAppStore.getState().applyChat('c1', { Seq: 1, Type: 'commands' });
    expect(useAppStore.getState().chats[0].Status).toBe('ready');
    useAppStore.getState().applyChat('c1', { Seq: 2, Type: 'user', MessageID: 'u1', Text: 'hi' });
    expect(useAppStore.getState().chats[0].Status).toBe('ready');
  });

  it('活动更新把状态拉回 running 后，再收 turn_done 仍回 ready；exited 不被活动更新复活', () => {
    const chat = {
      ID: 'c1',
      Kind: 'new',
      SessionID: '',
      Workspace: 'D:\\w',
      Title: 't',
      ToolID: 'codebuddy',
      Status: 'ready',
      ExitCode: 0,
      Error: '',
    };
    useAppStore.setState({ chats: [chat], chatSeq: {}, chatItems: {} });
    useAppStore.getState().applyChat('c1', { Seq: 1, Type: 'tool', ToolCallID: 't1', Tool: { ToolCallID: 't1', Title: 'Bash' } });
    expect(useAppStore.getState().chats[0].Status).toBe('running');
    useAppStore.getState().applyChat('c1', { Seq: 2, Type: 'turn_done', StopReason: 'end_turn' });
    expect(useAppStore.getState().chats[0].Status).toBe('ready');
    // 退出是终态：迟到的活动输出不得复活
    useAppStore.setState({
      chats: useAppStore.getState().chats.map((c) => (c.ID === 'c1' ? { ...c, Status: 'exited' } : c)),
    });
    useAppStore.getState().applyChat('c1', { Seq: 3, Type: 'tool', ToolCallID: 't2', Tool: { ToolCallID: 't2', Title: 'Bash' } });
    expect(useAppStore.getState().chats[0].Status).toBe('exited');
  });

  it('setChatItems 落位时 chatSeq 只升不降：实时更新已抬高 Seq 的，历史回放不得重置回去', () => {
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
    // 历史回放窗口期间实时更新先到（Seq=9），已抬高 chatSeq
    useAppStore.getState().applyChat('c1', { Seq: 9, Type: 'assistant', MessageID: 'm9', Text: 'live' });
    expect(useAppStore.getState().chatSeq.c1).toBe(9);
    // 随后历史（Seq=1）落位：chatSeq 不得被重置为 1，否则随后补放的旧轮
    // turn_done（Seq=2/3）会越过门控把 running 打回 ready。
    // items 本身是覆盖语义（调用方负责补放缓冲），这里只约束 Seq 单调。
    useAppStore.getState().setChatItems('c1', [
      { key: 'm1', type: 'assistant', text: 'old', seq: 1 },
    ]);
    expect(useAppStore.getState().chatSeq.c1).toBe(9);
    // 旧轮迟到 turn_done 被门控正确丢弃，Status 保持 running
    useAppStore.getState().applyChat('c1', { Seq: 3, Type: 'turn_done' });
    expect(useAppStore.getState().chats[0].Status).toBe('running');
  });

  it('setChatItems 空数组重置为 0 的既有语义保持（无实时更新时）', () => {
    useAppStore.setState({ chatSeq: { c1: 5 }, chatItems: { c1: [] } });
    useAppStore.getState().setChatItems('c1', []);
    expect(useAppStore.getState().chatSeq.c1).toBe(0);
  });

  it('已移除聊天的迟到 permission 事件不得重建孤儿 key', () => {
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
    useAppStore.setState({ chats: [chat], chatItems: {}, chatSeq: {}, chatPermissions: {} });
    useAppStore.getState().setChatPermission('c1', {
      RequestID: 'r1',
      SessionID: 's1',
      ToolCall: { ToolCallID: 'tc1' },
      Options: [],
    });
    expect(useAppStore.getState().chatPermissions.c1).toBeDefined();
    // 用户关闭页签：removeChat 清掉权限；此后 Wails 事件管道里迟到的
    // permission 推送不得把 key 重建回来（否则弹窗挂在已关闭的聊天上）
    useAppStore.getState().removeChat('c1');
    useAppStore.getState().setChatPermission('c1', {
      RequestID: 'r2',
      SessionID: 's1',
      ToolCall: { ToolCallID: 'tc2' },
      Options: [],
    });
    expect(useAppStore.getState().chatPermissions.c1).toBeUndefined();
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

  it('hubMessages 入 store 且不入 localStorage（源在 Go 磁盘，不重复持久化）', () => {
    useAppStore.setState({ openTabs: [{ id: 'D:\\proj-a', name: 'proj-a' }] });
    useAppStore.getState().setHubMessages([
      { id: 1, kind: 'user', text: 'hi', ts: 1 },
      { id: 2, kind: 'agent', tool: 'claude', text: 'done', ts: 2 },
    ]);
    expect(useAppStore.getState().hubMessages).toHaveLength(2);

    const parsed = JSON.parse(localStorage.getItem('kshell-tabs')!) as {
      state: Record<string, unknown>;
    };
    expect(parsed.state).not.toHaveProperty('hubMessages');
  });

  it('setHubMessages 支持函数式更新（App 重拉时按 prev 合并）', () => {
    useAppStore.setState({ hubMessages: [{ id: 1, kind: 'user', text: 'a', ts: 1 }] });
    useAppStore.getState().setHubMessages((prev) => [...prev, { id: 2, kind: 'agent', text: 'b', ts: 2 }]);
    expect(useAppStore.getState().hubMessages.map((m) => m.id)).toEqual([1, 2]);
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

  it('openTab 对仅大小写不同的同一路径只激活既有页签，不新增', () => {
    useAppStore.setState({
      openTabs: [{ id: 'D:\\data\\ws\\zfba', name: 'zfba' }],
      activeTabId: null,
    });
    useAppStore.getState().openTab({
      Path: 'd:\\data\\ws\\zfba', Name: 'zfba', LastUsed: '', SessionCount: 0, ToolCounts: {}, Source: 'manual',
    });
    const s = useAppStore.getState();
    expect(s.openTabs).toHaveLength(1);
    // 保留既有页签 id 作为激活态，避免页签 id 与数组不一致
    expect(s.activeTabId).toBe('D:\\data\\ws\\zfba');
  });

  it('dedupeTabs 合并仅大小写不同的存量页签，激活态跟随保留者', () => {
    const { tabs, activeTabId } = dedupeTabs(
      [
        { id: 'D:\\data\\ws\\zfba', name: 'zfba' },
        { id: 'd:\\data\\ws\\zfba', name: 'zfba' },
      ],
      'd:\\data\\ws\\zfba',
    );
    expect(tabs).toEqual([{ id: 'D:\\data\\ws\\zfba', name: 'zfba' }]);
    expect(activeTabId).toBe('D:\\data\\ws\\zfba');
  });

  it('dedupeTabs 对互不重复的页签原样保留', () => {
    const tabs = [
      { id: 'D:\\a', name: 'a' },
      { id: 'D:\\b', name: 'b' },
    ];
    expect(dedupeTabs(tabs, 'D:\\b')).toEqual({ tabs, activeTabId: 'D:\\b' });
  });

  it('dedupeTabs 保持页签原有顺序（先出现者优先），无激活态也能处理', () => {
    const { tabs, activeTabId } = dedupeTabs(
      [
        { id: 'd:\\ws', name: 'ws' },
        { id: 'D:\\other', name: 'other' },
        { id: 'D:\\WS', name: 'ws' },
      ],
      null,
    );
    expect(tabs.map((t) => t.id)).toEqual(['d:\\ws', 'D:\\other']);
    expect(activeTabId).toBeNull();
  });
});

describe('lastAgentId', () => {
  it('记录最后交互的 agent，未设时为 null', () => {
    expect(useAppStore.getState().lastAgentId).toBeNull();
    useAppStore.getState().setLastAgentId('c1');
    expect(useAppStore.getState().lastAgentId).toBe('c1');
  });
});

describe('chat 模型与技能命令状态', () => {
  it('setChatModels / setChatCommands 按 id 存取', () => {
    useAppStore.setState({ chatModels: {}, chatCommands: {} });
    useAppStore.getState().setChatModels('c1', {
      Current: 'm1',
      Models: [{ ID: 'm1', Name: 'One' }, { ID: 'm2', Name: 'Two', Description: 'D' }],
    });
    expect(useAppStore.getState().chatModels.c1?.Current).toBe('m1');
    expect(useAppStore.getState().chatModels.c1?.Models).toHaveLength(2);

    useAppStore.getState().setChatCommands('c1', [{ Name: 'plan', Description: 'P', InputHint: '主题' }]);
    expect(useAppStore.getState().chatCommands.c1?.[0]?.Name).toBe('plan');
    expect(useAppStore.getState().chatCommands.c1?.[0]?.InputHint).toBe('主题');
  });
});

describe('selectedProject', () => {
  it('setSelectedProject 赋值并写入 localStorage（kshell.home.selectedProject）', () => {
    useAppStore.getState().setSelectedProject('D:\\proj-a');
    expect(useAppStore.getState().selectedProject).toBe('D:\\proj-a');
    expect(localStorage.getItem('kshell.home.selectedProject')).toBe('D:\\proj-a');
  });

  it('setSelectedProject(null) 清空选中态并移除 localStorage 键', () => {
    useAppStore.getState().setSelectedProject('D:\\proj-a');
    useAppStore.getState().setSelectedProject(null);
    expect(useAppStore.getState().selectedProject).toBeNull();
    expect(localStorage.getItem('kshell.home.selectedProject')).toBeNull();
  });

  it('初始值从 localStorage 恢复（模块级单例，需 resetModules 后动态 import）', async () => {
    localStorage.clear();
    localStorage.setItem('kshell.home.selectedProject', 'D:\\proj-b');
    vi.resetModules();
    const fresh = await import('./store');
    expect(fresh.useAppStore.getState().selectedProject).toBe('D:\\proj-b');
  });

  it('初始值：空串视为未选中（null），缺失键同样为 null', async () => {
    localStorage.clear();
    localStorage.setItem('kshell.home.selectedProject', '');
    vi.resetModules();
    const fresh = await import('./store');
    expect(fresh.useAppStore.getState().selectedProject).toBeNull();
  });
});

describe('chatQueues（ChatView 本地消息队列）', () => {
  beforeEach(() => {
    useAppStore.setState({ chatQueues: {}, toasts: [] });
  });

  it('enqueueChat 追加到队尾，id 自增互不相同', () => {
    useAppStore.getState().enqueueChat('c1', { text: '第一条' });
    useAppStore.getState().enqueueChat('c1', { text: '第二条' });

    const q = useAppStore.getState().chatQueues.c1;
    expect(q).toHaveLength(2);
    expect(q[0]).toMatchObject({ text: '第一条' });
    expect(q[1]).toMatchObject({ text: '第二条' });
    expect(q[0]!.id).not.toBe(q[1]!.id);
  });

  it('enqueueChat 支持 blocks（附件消息）', () => {
    useAppStore.getState().enqueueChat('c1', {
      text: '看',
      blocks: [{ Type: 'text', Text: '看' }, { Type: 'image', Data: 'xx', MimeType: 'image/png' }],
    });
    expect(useAppStore.getState().chatQueues.c1![0]!.blocks).toHaveLength(2);
  });

  it('dequeueChatHead 取走队首并移除；空队列返回 null', () => {
    useAppStore.getState().enqueueChat('c1', { text: '第一条' });
    useAppStore.getState().enqueueChat('c1', { text: '第二条' });

    const head = useAppStore.getState().dequeueChatHead('c1');
    expect(head?.text).toBe('第一条');
    expect(useAppStore.getState().chatQueues.c1).toHaveLength(1);
    expect(useAppStore.getState().chatQueues.c1![0]!.text).toBe('第二条');

    useAppStore.getState().dequeueChatHead('c1');
    expect(useAppStore.getState().dequeueChatHead('c1')).toBeNull();
  });

  it('removeQueuedChat 按 id 撤回指定条目', () => {
    useAppStore.getState().enqueueChat('c1', { text: 'A' });
    useAppStore.getState().enqueueChat('c1', { text: 'B' });
    const bid = useAppStore.getState().chatQueues.c1![1]!.id;

    useAppStore.getState().removeQueuedChat('c1', bid);
    const q = useAppStore.getState().chatQueues.c1;
    expect(q).toHaveLength(1);
    expect(q[0]!.text).toBe('A');
  });

  it('clearChatQueue 清空；removeChat 联动清队', () => {
    useAppStore.getState().enqueueChat('c1', { text: 'A' });
    useAppStore.getState().clearChatQueue('c1');
    expect(useAppStore.getState().chatQueues.c1).toBeUndefined();

    useAppStore.getState().enqueueChat('c1', { text: 'A' });
    useAppStore.setState({ chats: [{ ID: 'c1', Kind: 'new', SessionID: 's', Workspace: 'D:\p', Title: 't', ToolID: 'claude', Status: 'ready', ExitCode: 0, Error: '' }] });
    useAppStore.getState().removeChat('c1');
    expect(useAppStore.getState().chatQueues.c1).toBeUndefined();
  });

  it('队列上限 50：超出丢最旧并提示 toast', () => {
    for (let i = 0; i < 51; i++) useAppStore.getState().enqueueChat('c1', { text: `m${i}` });
    const q = useAppStore.getState().chatQueues.c1;
    expect(q).toHaveLength(50);
    expect(q[0]!.text).toBe('m1');
    expect(q[49]!.text).toBe('m50');
    expect(useAppStore.getState().toasts.some((t) => t.tone === 'error')).toBe(true);
  });
});

describe('chatBackendQueues（后端待发送队列镜像）', () => {
  beforeEach(() => {
    useAppStore.setState({ chatBackendQueues: {}, chatQueues: {}, toasts: [] });
  });

  it('setChatBackendQueue 用快照整体替换（含清空）', () => {
    useAppStore.getState().setChatBackendQueue('c1', [
      { ID: '1', HubMsgID: '1', Text: 'A' },
      { ID: 'q1', Text: 'B' },
    ]);
    expect(useAppStore.getState().chatBackendQueues.c1).toHaveLength(2);

    useAppStore.getState().setChatBackendQueue('c1', [{ ID: 'q2', Text: 'C' }]);
    const q = useAppStore.getState().chatBackendQueues.c1;
    expect(q).toHaveLength(1);
    expect(q![0]!.Text).toBe('C');

    // 空快照删除键（组件拿 undefined 时用空数组兜底）
    useAppStore.getState().setChatBackendQueue('c1', []);
    expect(useAppStore.getState().chatBackendQueues.c1).toBeUndefined();
  });

  it('removeChatBackendQueueItem 只删指定条目，其余保留', () => {
    useAppStore.getState().setChatBackendQueue('c1', [
      { ID: '1', HubMsgID: '1', Text: 'A' },
      { ID: 'q1', Text: 'B' },
    ]);
    useAppStore.getState().removeChatBackendQueueItem('c1', '1');
    const q = useAppStore.getState().chatBackendQueues.c1;
    expect(q).toHaveLength(1);
    expect(q![0]!.ID).toBe('q1');

    // 删到空时键消失；未知 chat/条目是 no-op
    useAppStore.getState().removeChatBackendQueueItem('c1', 'q1');
    expect(useAppStore.getState().chatBackendQueues.c1).toBeUndefined();
    useAppStore.getState().removeChatBackendQueueItem('nope', 'x');
  });

  it('removeChat / removeChatState 联动清理镜像', () => {
    useAppStore.getState().setChatBackendQueue('c1', [{ ID: '1', Text: 'A' }]);
    useAppStore.getState().removeChatState('c1');
    expect(useAppStore.getState().chatBackendQueues.c1).toBeUndefined();

    useAppStore.getState().setChatBackendQueue('c1', [{ ID: '1', Text: 'A' }]);
    useAppStore.setState({
      chats: [{ ID: 'c1', Kind: 'new', SessionID: 's', Workspace: 'D:/p', Title: 't', ToolID: 'claude', Status: 'ready', ExitCode: 0, Error: '' }],
    });
    useAppStore.getState().removeChat('c1');
    expect(useAppStore.getState().chatBackendQueues.c1).toBeUndefined();
  });
});
