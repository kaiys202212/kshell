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
import { useAppStore } from '../state/store';
import type { ChatInfo, TerminalInfo } from '../lib/api';

const mocks = vi.hoisted(() => ({
  closeChat: vi.fn(),
  closeTerminal: vi.fn(),
  getTools: vi.fn(),
  listTerminals: vi.fn(),
  onScanDone: vi.fn(),
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
vi.mock('../components/PreviewToolPane', () => ({
  PREVIEW_SUB: 'preview',
  default: () => <div data-testid="preview-tool-pane" />,
}));
vi.mock('../components/SshPanel', () => ({ default: () => <div data-testid="ssh-panel" /> }));
vi.mock('../components/SessionList', () => ({ default: () => <div /> }));
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
  mocks.onScanDone.mockImplementation(() => () => {});
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
    activityCompleted: {},
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
    expect(screen.getByLabelText('执行中')).toBeInTheDocument();
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
    expect(screen.getByLabelText('待用户确认')).toBeInTheDocument();
    expect(screen.queryByLabelText('执行中')).not.toBeInTheDocument();
  });

  it('activityCompleted 且 ready 时页签出现「运行完成」', () => {
    useAppStore.setState({
      chats: [{ ...chat, Status: 'ready' }],
      terminals: [],
      activityCompleted: { c1: true },
    });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);
    expect(screen.getByLabelText('运行完成')).toBeInTheDocument();
  });

  it('从聊天页签切到预览后清除该 id 的 activityCompleted', () => {
    useAppStore.setState({
      chats: [{ ...chat, Status: 'ready' }],
      terminals: [],
      activityCompleted: { c1: true },
    });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    // 先切到聊天（从 preview 切入不清除），再切回预览应清掉 completed
    fireEvent.click(screen.getByRole('tab', { name: '新会话' }));
    expect(useAppStore.getState().activityCompleted.c1).toBe(true);

    fireEvent.click(screen.getByRole('tab', { name: '预览与命令行' }));
    expect(useAppStore.getState().activityCompleted.c1).toBeUndefined();
  });

  it('预览页签钉在中心区最右，shell/ssh 不进左侧 agent 页签', () => {
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
    expect(labels.at(-1)).toMatch(/预览/);
    expect(screen.queryByRole('tab', { name: '终端' })).not.toBeInTheDocument();
    expect(screen.getByTestId('preview-tool-pane')).toBeInTheDocument();
  });

  it('连续切换聊天页签时立即更新 ref，离开上一页签会清 activityCompleted', () => {
    const chat2: ChatInfo = { ...chat, ID: 'c2', Title: '会话二' };
    useAppStore.setState({
      chats: [{ ...chat, Status: 'ready' }, { ...chat2, Status: 'ready' }],
      terminals: [],
      activityCompleted: { c1: true, c2: true },
    });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} visible />);

    fireEvent.click(screen.getByRole('tab', { name: '新会话' }));
    fireEvent.click(screen.getByRole('tab', { name: '会话二' }));

    expect(useAppStore.getState().activityCompleted.c1).toBeUndefined();
    expect(useAppStore.getState().activityCompleted.c2).toBe(true);
  });
});
