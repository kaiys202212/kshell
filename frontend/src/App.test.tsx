// App 主框架测试：挂载时调 ListTerminals 重建终端镜像、
// 标题栏「首页/设置/工作区页签」切换与关闭（关闭工作区页签连带结束其内嵌终端）、
// 终端事件总线（terminal:exit 更新镜像并提示）、
// 全局快捷键 Ctrl+K 打开快速切换器 / Ctrl+F 派发聚焦搜索事件。
// api 层整体打桩（vi.mock），与 SessionList.test 同一套模式。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import App from './App';
import { SETTINGS_TAB_ID, useAppStore } from './state/store';
import type { ChatInfo, ChatPermissionRequest, TerminalInfo, Workspace } from './lib/api';

const mocks = vi.hoisted(() => ({
  getWorkspaces: vi.fn(),
  getSessions: vi.fn(),
  scanSessions: vi.fn(),
  onScanDone: vi.fn(),
  onWindowClosed: vi.fn(),
  listFiles: vi.fn(),
  previewFile: vi.fn(),
  listConnections: vi.fn(),
  openSSH: vi.fn(),
  execRemote: vi.fn(),
  getTools: vi.fn(),
  loadProvidersYAML: vi.fn(),
  saveProvidersYAML: vi.fn(),
  restartApp: vi.fn(),
  openSessionTerminal: vi.fn(),
  openWorkspaceTerminal: vi.fn(),
  writeTerminal: vi.fn(),
  resizeTerminal: vi.fn(),
  closeTerminal: vi.fn(),
  listTerminals: vi.fn(),
  scrollbackTerminal: vi.fn(),
  newSessionWithTool: vi.fn(),
  onTerminalData: vi.fn(),
  onTerminalExit: vi.fn(),
  createProject: vi.fn(),
  hideProject: vi.fn(),
  restoreProject: vi.fn(),
  getDeletedProjects: vi.fn(),
  onProjectsChanged: vi.fn(),
  getAppearance: vi.fn(),
  onAppearanceChanged: vi.fn(),
  getCloseBehavior: vi.fn(),
  setCloseBehavior: vi.fn(),
  getModelConfig: vi.fn(),
  setModelConfig: vi.fn(),
  listModelPresets: vi.fn(),
  getSessionMode: vi.fn(),
  setSessionMode: vi.fn(),
  getPermissionMode: vi.fn(),
  setPermissionMode: vi.fn(),
  openSession: vi.fn(),
  openSessionACP: vi.fn(),
  openWorkspace: vi.fn(),
  openWorkspaceACP: vi.fn(),
  closeChat: vi.fn(),
  listChats: vi.fn(),
  chatHistory: vi.fn(),
  onChatUpdate: vi.fn(),
  onChatPermission: vi.fn(),
  onChatExit: vi.fn(),
  refreshFiles: vi.fn().mockResolvedValue(undefined),
  startFileWatch: vi.fn().mockResolvedValue(undefined),
  stopFileWatch: vi.fn(),
  onFilesChanged: vi.fn(() => () => {}),
  revealInExplorer: vi.fn().mockResolvedValue(undefined),
}));
vi.mock('./lib/api', () => mocks);

// wailsjs runtime：jsdom 下无 window.runtime，OnFileDrop 会直接抛 TypeError，必须打桩
const runtimeMocks = vi.hoisted(() => ({
  OnFileDrop: vi.fn(),
  OnFileDropOff: vi.fn(),
}));
vi.mock('../wailsjs/runtime/runtime', () => runtimeMocks);

// xterm 在 jsdom 下跑不了（无 matchMedia/布局）：这里只验证「页签常挂载 + 激活态」，
// 终端本体的行为由 TerminalView.test 覆盖
vi.mock('./components/TerminalView', () => ({
  default: ({ term, active }: { term: TerminalInfo; active: boolean }) => (
    <div data-testid={`terminal-${term.ID}`} data-active={String(active)} />
  ),
}));

// 页签内容常挂载（非激活只是 hidden），断言可见性要看包裹层的 class。
// 用属性值精确比较而不是 CSS 选择器：工作区 id 是 Windows 路径，反斜杠在
// 属性选择器里会被当成转义序列。
function pane(name: string): HTMLElement {
  const el = Array.from(document.querySelectorAll('[data-pane]')).find(
    (node) => node.getAttribute('data-pane') === name,
  );
  if (!el) throw new Error(`未找到页签容器：${name}`);
  return el as HTMLElement;
}

const term: TerminalInfo = {
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

const chat: ChatInfo = {
  ID: 'c1',
  Kind: 'new',
  SessionID: 's1',
  Workspace: 'D:\\proj-a',
  Title: '新会话',
  ToolID: 'claude',
  Status: 'ready',
  ExitCode: 0,
  Error: '',
};

const permission: ChatPermissionRequest = {
  RequestID: 'r1',
  SessionID: 's1',
  ToolCall: { ToolCallID: 'tc1' },
  Options: [],
};

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  mocks.getWorkspaces.mockResolvedValue([]);
  mocks.getSessions.mockResolvedValue([]);
  mocks.scanSessions.mockResolvedValue(undefined);
  mocks.onScanDone.mockImplementation(() => () => {});
  mocks.onWindowClosed.mockImplementation(() => () => {});
  mocks.onTerminalData.mockImplementation(() => () => {});
  mocks.onTerminalExit.mockImplementation(() => () => {});
  mocks.getTools.mockResolvedValue([]);
  mocks.loadProvidersYAML.mockResolvedValue('');
  mocks.listFiles.mockResolvedValue([]);
  mocks.listConnections.mockResolvedValue([]);
  mocks.listTerminals.mockResolvedValue([]);
  mocks.closeTerminal.mockResolvedValue(undefined);
  mocks.getDeletedProjects.mockResolvedValue([]);
  mocks.createProject.mockResolvedValue('');
  mocks.hideProject.mockResolvedValue(undefined);
  mocks.restoreProject.mockResolvedValue(undefined);
  mocks.onProjectsChanged.mockImplementation(() => () => {});
  mocks.getAppearance.mockResolvedValue({ mode: 'system', resolved: 'dark', fontSize: 13 });
  mocks.onAppearanceChanged.mockReturnValue(() => {});
  mocks.getCloseBehavior.mockResolvedValue('tray');
  mocks.setCloseBehavior.mockResolvedValue(undefined);
  mocks.getModelConfig.mockResolvedValue({
    Enabled: false,
    Preset: '',
    OpenAIBaseURL: '',
    AnthropicBaseURL: '',
    Agents: {},
    APIKeySet: false,
  });
  mocks.setModelConfig.mockResolvedValue(undefined);
  mocks.listModelPresets.mockResolvedValue([]);
  mocks.getSessionMode.mockResolvedValue('tui');
  mocks.setSessionMode.mockResolvedValue(undefined);
  mocks.getPermissionMode.mockResolvedValue('default');
  mocks.setPermissionMode.mockResolvedValue(undefined);
  mocks.closeChat.mockResolvedValue(undefined);
  mocks.listChats.mockResolvedValue([]);
  mocks.chatHistory.mockResolvedValue([]);
  mocks.onChatUpdate.mockImplementation(() => () => {});
  mocks.onChatPermission.mockImplementation(() => () => {});
  mocks.onChatExit.mockImplementation(() => () => {});
  useAppStore.setState({
    openTabs: [],
    activeTabId: null,
    toasts: [],
    windowStatus: {},
    scanState: 'idle',
    terminals: [],
    chats: [],
    chatItems: {},
    chatSeq: {},
    chatPermissions: {},
    layout: { left: 288, right: 300 },
  });
});

describe('App', () => {
  it('挂载时调 ListTerminals 重建终端镜像（前端重载后 Go 侧终端仍在跑）', async () => {
    mocks.listTerminals.mockResolvedValue([term]);
    render(<App />);

    await waitFor(() => {
      expect(useAppStore.getState().terminals).toEqual([term]);
    });
    expect(mocks.listTerminals).toHaveBeenCalledTimes(1);
  });

  it('标题栏有固定的「设置」页签，点击切换到设置页，再点「首页」切回', async () => {
    render(<App />);

    // 初始：首页可见、设置页已挂载但隐藏
    expect(pane('home').className).not.toContain('hidden');
    expect(pane('settings').className).toContain('hidden');

    fireEvent.click(screen.getByRole('button', { name: '设置' }));
    await waitFor(() => {
      expect(useAppStore.getState().activeTabId).toBe(SETTINGS_TAB_ID);
    });
    expect(pane('settings').className).not.toContain('hidden');
    expect(pane('home').className).toContain('hidden');
    // 设置页左导航分区（通用为默认选中）
    expect(pane('settings').querySelector('[aria-label="设置分区"]')).toBeTruthy();
    expect(screen.getByRole('button', { name: '通用' })).toHaveAttribute('aria-current', 'page');

    fireEvent.click(screen.getByRole('button', { name: '首页' }));
    expect(useAppStore.getState().activeTabId).toBeNull();
    expect(pane('home').className).not.toContain('hidden');
  });

  it('工作区页签常挂载：切到首页后会话列表仍在 DOM（hidden 而非卸载）', async () => {
    useAppStore.setState({ openTabs: [{ id: 'D:\\proj-a', name: 'proj-a' }], activeTabId: 'D:\\proj-a' });
    render(<App />);
    // 工作区页签内的文件面板可见
    expect(await screen.findByText('没有可显示的文件')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: '首页' }));
    expect(pane('home').className).not.toContain('hidden');
    // 工作区页签内容没有卸载（只是 hidden），文件面板仍在 DOM 里
    expect(pane('D:\\proj-a').className).toContain('hidden');
    expect(screen.getByText('没有可显示的文件')).toBeInTheDocument();
  });

  it('终端退出事件：更新镜像状态并给出提示', async () => {
    let exitCb: ((p: { id: string; exitCode: number }) => void) | undefined;
    mocks.onTerminalExit.mockImplementation((cb: (p: { id: string; exitCode: number }) => void) => {
      exitCb = cb;
      return () => {};
    });
    mocks.listTerminals.mockResolvedValue([term]);
    render(<App />);
    await waitFor(() => expect(useAppStore.getState().terminals).toHaveLength(1));

    act(() => {
      exitCb?.({ id: 't1', exitCode: 3 });
    });

    expect(useAppStore.getState().terminals[0].Status).toBe('exited');
    expect(useAppStore.getState().terminals[0].ExitCode).toBe(3);
    expect(useAppStore.getState().toasts.some((t) => t.title.includes('退出码 3'))).toBe(true);
  });

  it('终端 data 事件 bump terminalBusy；exit 清除', async () => {
    let dataCb: ((p: { id: string; data: string }) => void) | undefined;
    let exitCb: ((p: { id: string; exitCode: number }) => void) | undefined;
    mocks.onTerminalData.mockImplementation((cb: (p: { id: string; data: string }) => void) => {
      dataCb = cb;
      return () => {};
    });
    mocks.onTerminalExit.mockImplementation((cb: (p: { id: string; exitCode: number }) => void) => {
      exitCb = cb;
      return () => {};
    });
    useAppStore.setState({ terminalBusy: {} });
    render(<App />);

    act(() => {
      dataCb?.({ id: 't1', data: 'QQ==' });
    });
    expect(useAppStore.getState().terminalBusy.t1).toBe(true);

    act(() => {
      exitCb?.({ id: 't1', exitCode: 0 });
    });
    expect(useAppStore.getState().terminalBusy.t1).toBeUndefined();
  });

  it('关闭工作区页签会连带关闭该工作区的内嵌终端（进程不留后台）', async () => {
    // 挂载时会用 ListTerminals 重建镜像：这里让它返回同一份，避免把预置状态冲掉
    mocks.listTerminals.mockResolvedValue([term]);
    useAppStore.setState({
      openTabs: [{ id: 'D:\\proj-a', name: 'proj-a' }],
      activeTabId: 'D:\\proj-a',
      terminals: [term],
    });
    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '关闭 proj-a' }));

    expect(useAppStore.getState().openTabs).toHaveLength(0);
    expect(useAppStore.getState().terminals).toHaveLength(0);
    await waitFor(() => expect(mocks.closeTerminal).toHaveBeenCalledWith('t1'));
  });

  it('Ctrl+K 打开快速切换器（大小写 K 均可触发）', () => {
    // 本文件 store 无工作区/页签数据：面板打开后应显示引导空态
    render(<App />);

    fireEvent.keyDown(window, { key: 'k', ctrlKey: true });
    expect(screen.getByLabelText('搜索页签或工作区')).toBeInTheDocument();
    expect(screen.getByText('暂无工作区，请先在首页完成扫描')).toBeInTheDocument();
  });

  it('Ctrl+F 仅在工作区页签内派发 kshell:focus-search（首页/设置页不派发）', () => {
    const spy = vi.fn();
    window.addEventListener('kshell:focus-search', spy);

    render(<App />);
    fireEvent.keyDown(window, { key: 'f', ctrlKey: true }); // 首页
    fireEvent.keyDown(window, { key: 'f', ctrlKey: true }); // 设置页
    expect(spy).not.toHaveBeenCalled();

    // 激活工作区页签后再按：派发一次
    act(() => {
      useAppStore.setState({ activeTabId: 'D:\\proj-a' });
    });
    fireEvent.keyDown(window, { key: 'f', ctrlKey: true });
    expect(spy).toHaveBeenCalledTimes(1);

    window.removeEventListener('kshell:focus-search', spy);
  });

  it('工作区页签中键（button=1）关闭，左键不关闭', () => {
    useAppStore.setState({ openTabs: [{ id: 'D:\\proj-a', name: 'proj-a' }], activeTabId: null });
    render(<App />);

    // RTL 无 auxClick 快捷方法，直接派生 MouseEvent（须冒泡到 React 根监听）
    const auxClick = (el: Element, button: number) =>
      fireEvent(el, new MouseEvent('auxclick', { button, bubbles: true }));

    // 页签容器 div（含关闭钮的父级）
    const tab = screen.getByText('proj-a').closest('div.group')!;
    auxClick(tab, 1);
    expect(useAppStore.getState().openTabs).toHaveLength(0);

    // 左键 auxclick 不触发关闭（act 包裹保证同步重渲染后再查询）
    act(() => {
      useAppStore.setState({ openTabs: [{ id: 'D:\\proj-a', name: 'proj-a' }] });
    });
    const tab2 = screen.getByText('proj-a').closest('div.group')!;
    auxClick(tab2, 0);
    expect(useAppStore.getState().openTabs).toHaveLength(1);
  });

  it('projects:changed：同步最新工作区列表并关闭已消失项目的页签', async () => {
    // App 与 Home 各订阅一份（Home 负责重拉列表，App 负责关页签）：全部触发
    const cbs: Array<(p: { workspaces: Workspace[] }) => void> = [];
    mocks.onProjectsChanged.mockImplementation((fn: (p: { workspaces: Workspace[] }) => void) => {
      cbs.push(fn);
      return () => {};
    });

    const wsB: Workspace = {
      Path: 'D:\\proj-b',
      Name: 'proj-b',
      LastUsed: '',
      SessionCount: 0,
      ToolCounts: {},
      Source: 'sessions',
    };
    mocks.getWorkspaces.mockResolvedValue([wsB]);
    useAppStore.setState({
      openTabs: [
        { id: 'D:\\proj-a', name: 'proj-a' },
        { id: 'D:\\proj-b', name: 'proj-b' },
      ],
      activeTabId: 'D:\\proj-a',
    });
    render(<App />);
    await waitFor(() => expect(cbs.length).toBeGreaterThan(0));

    act(() => {
      for (const fn of cbs) fn({ workspaces: [wsB] });
    });

    // proj-a 已不在列表：页签被关闭；store 同步为最新列表
    expect(useAppStore.getState().openTabs.map((t) => t.id)).toEqual(['D:\\proj-b']);
    expect(useAppStore.getState().workspaces).toEqual([wsB]);
  });

  it('外部文件拖入：挂载注册 OnFileDrop 回调（不带 Wails 遮罩），卸载时 OnFileDropOff 注销', () => {
    const { unmount } = render(<App />);

    expect(runtimeMocks.OnFileDrop).toHaveBeenCalledTimes(1);
    const [cb, useDropTarget] = runtimeMocks.OnFileDrop.mock.calls[0];
    expect(typeof cb).toBe('function');
    expect(useDropTarget).toBe(false);

    unmount();
    expect(runtimeMocks.OnFileDropOff).toHaveBeenCalledTimes(1);
  });

  it('chat:update 事件写入时间线（历史已落位后直接 apply）', async () => {
    let updateCb: ((p: { id: string; update: { Seq: number; Type: string; MessageID?: string; Text?: string } }) => void) | undefined;
    mocks.onChatUpdate.mockImplementation((cb: typeof updateCb) => {
      updateCb = cb;
      return () => {};
    });
    mocks.listChats.mockResolvedValue([chat]);
    render(<App />);

    // chatHistory 默认返回 []：落位后 chatSeq.c1 === 0，说明已 seeded
    await waitFor(() => expect(useAppStore.getState().chatSeq.c1).toBe(0));

    act(() => {
      updateCb?.({ id: 'c1', update: { Seq: 1, Type: 'assistant', MessageID: 'm1', Text: 'hi' } });
    });
    expect(useAppStore.getState().chatItems.c1?.some((it) => it.text === 'hi')).toBe(true);
  });

  it('chat:update 早于历史回放时先入缓冲，补放后历史前缀不丢', async () => {
    let updateCb: ((p: { id: string; update: { Seq: number; Type: string; MessageID?: string; Text?: string } }) => void) | undefined;
    let resolveHist: ((h: unknown[]) => void) | undefined;
    mocks.onChatUpdate.mockImplementation((cb: typeof updateCb) => {
      updateCb = cb;
      return () => {};
    });
    mocks.chatHistory.mockImplementation(
      () => new Promise<unknown[]>((res) => { resolveHist = res; }),
    );
    mocks.listChats.mockResolvedValue([chat]);
    render(<App />);

    await waitFor(() => expect(resolveHist).toBeTypeOf('function'));

    // 历史尚未返回，实时高 Seq 更新先入缓冲、不得落位
    act(() => {
      updateCb?.({ id: 'c1', update: { Seq: 9, Type: 'assistant', MessageID: 'm9', Text: 'live' } });
    });
    expect(useAppStore.getState().chatItems.c1 ?? []).toHaveLength(0);

    // 历史（Seq 1）返回后再补放缓冲的 Seq 9：前缀与实时更新都在，Seq 取最大
    await act(async () => {
      resolveHist?.([{ Seq: 1, Type: 'assistant', MessageID: 'm1', Text: 'old' }]);
    });
    expect((useAppStore.getState().chatItems.c1 ?? []).map((it) => it.text)).toEqual(['old', 'live']);
    expect(useAppStore.getState().chatSeq.c1).toBe(9);
  });

  it('chat:permission 事件写入待回应权限', () => {
    let permCb: ((p: { id: string; request: ChatPermissionRequest }) => void) | undefined;
    mocks.onChatPermission.mockImplementation((cb: typeof permCb) => {
      permCb = cb;
      return () => {};
    });
    render(<App />);

    act(() => {
      permCb?.({ id: 'c1', request: permission });
    });
    expect(useAppStore.getState().chatPermissions.c1?.RequestID).toBe('r1');
  });

  it('chat:exit 事件标记聊天退出并清除权限', async () => {
    let exitCb: ((p: { id: string; exitCode: number; error: string }) => void) | undefined;
    mocks.onChatExit.mockImplementation((cb: typeof exitCb) => {
      exitCb = cb;
      return () => {};
    });
    mocks.listChats.mockResolvedValue([chat]);
    render(<App />);
    await waitFor(() => expect(useAppStore.getState().chats).toHaveLength(1));

    act(() => {
      useAppStore.getState().setChatPermission('c1', permission);
    });
    act(() => {
      exitCb?.({ id: 'c1', exitCode: 3, error: '' });
    });

    const c = useAppStore.getState().chats.find((x) => x.ID === 'c1');
    expect(c?.Status).toBe('exited');
    expect(c?.ExitCode).toBe(3);
    expect(useAppStore.getState().chatPermissions.c1).toBeUndefined();
    expect(useAppStore.getState().toasts.some((t) => t.title.includes('退出码 3'))).toBe(true);
  });

  it('挂载时回放 chatHistory：时间线落位且 chatSeq 取历史最大 Seq', async () => {
    mocks.listChats.mockResolvedValue([chat]);
    mocks.chatHistory.mockResolvedValue([
      { Seq: 1, Type: 'assistant', MessageID: 'm1', Text: 'old' },
    ]);
    render(<App />);

    await waitFor(() => {
      expect(useAppStore.getState().chatItems.c1?.[0]?.text).toBe('old');
    });
    expect(useAppStore.getState().chatSeq.c1).toBe(1);
  });

  it('挂载后新建的聊天（不在初始 listChats）的 chat:update 立即应用，不被永久缓冲', async () => {
    let updateCb: ((p: { id: string; update: { Seq: number; Type: string; MessageID?: string; Text?: string } }) => void) | undefined;
    mocks.onChatUpdate.mockImplementation((cb: typeof updateCb) => {
      updateCb = cb;
      return () => {};
    });
    mocks.listChats.mockResolvedValue([]); // 初始无聊天：'late' 只会通过 openSession/openWorkspace 出现
    render(<App />);
    await act(async () => {}); // 让 listChats 落位

    act(() => {
      updateCb?.({ id: 'late', update: { Seq: 1, Type: 'assistant', MessageID: 'm1', Text: 'late-hi' } });
    });
    expect(useAppStore.getState().chatItems.late?.some((it) => it.text === 'late-hi')).toBe(true);
  });
});
