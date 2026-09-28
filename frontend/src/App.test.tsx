// App 主框架测试：挂载时调 GetBasket 重建篮子镜像（防刷新漂移）、
// 顶部「设置」固定页签切换（与「首页」并列，不可关闭）、
// 全局快捷键 Ctrl+K 打开快速切换器 / Ctrl+F 派发聚焦搜索事件。
// api 层整体打桩（vi.mock），与 SessionList.test 同一套模式。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import App from './App';
import { SETTINGS_TAB_ID, useAppStore } from './state/store';

const mocks = vi.hoisted(() => ({
  getWorkspaces: vi.fn(),
  scanSessions: vi.fn(),
  onScanDone: vi.fn(),
  getBasket: vi.fn(),
  getTools: vi.fn(),
  loadProvidersYAML: vi.fn(),
  saveProvidersYAML: vi.fn(),
}));
vi.mock('./lib/api', () => mocks);

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  mocks.getWorkspaces.mockResolvedValue([]);
  mocks.scanSessions.mockResolvedValue(undefined);
  mocks.onScanDone.mockImplementation(() => () => {});
  mocks.getBasket.mockResolvedValue(['D:\\proj-a\\README.md', 'D:\\proj-a\\src\\main.ts']);
  mocks.getTools.mockResolvedValue([]);
  mocks.loadProvidersYAML.mockResolvedValue('');
  useAppStore.setState({
    openTabs: [],
    activeTabId: null,
    basket: [],
    toasts: [],
    windowStatus: {},
    scanState: 'idle',
  });
});

describe('App', () => {
  it('挂载时调 GetBasket 重建 store 篮子镜像（解决刷新后镜像漂移）', async () => {
    render(<App />);

    await waitFor(() => {
      expect(useAppStore.getState().basket).toEqual([
        'D:\\proj-a\\README.md',
        'D:\\proj-a\\src\\main.ts',
      ]);
    });
    expect(mocks.getBasket).toHaveBeenCalledTimes(1);
  });

  it('顶部有固定的「设置」页签，点击切换到设置页，再点「首页」切回', async () => {
    render(<App />);

    // 初始显示首页内容
    expect(screen.getByText('工作区')).toBeInTheDocument();
    expect(screen.queryByText('工具检测')).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: '设置' }));
    await waitFor(() => {
      expect(useAppStore.getState().activeTabId).toBe(SETTINGS_TAB_ID);
    });
    expect(await screen.findByText('工具检测')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: '首页' }));
    expect(useAppStore.getState().activeTabId).toBeNull();
    expect(screen.getByText('工作区')).toBeInTheDocument();
  });

  it('Ctrl+K 打开快速切换器（大小写 K 均可触发）', () => {
    // 本文件 store 无工作区/页签数据：面板打开后应显示引导空态
    render(<App />);

    fireEvent.keyDown(window, { key: 'k', ctrlKey: true });
    expect(screen.getByLabelText('搜索页签或工作区')).toBeInTheDocument();
    expect(screen.getByText('暂无工作区，请先在首页完成扫描')).toBeInTheDocument();
  });

  it('Ctrl+F 仅在工作区页签内派发 kshell:focus-search（首页/设置页不派发）', () => {
    const spy = vi.fn();
    window.addEventListener('kshell:focus-search', spy);

    render(<App />);
    fireEvent.keyDown(window, { key: 'f', ctrlKey: true }); // 首页
    fireEvent.keyDown(window, { key: 'f', ctrlKey: true }); // 设置页
    expect(spy).not.toHaveBeenCalled();

    // 激活工作区页签后再按：派发一次
    useAppStore.setState({ activeTabId: 'D:\\proj-a' });
    fireEvent.keyDown(window, { key: 'f', ctrlKey: true });
    expect(spy).toHaveBeenCalledTimes(1);

    window.removeEventListener('kshell:focus-search', spy);
  });

  it('工作区页签中键（button=1）关闭，左键不关闭', () => {
    useAppStore.setState({ openTabs: [{ id: 'D:\\proj-a', name: 'proj-a' }], activeTabId: null });
    render(<App />);

    // RTL 无 auxClick 快捷方法，直接派生 MouseEvent（须冒泡到 React 根监听）
    const auxClick = (el: Element, button: number) =>
      fireEvent(el, new MouseEvent('auxclick', { button, bubbles: true }));

    // 页签容器 div（含关闭钮的父级）
    const tab = screen.getByText('proj-a').closest('div.group')!;
    auxClick(tab, 1);
    expect(useAppStore.getState().openTabs).toHaveLength(0);

    // 左键 auxclick 不触发关闭（act 包裹保证同步重渲染后再查询）
    act(() => {
      useAppStore.setState({ openTabs: [{ id: 'D:\\proj-a', name: 'proj-a' }] });
    });
    const tab2 = screen.getByText('proj-a').closest('div.group')!;
    auxClick(tab2, 0);
    expect(useAppStore.getState().openTabs).toHaveLength(1);
  });
});
