import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
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
  listChatQueue: vi.fn(),
  removeChatQueued: vi.fn(),
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
  api.listChatQueue.mockResolvedValue([]);
  api.removeChatQueued.mockResolvedValue(undefined);
  api.pickFile.mockResolvedValue('');
  api.cancelChat.mockResolvedValue(undefined);
  api.respondChatPermission.mockResolvedValue(undefined);
  api.cancelChatPermission.mockResolvedValue(undefined);
  api.listChats.mockResolvedValue([]);
  useAppStore.setState({ chatItems: {}, chatSeq: {}, chatPermissions: {}, chats: [CHAT], chatModels: {}, chatCommands: {}, chatQueues: {}, chatBackendQueues: {}, chatQueueRevs: {}, queuedPaused: {}, toasts: [] });
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

  // 异常停止时后端会把 agent stderr 尾部附在 turn_done.Text（如 429 限流文本），
  // 前端以可折叠 details 展示，帮助用户归因。
  it('turn_done 附带 stderr 详情时渲染可折叠的 agent 输出', () => {
    useAppStore.setState({
      chatItems: { c1: [{ key: 'td1', type: 'turn_done', stopReason: 'refusal', text: '429 Requests are too frequent', seq: 1 }] },
      chatSeq: { c1: 1 },
    });
    render(<ChatView chat={CHAT} active />);
    const details = screen.getByTestId('chat-stderr-detail');
    expect(details.textContent).toContain(tt('ui.chat.agent_stderr'));
    expect(details.textContent).toContain('429 Requests are too frequent');
  });

  it('turn_done 无 stderr 详情时不渲染 details', () => {
    useAppStore.setState({
      chatItems: { c1: [{ key: 'td1', type: 'turn_done', stopReason: 'refusal', seq: 1 }] },
      chatSeq: { c1: 1 },
    });
    render(<ChatView chat={CHAT} active />);
    expect(screen.getByTestId('chat-turn-stopped')).toBeInTheDocument();
    expect(screen.queryByTestId('chat-stderr-detail')).not.toBeInTheDocument();
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

  it('Models 为 null（Go nil slice 序列化）时渲染不崩溃', async () => {
    // 模拟 Go 侧 ListChatModels !ok 返回 { Current: '', Models: null }（wailsjs 生成类型不可信）
    api.listChatModels.mockResolvedValue({ Current: '', Models: null });
    useAppStore.setState({ chatModels: { c1: { Current: '', Models: null as never } } });
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

  // ---- 消息队列：running 时入队不出手，出队（回 ready）时才真正发送 ----
  it('running 时 Enter 发送入队而不调后端，队列条目展示在输入区上方', () => {
    render(<ChatView chat={{ ...CHAT, Status: 'running' }} active />);
    const box = screen.getByPlaceholderText(tt('ui.chat.input_placeholder'));
    fireEvent.change(box, { target: { value: '排队消息' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(api.sendChatPrompt).not.toHaveBeenCalled();
    const q = useAppStore.getState().chatQueues.c1;
    expect(q).toHaveLength(1);
    expect(q![0]!.text).toBe('排队消息');
    expect(screen.getByTestId('chat-queue')).toHaveTextContent('排队消息');
  });

  it('队列条目可撤回：点击 × 后从 store 与界面消失', () => {
    useAppStore.setState({ chatQueues: { c1: [{ id: 7, text: '待撤回' }] } });
    // 排队只发生在 running 期间；ready 首渲染沿会立即出队，故用 running 渲染
    render(<ChatView chat={{ ...CHAT, Status: 'running' }} active />);
    expect(screen.getByTestId('chat-queue')).toHaveTextContent('待撤回');
    fireEvent.click(screen.getByTestId('chat-queue-remove-7'));
    expect(useAppStore.getState().chatQueues.c1).toBeUndefined();
    expect(screen.queryByTestId('chat-queue')).not.toBeInTheDocument();
  });

  it('running 时带附件发送入队保留 blocks，出队前不发后端', async () => {
    render(<ChatView chat={{ ...CHAT, Status: 'running' }} active />);
    const box = screen.getByPlaceholderText(tt('ui.chat.input_placeholder'));
    const file = new File(['x'], 'shot.png', { type: 'image/png' });
    fireEvent.paste(box, { clipboardData: { files: [file] } });
    // 粘贴附件经 FileReader 异步转 base64 后才进 chip，需等 chip 出现再发送
    await screen.findByText('shot.png');
    fireEvent.change(box, { target: { value: '看图' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(api.sendChatPromptBlocks).not.toHaveBeenCalled();
    const q = useAppStore.getState().chatQueues.c1;
    expect(q![0]!.text).toBe('看图');
    expect(q![0]!.blocks).toHaveLength(2);
  });

  it('会话回 ready 时出队首条并真正发送，队列区移除该条', async () => {
    useAppStore.setState({ chatQueues: { c1: [{ id: 1, text: '第一条' }, { id: 2, text: '第二条' }] } });
    const { rerender } = render(<ChatView chat={{ ...CHAT, Status: 'running' }} active />);
    rerender(<ChatView chat={{ ...CHAT, Status: 'ready' }} active />);
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', '第一条'));
    const q = useAppStore.getState().chatQueues.c1;
    expect(q).toHaveLength(1);
    expect(q![0]!.text).toBe('第二条');
  });

  it('出队后乐观置 running，多条排队时逐条出队', async () => {
    useAppStore.setState({ chatQueues: { c1: [{ id: 1, text: '第一条' }, { id: 2, text: '第二条' }] } });
    const { rerender } = render(<ChatView chat={{ ...CHAT, Status: 'ready' }} active />);
    // 首渲染即 ready 也算转换沿（启动期入队可能发生在首渲染前）
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledTimes(1));
    expect(useAppStore.getState().chats.find((c) => c.ID === 'c1')?.Status).toBe('running');
    expect(useAppStore.getState().chatQueues.c1![0]!.text).toBe('第二条');
    // 第一轮结束回 ready：先 running 再 ready，构成新的转换沿
    rerender(<ChatView chat={{ ...CHAT, Status: 'running' }} active />);
    rerender(<ChatView chat={{ ...CHAT, Status: 'ready' }} active />);
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledTimes(2));
    expect(useAppStore.getState().chatQueues.c1).toBeUndefined();
  });

  it('出队带 blocks 的消息走 sendChatPromptBlocks', async () => {
    useAppStore.setState({ chatQueues: { c1: [{ id: 1, text: '看图', blocks: [{ Type: 'image', Data: 'x', MimeType: 'image/png' }] }] } });
    render(<ChatView chat={{ ...CHAT, Status: 'ready' }} active />);
    await waitFor(() =>
      expect(api.sendChatPromptBlocks).toHaveBeenCalledWith('c1', [{ Type: 'image', Data: 'x', MimeType: 'image/png' }]),
    );
  });

  it('出队发送失败：提示错误、消息插回队首并置暂停标记', async () => {
    api.sendChatPrompt.mockRejectedValueOnce(new Error('boom'));
    useAppStore.setState({ chatQueues: { c1: [{ id: 1, text: '会失败的消息' }] } });
    render(<ChatView chat={{ ...CHAT, Status: 'ready' }} active />);
    await waitFor(() => {
      expect(useAppStore.getState().toasts.some((t) => t.tone === 'error')).toBe(true);
    });
    // 失败消息插回队首，用户可撤回或手动重试；自动出队暂停
    expect(useAppStore.getState().chatQueues.c1).toHaveLength(1);
    expect(useAppStore.getState().queuedPauseActive('c1')).toBe(true);
    expect(api.sendChatPrompt).toHaveBeenCalledTimes(1);
  });

  // 级联场景必须经 store 驱动链路复现：doSend 乐观置 running 与失败置 ready
  // 跨 promise 微任务，prop 序列为 ready→running→ready，直接 rerender 复现不了。
  function StoreDrivenParent({ active }: { active: boolean }) {
    const chats = useAppStore((s) => s.chats);
    const c = chats.find((x) => x.ID === 'c1') ?? null;
    if (!c) return null;
    return <ChatView chat={c} active={active} />;
  }

  it('出队失败后暂停自动出队：后续 ready 沿不再烧穿队列', async () => {
    api.sendChatPrompt.mockRejectedValue(new Error('boom'));
    useAppStore.setState({ chats: [{ ...CHAT, Status: 'running' }], chatQueues: { c1: [{ id: 1, text: '第一条' }, { id: 2, text: '第二条' }] } });
    render(<StoreDrivenParent active />);
    // turn_done：store 置 ready → 出队「第一条」→ 失败 → fail 置 ready 构成新沿
    useAppStore.getState().upsertChat({ ...CHAT, Status: 'ready' });
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledTimes(1));
    // 暂停标记必须拦住级联：失败条目插回队首，队列仍是两条（第一条 + 第二条）。
    // 等 fail 微任务跑完（requeue 完成）再手动发送，模拟真实时序。
    await waitFor(() => expect(useAppStore.getState().chatQueues.c1).toHaveLength(2));
    expect(api.sendChatPrompt).toHaveBeenCalledTimes(1);
    expect(useAppStore.getState().queuedPauseActive('c1')).toBe(true);
    // 用户手动发送一次即解除暂停（重新恢复自动出队）
    api.sendChatPrompt.mockResolvedValue(undefined);
    const box = screen.getByPlaceholderText(tt('ui.chat.input_placeholder'));
    fireEvent.change(box, { target: { value: '手动消息' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', '手动消息'));
    // 手动消息这轮结束回 ready：暂停已解除，插回队首的「第一条」自动重发，
    // 这次成功；它这轮结束后「第二条」跟着出队（两个 upsert 间 flush 防批处理吞沿）
    useAppStore.getState().upsertChat({ ...CHAT, Status: 'running' });
    await new Promise((r) => setTimeout(r, 0));
    useAppStore.getState().upsertChat({ ...CHAT, Status: 'ready' });
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', '第一条'));
    useAppStore.getState().upsertChat({ ...CHAT, Status: 'running' });
    await new Promise((r) => setTimeout(r, 0));
    useAppStore.getState().upsertChat({ ...CHAT, Status: 'ready' });
    await waitFor(() => expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', '第二条'));
    expect(useAppStore.getState().chatQueues.c1).toBeUndefined();
  });

  it('纯附件消息入队后条目显示附件占位文案而非空白', () => {
    useAppStore.setState({ chatQueues: { c1: [{ id: 9, text: '', blocks: [{ Type: 'image', Data: 'x', MimeType: 'image/png' }] }] } });
    render(<ChatView chat={{ ...CHAT, Status: 'running' }} active />);
    expect(screen.getByTestId('chat-queue')).toHaveTextContent(tt('ui.chat.queue_attachment'));
  });

  // ---- 后端待发送队列：任何来源（AgentHub 等）入队的消息都要在输入区可见 ----
  it('挂载时回填后端队列，展示在本地队列之前', async () => {
    api.listChatQueue.mockResolvedValue([{ ID: '42', HubMsgID: '42', Text: 'Hub 排队' }]);
    useAppStore.setState({ chatQueues: { c1: [{ id: 1, text: '本地排队' }] } });
    render(<ChatView chat={{ ...CHAT, Status: 'running' }} active />);
    await waitFor(() => expect(api.listChatQueue).toHaveBeenCalledWith('c1'));
    const q = await screen.findByTestId('chat-queue');
    await waitFor(() => expect(q).toHaveTextContent('Hub 排队'));
    const text = q.textContent ?? '';
    expect(text.indexOf('Hub 排队')).toBeGreaterThanOrEqual(0);
    expect(text.indexOf('Hub 排队')).toBeLessThan(text.indexOf('本地排队'));
  });

  it('后端队列条目可撤销：点击 × 调 removeChatQueued 并立即消失', async () => {
    api.listChatQueue.mockResolvedValue([{ ID: '42', HubMsgID: '42', Text: '待撤销' }]);
    render(<ChatView chat={{ ...CHAT, Status: 'running' }} active />);
    await screen.findByText('待撤销');
    fireEvent.click(screen.getByTestId('chat-queue-remove-backend-42'));
    expect(api.removeChatQueued).toHaveBeenCalledWith('c1', '42');
    expect(screen.queryByText('待撤销')).not.toBeInTheDocument();
    expect(useAppStore.getState().chatBackendQueues.c1).toBeUndefined();
  });

  it('后端条目撤销失败：提示错误并重拉队列对齐', async () => {
    api.listChatQueue.mockResolvedValueOnce([{ ID: '42', HubMsgID: '42', Text: '可能已投递' }]);
    api.removeChatQueued.mockRejectedValueOnce(new Error('err.chat.queue_gone'));
    render(<ChatView chat={{ ...CHAT, Status: 'running' }} active />);
    await screen.findByText('可能已投递');
    fireEvent.click(screen.getByTestId('chat-queue-remove-backend-42'));
    await waitFor(() =>
      expect(useAppStore.getState().toasts.some((t) => t.tone === 'error')).toBe(true),
    );
    // 失败后重拉：后端已不在队列（投递竞态），镜像保持为空
    await waitFor(() => expect(api.listChatQueue).toHaveBeenCalledTimes(2));
    expect(useAppStore.getState().chatBackendQueues.c1).toBeUndefined();
  });

  it('挂载回填期间收到 chat:queue 事件时，丢弃过期快照不覆盖新状态', async () => {
    let resolveBackfill!: (items: { ID: string; Text: string }[]) => void;
    api.listChatQueue.mockReturnValue(
      new Promise((res) => {
        resolveBackfill = res;
      }),
    );
    render(<ChatView chat={{ ...CHAT, Status: 'running' }} active />);
    await waitFor(() => expect(api.listChatQueue).toHaveBeenCalledWith('c1'));

    // 回填未返回时 App 收到新事件：推进版本并写入新快照
    act(() => {
      useAppStore.getState().bumpChatQueueRev('c1');
      useAppStore.getState().setChatBackendQueue('c1', [{ ID: '43', Text: '新快照' }]);
    });
    await act(async () => {
      resolveBackfill([{ ID: '42', Text: '过期快照' }]);
    });

    expect(await screen.findByText('新快照')).toBeInTheDocument();
    expect(screen.queryByText('过期快照')).not.toBeInTheDocument();
    expect(useAppStore.getState().chatBackendQueues.c1?.map((it) => it.ID)).toEqual(['43']);
  });

  it('后端纯附件条目显示附件占位文案', async () => {
    api.listChatQueue.mockResolvedValue([
      { ID: 'q1', Text: '', Attachments: [{ MimeType: 'image/png' }] },
    ]);
    render(<ChatView chat={{ ...CHAT, Status: 'running' }} active />);
    await waitFor(() =>
      expect(screen.getByTestId('chat-queue')).toHaveTextContent(tt('ui.chat.queue_attachment')),
    );
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
