// App 主框架测试：挂载时调 GetBasket 重建篮子镜像（防刷新漂移）、
// 顶部「设置」固定页签切换（与「首页」并列，不可关闭）。
// api 层整体打桩（vi.mock），与 SessionList.test 同一套模式。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
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
});
