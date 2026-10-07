// 工作区页签测试：中心区页签标题的文件拖入（DRAG_MIME）——
// 终端页签：先切页签再把带引号路径写入终端；已退出终端只切页签不写入；
// 聊天页签：先切页签再经 chatInputRegistry 投递到输入框草稿。
// api 层整体打桩（vi.mock），与 App.test.tsx 同一套模式；重子组件替换为轻桩。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import WorkspaceTabView from './WorkspaceTab';
import { encodeTerminalInput } from '../lib/base64';
import { registerChatInput, unregisterChatInput } from '../lib/chatInputRegistry';
import { DRAG_MIME } from '../lib/dragPath';
import { OPEN_FILE_EVENT } from '../lib/openHref';
import { useAppStore } from '../state/store';
import { tt } from '../test/i18n';
import type { ChatInfo, TerminalInfo } from '../lib/api';

const mocks = vi.hoisted(() => ({
  closeChat: vi.fn(),
  closeTerminal: vi.fn(),
  getTools: vi.fn(),
  listTerminals: vi.fn(),
  listChats: vi.fn(),
  restoreSession: vi.fn(),
  archiveSession: vi.fn(),
  onScanDone: vi.fn(),
  onToolsUpdated: vi.fn(),
  openSession: vi.fn(),
  openShellTerminal: vi.fn(),
  openSSHTerminal: vi.fn(),
  openWorkspace: vi.fn(),
  openWorkspaceACP: vi.fn(),
  scanSessions: vi.fn(),
  writeTerminal: vi.fn(),
}));
vi.mock('../lib/api', () => mocks);

// 重子组件替换为轻桩：本文件只验证页签条行为，xterm/文件树等由各自测试覆盖
vi.mock('../components/TerminalView', () => ({
  default: ({ term, active }: { term: TerminalInfo; active: boolean }) => (
    <div data-testid={`terminal-${term.ID}`} data-active={String(active)} />
  ),
}));
vi.mock('../components/ChatView', () => ({
  default: ({ chat, active }: { chat: ChatInfo; active: boolean }) => (
    <div data-testid={`chat-${chat.ID}`} data-active={String(active)} />
  ),
}));
vi.mock('../components/FileTree', () => ({ default: () => <div data-testid="file-tree" /> }));
vi.mock('../components/FileTabsPane', () => ({
  default: ({ state }: { state: { activePath: string | null } }) => (
    <div data-testid="file-tabs-pane" data-active={state.activePath ?? ''} />
  ),
}));
vi.mock('../components/PreviewToolPane', () => ({
  default: () => <div data-testid="preview-tool-pane" />,
}));
vi.mock('../components/SshPanel', () => ({ default: () => <div data-testid="ssh-panel" /> }));
vi.mock('../components/GitPanel', () => ({ default: () => <div data-testid="git-panel" /> }));
vi.mock('../components/SessionList', () => ({
  default: (p: { showArchived?: boolean }) => (
    <div data-testid="session-list" data-show-archived={String(!!p.showArchived)} />
  ),
}));
vi.mock('../components/NewSessionMenu', () => ({ default: () => <div /> }));
vi.mock('../components/ResizeHandle', () => ({ default: () => <div /> }));

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

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  mocks.getTools.mockResolvedValue([]);
  mocks.listTerminals.mockResolvedValue([]);
  mocks.listChats.mockResolvedValue([]);
  mocks.restoreSession.mockResolvedValue(undefined);
  mocks.archiveSession.mockResolvedValue(undefined);
  mocks.onScanDone.mockImplementation(() => () => {});
  mocks.onToolsUpdated.mockImplementation(() => () => {});
  mocks.scanSessions.mockResolvedValue(undefined);
  mocks.openSession.mockResolvedValue({});
  mocks.openWorkspace.mockResolvedValue({});
  mocks.closeTerminal.mockResolvedValue(undefined);
  mocks.closeChat.mockResolvedValue(undefined);
  mocks.writeTerminal.mockResolvedValue(undefined);
  useAppStore.setState({
    terminals: [term],
    chats: [chat],
    chatPermissions: {},
    terminalBusy: {},
    layout: { left: 288, right: 300 },
  });
});

// 模拟往页签标题 div 拖入文件（页签 div 是标题按钮的父级 div.group）
function dropFile(el: Element, path: string) {
  fireEvent.drop(el, {
    dataTransfer: {
      types: [DRAG_MIME],
      getData: (mime: string) => (mime === DRAG_MIME ? path : ''),
    },
  });
}

describe('WorkspaceTabView 页签标题', () => {
  it('新建会话按钮后的归档勾选切换列表', () => {
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);
    const box = screen.getByRole('checkbox', { name: '显示归档会话' });
    expect(screen.getByTestId('session-list')).toHaveAttribute('data-show-archived', 'false');
    fireEvent.click(box);
    expect(screen.getByTestId('session-list')).toHaveAttribute('data-show-archived', 'true');
  });

  it('中心区页签标题剥掉 Cursor 的 <timestamp>Sunday… 包装，只留正文', () => {
    useAppStore.setState({
      terminals: [
        {
          ...term,
          Title:
            '<timestamp>Sunday, Oct 4, 2026, 11:44 AM (UTC+8)</timestamp>\n<user_query>\n几个 bug 需要修复下\n</user_query>',
        },
      ],
      chats: [],
    });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);
    expect(screen.getByRole('tab', { name: '几个 bug 需要修复下' })).toBeInTheDocument();
    expect(screen.queryByText(/Sunday/i)).not.toBeInTheDocument();
  });
});

describe('WorkspaceTabView 页签拖入', () => {
  it('拖文件到终端页签标题：先切页签，再把带引号路径写入终端', () => {
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);
    expect(screen.getByTestId('terminal-t1').getAttribute('data-active')).toBe('false');

    const tab = screen.getByText('修登录页').closest('div.group')!;
    dropFile(tab, 'C:\\f.txt');

    // 先切页签：终端面板变为激活
    expect(screen.getByTestId('terminal-t1').getAttribute('data-active')).toBe('true');
    expect(mocks.writeTerminal).toHaveBeenCalledTimes(1);
    expect(mocks.writeTerminal).toHaveBeenCalledWith('t1', encodeTerminalInput('C:\\f.txt'));
  });

  it('拖文件到已退出的终端页签：只切页签，不写入', () => {
    useAppStore.setState({ terminals: [{ ...term, Status: 'exited' }] });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    const tab = screen.getByText('修登录页').closest('div.group')!;
    dropFile(tab, 'C:\\f.txt');

    expect(screen.getByTestId('terminal-t1').getAttribute('data-active')).toBe('true');
    expect(mocks.writeTerminal).not.toHaveBeenCalled();
  });

  it('拖文件到聊天页签标题：先切页签，再投递到输入框草稿', () => {
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);
    expect(screen.getByTestId('chat-c1').getAttribute('data-active')).toBe('false');

    const appended: string[] = [];
    registerChatInput('c1', { append: (text) => appended.push(text) });
    try {
      const tab = screen.getByText('新会话').closest('div.group')!;
      dropFile(tab, 'C:\\my file.txt');

      expect(screen.getByTestId('chat-c1').getAttribute('data-active')).toBe('true');
      expect(appended).toEqual(['"C:\\my file.txt"']); // 含空格路径被加引号
    } finally {
      unregisterChatInput('c1');
    }
  });
});

describe('WorkspaceTabView agent 活动图标', () => {
  it('chat Status=running 时页签出现「执行中」', () => {
    useAppStore.setState({ chats: [{ ...chat, Status: 'running' }], terminals: [] });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);
    expect(screen.getByLabelText(tt('ui.agent_activity.running'))).toBeInTheDocument();
  });

  it('chatPermissions 存在时页签出现「待用户确认」', () => {
    useAppStore.setState({
      chats: [{ ...chat, Status: 'running' }],
      terminals: [],
      chatPermissions: {
        c1: {
          RequestID: 'r1',
          SessionID: 's1',
          ToolCall: { ToolCallID: 'tc1' },
          Options: [{ OptionID: 'allow', Name: '允许', Kind: 'allow_once' }],
        },
      },
    });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);
    expect(screen.getByLabelText(tt('ui.agent_activity.awaiting'))).toBeInTheDocument();
    expect(screen.queryByLabelText(tt('ui.agent_activity.running'))).not.toBeInTheDocument();
  });

  it('chat Status=ready 时页签出现「等待用户」', () => {
    useAppStore.setState({
      chats: [{ ...chat, Status: 'ready' }],
      terminals: [],
    });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);
    expect(screen.getByLabelText(tt('ui.agent_activity.waiting'))).toBeInTheDocument();
  });

  it('从聊天页签切到文件后不改 Status', () => {
    useAppStore.setState({
      chats: [{ ...chat, Status: 'ready' }],
      terminals: [],
    });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    fireEvent.click(screen.getByRole('tab', { name: '新会话' }));
    fireEvent.click(screen.getByRole('tab', { name: '文件' }));
    expect(useAppStore.getState().chats.find((c) => c.ID === 'c1')?.Status).toBe('ready');
  });

  it('会话预览钉在最左，文件与终端钉在最右；shell/ssh 不进左侧 agent 页签', () => {
    useAppStore.setState({
      terminals: [
        term,
        {
          ...term,
          ID: 'sh1',
          Kind: 'shell',
          Title: '终端',
          ToolID: '',
          SessionID: '',
        },
      ],
      chats: [],
    });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);
    const tablist = screen.getByRole('tablist', { name: '中心区页签' });
    const tabs = tablist.querySelectorAll('[role="tab"]');
    const labels = [...tabs].map((t) => t.getAttribute('aria-label') || t.textContent);
    expect(labels[0]).toMatch(/会话预览/);
    expect(labels.at(-2)).toMatch(/文件/);
    expect(labels.at(-1)).toMatch(/终端/);
    expect(screen.getByTestId('preview-tool-pane')).toBeInTheDocument();
    expect(screen.getByTestId('file-tabs-pane')).toBeInTheDocument();
  });

  it('连续切换聊天页签时 Status 保持不变', () => {
    const chat2: ChatInfo = { ...chat, ID: 'c2', Title: '会话二' };
    useAppStore.setState({
      chats: [{ ...chat, Status: 'ready' }, { ...chat2, Status: 'ready' }],
      terminals: [],
    });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    fireEvent.click(screen.getByRole('tab', { name: '新会话' }));
    fireEvent.click(screen.getByRole('tab', { name: '会话二' }));

    const statuses = useAppStore.getState().chats.map((c) => c.Status);
    expect(statuses).toEqual(['ready', 'ready']);
    expect(screen.getAllByLabelText(tt('ui.agent_activity.waiting'))).toHaveLength(2);
  });

  it('右栏有 Git 页签', () => {
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);
    fireEvent.click(screen.getByRole('button', { name: 'Git' }));
    expect(screen.getByTestId('git-panel')).toBeInTheDocument();
  });

  it('kshell:open-file 打开本工作区文件预览', () => {
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);
    fireEvent(
      window,
      new CustomEvent(OPEN_FILE_EVENT, {
        detail: { workspace: 'D:\\proj-a', path: 'D:\\proj-a\\README.md' },
      }),
    );
    expect(screen.getByTestId('file-tabs-pane')).toHaveAttribute(
      'data-active',
      'D:\\proj-a\\README.md',
    );
  });

  it('其他工作区的 kshell:open-file 忽略', () => {
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);
    fireEvent(
      window,
      new CustomEvent(OPEN_FILE_EVENT, {
        detail: { workspace: 'D:\\other', path: 'D:\\other\\a.md' },
      }),
    );
    expect(screen.getByTestId('file-tabs-pane')).toHaveAttribute('data-active', '');
  });
});
