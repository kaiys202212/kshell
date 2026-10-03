// Settings 页面测试：工具检测状态渲染（Source=config-dir 的 generic 工具显示「未验证」徽标、
// 未安装灰显）、providers.yaml 回填编辑保存（成功提示重启生效、YAML 非法显示错误）、
// 「立即重启」按钮走 RestartApp。
// api 层整体打桩（vi.mock），与 SessionList.test 同一套模式。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import Settings from './Settings';
import { useAppStore } from '../state/store';
import type { ToolInfo } from '../lib/api';

const mocks = vi.hoisted(() => ({
  getTools: vi.fn(),
  loadProvidersYAML: vi.fn(),
  saveProvidersYAML: vi.fn(),
  restartApp: vi.fn(),
  getAppearance: vi.fn(),
  setAppearanceMode: vi.fn(),
  getCloseBehavior: vi.fn(),
  setCloseBehavior: vi.fn(),
}));
vi.mock('../lib/api', () => mocks);

const tools: ToolInfo[] = [
  {
    ID: 'codebuddy',
    Name: 'CodeBuddy',
    BinPath: 'C:\\bin\\codebuddy.exe',
    Version: '2.0.0',
    Installed: true,
    Source: 'path',
  },
  {
    ID: 'mytool',
    Name: 'MyTool',
    BinPath: '',
    Version: '',
    Installed: true,
    Source: 'config-dir',
  },
  {
    ID: 'gemini',
    Name: 'Gemini',
    BinPath: '',
    Version: '',
    Installed: false,
    Source: '',
  },
];

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  mocks.getTools.mockResolvedValue(tools);
  mocks.loadProvidersYAML.mockResolvedValue('providers: []\n');
  mocks.saveProvidersYAML.mockResolvedValue(undefined);
  mocks.getAppearance.mockResolvedValue({ mode: 'system', resolved: 'dark' });
  mocks.setAppearanceMode.mockResolvedValue(undefined);
  mocks.getCloseBehavior.mockResolvedValue('tray');
  mocks.setCloseBehavior.mockResolvedValue(undefined);
});

describe('Settings', () => {
  it('渲染工具检测状态：已安装带版本，未安装灰显「未安装」', async () => {
    render(<Settings />);

    const codebuddy = await screen.findByText('CodeBuddy');
    expect(codebuddy.closest('li')).toHaveTextContent('2.0.0');
    expect(codebuddy.closest('li')).not.toHaveTextContent('未验证');
    expect(codebuddy.closest('li')).not.toHaveTextContent('未安装');

    const gemini = screen.getByText('Gemini');
    expect(gemini.closest('li')).toHaveTextContent('未安装');
    expect(gemini.closest('li')).toHaveClass('opacity-50');
  });

  it('Source=config-dir 的 generic 工具显示「未验证」徽标', async () => {
    render(<Settings />);

    const mytool = await screen.findByText('MyTool');
    expect(mytool.closest('li')).toHaveTextContent('未验证');
    expect(mytool.closest('li')).toHaveAttribute('title', expect.stringContaining('只检测到配置目录'));
  });

  it('providers.yaml 回填编辑器，编辑后保存调用 SaveProvidersYAML 并提示重启生效', async () => {
    render(<Settings />);

    const editor = await screen.findByLabelText('providers.yaml 编辑器');
    expect(editor).toHaveValue('providers: []\n');

    fireEvent.change(editor, { target: { value: 'providers:\n  - name: foo\n' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '保存' }));
    });

    expect(mocks.saveProvidersYAML).toHaveBeenCalledWith('providers:\n  - name: foo\n');
    expect(await screen.findByText(/已保存.*重启/)).toBeInTheDocument();
  });

  it('保存失败（YAML 解析失败等）时显示错误，不显示成功提示', async () => {
    mocks.saveProvidersYAML.mockRejectedValue(new Error('YAML 解析失败：line 1: bad indent'));
    render(<Settings />);

    const editor = await screen.findByLabelText('providers.yaml 编辑器');
    fireEvent.change(editor, { target: { value: 'bad: [' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '保存' }));
    });

    expect(await screen.findByText(/YAML 解析失败：line 1: bad indent/)).toBeInTheDocument();
    expect(screen.queryByText(/已保存/)).not.toBeInTheDocument();
  });

  it('GetTools / LoadProvidersYAML 失败时分别显示错误，不崩溃', async () => {
    mocks.getTools.mockRejectedValue(new Error('绑定异常'));
    mocks.loadProvidersYAML.mockRejectedValue(new Error('读取失败'));
    render(<Settings />);

    expect(await screen.findByText(/绑定异常/)).toBeInTheDocument();
    expect(await screen.findByText(/读取失败/)).toBeInTheDocument();
  });

  it('保存成功后可「立即重启」：调用 RestartApp；失败恢复按钮并以 error 语气提示', async () => {
    useAppStore.setState({ toasts: [] });
    mocks.restartApp.mockRejectedValueOnce(new Error('启动失败'));
    render(<Settings />);

    const editor = await screen.findByLabelText('providers.yaml 编辑器');
    fireEvent.change(editor, { target: { value: 'providers: []\n' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '保存' }));
    });
    expect(await screen.findByText('立即重启')).toBeInTheDocument();

    // 失败：恢复按钮可再试，并轻量提示
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '立即重启' }));
    });
    expect(mocks.restartApp).toHaveBeenCalledTimes(1);
    await waitFor(() => {
      expect(
        useAppStore.getState().toasts.some((t) => t.tone === 'error' && t.title === '重启失败'),
      ).toBe(true);
    });
    expect(screen.getByRole('button', { name: '立即重启' })).toBeEnabled();

    // 成功：按钮进入「正在重启…」禁用态等待进程退出
    mocks.restartApp.mockResolvedValueOnce(undefined);
    fireEvent.click(screen.getByRole('button', { name: '立即重启' }));
    expect(await screen.findByText('正在重启…')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '正在重启…' })).toBeDisabled();
  });

  it('外观选择调用 SetAppearanceMode', async () => {
    render(<Settings />);
    const darkBtn = await screen.findByRole('button', { name: '深色' });
    await act(async () => {
      fireEvent.click(darkBtn);
    });
    expect(mocks.setAppearanceMode).toHaveBeenCalledWith('dark');
  });

  it('关闭行为默认收进托盘，切换为直接退出调用 SetCloseBehavior', async () => {
    useAppStore.setState({ toasts: [] });
    render(<Settings />);

    const trayBtn = await screen.findByRole('button', { name: '收进托盘' });
    expect(trayBtn).toHaveAttribute('aria-pressed', 'true');

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '直接退出' }));
    });
    expect(mocks.setCloseBehavior).toHaveBeenCalledWith('exit');
    expect(screen.getByRole('button', { name: '直接退出' })).toHaveAttribute('aria-pressed', 'true');
  });

  it('加载时回填关闭行为：返回 exit 时「直接退出」选中', async () => {
    mocks.getCloseBehavior.mockResolvedValueOnce('exit');
    render(<Settings />);

    expect(await screen.findByRole('button', { name: '直接退出' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('button', { name: '收进托盘' })).toHaveAttribute('aria-pressed', 'false');
  });

  it('切换关闭行为失败时提示错误且保持原选中态', async () => {
    useAppStore.setState({ toasts: [] });
    mocks.setCloseBehavior.mockRejectedValueOnce(new Error('写盘失败'));
    render(<Settings />);

    await screen.findByRole('button', { name: '收进托盘' });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '直接退出' }));
    });
    await waitFor(() => {
      expect(
        useAppStore.getState().toasts.some((t) => t.tone === 'error' && t.title === '写盘失败'),
      ).toBe(true);
    });
    expect(screen.getByRole('button', { name: '收进托盘' })).toHaveAttribute('aria-pressed', 'true');
  });
});
