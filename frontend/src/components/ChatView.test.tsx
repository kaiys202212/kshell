import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ChatInfo } from '../lib/api';
import { useAppStore } from '../state/store';
import ChatView from './ChatView';

const api = vi.hoisted(() => ({
  sendChatPrompt: vi.fn(),
  cancelChat: vi.fn(),
  respondChatPermission: vi.fn(),
  cancelChatPermission: vi.fn(),
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
});
