// Settings 页面测试：分区导航、工具检测、providers.yaml、外观/关闭/会话/权限、模型预设。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
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
  getToolInstallRecipe: vi.fn(),
  installBuiltinTool: vi.fn(),
  uninstallBuiltinTool: vi.fn(),
  getToolInstallJob: vi.fn(),
  onToolInstallLog: vi.fn(),
  onToolInstallDone: vi.fn(),
  onScanDone: vi.fn(),
  onToolsUpdated: vi.fn(),
  getAppVersion: vi.fn(),
  checkForUpdate: vi.fn(),
  applyUpdate: vi.fn(),
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
  {
    ID: 'claude',
    Name: 'Claude Code',
    BinPath: 'C:\\npm\\claude.cmd',
    Version: '1.0.0',
    Installed: true,
    Source: 'path',
  },
];

const recipes: Record<
  string,
  {
    ToolID: string;
    Name: string;
    InstallCmd: string;
    UninstallCmd: string;
    PurgeDirs: string[];
    CanPurge: boolean;
  }
> = {
  gemini: {
    ToolID: 'gemini',
    Name: 'Gemini',
    InstallCmd: 'npm install -g @google/gemini-cli',
    UninstallCmd: 'npm uninstall -g @google/gemini-cli',
    PurgeDirs: ['~/.gemini'],
    CanPurge: true,
  },
  claude: {
    ToolID: 'claude',
    Name: 'Claude Code',
    InstallCmd: 'npm install -g @anthropic-ai/claude-code',
    UninstallCmd: 'npm uninstall -g @anthropic-ai/claude-code',
    PurgeDirs: ['~/.claude'],
    CanPurge: true,
  },
  cursor: {
    ToolID: 'cursor',
    Name: 'Cursor',
    InstallCmd: "irm 'https://cursor.com/install?win32=true' | iex",
    UninstallCmd: '',
    PurgeDirs: [],
    CanPurge: false,
  },
};

let logCb: ((p: { toolID: string; text: string }) => void) | undefined;
let doneCb: ((p: { toolID: string; action: string; ok: boolean; error?: string }) => void) | undefined;
let scanDoneCb: ((payload?: unknown) => void) | undefined;
let toolsUpdatedCb: (() => void) | undefined;

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
  mocks.getAppVersion.mockResolvedValue('dev');
  mocks.checkForUpdate.mockResolvedValue({
    Current: 'dev',
    Latest: '',
    Notes: '',
    Source: '',
    Available: false,
    Skipped: true,
    Reason: '开发构建不检查更新',
  });
  mocks.applyUpdate.mockResolvedValue(undefined);
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
  mocks.getToolInstallJob.mockResolvedValue({
    ToolID: '',
    Action: '',
    Running: false,
    Log: '',
    Error: '',
  });
  mocks.getToolInstallRecipe.mockImplementation(async (id: string) => {
    const r = recipes[id];
    if (!r) throw new Error('该工具不支持一键安装');
    return r;
  });
  mocks.installBuiltinTool.mockResolvedValue(undefined);
  mocks.uninstallBuiltinTool.mockResolvedValue(undefined);
  logCb = undefined;
  doneCb = undefined;
  scanDoneCb = undefined;
  toolsUpdatedCb = undefined;
  mocks.onScanDone.mockImplementation((cb: NonNullable<typeof scanDoneCb>) => {
    scanDoneCb = cb;
    return () => {
      scanDoneCb = undefined;
    };
  });
  mocks.onToolsUpdated.mockImplementation((cb: NonNullable<typeof toolsUpdatedCb>) => {
    toolsUpdatedCb = cb;
    return () => {
      toolsUpdatedCb = undefined;
    };
  });
  mocks.onToolInstallLog.mockImplementation((cb: NonNullable<typeof logCb>) => {
    logCb = cb;
    return () => {
      logCb = undefined;
    };
  });
  mocks.onToolInstallDone.mockImplementation((cb: NonNullable<typeof doneCb>) => {
    doneCb = cb;
    return () => {
      doneCb = undefined;
    };
  });
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

    const exitBtn = await screen.findByRole('button', { name: '直接退出' });
    await waitFor(() => {
      expect(exitBtn).toHaveAttribute('aria-pressed', 'true');
    });
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

  it('内置工具按 BinPath 显示安装/卸载，自定义工具无按钮', async () => {
    render(<Settings />);
    goTools();

    const claude = (await screen.findByText('Claude Code')).closest('li')!;
    expect(within(claude).getByRole('button', { name: '卸载' })).toBeInTheDocument();

    const gemini = screen.getByText('Gemini').closest('li')!;
    expect(within(gemini).getByRole('button', { name: '安装' })).toBeInTheDocument();

    const codebuddy = screen.getByText('CodeBuddy').closest('li')!;
    expect(within(codebuddy).queryByRole('button', { name: '安装' })).not.toBeInTheDocument();
    expect(within(codebuddy).queryByRole('button', { name: '卸载' })).not.toBeInTheDocument();

    const mytool = screen.getByText('MyTool').closest('li')!;
    expect(within(mytool).queryByRole('button', { name: '安装' })).not.toBeInTheDocument();
    expect(within(mytool).queryByRole('button', { name: '卸载' })).not.toBeInTheDocument();
  });

  it('点 Gemini 安装调用 installBuiltinTool 且按钮变为安装中…', async () => {
    render(<Settings />);
    goTools();
    const gemini = (await screen.findByText('Gemini')).closest('li')!;
    await act(async () => {
      fireEvent.click(within(gemini).getByRole('button', { name: '安装' }));
    });
    expect(mocks.installBuiltinTool).toHaveBeenCalledWith('gemini');
    expect(within(gemini).getByRole('button', { name: '安装中…' })).toBeInTheDocument();
  });

  it('点 Claude 卸载弹出确认，默认不清配置', async () => {
    render(<Settings />);
    goTools();
    const claude = (await screen.findByText('Claude Code')).closest('li')!;
    await act(async () => {
      fireEvent.click(within(claude).getByRole('button', { name: '卸载' }));
    });
    expect(await screen.findByText('卸载 Claude Code')).toBeInTheDocument();
    expect(screen.getByLabelText('同时清除配置')).not.toBeChecked();
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '确认卸载' }));
    });
    expect(mocks.uninstallBuiltinTool).toHaveBeenCalledWith('claude', false);
  });

  it('勾选同时清除配置后确认并列出目录', async () => {
    render(<Settings />);
    goTools();
    const claude = (await screen.findByText('Claude Code')).closest('li')!;
    await act(async () => {
      fireEvent.click(within(claude).getByRole('button', { name: '卸载' }));
    });
    await screen.findByText('卸载 Claude Code');
    fireEvent.click(screen.getByLabelText('同时清除配置'));
    expect(screen.getByText('~/.claude')).toBeInTheDocument();
    expect(screen.getByText(/将删除会话历史/)).toBeInTheDocument();
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '确认卸载' }));
    });
    expect(mocks.uninstallBuiltinTool).toHaveBeenCalledWith('claude', true);
  });

  it('cursor 卸载确认无同时清除配置', async () => {
    mocks.getTools.mockResolvedValue([
      ...tools,
      {
        ID: 'cursor',
        Name: 'Cursor',
        BinPath: 'C:\\cursor-agent.exe',
        Version: '1.0.0',
        Installed: true,
        Source: 'path',
      },
    ]);
    render(<Settings />);
    goTools();
    const cursor = (await screen.findByText('Cursor')).closest('li')!;
    await act(async () => {
      fireEvent.click(within(cursor).getByRole('button', { name: '卸载' }));
    });
    expect(await screen.findByText('卸载 Cursor')).toBeInTheDocument();
    expect(screen.queryByLabelText('同时清除配置')).not.toBeInTheDocument();
  });

  it('安装日志出现且完成后重刷工具列表', async () => {
    render(<Settings />);
    goTools();
    const gemini = (await screen.findByText('Gemini')).closest('li')!;
    await act(async () => {
      fireEvent.click(within(gemini).getByRole('button', { name: '安装' }));
    });
    const before = mocks.getTools.mock.calls.length;
    await act(async () => {
      logCb?.({ toolID: 'gemini', text: 'npm installing gemini-cli' });
    });
    expect(await screen.findByText(/npm installing gemini-cli/)).toBeInTheDocument();
    await act(async () => {
      doneCb?.({ toolID: 'gemini', action: 'install', ok: true });
    });
    await waitFor(() => {
      expect(mocks.getTools.mock.calls.length).toBeGreaterThan(before);
    });
  });

  it('收到 tools:updated 后重刷工具列表', async () => {
    render(<Settings />);
    goTools();
    await screen.findByText('Gemini');
    const before = mocks.getTools.mock.calls.length;
    expect(toolsUpdatedCb).toBeTypeOf('function');
    await act(async () => {
      toolsUpdatedCb?.();
    });
    await waitFor(() => {
      expect(mocks.getTools.mock.calls.length).toBeGreaterThan(before);
    });
  });

  it('收到 scan:done 后重刷工具列表', async () => {
    render(<Settings />);
    goTools();
    await screen.findByText('Gemini');
    const before = mocks.getTools.mock.calls.length;
    expect(scanDoneCb).toBeTypeOf('function');
    await act(async () => {
      scanDoneCb?.({});
    });
    await waitFor(() => {
      expect(mocks.getTools.mock.calls.length).toBeGreaterThan(before);
    });
  });

  it('通用分区显示关于与当前版本', async () => {
    mocks.getAppVersion.mockResolvedValue('v0.1.0');
    render(<Settings />);
    expect(await screen.findByRole('heading', { name: '关于' })).toBeInTheDocument();
    expect(await screen.findByText(/v0\.1\.0/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '检查更新' })).toBeInTheDocument();
  });

  it('检查更新后展示新版本并可立即升级', async () => {
    mocks.getAppVersion.mockResolvedValue('v0.1.0');
    mocks.checkForUpdate.mockResolvedValue({
      Current: 'v0.1.0',
      Latest: 'v0.2.0',
      Notes: '修复升级',
      Source: 'ghfast',
      Available: true,
      Skipped: false,
      Reason: '',
    });
    render(<Settings />);
    await screen.findByRole('heading', { name: '关于' });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '检查更新' }));
    });
    expect(await screen.findByText(/v0\.2\.0/)).toBeInTheDocument();
    expect(screen.getByText(/来源：ghfast/)).toBeInTheDocument();
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '立即升级' }));
    });
    expect(mocks.applyUpdate).toHaveBeenCalledTimes(1);
  });
});
