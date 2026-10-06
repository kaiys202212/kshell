import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ChatInfo } from '../lib/api';
import { DRAG_MIME } from '../lib/dragPath';
import { OPEN_FILE_EVENT } from '../lib/openHref';
import { useAppStore } from '../state/store';
import ChatView from './ChatView';

const api = vi.hoisted(() => ({
  sendChatPrompt: vi.fn(),
  cancelChat: vi.fn(),
  respondChatPermission: vi.fn(),
  cancelChatPermission: vi.fn(),
  listChats: vi.fn(),
}));
vi.mock('../lib/api', () => api);

const CHAT: ChatInfo = {
  ID: 'c1', Kind: 'new', SessionID: 's1', Workspace: 'D:\\p', Title: '新会话',
  ToolID: 'claude', Status: 'ready', ExitCode: 0, Error: '',
};

beforeEach(() => {
  vi.clearAllMocks();
  api.sendChatPrompt.mockResolvedValue(undefined);
  api.cancelChat.mockResolvedValue(undefined);
  api.respondChatPermission.mockResolvedValue(undefined);
  api.cancelChatPermission.mockResolvedValue(undefined);
  api.listChats.mockResolvedValue([]);
  useAppStore.setState({ chatItems: {}, chatSeq: {}, chatPermissions: {}, chats: [CHAT] });
});
afterEach(cleanup);

describe('ChatView', () => {
  it('输入并发送调用 sendChatPrompt', () => {
    render(<ChatView chat={CHAT} active />);
    const box = screen.getByPlaceholderText(/输入/);
    fireEvent.change(box, { target: { value: 'hello' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', 'hello');
  });

  it('发送后乐观置 running', () => {
    useAppStore.setState({ chats: [CHAT] });
    render(<ChatView chat={CHAT} active />);
    const box = screen.getByPlaceholderText(/输入/);
    fireEvent.change(box, { target: { value: 'hello' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(useAppStore.getState().chats.find((c) => c.ID === 'c1')?.Status).toBe('running');
    expect(api.listChats).not.toHaveBeenCalled();
  });

  it('发送失败：回退 ready 并提示错误', async () => {
    api.sendChatPrompt.mockRejectedValueOnce(new Error('boom'));
    render(<ChatView chat={CHAT} active />);
    const box = screen.getByPlaceholderText(/输入/);
    fireEvent.change(box, { target: { value: 'hello' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => {
      expect(useAppStore.getState().chats.find((c) => c.ID === 'c1')?.Status).toBe('ready');
    });
    expect(useAppStore.getState().toasts.some((t) => t.title.includes('发送失败'))).toBe(true);
  });

  it('running 时显示停止并调用 cancelChat', () => {
    render(<ChatView chat={{ ...CHAT, Status: 'running' }} active />);
    fireEvent.click(screen.getByRole('button', { name: /停止/ }));
    expect(api.cancelChat).toHaveBeenCalledWith('c1');
  });

  it('渲染时间线与权限弹窗', () => {
    useAppStore.setState({
      chatItems: { c1: [
        { key: 'm1', type: 'user', text: 'hi', seq: 1 },
        { key: 'm2', type: 'assistant', text: '**好的**', seq: 2 },
      ] },
      chatPermissions: { c1: { RequestID: 'r1', SessionID: 's1', ToolCall: { ToolCallID: 'c1' }, Options: [{ OptionID: 'allow', Name: '允许', Kind: 'allow_once' }] } },
    });
    render(<ChatView chat={CHAT} active />);
    expect(screen.getByText('hi')).toBeInTheDocument();
    expect(screen.getByText('好的')).toBeInTheDocument(); // markdown 渲染成 <strong>好的</strong>
    fireEvent.click(screen.getByRole('button', { name: '允许' }));
    expect(api.respondChatPermission).toHaveBeenCalledWith('c1', 'r1', 'allow');
  });

  it('选择权限选项后立即清除弹窗状态（乐观关闭）', () => {
    useAppStore.setState({
      chatItems: {},
      chatPermissions: { c1: { RequestID: 'r1', SessionID: 's1', ToolCall: { ToolCallID: 'c1' }, Options: [{ OptionID: 'allow', Name: '允许', Kind: 'allow_once' }] } },
    });
    render(<ChatView chat={CHAT} active />);

    fireEvent.click(screen.getByRole('button', { name: '允许' }));

    expect(api.respondChatPermission).toHaveBeenCalledWith('c1', 'r1', 'allow');
    expect(useAppStore.getState().chatPermissions.c1 ?? null).toBeNull();
  });

  it('拒绝权限同样立即清除弹窗状态', () => {
    useAppStore.setState({
      chatItems: {},
      chatPermissions: { c1: { RequestID: 'r1', SessionID: 's1', ToolCall: { ToolCallID: 'c1' }, Options: [] } },
    });
    render(<ChatView chat={CHAT} active />);

    fireEvent.click(screen.getByRole('button', { name: '拒绝' }));

    expect(api.cancelChatPermission).toHaveBeenCalledWith('c1', 'r1');
    expect(useAppStore.getState().chatPermissions.c1 ?? null).toBeNull();
  });

  it('工具调用渲染文本内容（diff 无高亮时降级为纯文本）', () => {
    useAppStore.setState({
      chatItems: { c1: [
        {
          key: 't1',
          type: 'tool',
          seq: 1,
          tool: {
            ToolCallID: 'tc1',
            Title: '读取文件',
            Kind: 'read',
            Status: 'completed',
            Content: [{ type: 'content', content: { type: 'text', text: '结果文本' } }],
          },
        },
      ] },
      chatPermissions: {},
    });
    render(<ChatView chat={CHAT} active />);

    expect(screen.getByText(/结果文本/)).toBeInTheDocument();
  });

  it('thought 项渲染为可折叠的「思考」区块', () => {
    useAppStore.setState({
      chatItems: { c1: [{ key: 'th1', type: 'thought', text: '內部推理', seq: 1 }] },
      chatPermissions: {},
    });
    render(<ChatView chat={CHAT} active />);

    expect(screen.getByText('思考')).toBeInTheDocument();
    expect(screen.getByText('內部推理')).toBeInTheDocument();
  });

  it('点开页签时若前端仍 running、后端已 ready，对账后不再转圈', async () => {
    const running = { ...CHAT, Status: 'running', Prompted: true };
    api.listChats.mockResolvedValue([{ ...CHAT, Status: 'ready', Prompted: true }]);
    useAppStore.setState({ chats: [running] });
    render(<ChatView chat={running} active />);
    await waitFor(() => {
      expect(useAppStore.getState().chats.find((c) => c.ID === 'c1')?.Status).toBe('ready');
    });
  });

  it('未激活或并非 running 时不对账 ListChats', () => {
    render(<ChatView chat={CHAT} active />);
    expect(api.listChats).not.toHaveBeenCalled();
    cleanup();
    const running = { ...CHAT, Status: 'running' };
    useAppStore.setState({ chats: [running] });
    render(<ChatView chat={running} active={false} />);
    expect(api.listChats).not.toHaveBeenCalled();
  });

  it('drop 携带 DRAG_MIME：路径包引号后追加进输入草稿', () => {
    render(<ChatView chat={CHAT} active />);
    const root = document.querySelector('[data-drop-zone="chat:c1"]') as HTMLElement;
    expect(root).not.toBeNull();

    fireEvent.drop(root, {
      dataTransfer: { types: [DRAG_MIME], getData: () => 'D:\\my file\\a.go' },
    });

    const box = screen.getByPlaceholderText(/输入/) as HTMLTextAreaElement;
    expect(box.value).toBe('"D:\\my file\\a.go"');
  });

  it('助手 Markdown 的 http 链接走系统打开', () => {
    const open = vi.fn();
    vi.stubGlobal('runtime', { BrowserOpenURL: open });
    useAppStore.setState({
      chatItems: { c1: [{ key: 'm2', type: 'assistant', text: '[x](https://ex.com)', seq: 2 }] },
    });
    render(<ChatView chat={CHAT} active />);
    fireEvent.click(screen.getByRole('link', { name: 'x' }));
    expect(open).toHaveBeenCalledWith('https://ex.com');
    vi.unstubAllGlobals();
  });

  it('助手 Markdown 相对路径打开工作区文件', () => {
    const seen: unknown[] = [];
    const onOpen = (e: Event) => seen.push((e as CustomEvent).detail);
    window.addEventListener(OPEN_FILE_EVENT, onOpen);
    useAppStore.setState({
      chatItems: { c1: [{ key: 'm2', type: 'assistant', text: '[r](./README.md)', seq: 2 }] },
    });
    render(<ChatView chat={CHAT} active />);
    fireEvent.click(screen.getByRole('link', { name: 'r' }));
    window.removeEventListener(OPEN_FILE_EVENT, onOpen);
    expect(seen).toEqual([{ workspace: 'D:\\p', path: 'D:\\p\\README.md' }]);
  });
});
