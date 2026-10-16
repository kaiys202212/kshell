// RequirementList 组件测试：条目渲染（文本/优先级点/标签/已派发徽标）/ 新增流程 /
// 编辑流程 / 删除流程 / 空态 / dragstart 设置 dataTransfer / requirements:changed 重拉。
// api 层整体打桩（vi.mock），与 Home.test.tsx 同一套模式。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import RequirementList from './RequirementList';
import type { Requirement } from '../lib/api';
import { tt } from '../test/i18n';
import { useAppStore } from '../state/store';

const mocks = vi.hoisted(() => ({
  listRequirements: vi.fn(),
  addRequirement: vi.fn(),
  updateRequirement: vi.fn(),
  deleteRequirement: vi.fn(),
  setRequirementStatus: vi.fn(),
  archiveRequirement: vi.fn(),
  markRequirementDispatched: vi.fn(),
  onRequirementsChanged: vi.fn(),
  readClipboardPaste: vi.fn(),
  openWorkspaceACP: vi.fn(),
  getTools: vi.fn(),
}));
vi.mock('../lib/api', () => ({ ...mocks, onRequirementsChanged: mocks.onRequirementsChanged }));

// 优先级色点用 data-testid 定位（直接断言 class，比比对完整 className 稳定）
const dotOf = (li: HTMLElement) => li.querySelector('[data-testid="req-dot"]');

function req(over: Partial<Requirement> & Pick<Requirement, 'id' | 'text'>): Requirement {
  return { priority: 'medium', tags: [], ts: 1, ...over };
}

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  mocks.listRequirements.mockResolvedValue([]);
  mocks.addRequirement.mockImplementation(
    (text: string, p: string, tags: string[]) =>
      Promise.resolve(req({ id: 'r-new', text, priority: p as Requirement['priority'], tags })),
  );
  mocks.updateRequirement.mockImplementation(
    (id: string, text: string, p: string, tags: string[]) =>
      Promise.resolve(req({ id, text, priority: p as Requirement['priority'], tags })),
  );
  mocks.deleteRequirement.mockResolvedValue(undefined);
  mocks.setRequirementStatus.mockResolvedValue(undefined);
  mocks.archiveRequirement.mockResolvedValue({ released: true, chatId: '' });
  mocks.markRequirementDispatched.mockResolvedValue(undefined);
  mocks.onRequirementsChanged.mockImplementation(() => () => {});
  // 重开会话走真实 dispatchRequirement.reopenRequirementSession → resolveToolID 读 getTools
  mocks.getTools.mockResolvedValue([
    { ID: 'tool-a', ACP: { Available: true, Source: '' } } as never,
  ]);
});

// 填写并提交新增/编辑对话框；text 必填
async function submitDialog(fields: { text: string; priority?: string; tags?: string }) {
  fireEvent.change(screen.getByTestId('req-text'), { target: { value: fields.text } });
  if (fields.priority !== undefined) {
    fireEvent.change(screen.getByTestId('req-priority'), { target: { value: fields.priority } });
  }
  if (fields.tags !== undefined) {
    fireEvent.change(screen.getByTestId('req-tags'), { target: { value: fields.tags } });
  }
  fireEvent.click(screen.getByRole('button', { name: tt('ui.requirements.save') }));
}

describe('RequirementList', () => {
  it('空态：无条目显示空态文案', async () => {
    render(<RequirementList />);
    expect(await screen.findByText(tt('ui.requirements.empty'))).toBeInTheDocument();
  });

  it('渲染条目：文本、high 优先级色点、标签徽标', async () => {
    mocks.listRequirements.mockResolvedValue([
      req({ id: 'r1', text: '实现导出功能', priority: 'high', tags: ['前端', 'P0'] }),
    ]);
    render(<RequirementList />);
    const li = (await screen.findByText('实现导出功能')).closest('li') as HTMLElement;
    expect(dotOf(li)).toHaveClass('bg-destructive');
    expect(within(li).getByText('前端')).toBeInTheDocument();
    expect(within(li).getByText('P0')).toBeInTheDocument();
  });

  it('存量数据 tags 为 null（旧版 Go 落盘）渲染不崩溃', async () => {
    // Go nil slice 经 JSON 序列化是 null；类型上撒谎为 string[]，运行时是 null
    mocks.listRequirements.mockResolvedValue([
      req({ id: 'r9', text: '无标签存量需求', tags: null as unknown as string[] }),
    ]);
    render(<RequirementList />);
    expect(await screen.findByText('无标签存量需求')).toBeInTheDocument();
  });

  it('优先级色点：medium 用主色，low 用 muted 前景色', async () => {
    mocks.listRequirements.mockResolvedValue([
      req({ id: 'r2', text: '中优先级', priority: 'medium' }),
      req({ id: 'r3', text: '低优先级', priority: 'low' }),
    ]);
    render(<RequirementList />);
    const li1 = (await screen.findByText('中优先级')).closest('li') as HTMLElement;
    const li2 = (await screen.findByText('低优先级')).closest('li') as HTMLElement;
    expect(dotOf(li1)).toHaveClass('bg-primary');
    expect(dotOf(li2)).toHaveClass('bg-muted-foreground');
  });

  it('已派发条目显示目标项目基名徽标', async () => {
    mocks.listRequirements.mockResolvedValue([
      req({ id: 'r4', text: '已派发需求', dispatchedTo: 'D:\\workspace\\demo-proj' }),
    ]);
    render(<RequirementList />);
    const li = (await screen.findByText('已派发需求')).closest('li') as HTMLElement;
    expect(li).toHaveTextContent('demo-proj');
  });

  it('新增流程：填表保存调 addRequirement（tags 逗号分隔转数组）', async () => {
    render(<RequirementList />);
    await screen.findByText(tt('ui.requirements.empty'));

    // 按钮文案带「+ 」前缀（与 AgentHubPanel 新建按钮同款），按包含匹配
    fireEvent.click(
      screen.getByRole('button', { name: (n) => n.includes(tt('ui.requirements.add')) }),
    );
    await submitDialog({ text: '新需求', priority: 'high', tags: 'a, b ,c' });

    await waitFor(() =>
      expect(mocks.addRequirement).toHaveBeenCalledWith('新需求', 'high', ['a', 'b', 'c']),
    );
  });

  it('编辑流程：点编辑回填表单，保存调 updateRequirement', async () => {
    mocks.listRequirements.mockResolvedValue([req({ id: 'r1', text: '旧文本', priority: 'low', tags: ['x'] })]);
    render(<RequirementList />);
    await screen.findByText('旧文本');

    fireEvent.click(screen.getByLabelText(tt('ui.requirements.edit')));
    expect(screen.getByTestId('req-text')).toHaveValue('旧文本');
    await submitDialog({ text: '新文本', priority: 'high', tags: 'x, y' });

    await waitFor(() =>
      expect(mocks.updateRequirement).toHaveBeenCalledWith('r1', '新文本', 'high', ['x', 'y']),
    );
  });

  it('删除流程：点删除按钮直接调 deleteRequirement（无确认）', async () => {
    mocks.listRequirements.mockResolvedValue([req({ id: 'r1', text: '待删除' })]);
    render(<RequirementList />);
    await screen.findByText('待删除');

    fireEvent.click(screen.getByLabelText(tt('ui.requirements.delete')));

    await waitFor(() => expect(mocks.deleteRequirement).toHaveBeenCalledWith('r1'));
  });

  it('dragstart 设置 dataTransfer：自定义 MIME 带 JSON、text/plain 带纯文本、effectAllowed=copy', async () => {
    mocks.listRequirements.mockResolvedValue([req({ id: 'r1', text: '拖我' })]);
    render(<RequirementList />);
    const li = await screen.findByText('拖我');
    const draggable = li.closest('[draggable="true"]') as HTMLElement;
    expect(draggable).not.toBeNull();

    const dataTransfer = { setData: vi.fn(), effectAllowed: 'all' };
    await act(async () => {
      fireEvent.dragStart(draggable, { dataTransfer });
    });
    expect(dataTransfer.setData).toHaveBeenCalledWith(
      'application/x-kshell-requirement',
      JSON.stringify({ id: 'r1', text: '拖我' }),
    );
    expect(dataTransfer.setData).toHaveBeenCalledWith('text/plain', '拖我');
    expect(dataTransfer.effectAllowed).toBe('copy');
  });

  it('终态条目不可拖拽：done 条目 draggable 非 true，dragStart 不写 dataTransfer', async () => {
    mocks.listRequirements.mockResolvedValue([
      req({ id: 'r1', text: '已完成需求', status: 'done' }),
    ]);
    render(<RequirementList />);
    const li = (await screen.findByText('已完成需求')).closest('li') as HTMLElement;
    // React 对 draggable={false} 渲染 "false" 属性；语义断言只认 "true"
    expect(li.getAttribute('draggable')).not.toBe('true');

    const dataTransfer = { setData: vi.fn(), effectAllowed: 'all' };
    await act(async () => {
      fireEvent.dragStart(li, { dataTransfer });
    });
    expect(dataTransfer.setData).not.toHaveBeenCalled();
  });

  it('archived 条目不进主列表，active/pending 仍可拖拽', async () => {
    mocks.listRequirements.mockResolvedValue([
      req({ id: 'r1', text: '未开始需求' }),
      req({ id: 'r2', text: '执行中需求', status: 'active' }),
    ]);
    render(<RequirementList />);
    const li1 = (await screen.findByText('未开始需求')).closest('li') as HTMLElement;
    const li2 = (await screen.findByText('执行中需求')).closest('li') as HTMLElement;
    expect(li1).toHaveAttribute('draggable', 'true');
    expect(li2).toHaveAttribute('draggable', 'true');
  });

  // ---- 会话行（会话名 + 运行状态 + 跳转/重开） ----

  // 会话状态点用 data-testid 定位（class 断言比比对完整 className 稳定）
  const sessionDotOf = (li: HTMLElement) => li.querySelector('[data-testid="req-session-dot"]');

  const liveChat = {
    ID: 'c1', Kind: 'new', SessionID: '', Workspace: 'D:\\ws\\proj',
    Title: '实时标题', ToolID: 'claude', Status: 'running', ExitCode: 0, Error: '', Key: 'new:1',
  };

  it('已派发条目渲染会话行：活跃会话实时标题优先，running 状态点 + 跳转按钮', async () => {
    useAppStore.setState({ chats: [liveChat as never] });
    mocks.listRequirements.mockResolvedValue([
      req({ id: 'r1', text: '已派发需求', status: 'active', chatId: 'c1', dispatchedTo: 'D:\\ws\\proj', sessionTitle: '落盘标题' }),
    ]);
    render(<RequirementList />);
    const li = (await screen.findByText('已派发需求')).closest('li') as HTMLElement;
    // 实时镜像标题优先于落盘快照
    expect(within(li).getByText('实时标题')).toBeInTheDocument();
    expect(within(li).queryByText('落盘标题')).not.toBeInTheDocument();
    expect(sessionDotOf(li)).toHaveClass('bg-success');
    expect(within(li).getByLabelText(tt('ui.requirements.session_jump'))).toBeInTheDocument();
  });

  it('会话不存在：显示 sessionTitle 快照 + 已结束点 + 重新打开按钮', async () => {
    useAppStore.setState({ chats: [] });
    mocks.listRequirements.mockResolvedValue([
      req({ id: 'r1', text: '重启后需求', status: 'active', chatId: 'c-dead', dispatchedTo: 'D:\\ws\\proj', sessionTitle: '旧会话名' }),
    ]);
    render(<RequirementList />);
    const li = (await screen.findByText('重启后需求')).closest('li') as HTMLElement;
    expect(within(li).getByText('旧会话名')).toBeInTheDocument();
    expect(sessionDotOf(li)).toHaveClass('bg-destructive');
    expect(within(li).getByLabelText(tt('ui.requirements.session_reopen'))).toBeInTheDocument();
  });

  it('done 条目会话已结束：不显示重开按钮（done 为终态，重开会被派发守卫拒绝）', async () => {
    useAppStore.setState({ chats: [] });
    mocks.listRequirements.mockResolvedValue([
      req({ id: 'r1', text: '已完成需求', status: 'done', chatId: 'c-dead', dispatchedTo: 'D:\\ws\\proj', sessionTitle: '旧会话名' }),
    ]);
    render(<RequirementList />);
    const li = (await screen.findByText('已完成需求')).closest('li') as HTMLElement;
    // 会话行仍显示名称与状态点，但无重开入口
    expect(within(li).getByText('旧会话名')).toBeInTheDocument();
    expect(within(li).queryByLabelText(tt('ui.requirements.session_reopen'))).not.toBeInTheDocument();
  });

  it('会话不存在且无快照：回退显示 chatId', async () => {
    useAppStore.setState({ chats: [] });
    mocks.listRequirements.mockResolvedValue([
      req({ id: 'r1', text: '无快照需求', status: 'active', chatId: 'c-raw', dispatchedTo: 'D:\\ws\\proj' }),
    ]);
    render(<RequirementList />);
    const li = (await screen.findByText('无快照需求')).closest('li') as HTMLElement;
    expect(within(li).getByText('c-raw')).toBeInTheDocument();
  });

  it('exited 会话：红色状态点 + 跳转按钮（聚焦工作区可见历史）', async () => {
    useAppStore.setState({ chats: [{ ...liveChat, Status: 'exited' } as never] });
    mocks.listRequirements.mockResolvedValue([
      req({ id: 'r1', text: '已退出需求', status: 'active', chatId: 'c1', dispatchedTo: 'D:\\ws\\proj' }),
    ]);
    render(<RequirementList />);
    const li = (await screen.findByText('已退出需求')).closest('li') as HTMLElement;
    expect(sessionDotOf(li)).toHaveClass('bg-destructive');
    expect(within(li).getByLabelText(tt('ui.requirements.session_jump'))).toBeInTheDocument();
  });

  it('点击重新打开：调 openWorkspaceACP 与 markRequirementDispatched（带新 chat.ID 与标题）', async () => {
    useAppStore.setState({ chats: [] });
    mocks.listRequirements.mockResolvedValue([
      req({ id: 'r1', text: '待重开需求', status: 'active', chatId: 'c-dead', dispatchedTo: 'D:\\ws\\proj', sessionTitle: '旧会话名' }),
    ]);
    mocks.openWorkspaceACP.mockResolvedValue({
      Kind: 'chat',
      Chat: { ID: 'c-new', Kind: 'new', SessionID: '', Workspace: 'D:\\ws\\proj', Title: '新标题', ToolID: 'claude', Status: 'ready', ExitCode: 0, Error: '', Key: 'new:2' },
    });
    render(<RequirementList />);
    const li = (await screen.findByText('待重开需求')).closest('li') as HTMLElement;
    fireEvent.click(within(li).getByLabelText(tt('ui.requirements.session_reopen')));

    await waitFor(() => expect(mocks.openWorkspaceACP).toHaveBeenCalledWith('D:\\ws\\proj', expect.any(String)));
    await waitFor(() =>
      expect(mocks.markRequirementDispatched).toHaveBeenCalledWith('r1', 'D:\\ws\\proj', 'c-new', '新标题'),
    );
  });

  it('重新打开失败：提示 reopen_failed', async () => {
    useAppStore.setState({ chats: [] });
    mocks.listRequirements.mockResolvedValue([
      req({ id: 'r1', text: '重开失败需求', status: 'active', chatId: 'c-dead', dispatchedTo: 'D:\\ws\\proj', sessionTitle: '旧会话名' }),
    ]);
    mocks.openWorkspaceACP.mockRejectedValue(new Error('boom'));
    render(<RequirementList />);
    const li = (await screen.findByText('重开失败需求')).closest('li') as HTMLElement;
    fireEvent.click(within(li).getByLabelText(tt('ui.requirements.session_reopen')));

    await waitFor(() =>
      expect(
        useAppStore
          .getState()
          .toasts.some((x) => x.title.includes(tt('ui.requirements.reopen_failed').replace('{{err}}', 'boom'))),
      ).toBe(true),
    );
  });

  it('未派发条目不渲染会话行', async () => {
    useAppStore.setState({ chats: [] });
    mocks.listRequirements.mockResolvedValue([req({ id: 'r1', text: '纯 pending' })]);
    render(<RequirementList />);
    const li = (await screen.findByText('纯 pending')).closest('li') as HTMLElement;
    expect(sessionDotOf(li)).toBeNull();
  });

  it('操作图标移到徽章行（非右上角悬浮）：仍在条目内且 hover 显现', async () => {
    mocks.listRequirements.mockResolvedValue([req({ id: 'r1', text: '移位检查' })]);
    render(<RequirementList />);
    const li = (await screen.findByText('移位检查')).closest('li') as HTMLElement;
    const editBtn = within(li).getByLabelText(tt('ui.requirements.edit'));
    // 不再是 absolute 悬浮容器（top-1.5 right-1.5），而在徽章 flex 行内
    expect(editBtn.closest('span')?.className).not.toContain('absolute');
    expect(editBtn.closest('span')?.className).toContain('ml-auto');
  });

  it('requirements:changed 事件触发重拉列表', async () => {
    let changedCb: (() => void) | undefined;
    mocks.onRequirementsChanged.mockImplementation((cb: () => void) => {
      changedCb = cb;
      return () => {};
    });
    mocks.listRequirements.mockResolvedValue([req({ id: 'r1', text: '第一条' })]);
    render(<RequirementList />);
    await screen.findByText('第一条');
    const calls = mocks.listRequirements.mock.calls.length;

    await act(async () => {
      changedCb?.();
    });
    await waitFor(() => expect(mocks.listRequirements.mock.calls.length).toBeGreaterThan(calls));
  });

  // 粘贴附件：clipboardData 含 Files 时拦截默认粘贴，readClipboardPaste 拿落盘
  // 路径（自动转地址）追加到需求文本；路径含空格自动加引号。
  function pasteEvent(files: boolean) {
    return {
      clipboardData: {
        types: files ? ['Files'] : ['text/plain'],
        getData: () => '',
      },
      preventDefault: vi.fn(),
    };
  }

  async function openAddDialog() {
    render(<RequirementList />);
    await screen.findByText(tt('ui.requirements.empty'));
    fireEvent.click(
      screen.getByRole('button', { name: (n) => n.includes(tt('ui.requirements.add')) }),
    );
    await screen.findByTestId('req-text');
  }

  it('粘贴文件：读剪贴板路径并追加到需求文本（含空格路径加引号）', async () => {
    mocks.readClipboardPaste.mockResolvedValue({ Path: 'D:\\my dir\\shot.png', Text: '' });
    await openAddDialog();
    const box = screen.getByTestId('req-text');

    await act(async () => {
      fireEvent.paste(box, pasteEvent(true));
    });
    expect((box as HTMLTextAreaElement).value).toBe('"D:\\my dir\\shot.png"');
  });

  it('粘贴文件叠加已有文本：空格分隔追加', async () => {
    mocks.readClipboardPaste.mockResolvedValue({ Path: 'C:\\a.txt', Text: '' });
    await openAddDialog();
    const box = screen.getByTestId('req-text');
    fireEvent.change(box, { target: { value: '实现导出' } });

    await act(async () => {
      fireEvent.paste(box, pasteEvent(true));
    });
    expect((box as HTMLTextAreaElement).value).toBe('实现导出 C:\\a.txt');
  });

  it('粘贴纯文本：不拦截走默认粘贴，不调 readClipboardPaste', async () => {
    await openAddDialog();
    const box = screen.getByTestId('req-text');
    const ev = pasteEvent(false);

    fireEvent.paste(box, ev);
    expect(ev.preventDefault).not.toHaveBeenCalled();
    expect(mocks.readClipboardPaste).not.toHaveBeenCalled();
    // 默认粘贴由浏览器执行，jsdom 不模拟插入；文本保持不变即可
    expect((box as HTMLTextAreaElement).value).toBe('');
  });

  it('读剪贴板失败：静默不崩，文本不变', async () => {
    mocks.readClipboardPaste.mockRejectedValue(new Error('clip error'));
    await openAddDialog();
    const box = screen.getByTestId('req-text');
    fireEvent.change(box, { target: { value: '保持原样' } });

    await act(async () => {
      fireEvent.paste(box, pasteEvent(true));
    });
    expect((box as HTMLTextAreaElement).value).toBe('保持原样');
  });

  // ---- 状态徽章与按状态操作 ----

  it('pending 条目：状态徽章「未开始」，hover 操作为编辑+删除', async () => {
    mocks.listRequirements.mockResolvedValue([req({ id: 'r1', text: '待派发需求' })]);
    render(<RequirementList />);
    const li = (await screen.findByText('待派发需求')).closest('li') as HTMLElement;
    expect(within(li).getByText(tt('ui.requirements.status_pending'))).toBeInTheDocument();
    expect(within(li).getByLabelText(tt('ui.requirements.edit'))).toBeInTheDocument();
    expect(within(li).getByLabelText(tt('ui.requirements.delete'))).toBeInTheDocument();
  });

  it('active 条目：徽章「执行中」，有标记完成无编辑；点击调 setRequirementStatus(id, "done")', async () => {
    mocks.listRequirements.mockResolvedValue([
      req({ id: 'r1', text: '执行中需求', status: 'active' }),
    ]);
    render(<RequirementList />);
    const li = (await screen.findByText('执行中需求')).closest('li') as HTMLElement;
    expect(within(li).getByText(tt('ui.requirements.status_active'))).toBeInTheDocument();
    expect(within(li).queryByLabelText(tt('ui.requirements.edit'))).not.toBeInTheDocument();
    fireEvent.click(within(li).getByRole('button', { name: tt('ui.requirements.mark_done') }));

    await waitFor(() => expect(mocks.setRequirementStatus).toHaveBeenCalledWith('r1', 'done'));
  });

  it('done 条目：徽章「已完成」，点击归档调 archiveRequirement；提示名优先 dispatchedTo 基名', async () => {
    mocks.listRequirements.mockResolvedValue([
      req({ id: 'r1', text: '已完成需求', status: 'done', chatId: 'chat-9', dispatchedTo: 'D:\\ws\\proj-a' }),
    ]);
    mocks.archiveRequirement.mockResolvedValue({ released: true, chatId: 'chat-9' });
    render(<RequirementList />);
    const li = (await screen.findByText('已完成需求')).closest('li') as HTMLElement;
    expect(within(li).getByText(tt('ui.requirements.status_done'))).toBeInTheDocument();
    fireEvent.click(within(li).getByRole('button', { name: tt('ui.requirements.archive_btn') }));

    await waitFor(() => expect(mocks.archiveRequirement).toHaveBeenCalledWith('r1'));
    await waitFor(() =>
      expect(
        useAppStore
          .getState()
          .toasts.some((x) =>
            x.title.includes(
              tt('ui.requirements.archived_released').replace('{{name}}', 'proj-a'),
            ),
          ),
      ).toBe(true),
    );
  });

  it('归档时 released=false：提示会话忙碌未关闭', async () => {
    mocks.listRequirements.mockResolvedValue([
      req({ id: 'r1', text: '已完成需求', status: 'done', chatId: 'chat-9' }),
    ]);
    mocks.archiveRequirement.mockResolvedValue({ released: false, chatId: 'chat-9' });
    render(<RequirementList />);
    fireEvent.click(
      within(await screen.findByText('已完成需求').then((el) => el.closest('li') as HTMLElement)).getByRole(
        'button',
        { name: tt('ui.requirements.archive_btn') },
      ),
    );

    await waitFor(() =>
      expect(
        useAppStore
          .getState()
          .toasts.some((x) =>
            x.title.includes(
              tt('ui.requirements.archived_busy_kept').replace('{{name}}', 'chat-9'),
            ),
          ),
      ).toBe(true),
    );
  });

  it('归档时无 chatId 也无 dispatchedTo：退化为不含 name 的通用成功提示', async () => {
    mocks.listRequirements.mockResolvedValue([
      req({ id: 'r1', text: '已完成需求', status: 'done' }),
    ]);
    mocks.archiveRequirement.mockResolvedValue({ released: true, chatId: '' });
    render(<RequirementList />);
    const li = (await screen.findByText('已完成需求')).closest('li') as HTMLElement;
    fireEvent.click(within(li).getByRole('button', { name: tt('ui.requirements.archive_btn') }));

    await waitFor(() =>
      expect(
        useAppStore
          .getState()
          .toasts.some((x) => x.title === tt('ui.requirements.archived_plain')),
      ).toBe(true),
    );
  });

  it('归档失败（非 done 等）：提示 update_failed', async () => {
    mocks.listRequirements.mockResolvedValue([
      req({ id: 'r1', text: '已完成需求', status: 'done' }),
    ]);
    mocks.archiveRequirement.mockRejectedValue(new Error('not done'));
    render(<RequirementList />);
    const li = (await screen.findByText('已完成需求')).closest('li') as HTMLElement;
    fireEvent.click(within(li).getByRole('button', { name: tt('ui.requirements.archive_btn') }));

    await waitFor(() =>
      expect(
        useAppStore
          .getState()
          .toasts.some((x) =>
            x.title.includes(
              tt('ui.requirements.update_failed').replace('{{err}}', 'not done'),
            ),
          ),
      ).toBe(true),
    );
  });

  it('主列表不含 archived 条目', async () => {
    mocks.listRequirements.mockResolvedValue([
      req({ id: 'r1', text: '活跃需求', status: 'active' }),
      // 真实纳秒量级（Go UnixNano），不是秒
      req({ id: 'r2', text: '已归档需求', status: 'archived', archivedAt: Date.now() * 1e6 }),
    ]);
    render(<RequirementList />);
    expect(await screen.findByText('活跃需求')).toBeInTheDocument();
    expect(screen.queryByText('已归档需求')).not.toBeInTheDocument();
  });

  // ---- 归档视图 ----

  async function openArchiveDialog() {
    // archivedAt 用真实纳秒量级（Go UnixNano），防止假数据掩盖单位错配
    mocks.listRequirements.mockResolvedValue([
      req({
        id: 'a1',
        text: '归档条目甲',
        status: 'archived',
        archivedAt: Date.now() * 1e6 - 10_000_000_000,
        tags: ['前端'],
        dispatchedTo: 'D:\\ws\\proj-a',
      }),
      req({
        id: 'a2',
        text: '归档条目乙',
        status: 'archived',
        archivedAt: Date.now() * 1e6 - 20_000_000_000,
        tags: ['后端'],
        dispatchedTo: 'D:\\ws\\proj-b',
      }),
      req({ id: 'live', text: '活跃需求', status: 'pending' }),
    ]);
    render(<RequirementList />);
    await screen.findByText('活跃需求');
    // 头部归档入口（archive_open），与「+ 新增」按钮并列
    fireEvent.click(screen.getByRole('button', { name: tt('ui.requirements.archive_open') }));
    await screen.findByText(tt('ui.requirements.archived_title'));
    // Radix Portal 渲染在组件树外，归档视图断言统一限定在该弹层内
    const dialogEl = screen.getByText(tt('ui.requirements.archived_title')).closest(
      '[role="dialog"]',
    ) as HTMLElement;
    return within(dialogEl);
  }

  it('归档视图：按 archivedAt 降序列出归档条目，含归档时间与项目名', async () => {
    const dlg = await openArchiveDialog();
    const items = dlg.getAllByText(/归档条目/);
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent('归档条目甲');
    expect(items[1]).toHaveTextContent('归档条目乙');
    // 项目基名展示
    expect(dlg.getAllByText('proj-a').length).toBeGreaterThan(0);
    // 活跃条目不进归档视图
    expect(dlg.queryByText('活跃需求')).not.toBeInTheDocument();
  });

  it('归档视图：存量条目 tags 为 null 渲染与过滤不崩溃', async () => {
    mocks.listRequirements.mockResolvedValue([
      req({
        id: 'a9',
        text: '无标签归档条目',
        status: 'archived',
        archivedAt: Date.now() * 1e6,
        tags: null as unknown as string[],
      }),
    ]);
    render(<RequirementList />);
    // archived 条目不进活跃列表（空态），直接开归档视图
    fireEvent.click(screen.getByRole('button', { name: tt('ui.requirements.archive_open') }));
    await screen.findByText(tt('ui.requirements.archived_title'));
    // tag chip 区为空但不崩；列表条目正常显示
    expect(screen.getByText('无标签归档条目')).toBeInTheDocument();
  });

  it('归档视图：项目下拉过滤', async () => {
    const dlg = await openArchiveDialog();
    // 下拉选项为 dispatchedTo 基名去重
    fireEvent.change(dlg.getByLabelText(tt('ui.requirements.filter_project')), {
      target: { value: 'proj-b' },
    });
    expect(dlg.getByText('归档条目乙')).toBeInTheDocument();
    expect(dlg.queryByText('归档条目甲')).not.toBeInTheDocument();
  });

  it('归档视图：关键字输入过滤（匹配 text 子串）', async () => {
    const dlg = await openArchiveDialog();
    fireEvent.change(dlg.getByPlaceholderText(tt('ui.requirements.filter_keyword')), {
      target: { value: '甲' },
    });
    expect(dlg.getByText('归档条目甲')).toBeInTheDocument();
    expect(dlg.queryByText('归档条目乙')).not.toBeInTheDocument();
  });

  it('归档视图：纳秒时间戳渲染不抛错且时间显示合理', async () => {
    // 回归：Go 侧 ArchivedAt 是 UnixNano（~1.77e18），按秒解析会溢出 Date 抛 RangeError
    const tenMinAgoNs = Date.now() * 1e6 - 10 * 60 * 1e9;
    mocks.listRequirements.mockResolvedValue([
      req({ id: 'a1', text: '纳秒归档条目', status: 'archived', archivedAt: tenMinAgoNs }),
    ]);
    render(<RequirementList />);
    await screen.findByText(tt('ui.requirements.empty'));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.requirements.archive_open') }));
    // 不抛 RangeError 且条目可见；10 分钟前应显示「N minutes ago」类相对时间
    expect(await screen.findByText('纳秒归档条目')).toBeInTheDocument();
    // 相对时间渲染在叶子 span（父容器 textContent 也含文本，getAllByText + 叶子过滤）
    const rels = screen.getAllByText((_, el) =>
      el?.tagName === 'SPAN' && !el.querySelector('span')
        && /\d+ (minutes?|minute) ago|分钟前|just now|刚刚/.test(el.textContent ?? ''),
    );
    expect(rels.length).toBeGreaterThan(0);
  });

  it('归档视图：关键字过滤大小写不敏感', async () => {
    mocks.listRequirements.mockResolvedValue([
      req({ id: 'a1', text: 'Implement Export Feature', status: 'archived', archivedAt: Date.now() * 1e6 }),
    ]);
    render(<RequirementList />);
    // 全 archived 时主列表为空态，等渲染稳定后直接开归档视图
    await screen.findByText(tt('ui.requirements.empty'));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.requirements.archive_open') }));
    await screen.findByText(tt('ui.requirements.archived_title'));
    const dialogEl = screen.getByText(tt('ui.requirements.archived_title')).closest(
      '[role="dialog"]',
    ) as HTMLElement;
    const dlg = within(dialogEl);

    fireEvent.change(dlg.getByPlaceholderText(tt('ui.requirements.filter_keyword')), {
      target: { value: 'export' },
    });
    expect(dlg.getByText('Implement Export Feature')).toBeInTheDocument();
  });

  it('归档视图空态：用独立文案 archived_empty', async () => {
    render(<RequirementList />);
    await screen.findByText(tt('ui.requirements.empty'));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.requirements.archive_open') }));
    await screen.findByText(tt('ui.requirements.archived_title'));
    const dialogEl = screen.getByText(tt('ui.requirements.archived_title')).closest(
      '[role="dialog"]',
    ) as HTMLElement;
    expect(within(dialogEl).getByText(tt('ui.requirements.archived_empty'))).toBeInTheDocument();
  });

  it('归档视图：tag 点击过滤，再次点击取消', async () => {
    const dlg = await openArchiveDialog();
    fireEvent.click(dlg.getByRole('button', { name: '前端' }));
    expect(dlg.getByText('归档条目甲')).toBeInTheDocument();
    expect(dlg.queryByText('归档条目乙')).not.toBeInTheDocument();

    // 再次点击取消选中，恢复全量
    fireEvent.click(dlg.getByRole('button', { name: '前端' }));
    expect(dlg.getByText('归档条目甲')).toBeInTheDocument();
    expect(dlg.getByText('归档条目乙')).toBeInTheDocument();
  });
});
