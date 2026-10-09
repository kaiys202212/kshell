// WorkspaceTab 集成测试：新建会话（「新建会话」下拉菜单选 agent）开中心区内嵌终端、会话「恢复」开终端页签、
// 文件树点文件联动预览页签、SSH 双面板常挂载、三栏拖动条存在。
// api 层整体打桩；TerminalView 单独打桩（jsdom 里跑不了真 xterm）。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import WorkspaceTabView from '../pages/WorkspaceTab';
import { useAppStore } from '../state/store';
import { tt } from '../test/i18n';
import type { ChatInfo, TerminalInfo, ToolInfo } from '../lib/api';

const mocks = vi.hoisted(() => ({
  getSessions: vi.fn(),
  getSessionPreview: vi.fn(),
  resumeSession: vi.fn(),
  focusSession: vi.fn(),
  onScanDone: vi.fn(),
  onToolsUpdated: vi.fn(),
  onWindowClosed: vi.fn(),
  listFiles: vi.fn(),
  previewFile: vi.fn(),
  readFileForEdit: vi.fn(),
  saveFile: vi.fn(),
  newSession: vi.fn(),
  newSessionWithTool: vi.fn(),
  scanSessions: vi.fn(),
  listConnections: vi.fn(),
  openSSH: vi.fn(),
  openSSHTerminal: vi.fn(),
  openShellTerminal: vi.fn(),
  upsertConnection: vi.fn(),
  deleteConnection: vi.fn(),
  execRemote: vi.fn(),
  getTools: vi.fn(),
  listTerminals: vi.fn(),
  openSession: vi.fn(),
  openWorkspace: vi.fn(),
  openWorkspaceACP: vi.fn(),
  restoreSession: vi.fn().mockResolvedValue(undefined),
  archiveSession: vi.fn().mockResolvedValue(undefined),
  writeTerminal: vi.fn(),
  resizeTerminal: vi.fn(),
  closeTerminal: vi.fn(),
  scrollbackTerminal: vi.fn(),
  sendChatPrompt: vi.fn(),
  cancelChat: vi.fn(),
  respondChatPermission: vi.fn(),
  cancelChatPermission: vi.fn(),
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
vi.mock('../lib/api', () => mocks);

// xterm 在 jsdom 下无法真实运行：只保留「页签与激活态」这一层契约
vi.mock('./TerminalView', () => ({
  default: ({ term, active }: { term: TerminalInfo; active: boolean }) => (
    <div data-testid={`terminal-${term.ID}`} data-active={String(active)}>
      {term.Title}
    </div>
  ),
}));
vi.mock('./PdfPreview', () => ({
  default: () => null,
}));

// scanDoneCb 记录被订阅的 scan:done 回调，供用例手动触发「扫描完成」。
let scanDoneCb: ((payload?: unknown) => void) | null = null;

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

// 工具列表（父级已按「已安装且有可执行文件」过滤）
const toolClaude: ToolInfo = {
  ID: 'claude',
  Name: 'Claude Code',
  BinPath: 'C:\\bin\\claude.cmd',
  Version: '1.0.0',
  Installed: true,
  Source: 'path',
};

const toolCodex: ToolInfo = {
  ID: 'codex',
  Name: 'Codex',
  BinPath: 'C:\\bin\\codex.exe',
  Version: '2.0.0',
  Installed: true,
  Source: 'path',
};

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  scanDoneCb = null;
  mocks.onScanDone.mockImplementation((cb: (payload?: unknown) => void) => {
    scanDoneCb = cb;
    return () => {};
  });
  mocks.onToolsUpdated.mockImplementation(() => () => {});
  mocks.onWindowClosed.mockReturnValue(() => {});
  mocks.getSessions.mockResolvedValue([]);
  mocks.getSessionPreview.mockResolvedValue({ Markdown: '## 用户\n\nhi', Truncated: false });
  mocks.listFiles.mockResolvedValue([]);
  mocks.readFileForEdit.mockResolvedValue({ Text: '', EOL: 'lf', Size: 0 });
  mocks.listConnections.mockResolvedValue([]);
  mocks.getTools.mockResolvedValue([]);
  mocks.listTerminals.mockResolvedValue([]);
  mocks.closeTerminal.mockResolvedValue(undefined);
  mocks.scanSessions.mockResolvedValue(undefined);
  // 统一入口：默认回退为终端（与 Go 侧降级行为一致），需要聊天的用例自行覆盖
  mocks.openSession.mockResolvedValue({ Kind: 'terminal', Terminal: term });
  mocks.openWorkspace.mockResolvedValue({ Kind: 'terminal', Terminal: term });
  mocks.closeChat.mockResolvedValue(undefined);
  mocks.listChats.mockResolvedValue([]);
  mocks.chatHistory.mockResolvedValue([]);
  mocks.onChatUpdate.mockReturnValue(() => {});
  mocks.onChatPermission.mockReturnValue(() => {});
  mocks.onChatExit.mockReturnValue(() => {});
  mocks.sendChatPrompt.mockResolvedValue(undefined);
  mocks.cancelChat.mockResolvedValue(undefined);
  mocks.respondChatPermission.mockResolvedValue(undefined);
  mocks.cancelChatPermission.mockResolvedValue(undefined);
  useAppStore.setState({
    windowStatus: {},
    scanState: 'done',
    terminals: [],
    chats: [],
    chatItems: {},
    chatSeq: {},
    chatPermissions: {},
    toasts: [],
    newSessionTool: '',
    layout: { left: 288, right: 300 },
  });
});

describe('WorkspaceTab', () => {
  it('收到 scan:done 后重取终端/聊天镜像（Go 侧扫描回填了新会话标题，页签要跟着更新）', async () => {
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);
    await act(async () => {}); // 让挂载期请求落位（聊天镜像挂载时由 App 层负责，这里只认事件后的重取）
    expect(mocks.listTerminals).toHaveBeenCalledTimes(1);
    expect(mocks.listChats).not.toHaveBeenCalled();

    await act(async () => {
      scanDoneCb?.({});
    });

    expect(mocks.listTerminals).toHaveBeenCalledTimes(2);
    expect(mocks.listChats).toHaveBeenCalledTimes(1);
  });

  it('「新建会话」下拉菜单选 agent 后开中心区内嵌终端并激活其页签', async () => {
    mocks.getTools.mockResolvedValue([toolClaude]);
    mocks.openWorkspace.mockResolvedValue({ Kind: 'terminal', Terminal: term });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    // 等工具列表就绪（扫描是异步的）再点新建会话
    await screen.findByRole('button', { name: tt('ui.new_session.title') });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.new_session.title') }));
    fireEvent.click(await screen.findByRole('menuitemradio', { name: /Claude Code/ }));

    await waitFor(() => {
      // 选中的 agent 原样传给 Go 侧
      expect(mocks.openWorkspace).toHaveBeenCalledWith('D:\\proj-a', 'claude');
    });
    expect(await screen.findByTestId('terminal-t1')).toBeInTheDocument();
    expect(screen.getByTestId('terminal-t1')).toHaveAttribute('data-active', 'true');
  });

  it('「自动」选项也能新建（空工具 id 交给 Go 侧挑该工作区最常用的工具）', async () => {
    mocks.getTools.mockResolvedValue([toolClaude]);
    mocks.openWorkspace.mockResolvedValue({ Kind: 'terminal', Terminal: term });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    await screen.findByRole('button', { name: tt('ui.new_session.title') });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.new_session.title') }));
    fireEvent.click(await screen.findByRole('menuitemradio', { name: tt('ui.new_session.auto') }));

    await waitFor(() => {
      expect(mocks.openWorkspace).toHaveBeenCalledWith('D:\\proj-a', '');
    });
  });

  it('新建会话后按退避多次触发后台重扫（工具落盘较晚；单次 3s 常查不到）', async () => {
    vi.useFakeTimers();
    try {
      mocks.getTools.mockResolvedValue([toolClaude]);
      mocks.openWorkspace.mockResolvedValue({ Kind: 'terminal', Terminal: term });
      render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

      await act(async () => {}); // 让挂载期的工具/终端列表请求落位
      fireEvent.click(screen.getByRole('button', { name: tt('ui.new_session.title') }));
      fireEvent.click(screen.getByRole('menuitemradio', { name: /Claude Code/ }));
      await act(async () => {}); // 等 openWorkspace 落位并装上定时器

      expect(mocks.scanSessions).not.toHaveBeenCalled(); // 不是立刻重扫
      await act(async () => {
        vi.advanceTimersByTime(3000);
      });
      expect(mocks.scanSessions).toHaveBeenCalledTimes(1);
      await act(async () => {
        vi.advanceTimersByTime(5000); // 3s + 5s = 8s 第二档
      });
      expect(mocks.scanSessions).toHaveBeenCalledTimes(2);
      await act(async () => {
        vi.advanceTimersByTime(7000); // 再 +7s = 15s 第三档
      });
      expect(mocks.scanSessions).toHaveBeenCalledTimes(3);
    } finally {
      vi.useRealTimers();
    }
  });

  it('未检测到 agent 时「新建会话」禁用并显示安装引导，不启动终端', async () => {
    mocks.getTools.mockResolvedValue([]);
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    const trigger = await screen.findByRole('button', { name: tt('ui.new_session.no_tools') });
    expect(trigger).toBeDisabled();
    expect(screen.getByText(tt('ui.workspace.no_agent_hint'))).toBeInTheDocument();

    fireEvent.click(trigger);
    expect(mocks.openWorkspace).not.toHaveBeenCalled();
  });

  it('新建会话失败时以 error 语气轻量提示（toast）', async () => {
    mocks.getTools.mockResolvedValue([toolClaude]);
    mocks.openWorkspace.mockRejectedValueOnce(new Error('工作区不存在'));
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    await screen.findByRole('button', { name: tt('ui.new_session.title') });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.new_session.title') }));
    fireEvent.click(await screen.findByRole('menuitemradio', { name: /Claude Code/ }));

    await waitFor(() => {
      expect(
        useAppStore
          .getState()
          .toasts.some(
            (t) => t.tone === 'error' && t.title.includes(tt('ui.workspace.new_session_failed').split('{{')[0]),
          ),
      ).toBe(true);
    });
  });

  it('新建会话回退终端时，回退原因（wire key）经 translateBackend 翻译后展示', async () => {
    mocks.getTools.mockResolvedValue([toolClaude]);
    mocks.openWorkspace.mockResolvedValue({ Kind: 'terminal', Terminal: term, Fallback: 'err.launcher.no_exec' });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    await screen.findByRole('button', { name: tt('ui.new_session.title') });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.new_session.title') }));
    fireEvent.click(await screen.findByRole('menuitemradio', { name: /Claude Code/ }));

    const expected = tt('ui.workspace.fallback').replace('{{reason}}', tt('err.launcher.no_exec'));
    await waitFor(() => {
      expect(useAppStore.getState().toasts.some((t) => t.tone === 'info' && t.title === expected)).toBe(true);
    });
  });

  it('恢复会话回退终端时，回退原因（wire key）经 translateBackend 翻译后展示', async () => {
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
    mocks.openSession.mockResolvedValue({ Kind: 'terminal', Terminal: term, Fallback: 'err.launcher.no_exec' });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    fireEvent.click(await screen.findByRole('button', { name: tt('ui.session_list.activate') }));

    const expected = tt('ui.workspace.fallback').replace('{{reason}}', tt('err.launcher.no_exec'));
    await waitFor(() => {
      expect(useAppStore.getState().toasts.some((t) => t.tone === 'info' && t.title === expected)).toBe(true);
    });
  });

  it('菜单里挑哪个 agent，就按哪个 agent 新建', async () => {
    mocks.getTools.mockResolvedValue([toolClaude, toolCodex]);
    mocks.openWorkspace.mockResolvedValue({ Kind: 'terminal', Terminal: term });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    await screen.findByRole('button', { name: tt('ui.new_session.title') });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.new_session.title') }));
    fireEvent.click(await screen.findByRole('menuitemradio', { name: /Codex/ }));

    await waitFor(() => {
      expect(mocks.openWorkspace).toHaveBeenCalledWith('D:\\proj-a', 'codex');
    });
  });

  it('扫描完成后重取工具列表（首扫未完成时挂载会拿到空列表，否则按钮一直是「无可用工具」）', async () => {
    mocks.getTools.mockResolvedValue([]); // 挂载时后台扫描还没跑完
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    await waitFor(() => expect(mocks.getTools).toHaveBeenCalledTimes(1));
    expect(await screen.findByRole('button', { name: tt('ui.new_session.no_tools') })).toBeDisabled();

    // 扫描完成事件到达时工具才被探测出来
    mocks.getTools.mockResolvedValue([
      { ID: 'claude', Name: 'Claude Code', BinPath: 'claude.cmd', Version: '1.0', Installed: true, Source: 'path' },
    ]);
    expect(scanDoneCb).toBeTypeOf('function');
    scanDoneCb!();

    await waitFor(() => expect(mocks.getTools).toHaveBeenCalledTimes(2));
    expect(await screen.findByRole('button', { name: tt('ui.new_session.title') })).toBeEnabled();
  });

  it('会话列表激活图标走统一入口（openSession）开中心区内嵌终端', async () => {
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
    mocks.openSession.mockResolvedValue({ Kind: 'terminal', Terminal: term });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    fireEvent.click(await screen.findByRole('button', { name: tt('ui.session_list.activate') }));

    await waitFor(() => expect(mocks.openSession).toHaveBeenCalledWith('s1'));
    expect(await screen.findByTestId('terminal-t1')).toBeInTheDocument();
  });

  it('点未激活会话行打开会话预览子页签而不调用 openSession', async () => {
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
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);
    const title = await screen.findByTestId('session-title');
    fireEvent.click(title.closest('li')!);
    expect(await screen.findByRole('tab', { name: tt('ui.workspace.session_preview') })).toBeInTheDocument();
    expect(mocks.openSession).not.toHaveBeenCalled();
  });

  it('openWorkspace 返回 chat 时中心区出现聊天页签（不再走终端入口）', async () => {
    mocks.getTools.mockResolvedValue([toolClaude]);
    const chat: ChatInfo = {
      ID: 'c1',
      Kind: 'new',
      SessionID: 's1',
      Workspace: 'D:\\proj-a',
      Title: '新会话 · Claude Code',
      ToolID: 'claude',
      Status: 'ready',
      ExitCode: 0,
      Error: '',
    };
    mocks.openWorkspace.mockResolvedValue({ Kind: 'chat', Chat: chat });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    await screen.findByRole('button', { name: tt('ui.new_session.title') });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.new_session.title') }));
    fireEvent.click(await screen.findByRole('menuitemradio', { name: /Claude Code/ }));

    await waitFor(() => {
      expect(mocks.openWorkspace).toHaveBeenCalledWith('D:\\proj-a', 'claude');
    });
    // 标题出现在中心区页签条里，且走的是聊天入口
    expect(await screen.findByRole('tab', { name: /新会话 · Claude Code/ })).toBeInTheDocument();
  });

  it('关闭终端页签：结束进程、移出镜像并退回预览页签', async () => {
    // 挂载时会用 ListTerminals 重建镜像：让它返回同一份，避免预置状态被冲掉
    mocks.listTerminals.mockResolvedValue([term]);
    useAppStore.setState({ terminals: [term] });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    // 终端页签默认不激活，先点进去
    fireEvent.click(await screen.findByRole('tab', { name: /修登录页/ }));
    expect(screen.getByTestId('terminal-t1')).toHaveAttribute('data-active', 'true');

    fireEvent.click(
      screen.getByRole('button', {
        name: tt('ui.workspace.close_terminal').replace('{{label}}', '修登录页'),
      }),
    );

    await waitFor(() => expect(mocks.closeTerminal).toHaveBeenCalledWith('t1'));
    expect(useAppStore.getState().terminals).toHaveLength(0);
    expect(screen.queryByTestId('terminal-t1')).not.toBeInTheDocument();
  });

  it('点终端页签右侧的工具徽标也能切换（此前只有标题按钮可点）', async () => {
    const t1: TerminalInfo = { ...term, ID: 't1', SessionID: 's1', Title: 'kshell · opencode', ToolID: 'opencode' };
    const t2: TerminalInfo = { ...term, ID: 't2', SessionID: 's2', Title: 'opencode', ToolID: 'opencode' };
    mocks.listTerminals.mockResolvedValue([t1, t2]);
    useAppStore.setState({ terminals: [t1, t2] });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    // 先点第一个页签标题切过去
    fireEvent.click(await screen.findByRole('tab', { name: /kshell/ }));
    expect(screen.getByTestId('terminal-t1')).toHaveAttribute('data-active', 'true');

    // 点第二个页签的工具图标（标题右侧）应切到 t2
    const badges = await screen.findAllByTestId('tool-icon');
    fireEvent.click(badges[1]);
    expect(screen.getByTestId('terminal-t2')).toHaveAttribute('data-active', 'true');
  });

  it('新建会话页签标题已含工具名时，徽标只留图标不重复显示工具名', async () => {
    const tNew: TerminalInfo = { ...term, ID: 't1', Kind: 'new', Title: 'kshell · opencode', ToolID: 'opencode' };
    mocks.listTerminals.mockResolvedValue([tNew]);
    useAppStore.setState({ terminals: [tNew] });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    expect(await screen.findByRole('tab', { name: /kshell/ })).toBeInTheDocument();
    expect(screen.queryByText('OpenCode')).not.toBeInTheDocument();
  });

  it('恢复的会话页签徽标只留图标，不显示工具名文字', async () => {
    const tSess: TerminalInfo = { ...term, ID: 't1', Kind: 'session', Title: '修复登录页', ToolID: 'opencode' };
    mocks.listTerminals.mockResolvedValue([tSess]);
    useAppStore.setState({ terminals: [tSess] });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    expect(await screen.findByRole('tab', { name: /修复登录页/ })).toBeInTheDocument();
    expect(screen.queryByText('OpenCode')).not.toBeInTheDocument();
    expect(screen.getByTestId('tool-icon')).toHaveAttribute('data-tool', 'opencode');
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
    mocks.readFileForEdit.mockResolvedValue({
      Text: 'const a = 1;',
      EOL: 'lf',
      Size: 12,
    });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    fireEvent.click(await screen.findByText('main.ts'));

    expect(await screen.findByRole('tab', { name: tt('ui.workspace.files_tab') })).toHaveAttribute('aria-selected', 'true');
    expect(await screen.findByTestId('code-editor')).toBeInTheDocument();
    expect(mocks.readFileForEdit).toHaveBeenCalledWith('D:\\proj-a', 'D:\\proj-a\\main.ts');
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
        Password: '',
        Workspace: '',
        Source: 'sshconfig',
        SourceFile: '',
        Verified: true,
      },
    ]);
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    expect(await screen.findByText('proj-a')).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: tt('ui.workspace.active_terminals_tab') }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'SSH' }));
    expect(await screen.findByText('生产机')).toBeInTheDocument();
    expect(mocks.listConnections).toHaveBeenCalledWith('D:\\proj-a');

    // 双面板常挂载：切到 SSH 后文件树仍在 DOM（hidden 切换而非卸载重载）
    expect(screen.getByText('proj-a')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: tt('ui.workspace.files_tab') }));
    expect(await screen.findByText('proj-a')).toBeInTheDocument();
    expect(screen.getByText('生产机')).toBeInTheDocument();
  });

  it('三栏各有一个可拖动分隔条（左右两栏宽度可调）', () => {
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    expect(screen.getByRole('separator', { name: tt('ui.workspace.resize_sessions') })).toBeInTheDocument();
    expect(screen.getByRole('separator', { name: tt('ui.workspace.resize_files') })).toBeInTheDocument();
  });
});
