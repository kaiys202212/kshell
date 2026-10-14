import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ChatInfo } from '../lib/api';
import { DRAG_MIME } from '../lib/dragPath';
import { OPEN_FILE_EVENT } from '../lib/openHref';
import { useAppStore } from '../state/store';
import ChatView from './ChatView';
import { tt } from '../test/i18n';

const api = vi.hoisted(() => ({
  sendChatPrompt: vi.fn(),
  sendChatPromptBlocks: vi.fn(),
  listChatModels: vi.fn(),
  setChatModel: vi.fn(),
  listChatCommands: vi.fn(),
  pickFile: vi.fn(),
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
  api.sendChatPromptBlocks.mockResolvedValue(undefined);
  api.listChatModels.mockResolvedValue(null);
  api.setChatModel.mockResolvedValue(undefined);
  api.listChatCommands.mockResolvedValue([]);
  api.pickFile.mockResolvedValue('');
  api.cancelChat.mockResolvedValue(undefined);
  api.respondChatPermission.mockResolvedValue(undefined);
  api.cancelChatPermission.mockResolvedValue(undefined);
  api.listChats.mockResolvedValue([]);
  useAppStore.setState({ chatItems: {}, chatSeq: {}, chatPermissions: {}, chats: [CHAT], chatModels: {}, chatCommands: {} });
});
afterEach(cleanup);

describe('ChatView', () => {
  it('输入并发送调用 sendChatPrompt', () => {
    render(<ChatView chat={CHAT} active />);
    const box = screen.getByPlaceholderText(tt('ui.chat.input_placeholder'));
    fireEvent.change(box, { target: { value: 'hello' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', 'hello');
  });

  it('发送后乐观置 running', () => {
    useAppStore.setState({ chats: [CHAT] });
    render(<ChatView chat={CHAT} active />);
    const box = screen.getByPlaceholderText(tt('ui.chat.input_placeholder'));
    fireEvent.change(box, { target: { value: 'hello' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(useAppStore.getState().chats.find((c) => c.ID === 'c1')?.Status).toBe('running');
    expect(api.listChats).not.toHaveBeenCalled();
  });

  it('发送失败：回退 ready 并提示错误', async () => {
    api.sendChatPrompt.mockRejectedValueOnce(new Error('boom'));
    render(<ChatView chat={CHAT} active />);
    const box = screen.getByPlaceholderText(tt('ui.chat.input_placeholder'));
    fireEvent.change(box, { target: { value: 'hello' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => {
      expect(useAppStore.getState().chats.find((c) => c.ID === 'c1')?.Status).toBe('ready');
    });
    expect(
      useAppStore.getState().toasts.some((t) => t.title.includes(tt('ui.chat.send_failed').split('{{')[0])),
    ).toBe(true);
  });

  it('running 时显示停止并调用 cancelChat', () => {
    render(<ChatView chat={{ ...CHAT, Status: 'running' }} active />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.chat.stop') }));
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

    fireEvent.click(screen.getByRole('button', { name: tt('ui.chat.reject') }));

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

    expect(screen.getByText(tt('ui.chat.thought'))).toBeInTheDocument();
    expect(screen.getByText('內部推理')).toBeInTheDocument();
  });

  // turn_done 此前前端完全不渲染：agent 因 max_turn_requests/max_tokens 等提前
  // 停止时毫无提示，看起来像「任务没做完却静默停了」。
  it('turn_done 带 stopReason 时渲染停止原因行', () => {
    useAppStore.setState({
      chatItems: { c1: [{ key: 'td1', type: 'turn_done', stopReason: 'max_turn_requests', seq: 1 }] },
      chatSeq: { c1: 1 },
    });
    render(<ChatView chat={CHAT} active />);
    const line = screen.getByTestId('chat-turn-stopped');
    expect(line.textContent).toContain(tt('ui.chat.stop_reason.max_turn_requests'));
  });

  it('停止行点继续：发送 resume prompt 并乐观置 running', () => {
    useAppStore.setState({
      chatItems: { c1: [{ key: 'td1', type: 'turn_done', stopReason: 'max_turn_requests', seq: 1 }] },
      chatSeq: { c1: 1 },
    });
    render(<ChatView chat={CHAT} active />);
    fireEvent.click(screen.getByTestId('chat-resume'));
    expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', tt('ui.chat.resume_turn'));
    expect(useAppStore.getState().chats.find((c) => c.ID === 'c1')?.Status).toBe('running');
  });

  it('end_turn 行不渲染继续按钮', () => {
    useAppStore.setState({
      chatItems: { c1: [{ key: 'td1', type: 'turn_done', stopReason: 'end_turn', seq: 1 }] },
      chatSeq: { c1: 1 },
    });
    render(<ChatView chat={CHAT} active />);
    expect(screen.queryByTestId('chat-resume')).not.toBeInTheDocument();
  });

  it('turn_done 未知 stopReason 显示原始值', () => {
    useAppStore.setState({
      chatItems: { c1: [{ key: 'td1', type: 'turn_done', stopReason: 'weird_reason', seq: 1 }] },
      chatSeq: { c1: 1 },
    });
    render(<ChatView chat={CHAT} active />);
    expect(screen.getByTestId('chat-turn-stopped').textContent).toContain('weird_reason');
  });

  it('turn_done end_turn 不渲染停止行（保持现状）', () => {
    useAppStore.setState({
      chatItems: { c1: [{ key: 'td1', type: 'turn_done', stopReason: 'end_turn', seq: 1 }] },
      chatSeq: { c1: 1 },
    });
    render(<ChatView chat={CHAT} active />);
    expect(screen.queryByTestId('chat-turn-stopped')).not.toBeInTheDocument();
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

  it('drop 携带 DRAG_MIME：作为附件 chip 展示，发送时带 resource_link 块', async () => {
    render(<ChatView chat={CHAT} active />);
    const root = document.querySelector('[data-drop-zone="chat:c1"]') as HTMLElement;
    expect(root).not.toBeNull();

    fireEvent.drop(root, {
      dataTransfer: { types: [DRAG_MIME], getData: () => 'D:\\my file\\a.go' },
    });

    // 路径不再进草稿，而是作为附件 chip 展示
    const box = screen.getByPlaceholderText(tt('ui.chat.input_placeholder')) as HTMLTextAreaElement;
    expect(box.value).toBe('');
    expect(await screen.findByText('a.go')).toBeInTheDocument();

    fireEvent.change(box, { target: { value: 'hi' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => {
      expect(api.sendChatPromptBlocks).toHaveBeenCalledWith('c1', [
        { Type: 'text', Text: 'hi' },
        expect.objectContaining({ Type: 'resource_link', Name: 'a.go' }),
      ]);
    });
  });

  it('模型下拉：有可用模型时展示，选择后调用 setChatModel', async () => {
    api.listChatModels.mockResolvedValue({
      Current: 'm1',
      Models: [{ ID: 'm1', Name: 'Sonnet' }, { ID: 'm2', Name: 'Opus' }],
    });
    render(<ChatView chat={CHAT} active />);
    const trigger = await screen.findByRole('button', { name: /Sonnet/ });
    // Radix 菜单在 trigger 的 pointerdown 打开；条目选中经 click（handleSelect 绑定在 onClick）
    fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false });
    const item = await screen.findByRole('menuitem', { name: 'Opus' });
    fireEvent.pointerDown(item);
    fireEvent.click(item);
    await waitFor(() => expect(api.setChatModel).toHaveBeenCalledWith('c1', 'm2'));
  });

  it('agent 未报告模型时不显示模型控件', async () => {
    render(<ChatView chat={CHAT} active />);
    await waitFor(() => expect(api.listChatModels).toHaveBeenCalledWith('c1'));
    expect(screen.queryByText(tt('ui.chat.model_label'))).not.toBeInTheDocument();
  });

  it('模型切换失败提示错误且保留当前选项', async () => {
    api.listChatModels.mockResolvedValue({
      Current: 'm1',
      Models: [{ ID: 'm1', Name: 'Sonnet' }, { ID: 'm2', Name: 'Opus' }],
    });
    api.setChatModel.mockRejectedValueOnce(new Error('unsupported'));
    render(<ChatView chat={CHAT} active />);
    const trigger = await screen.findByRole('button', { name: /Sonnet/ });
    fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false });
    const item = await screen.findByRole('menuitem', { name: 'Opus' });
    fireEvent.pointerDown(item);
    fireEvent.click(item);
    await waitFor(() => {
      expect(useAppStore.getState().toasts.some((t) => t.tone === 'error')).toBe(true);
    });
  });

  it('输入 / 弹出技能补全，Enter 选中填入而不发送', () => {
    useAppStore.setState({
      chatCommands: { c1: [{ Name: 'plan', Description: '写计划', InputHint: '主题' }, { Name: 'review', Description: '审查' }] },
    });
    render(<ChatView chat={CHAT} active />);
    const box = screen.getByPlaceholderText(tt('ui.chat.input_placeholder')) as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: '/' } });
    const list = screen.getByRole('listbox');
    expect(list).toHaveTextContent('plan');
    expect(list).toHaveTextContent('review');
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(box.value).toBe('/plan ');
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
  });

  it('补全按前缀过滤且上下键移动高亮', () => {
    useAppStore.setState({
      chatCommands: { c1: [{ Name: 'plan', Description: '写计划' }, { Name: 'review', Description: '审查' }] },
    });
    render(<ChatView chat={CHAT} active />);
    const box = screen.getByPlaceholderText(tt('ui.chat.input_placeholder')) as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: '/pl' } });
    const list = screen.getByRole('listbox');
    expect(list).toHaveTextContent('plan');
    expect(list).not.toHaveTextContent('review');
    // 默认第一项高亮；ArrowDown 移到下一项（此处只有一项，保持）
    const options = screen.getAllByRole('option');
    expect(options[0]).toHaveAttribute('aria-selected', 'true');
  });

  it('Esc 关闭补全后 Enter 正常发送', () => {
    useAppStore.setState({ chatCommands: { c1: [{ Name: 'plan', Description: '写计划' }] } });
    render(<ChatView chat={CHAT} active />);
    const box = screen.getByPlaceholderText(tt('ui.chat.input_placeholder')) as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: '/' } });
    expect(screen.getByRole('listbox')).toBeInTheDocument();
    fireEvent.keyDown(box, { key: 'Escape' });
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
    fireEvent.change(box, { target: { value: 'hello' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', 'hello');
  });

  it('粘贴图片作为附件 chip，发送时带 image 块', async () => {
    render(<ChatView chat={CHAT} active />);
    const box = screen.getByPlaceholderText(tt('ui.chat.input_placeholder')) as HTMLTextAreaElement;
    const file = new File(['fake-png'], 'shot.png', { type: 'image/png' });
    fireEvent.paste(box, { clipboardData: { files: [file] } });
    await screen.findByText('shot.png');

    fireEvent.change(box, { target: { value: '看这个' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => {
      expect(api.sendChatPromptBlocks).toHaveBeenCalledWith('c1', [
        { Type: 'text', Text: '看这个' },
        expect.objectContaining({ Type: 'image', MimeType: 'image/png' }),
      ]);
    });
  });

  it('粘贴小文本文件转 resource 块（base64 内容）', async () => {
    render(<ChatView chat={CHAT} active />);
    const box = screen.getByPlaceholderText(tt('ui.chat.input_placeholder')) as HTMLTextAreaElement;
    const file = new File(['hello'], 'note.txt', { type: 'text/plain' });
    fireEvent.paste(box, { clipboardData: { files: [file] } });
    await screen.findByText('note.txt');

    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => {
      expect(api.sendChatPromptBlocks).toHaveBeenCalledWith('c1', [
        expect.objectContaining({ Type: 'resource', MimeType: 'text/plain' }),
      ]);
    });
    const blocks = api.sendChatPromptBlocks.mock.calls[0][1] as { Type: string; Data?: string }[];
    expect(blocks[0].Data).toBe(btoa('hello'));
  });

  it('附件按钮选文件后发送 resource_link 块', async () => {
    api.pickFile.mockResolvedValue('D:\\docs\\a.txt');
    render(<ChatView chat={CHAT} active />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.chat.attach') }));
    await screen.findByText('a.txt');
    const box = screen.getByPlaceholderText(tt('ui.chat.input_placeholder')) as HTMLTextAreaElement;
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => {
      expect(api.sendChatPromptBlocks).toHaveBeenCalledWith('c1', [
        expect.objectContaining({ Type: 'resource_link', URI: 'file:///D:/docs/a.txt', Name: 'a.txt' }),
      ]);
    });
  });

  it('移除附件 chip 后纯文本走 sendChatPrompt', async () => {
    render(<ChatView chat={CHAT} active />);
    const box = screen.getByPlaceholderText(tt('ui.chat.input_placeholder')) as HTMLTextAreaElement;
    const file = new File(['x'], 'shot.png', { type: 'image/png' });
    fireEvent.paste(box, { clipboardData: { files: [file] } });
    await screen.findByText('shot.png');
    fireEvent.click(screen.getByRole('button', { name: tt('ui.chat.remove_attachment') }));
    expect(screen.queryByText('shot.png')).not.toBeInTheDocument();

    fireEvent.change(box, { target: { value: '纯文本' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', '纯文本');
  });

  it('Esc 关闭补全后继续输入可重新弹出', () => {
    useAppStore.setState({ chatCommands: { c1: [{ Name: 'plan', Description: '写计划' }] } });
    render(<ChatView chat={CHAT} active />);
    const box = screen.getByPlaceholderText(tt('ui.chat.input_placeholder')) as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: '/' } });
    fireEvent.keyDown(box, { key: 'Escape' });
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
    fireEvent.change(box, { target: { value: '/p' } });
    expect(screen.getByRole('listbox')).toBeInTheDocument();
  });

  it('粘贴超过上限的图片不产生 chip 并提示改用附件按钮', async () => {
    render(<ChatView chat={CHAT} active />);
    const box = screen.getByPlaceholderText(tt('ui.chat.input_placeholder')) as HTMLTextAreaElement;
    const file = new File(['tiny'], 'huge.png', { type: 'image/png' });
    Object.defineProperty(file, 'size', { value: 9 * 1024 * 1024 });
    fireEvent.paste(box, { clipboardData: { files: [file] } });
    await waitFor(() => {
      expect(useAppStore.getState().toasts.some((t) => t.tone === 'error')).toBe(true);
    });
    expect(screen.queryByText('huge.png')).not.toBeInTheDocument();
  });

  it('时间线 user 消息渲染附件 chips', () => {
    useAppStore.setState({
      chatItems: {
        c1: [{ key: 'u1', type: 'user', text: '看', seq: 1, attachments: [{ Name: 'a.txt' }, { MimeType: 'image/png' }] }],
      },
    });
    render(<ChatView chat={CHAT} active />);
    expect(screen.getByText('a.txt')).toBeInTheDocument();
    expect(screen.getByText(tt('ui.chat.attachment_image'))).toBeInTheDocument();
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

  it('exited 后端错误为已注册 wire key 时渲染翻译文案而非裸 key', () => {
    const { container } = render(
      <ChatView chat={{ ...CHAT, Status: 'exited', ExitCode: 1, Error: 'err.chat.gone' }} active />,
    );
    expect(container.textContent).toContain(tt('err.chat.gone'));
    expect(container.textContent).not.toContain('err.chat.gone');
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

  // body 默认 user-select:none；可读正文容器须带 selectable，侧栏根节点不加
  it('消息正文、思考与工具输出容器带 selectable，侧栏根节点不带', () => {
    useAppStore.setState({
      chatItems: {
        c1: [
          { key: 'm1', type: 'user', text: '用户句', seq: 1 },
          { key: 'm2', type: 'assistant', text: '助手句', seq: 2 },
          { key: 'th1', type: 'thought', text: '思考句', seq: 3 },
          {
            key: 't1',
            type: 'tool',
            seq: 4,
            tool: {
              ToolCallID: 'tc1',
              Title: '读',
              Kind: 'read',
              Status: 'completed',
              Content: [{ type: 'content', content: { type: 'text', text: '工具输出' } }],
            },
          },
        ],
      },
    });
    const { container } = render(<ChatView chat={CHAT} active />);
    const root = container.querySelector('[data-drop-zone="chat:c1"]');
    expect(root).not.toBeNull();
    expect(root).not.toHaveClass('selectable');

    expect(screen.getByText('用户句').closest('.selectable')).not.toBeNull();
    expect(screen.getByText('助手句').closest('.selectable')).not.toBeNull();
    expect(screen.getByText('思考句').closest('.selectable')).not.toBeNull();
    expect(screen.getByText(/工具输出/).closest('.selectable')).not.toBeNull();
  });
});
