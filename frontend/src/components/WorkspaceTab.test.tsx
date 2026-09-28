// WorkspaceTab 集成测试：新建会话（含工具选择）开中心区内嵌终端、会话「恢复」开终端页签、
// 文件树点文件联动预览页签、SSH 双面板常挂载、三栏拖动条存在。
// api 层整体打桩；TerminalView 单独打桩（jsdom 里跑不了真 xterm）。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import WorkspaceTabView from '../pages/WorkspaceTab';
import { useAppStore } from '../state/store';
import type { TerminalInfo } from '../lib/api';

const mocks = vi.hoisted(() => ({
  getSessions: vi.fn(),
  resumeSession: vi.fn(),
  focusSession: vi.fn(),
  onScanDone: vi.fn(),
  onWindowClosed: vi.fn(),
  listFiles: vi.fn(),
  previewFile: vi.fn(),
  toggleBasket: vi.fn(),
  newSession: vi.fn(),
  newSessionWithTool: vi.fn(),
  listConnections: vi.fn(),
  openSSH: vi.fn(),
  execRemote: vi.fn(),
  getTools: vi.fn(),
  listTerminals: vi.fn(),
  openSessionTerminal: vi.fn(),
  openWorkspaceTerminal: vi.fn(),
  writeTerminal: vi.fn(),
  resizeTerminal: vi.fn(),
  closeTerminal: vi.fn(),
  scrollbackTerminal: vi.fn(),
}));
vi.mock('../lib/api', () => mocks);

// xterm 在 jsdom 下无法真实运行：只保留「页签与激活态」这一层契约
vi.mock('./TerminalView', () => ({
  default: ({ term, active }: { term: TerminalInfo; active: boolean }) => (
    <div data-testid={`terminal-${term.ID}`} data-active={String(active)}>
      {term.Title}
    </div>
  ),
}));

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
  mocks.onScanDone.mockReturnValue(() => {});
  mocks.onWindowClosed.mockReturnValue(() => {});
  mocks.getSessions.mockResolvedValue([]);
  mocks.listFiles.mockResolvedValue([]);
  mocks.listConnections.mockResolvedValue([]);
  mocks.getTools.mockResolvedValue([]);
  mocks.listTerminals.mockResolvedValue([]);
  mocks.closeTerminal.mockResolvedValue(undefined);
  useAppStore.setState({
    basket: [],
    windowStatus: {},
    scanState: 'done',
    terminals: [],
    toasts: [],
    layout: { left: 288, right: 300 },
  });
});

describe('WorkspaceTab', () => {
  it('「新建会话」开中心区内嵌终端并激活其页签', async () => {
    mocks.openWorkspaceTerminal.mockResolvedValue(term);
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    fireEvent.click(screen.getByRole('button', { name: '新建会话' }));

    await waitFor(() => {
      // 空工具 id = 交给 Go 侧挑该工作区最常用的工具；0,0 = 尺寸交给首次 fit 纠正
      expect(mocks.openWorkspaceTerminal).toHaveBeenCalledWith('D:\\proj-a', '', 0, 0);
    });
    expect(await screen.findByTestId('terminal-t1')).toBeInTheDocument();
    expect(screen.getByTestId('terminal-t1')).toHaveAttribute('data-active', 'true');
  });

  it('新建会话失败时以 error 语气轻量提示（toast）', async () => {
    mocks.openWorkspaceTerminal.mockRejectedValueOnce(new Error('工作区不存在'));
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    fireEvent.click(screen.getByRole('button', { name: '新建会话' }));

    await waitFor(() => {
      expect(
        useAppStore
          .getState()
          .toasts.some((t) => t.tone === 'error' && t.title.includes('新建会话失败')),
      ).toBe(true);
    });
  });

  it('工具选择生效：选中工具后新建会话带上该工具 id', async () => {
    mocks.getTools.mockResolvedValue([
      { ID: 'claude', Name: 'Claude Code', BinPath: 'claude.cmd', Version: '1.0', Installed: true, Source: 'path' },
      { ID: 'codex', Name: 'Codex', BinPath: 'codex.cmd', Version: '2.0', Installed: true, Source: 'path' },
    ]);
    mocks.openWorkspaceTerminal.mockResolvedValue(term);
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    fireEvent.click(await screen.findByRole('button', { name: /自动/ }));
    fireEvent.click(await screen.findByRole('option', { name: /Codex/ }));
    fireEvent.click(screen.getByRole('button', { name: '新建会话' }));

    await waitFor(() => {
      expect(mocks.openWorkspaceTerminal).toHaveBeenCalledWith('D:\\proj-a', 'codex', 0, 0);
    });
  });

  it('会话列表「恢复」开中心区内嵌终端（onOpenTerminal 已接线）', async () => {
    mocks.getSessions.mockResolvedValue([
      {
        ID: 's1',
        ToolID: 'claude',
        Workspace: 'D:\\proj-a',
        Title: '修登录页',
        CreatedAt: '2026-09-01T10:00:00Z',
        UpdatedAt: '2026-09-02T10:00:00Z',
        Messages: 12,
        Path: 'D:\\proj-a\\s1.jsonl',
      },
    ]);
    mocks.openSessionTerminal.mockResolvedValue(term);
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    fireEvent.click(await screen.findByRole('button', { name: '恢复' }));

    await waitFor(() => expect(mocks.openSessionTerminal).toHaveBeenCalledWith('s1', 0, 0));
    expect(await screen.findByTestId('terminal-t1')).toBeInTheDocument();
  });

  it('关闭终端页签：结束进程、移出镜像并退回预览页签', async () => {
    // 挂载时会用 ListTerminals 重建镜像：让它返回同一份，避免预置状态被冲掉
    mocks.listTerminals.mockResolvedValue([term]);
    useAppStore.setState({ terminals: [term] });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    // 终端页签默认不激活，先点进去
    fireEvent.click(await screen.findByRole('tab', { name: /修登录页/ }));
    expect(screen.getByTestId('terminal-t1')).toHaveAttribute('data-active', 'true');

    fireEvent.click(screen.getByRole('button', { name: '关闭终端 修登录页' }));

    await waitFor(() => expect(mocks.closeTerminal).toHaveBeenCalledWith('t1'));
    expect(useAppStore.getState().terminals).toHaveLength(0);
    expect(screen.queryByTestId('terminal-t1')).not.toBeInTheDocument();
  });

  it('工作区页签不可见时终端 active=false（避免隐藏态 fit 出 0 尺寸）', async () => {
    mocks.listTerminals.mockResolvedValue([term]);
    useAppStore.setState({ terminals: [term] });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible={false} />);

    fireEvent.click(await screen.findByRole('tab', { name: /修登录页/ }));
    expect(screen.getByTestId('terminal-t1')).toHaveAttribute('data-active', 'false');
  });

  it('文件树点文件后预览页签激活并加载内容', async () => {
    mocks.listFiles.mockResolvedValue([
      {
        Name: 'main.ts',
        Path: 'D:\\proj-a\\main.ts',
        IsDir: false,
        Expanded: false,
        Loaded: false,
      },
    ]);
    mocks.previewFile.mockResolvedValue({
      Lines: ['   1 │ const a = 1;'],
      Truncated: false,
      Binary: false,
      Info: '',
    });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    fireEvent.click(await screen.findByText('main.ts'));

    expect(await screen.findByText(/const a = 1;/)).toBeInTheDocument();
    expect(mocks.previewFile).toHaveBeenCalledWith('D:\\proj-a', 'D:\\proj-a\\main.ts');
  });

  it('SSH 页签渲染连接列表，文件页签渲染文件树；切页签双面板常挂载不卸载', async () => {
    mocks.listConnections.mockResolvedValue([
      {
        ID: 'c1',
        Name: '生产机',
        Host: '10.0.0.1',
        User: 'root',
        Port: 22,
        IdentityFile: '',
        Workspace: '',
        Source: 'sshconfig',
        SourceFile: '',
        Verified: true,
      },
    ]);
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    expect(await screen.findByText('没有可显示的文件')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'SSH' }));
    expect(await screen.findByText('生产机')).toBeInTheDocument();
    expect(mocks.listConnections).toHaveBeenCalledWith('D:\\proj-a');

    // 双面板常挂载：切到 SSH 后文件树仍在 DOM（hidden 切换而非卸载重载）
    expect(screen.getByText('没有可显示的文件')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '文件' }));
    expect(await screen.findByText('没有可显示的文件')).toBeInTheDocument();
    expect(screen.getByText('生产机')).toBeInTheDocument();
  });

  it('三栏各有一个可拖动分隔条（左右两栏宽度可调）', () => {
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    expect(screen.getByRole('separator', { name: '调整会话列表宽度' })).toBeInTheDocument();
    expect(screen.getByRole('separator', { name: '调整文件面板宽度' })).toBeInTheDocument();
  });
});
