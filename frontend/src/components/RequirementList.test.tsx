// RequirementList 组件测试：条目渲染（文本/优先级点/标签/已派发徽标）/ 新增流程 /
// 编辑流程 / 删除流程 / 空态 / dragstart 设置 dataTransfer / requirements:changed 重拉。
// api 层整体打桩（vi.mock），与 Home.test.tsx 同一套模式。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import RequirementList from './RequirementList';
import type { Requirement } from '../lib/api';
import { tt } from '../test/i18n';

const mocks = vi.hoisted(() => ({
  listRequirements: vi.fn(),
  addRequirement: vi.fn(),
  updateRequirement: vi.fn(),
  deleteRequirement: vi.fn(),
  markRequirementDispatched: vi.fn(),
  onRequirementsChanged: vi.fn(),
}));
vi.mock('../lib/api', () => mocks);

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
  mocks.markRequirementDispatched.mockResolvedValue(undefined);
  mocks.onRequirementsChanged.mockImplementation(() => () => {});
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
});
