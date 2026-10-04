// Settings 页面测试：分区导航、工具检测、providers.yaml、外观/关闭/会话/权限、模型预设。
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
  setAppearanceFontSize: vi.fn(),
  getCloseBehavior: vi.fn(),
  setCloseBehavior: vi.fn(),
  getModelConfig: vi.fn(),
  setModelConfig: vi.fn(),
  listModelPresets: vi.fn(),
  getSessionMode: vi.fn(),
  setSessionMode: vi.fn(),
  getPermissionMode: vi.fn(),
  setPermissionMode: vi.fn(),
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

function goTools() {
  fireEvent.click(screen.getByRole('button', { name: '工具' }));
}

function goModel() {
  fireEvent.click(screen.getByRole('button', { name: '模型' }));
}

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  mocks.getTools.mockResolvedValue(tools);
  mocks.loadProvidersYAML.mockResolvedValue('providers: []\n');
  mocks.saveProvidersYAML.mockResolvedValue(undefined);
  mocks.getAppearance.mockResolvedValue({ mode: 'dark', resolved: 'dark', fontSize: 13 });
  mocks.setAppearanceMode.mockResolvedValue(undefined);
  mocks.setAppearanceFontSize.mockResolvedValue(undefined);
  mocks.getCloseBehavior.mockResolvedValue('tray');
  mocks.setCloseBehavior.mockResolvedValue(undefined);
  mocks.getSessionMode.mockResolvedValue('tui');
  mocks.setSessionMode.mockResolvedValue(undefined);
  mocks.getPermissionMode.mockResolvedValue('default');
  mocks.setPermissionMode.mockResolvedValue(undefined);
  mocks.listModelPresets.mockResolvedValue([
    { ID: 'custom', Name: '自定义', OpenAIBaseURL: '', AnthropicBaseURL: '', RecommendedModel: '', Note: '' },
    {
      ID: 'deepseek',
      Name: 'DeepSeek',
      OpenAIBaseURL: 'https://api.deepseek.com',
      AnthropicBaseURL: 'https://api.deepseek.com/anthropic',
      RecommendedModel: 'deepseek-chat',
      Note: '',
    },
  ]);
  mocks.getModelConfig.mockResolvedValue({
    Enabled: false,
    Preset: '',
    OpenAIBaseURL: '',
    AnthropicBaseURL: '',
    Agents: {},
    APIKeySet: false,
  });
  mocks.setModelConfig.mockResolvedValue(undefined);
});

describe('Settings', () => {
  it('渲染工具检测状态：已安装带版本，未安装灰显「未安装」', async () => {
    render(<Settings />);
    goTools();

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
    goTools();

    const mytool = await screen.findByText('MyTool');
    expect(mytool.closest('li')).toHaveTextContent('未验证');
    expect(mytool.closest('li')).toHaveAttribute('title', expect.stringContaining('只检测到配置目录'));
  });

  it('providers.yaml 回填编辑器，编辑后保存调用 SaveProvidersYAML 并提示重启生效', async () => {
    render(<Settings />);
    goTools();

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
    goTools();

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
    goTools();

    expect(await screen.findByText(/绑定异常/)).toBeInTheDocument();
    expect(await screen.findByText(/读取失败/)).toBeInTheDocument();
  });

  it('保存成功后可「立即重启」：调用 RestartApp；失败恢复按钮并以 error 语气提示', async () => {
    useAppStore.setState({ toasts: [] });
    mocks.restartApp.mockRejectedValueOnce(new Error('启动失败'));
    render(<Settings />);
    goTools();

    const editor = await screen.findByLabelText('providers.yaml 编辑器');
    fireEvent.change(editor, { target: { value: 'providers: []\n' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '保存' }));
    });
    expect(await screen.findByText('立即重启')).toBeInTheDocument();

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

    mocks.restartApp.mockResolvedValueOnce(undefined);
    fireEvent.click(screen.getByRole('button', { name: '立即重启' }));
    expect(await screen.findByText('正在重启…')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '正在重启…' })).toBeDisabled();
  });

  it('外观选择调用 SetAppearanceMode', async () => {
    render(<Settings />);
    const darkBtn = await screen.findByRole('button', { name: '深色' });
    // 默认已是深色，切到浅色再切回
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '浅色' }));
    });
    expect(mocks.setAppearanceMode).toHaveBeenCalledWith('light');
    await act(async () => {
      fireEvent.click(darkBtn);
    });
    expect(mocks.setAppearanceMode).toHaveBeenCalledWith('dark');
  });

  it('字号滑条拖动预览不落盘，松手后调用 SetAppearanceFontSize', async () => {
    render(<Settings />);
    const slider = await screen.findByLabelText('界面字号');
    expect(slider).toHaveValue('13');
    expect(screen.getByText('13px')).toBeInTheDocument();

    fireEvent.change(slider, { target: { value: '16' } });
    expect(screen.getByText('16px')).toBeInTheDocument();
    expect(mocks.setAppearanceFontSize).not.toHaveBeenCalled();

    await act(async () => {
      fireEvent.pointerUp(slider);
    });
    expect(mocks.setAppearanceFontSize).toHaveBeenCalledWith(16);
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

  it('会话模式与权限模式可切换', async () => {
    render(<Settings />);
    await screen.findByRole('button', { name: 'TUI（终端）' });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'ACP（聊天）' }));
    });
    expect(mocks.setSessionMode).toHaveBeenCalledWith('acp');
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Bypass（跳过确认）' }));
    });
    expect(mocks.setPermissionMode).toHaveBeenCalledWith('bypass');
    expect(await screen.findByText(/仅建议在可信环境/)).toBeInTheDocument();
  });

  it('模型区回填双 URL、密钥掩码、留空保存', async () => {
    mocks.getModelConfig.mockResolvedValue({
      Enabled: true,
      Preset: 'deepseek',
      OpenAIBaseURL: 'https://oai/',
      AnthropicBaseURL: 'https://ant/',
      Agents: { claude: 'mimo-v2.5' },
      APIKeySet: true,
    });
    render(<Settings />);
    goModel();

    expect(await screen.findByLabelText('OpenAI Base URL')).toHaveValue('https://oai/');
    expect(screen.getByLabelText('Anthropic Base URL')).toHaveValue('https://ant/');
    expect(screen.getByLabelText('模型 API Key')).toHaveValue('');
    expect(screen.getByPlaceholderText(/已设置/)).toBeInTheDocument();
    expect(screen.getByLabelText('Claude Code 模型')).toHaveValue('mimo-v2.5');

    fireEvent.click(screen.getByRole('button', { name: '保存模型配置' }));
    await waitFor(() =>
      expect(mocks.setModelConfig).toHaveBeenCalledWith(
        expect.objectContaining({
          Enabled: true,
          OpenAIBaseURL: 'https://oai/',
          AnthropicBaseURL: 'https://ant/',
          APIKey: '',
          ClearAPIKey: false,
          Agents: { claude: 'mimo-v2.5' },
        }),
      ),
    );
  });

  it('选择预设填入双 URL 与空的模型槽', async () => {
    render(<Settings />);
    goModel();
    const sel = await screen.findByLabelText('提供商预设');
    await act(async () => {
      fireEvent.change(sel, { target: { value: 'deepseek' } });
    });
    expect(screen.getByLabelText('OpenAI Base URL')).toHaveValue('https://api.deepseek.com');
    expect(screen.getByLabelText('Anthropic Base URL')).toHaveValue(
      'https://api.deepseek.com/anthropic',
    );
    expect(screen.getByLabelText('Claude Code 模型')).toHaveValue('deepseek-chat');
  });

  it('勾选清除密钥时提交 ClearAPIKey', async () => {
    mocks.getModelConfig.mockResolvedValue({
      Enabled: false,
      Preset: '',
      OpenAIBaseURL: '',
      AnthropicBaseURL: '',
      Agents: {},
      APIKeySet: true,
    });
    render(<Settings />);
    goModel();
    fireEvent.click(await screen.findByLabelText('清除密钥'));
    fireEvent.click(screen.getByRole('button', { name: '保存模型配置' }));
    await waitFor(() =>
      expect(mocks.setModelConfig).toHaveBeenCalledWith(expect.objectContaining({ ClearAPIKey: true })),
    );
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
