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
import type { TerminalInfo, Workspace } from './lib/api';

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
}));
vi.mock('./lib/api', () => mocks);

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
  useAppStore.setState({
    openTabs: [],
    activeTabId: null,
    toasts: [],
    windowStatus: {},
    scanState: 'idle',
    terminals: [],
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
    expect(await screen.findByText('工具检测')).toBeInTheDocument();
    expect(pane('settings').className).not.toContain('hidden');
    expect(pane('home').className).toContain('hidden');

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
});
