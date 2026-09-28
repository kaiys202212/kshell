// 自绘标题栏测试：页签切换/关闭、设置按钮贴最右（在页签之后、窗口控件之前）、
// 自绘窗口控件走 Wails runtime、双击标题栏空白处最大化。
// Wails runtime 整体打桩：这些函数在纯 jsdom 里不存在。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import TitleBar from './TitleBar';
import { SETTINGS_TAB_ID } from '../state/store';

const runtime = vi.hoisted(() => ({
  WindowMinimise: vi.fn(),
  WindowToggleMaximise: vi.fn(),
  WindowIsMaximised: vi.fn(() => Promise.resolve(false)),
  WindowHide: vi.fn(),
}));
vi.mock('../../wailsjs/runtime/runtime', () => runtime);

const tabs = [
  { id: 'D:\\proj-a', name: 'proj-a' },
  { id: 'D:\\proj-b', name: 'proj-b' },
];

function setup(props: Partial<React.ComponentProps<typeof TitleBar>> = {}) {
  const onSelectTab = vi.fn();
  const onCloseTab = vi.fn();
  render(
    <TitleBar
      tabs={tabs}
      activeTabId={null}
      onSelectTab={onSelectTab}
      onCloseTab={onCloseTab}
      {...props}
    />,
  );
  return { onSelectTab, onCloseTab };
}

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  runtime.WindowIsMaximised.mockResolvedValue(false);
});

describe('TitleBar', () => {
  it('渲染首页 / 工作区页签 / 设置 / 三个窗口控件', () => {
    setup();

    expect(screen.getByRole('button', { name: '首页' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'proj-a' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'proj-b' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '设置' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '最小化' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '最大化' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '关闭' })).toBeInTheDocument();
  });

  it('设置按钮排在页签之后、窗口控件之前（贴最右语义）', () => {
    setup();

    const settings = screen.getByRole('button', { name: '设置' });
    const lastTab = screen.getByRole('button', { name: 'proj-b' });
    const minimise = screen.getByRole('button', { name: '最小化' });

    // compareDocumentPosition：settings 在 lastTab 之后（DOCUMENT_POSITION_PRECEDING 位）
    expect(lastTab.compareDocumentPosition(settings) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(settings.compareDocumentPosition(minimise) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('点页签回调 onSelectTab（首页回 null），点关闭钮回调 onCloseTab', () => {
    const { onSelectTab, onCloseTab } = setup();

    fireEvent.click(screen.getByRole('button', { name: 'proj-a' }));
    expect(onSelectTab).toHaveBeenCalledWith('D:\\proj-a');

    fireEvent.click(screen.getByRole('button', { name: '首页' }));
    expect(onSelectTab).toHaveBeenCalledWith(null);

    fireEvent.click(screen.getByRole('button', { name: '设置' }));
    expect(onSelectTab).toHaveBeenCalledWith(SETTINGS_TAB_ID);

    fireEvent.click(screen.getByRole('button', { name: '关闭 proj-a' }));
    expect(onCloseTab).toHaveBeenCalledWith('D:\\proj-a');
    expect(onSelectTab).toHaveBeenCalledTimes(3); // 关闭钮不触发选中
  });

  it('工作区页签中键关闭，左键不关闭', () => {
    const { onCloseTab } = setup();
    const wrapper = screen.getByRole('button', { name: 'proj-a' }).closest('div.group')!;

    fireEvent(wrapper, new MouseEvent('auxclick', { button: 0, bubbles: true }));
    expect(onCloseTab).not.toHaveBeenCalled();

    fireEvent(wrapper, new MouseEvent('auxclick', { button: 1, bubbles: true }));
    expect(onCloseTab).toHaveBeenCalledWith('D:\\proj-a');
  });

  it('窗口控件调用 Wails runtime：最小化 / 最大化切换 / 关闭=收进托盘', () => {
    setup();

    fireEvent.click(screen.getByRole('button', { name: '最小化' }));
    expect(runtime.WindowMinimise).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole('button', { name: '最大化' }));
    expect(runtime.WindowToggleMaximise).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole('button', { name: '关闭' }));
    expect(runtime.WindowHide).toHaveBeenCalledTimes(1);
  });

  it('已最大化时按钮语义变为「还原」（WindowIsMaximised 查询结果驱动）', async () => {
    runtime.WindowIsMaximised.mockResolvedValue(true);
    setup();

    expect(await screen.findByRole('button', { name: '还原' })).toBeInTheDocument();
  });

  it('双击标题栏空白处最大化；双击页签不触发', () => {
    setup();

    fireEvent.doubleClick(screen.getByRole('button', { name: '首页' }));
    expect(runtime.WindowToggleMaximise).not.toHaveBeenCalled();

    // 双击拖拽区（nav 容器本身，非按钮）
    const nav = screen.getByLabelText('页签');
    fireEvent.doubleClick(nav);
    expect(runtime.WindowToggleMaximise).toHaveBeenCalledTimes(1);
  });

  it('WindowIsMaximised 在非 Wails 环境（未返回 Promise）时不抛错', async () => {
    runtime.WindowIsMaximised.mockReturnValue(undefined as never);
    setup();

    await waitFor(() => expect(screen.getByRole('button', { name: '最大化' })).toBeInTheDocument());
  });
});
