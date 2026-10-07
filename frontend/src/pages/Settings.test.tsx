// Settings 页面测试：分区导航、工具检测、providers.yaml、外观/关闭/会话/权限、模型预设。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import Settings from './Settings';
import { useAppStore } from '../state/store';
import { initI18n } from '../i18n';
import { tt } from '../test/i18n';
import type { ToolInfo } from '../lib/api';

const mocks = vi.hoisted(() => ({
  getTools: vi.fn(),
  loadProvidersYAML: vi.fn(),
  saveProvidersYAML: vi.fn(),
  parseProvidersYAML: vi.fn(),
  formatProvidersYAML: vi.fn(),
  pickDirectory: vi.fn(),
  pickFile: vi.fn(),
  restartApp: vi.fn(),
  getAppearance: vi.fn(),
  setAppearanceMode: vi.fn(),
  setAppearanceFontSize: vi.fn(),
  // 语言接线（getLanguage / onLanguageChanged）在 main.tsx bootstrap，不在此文件测
  setLanguage: vi.fn(),
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
  scanSessions: vi.fn(),
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
  codebuddy: {
    ToolID: 'codebuddy',
    Name: 'CodeBuddy',
    InstallCmd: 'npm install -g @tencent-ai/codebuddy-code',
    UninstallCmd: 'npm uninstall -g @tencent-ai/codebuddy-code',
    PurgeDirs: ['~/.codebuddy'],
    CanPurge: true,
  },
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
  fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.nav.tools') }));
}

function goModel() {
  fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.nav.model') }));
}

afterEach(cleanup);

// 外部语言用例会经 initI18n 注册临时语言（如 ja），跑完还原为仅内置，
// 避免污染同文件后续用例的 getLanguageOptions / 资源库
afterEach(async () => {
  await initI18n('en', {});
});

beforeEach(() => {
  vi.clearAllMocks();
  mocks.getTools.mockResolvedValue(tools);
  mocks.loadProvidersYAML.mockResolvedValue('providers: []\n');
  mocks.parseProvidersYAML.mockResolvedValue([]);
  mocks.formatProvidersYAML.mockImplementation(async (specs: { ID?: string; Name?: string }[]) => {
    if (!specs?.length) return 'providers: []\n';
    return (
      'providers:\n' + specs.map((s) => `  - id: ${s.ID ?? ''}\n    name: ${s.Name ?? ''}\n`).join('')
    );
  });
  mocks.pickDirectory.mockResolvedValue('');
  mocks.pickFile.mockResolvedValue('');
  mocks.saveProvidersYAML.mockResolvedValue(undefined);
  mocks.getAppearance.mockResolvedValue({ mode: 'dark', resolved: 'dark', fontSize: 13 });
  mocks.setAppearanceMode.mockResolvedValue(undefined);
  mocks.setAppearanceFontSize.mockResolvedValue(undefined);
  mocks.setLanguage.mockResolvedValue(undefined);
  useAppStore.setState({ language: { configured: 'en', resolved: 'en' } });
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
  mocks.scanSessions.mockResolvedValue(undefined);
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
    expect(codebuddy.closest('li')).not.toHaveTextContent(tt('ui.settings.tools.unverified'));
    expect(codebuddy.closest('li')).not.toHaveTextContent(tt('ui.settings.tools.not_installed'));

    const gemini = screen.getByText('Gemini');
    expect(gemini.closest('li')).toHaveTextContent(tt('ui.settings.tools.not_installed'));
    expect(gemini.closest('li')).toHaveClass('opacity-50');
  });

  it('Source=config-dir 的 generic 工具显示「未验证」徽标', async () => {
    render(<Settings />);
    goTools();

    const mytool = await screen.findByText('MyTool');
    expect(mytool.closest('li')).toHaveTextContent(tt('ui.settings.tools.unverified'));
    expect(mytool.closest('li')).toHaveAttribute(
      'title',
      expect.stringContaining(tt('ui.settings.tools.title_config_only')),
    );
  });

  it('残缺安装显示「已损坏」徽标与「修复」按钮', async () => {
    mocks.getTools.mockResolvedValue([
      {
        ID: 'cursor',
        Name: 'Cursor',
        BinPath: '',
        Version: '',
        Installed: true,
        Broken: true,
        Source: 'config-dir',
      },
    ]);
    render(<Settings />);
    goTools();

    const cursor = await screen.findByText('Cursor');
    const row = cursor.closest('li')!;
    expect(row).toHaveTextContent(tt('ui.settings.tools.broken'));
    expect(row).not.toHaveTextContent(tt('ui.settings.tools.unverified'));
    expect(row).toHaveTextContent(tt('ui.settings.tools.missing_bin'));
    expect(within(row).getByRole('button', { name: tt('ui.settings.tools.repair') })).toBeInTheDocument();
    expect(row).toHaveAttribute('title', expect.stringContaining(tt('ui.settings.tools.title_broken')));
  });

  it('自动修复任务运行时显示自动修复文案', async () => {
    mocks.getToolInstallJob.mockResolvedValue({
      ToolID: 'cursor',
      Action: 'install',
      Running: true,
      Log: '',
      Error: '',
      Trigger: 'auto',
    });
    render(<Settings />);
    goTools();

    expect(
      await screen.findByText(tt('ui.settings.tools.auto_repairing').replace('{{0}}', 'cursor')),
    ).toBeInTheDocument();
  });

  it('挂载后后台启动的自动修复：log 事件刷新 job，文案与忙态才出现', async () => {
    // 挂载时 job 还是空闲；自动修复由后端扫描后台启动，前端只收到 log 事件
    mocks.getToolInstallJob.mockResolvedValueOnce({
      ToolID: '',
      Action: '',
      Running: false,
      Log: '',
      Error: '',
    });
    mocks.getToolInstallJob.mockResolvedValueOnce({
      ToolID: 'cursor',
      Action: 'install',
      Running: true,
      Log: '',
      Error: '',
      Trigger: 'auto',
    });
    mocks.getTools.mockResolvedValue([
      {
        ID: 'cursor',
        Name: 'Cursor',
        BinPath: '',
        Version: '',
        Installed: true,
        Broken: true,
        Source: 'config-dir',
      },
    ]);
    render(<Settings />);
    goTools();
    const cursor = await screen.findByText('Cursor');
    // 刷新前：无自动修复文案、行按钮仍可点
    expect(screen.queryByText(tt('ui.settings.tools.auto_repairing').replace('{{0}}', 'cursor'))).toBeNull();
    expect(within(cursor.closest('li')!).getByRole('button', { name: tt('ui.settings.tools.repair') })).toBeInTheDocument();

    await act(async () => {
      logCb?.({ toolID: 'cursor', text: '检测到安装损坏，正在自动修复…' });
    });

    // 提示行（含工具 ID 的特有文案）与行按钮忙态都依赖 job 刷新
    expect(
      await screen.findByText(tt('ui.settings.tools.auto_repairing').replace('{{0}}', 'cursor')),
    ).toBeInTheDocument();
    expect(
      within((await screen.findByText('Cursor')).closest('li')!).queryByRole('button', {
        name: tt('ui.settings.tools.repair'),
      }),
    ).toBeNull();
  });

  it('工具检测页「重新扫描」按钮触发扫描，tools:updated 后恢复可点', async () => {
    render(<Settings />);
    goTools();
    await screen.findByText('CodeBuddy');

    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.tools.rescan') }));
    expect(mocks.scanSessions).toHaveBeenCalledTimes(1);
    // 扫描中禁用防连点
    expect(screen.getByRole('button', { name: tt('ui.settings.tools.rescanning') })).toBeDisabled();
    // DetectAll 完成推 tools:updated → 恢复
    await act(async () => {
      toolsUpdatedCb?.();
    });
    expect(await screen.findByRole('button', { name: tt('ui.settings.tools.rescan') })).toBeEnabled();
  });

  it('「重新扫描」进行中点击不重复触发', async () => {
    render(<Settings />);
    goTools();
    await screen.findByText('CodeBuddy');

    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.tools.rescan') }));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.tools.rescanning') }));
    expect(mocks.scanSessions).toHaveBeenCalledTimes(1);
    await act(async () => {
      toolsUpdatedCb?.();
    });
  });

  it('providers.yaml 源码编辑器可保存，提示已重新加载', async () => {
    render(<Settings />);
    goTools();

    fireEvent.click(await screen.findByRole('button', { name: tt('ui.providers.mode_source') }));
    const editor = await screen.findByLabelText(tt('ui.providers.yaml_aria'));
    expect(editor).toHaveValue('providers: []\n');

    fireEvent.change(editor, { target: { value: 'providers:\n  - name: foo\n' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.providers.save') }));
    });

    expect(mocks.saveProvidersYAML).toHaveBeenCalledWith('providers:\n  - name: foo\n');
    expect(await screen.findByText(tt('ui.providers.saved'))).toBeInTheDocument();
  });

  it('保存失败（YAML 解析失败等）时显示错误，不显示成功提示', async () => {
    mocks.saveProvidersYAML.mockRejectedValue(new Error('YAML 解析失败：line 1: bad indent'));
    render(<Settings />);
    goTools();

    fireEvent.click(await screen.findByRole('button', { name: tt('ui.providers.mode_source') }));
    const editor = await screen.findByLabelText(tt('ui.providers.yaml_aria'));
    fireEvent.change(editor, { target: { value: 'bad: [' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.providers.save') }));
    });

    expect(await screen.findByText(/YAML 解析失败：line 1: bad indent/)).toBeInTheDocument();
    expect(screen.queryByText(tt('ui.providers.saved'))).not.toBeInTheDocument();
  });

  it('GetTools / LoadProvidersYAML 失败时分别显示错误，不崩溃', async () => {
    mocks.getTools.mockRejectedValue(new Error('绑定异常'));
    mocks.loadProvidersYAML.mockRejectedValue(new Error('读取失败'));
    render(<Settings />);
    goTools();

    expect(await screen.findByText(/绑定异常/)).toBeInTheDocument();
    expect(await screen.findByText(/读取失败/)).toBeInTheDocument();
  });

  it('表单添加工具后保存走 format，选择文件写入 command', async () => {
    mocks.pickFile.mockResolvedValueOnce('C:\\bin\\foo.exe');
    render(<Settings />);
    goTools();

    fireEvent.click(await screen.findByRole('button', { name: tt('ui.providers.add') }));
    fireEvent.change(await screen.findByLabelText(tt('ui.providers.id_aria')), { target: { value: 'foo' } });
    fireEvent.change(screen.getByLabelText(tt('ui.providers.name_aria')), { target: { value: 'Foo' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.providers.pick_file') }));
    });
    expect(await screen.findByLabelText(tt('ui.providers.detect_cmd_aria'))).toHaveValue('C:\\bin\\foo.exe');

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.providers.save') }));
    });
    expect(mocks.formatProvidersYAML).toHaveBeenCalled();
    expect(mocks.saveProvidersYAML).toHaveBeenCalled();
    expect(await screen.findByText(tt('ui.providers.saved'))).toBeInTheDocument();
  });

  it('外观选择调用 SetAppearanceMode', async () => {
    render(<Settings />);
    const darkBtn = await screen.findByRole('button', { name: tt('ui.settings.appearance.dark') });
    // 默认已是深色，切到浅色再切回
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.appearance.light') }));
    });
    expect(mocks.setAppearanceMode).toHaveBeenCalledWith('light');
    await act(async () => {
      fireEvent.click(darkBtn);
    });
    expect(mocks.setAppearanceMode).toHaveBeenCalledWith('dark');
  });

  it('字号滑条拖动预览不落盘，松手后调用 SetAppearanceFontSize', async () => {
    render(<Settings />);
    const slider = await screen.findByLabelText(tt('ui.settings.appearance.font_size'));
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

  it('语言行渲染三个内置选项，默认选中配置语言', async () => {
    render(<Settings />);
    const enBtn = await screen.findByRole('button', { name: tt('ui.settings.language.en') });
    expect(enBtn).toHaveAttribute('aria-pressed', 'true');
    const zhBtn = screen.getByRole('button', { name: tt('ui.settings.language.zh_cn') });
    expect(zhBtn).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByRole('button', { name: tt('ui.settings.language.system') })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: tt('ui.settings.language.title') })).toBeInTheDocument();
  });

  it('切换语言调用 SetLanguage 并即时更新选中态', async () => {
    render(<Settings />);
    await screen.findByRole('button', { name: tt('ui.settings.language.en') });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.language.zh_cn') }));
    });
    expect(mocks.setLanguage).toHaveBeenCalledWith('zh-CN');
    expect(screen.getByRole('button', { name: tt('ui.settings.language.zh_cn') })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.language.system') }));
    });
    expect(mocks.setLanguage).toHaveBeenCalledWith('system');
    expect(screen.getByRole('button', { name: tt('ui.settings.language.system') })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
  });

  it('切换语言失败时提示错误并保持原选中态', async () => {
    useAppStore.setState({ toasts: [] });
    mocks.setLanguage.mockRejectedValueOnce(new Error('写盘失败'));
    render(<Settings />);
    await screen.findByRole('button', { name: tt('ui.settings.language.en') });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.language.zh_cn') }));
    });
    await waitFor(() => {
      expect(
        useAppStore.getState().toasts.some((t) => t.tone === 'error' && t.title === '写盘失败'),
      ).toBe(true);
    });
    expect(screen.getByRole('button', { name: tt('ui.settings.language.en') })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
  });

  it('外部语言包选项追加按钮，配置值不在任何选项时兜底渲染该 code', async () => {
    await initI18n('en', { 'ja.json': JSON.stringify({ $name: '日本語' }) });
    useAppStore.setState({ language: { configured: 'de', resolved: 'de' } });
    render(<Settings />);
    // 配置值 de 的包已不在选项里：兜底按钮保证选中态可见（评审补记 #7）
    const deBtn = await screen.findByRole('button', { name: 'de' });
    expect(deBtn).toHaveAttribute('aria-pressed', 'true');
    // 外部语言按 $name 追加按钮
    const jaBtn = screen.getByRole('button', { name: '日本語' });
    await act(async () => {
      fireEvent.click(jaBtn);
    });
    expect(mocks.setLanguage).toHaveBeenCalledWith('ja');
  });

  it('关闭行为默认收进托盘，切换为直接退出调用 SetCloseBehavior', async () => {
    useAppStore.setState({ toasts: [] });
    render(<Settings />);

    const trayBtn = await screen.findByRole('button', { name: tt('ui.settings.close_behavior.tray') });
    expect(trayBtn).toHaveAttribute('aria-pressed', 'true');

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.close_behavior.exit') }));
    });
    expect(mocks.setCloseBehavior).toHaveBeenCalledWith('exit');
    expect(screen.getByRole('button', { name: tt('ui.settings.close_behavior.exit') })).toHaveAttribute('aria-pressed', 'true');
  });

  it('加载时回填关闭行为：返回 exit 时「直接退出」选中', async () => {
    mocks.getCloseBehavior.mockResolvedValueOnce('exit');
    render(<Settings />);

    const exitBtn = await screen.findByRole('button', { name: tt('ui.settings.close_behavior.exit') });
    await waitFor(() => {
      expect(exitBtn).toHaveAttribute('aria-pressed', 'true');
    });
    expect(screen.getByRole('button', { name: tt('ui.settings.close_behavior.tray') })).toHaveAttribute('aria-pressed', 'false');
  });

  it('会话模式与权限模式可切换', async () => {
    render(<Settings />);
    await screen.findByRole('button', { name: tt('ui.settings.session_mode.tui') });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.session_mode.acp') }));
    });
    expect(mocks.setSessionMode).toHaveBeenCalledWith('acp');
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.permission_mode.bypass') }));
    });
    expect(mocks.setPermissionMode).toHaveBeenCalledWith('bypass');
    expect(await screen.findByText(tt('ui.settings.permission_mode.bypass_warning'))).toBeInTheDocument();
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
    expect(screen.getByLabelText(tt('ui.settings.model.api_key'))).toHaveValue('');
    expect(screen.getByPlaceholderText(tt('ui.settings.model.api_key_set'))).toBeInTheDocument();
    expect(
      screen.getByLabelText(tt('ui.settings.model.agent_model').replace('{{0}}', 'Claude Code')),
    ).toHaveValue('mimo-v2.5');

    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.model.save') }));
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
    const sel = await screen.findByLabelText(tt('ui.settings.model.preset'));
    await act(async () => {
      fireEvent.change(sel, { target: { value: 'deepseek' } });
    });
    expect(screen.getByLabelText('OpenAI Base URL')).toHaveValue('https://api.deepseek.com');
    expect(screen.getByLabelText('Anthropic Base URL')).toHaveValue(
      'https://api.deepseek.com/anthropic',
    );
    expect(
      screen.getByLabelText(tt('ui.settings.model.agent_model').replace('{{0}}', 'Claude Code')),
    ).toHaveValue('deepseek-chat');
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
    fireEvent.click(await screen.findByLabelText(tt('ui.settings.model.clear_key')));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.model.save') }));
    await waitFor(() =>
      expect(mocks.setModelConfig).toHaveBeenCalledWith(expect.objectContaining({ ClearAPIKey: true })),
    );
  });

  it('切换关闭行为失败时提示错误且保持原选中态', async () => {
    useAppStore.setState({ toasts: [] });
    mocks.setCloseBehavior.mockRejectedValueOnce(new Error('写盘失败'));
    render(<Settings />);

    await screen.findByRole('button', { name: tt('ui.settings.close_behavior.tray') });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.close_behavior.exit') }));
    });
    await waitFor(() => {
      expect(
        useAppStore.getState().toasts.some((t) => t.tone === 'error' && t.title === '写盘失败'),
      ).toBe(true);
    });
    expect(screen.getByRole('button', { name: tt('ui.settings.close_behavior.tray') })).toHaveAttribute('aria-pressed', 'true');
  });

  it('内置工具按 BinPath 显示安装/卸载，自定义工具无按钮', async () => {
    render(<Settings />);
    goTools();

    const claude = (await screen.findByText('Claude Code')).closest('li')!;
    expect(within(claude).getByRole('button', { name: tt('ui.settings.tools.uninstall') })).toBeInTheDocument();

    const gemini = screen.getByText('Gemini').closest('li')!;
    expect(within(gemini).getByRole('button', { name: tt('ui.settings.tools.install') })).toBeInTheDocument();

    const codebuddy = screen.getByText('CodeBuddy').closest('li')!;
    expect(within(codebuddy).getByRole('button', { name: tt('ui.settings.tools.uninstall') })).toBeInTheDocument();

    const mytool = screen.getByText('MyTool').closest('li')!;
    expect(within(mytool).queryByRole('button', { name: tt('ui.settings.tools.install') })).not.toBeInTheDocument();
    expect(within(mytool).queryByRole('button', { name: tt('ui.settings.tools.uninstall') })).not.toBeInTheDocument();
  });

  it('点 Gemini 安装调用 installBuiltinTool 且按钮变为安装中…', async () => {
    render(<Settings />);
    goTools();
    const gemini = (await screen.findByText('Gemini')).closest('li')!;
    await act(async () => {
      fireEvent.click(within(gemini).getByRole('button', { name: tt('ui.settings.tools.install') }));
    });
    expect(mocks.installBuiltinTool).toHaveBeenCalledWith('gemini');
    expect(within(gemini).getByRole('button', { name: tt('ui.settings.tools.installing') })).toBeInTheDocument();
  });

  it('点 Claude 卸载弹出确认，默认不清配置', async () => {
    render(<Settings />);
    goTools();
    const claude = (await screen.findByText('Claude Code')).closest('li')!;
    await act(async () => {
      fireEvent.click(within(claude).getByRole('button', { name: tt('ui.settings.tools.uninstall') }));
    });
    expect(
      await screen.findByText(tt('ui.settings.tools.uninstall_title').replace('{{0}}', 'Claude Code')),
    ).toBeInTheDocument();
    expect(screen.getByLabelText(tt('ui.settings.tools.purge_config'))).not.toBeChecked();
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.tools.confirm_uninstall') }));
    });
    expect(mocks.uninstallBuiltinTool).toHaveBeenCalledWith('claude', false);
  });

  it('勾选同时清除配置后确认并列出目录', async () => {
    render(<Settings />);
    goTools();
    const claude = (await screen.findByText('Claude Code')).closest('li')!;
    await act(async () => {
      fireEvent.click(within(claude).getByRole('button', { name: tt('ui.settings.tools.uninstall') }));
    });
    await screen.findByText(tt('ui.settings.tools.uninstall_title').replace('{{0}}', 'Claude Code'));
    fireEvent.click(screen.getByLabelText(tt('ui.settings.tools.purge_config')));
    expect(screen.getByText('~/.claude')).toBeInTheDocument();
    expect(screen.getByText(tt('ui.settings.tools.purge_note'))).toBeInTheDocument();
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.tools.confirm_uninstall') }));
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
      fireEvent.click(within(cursor).getByRole('button', { name: tt('ui.settings.tools.uninstall') }));
    });
    expect(
      await screen.findByText(tt('ui.settings.tools.uninstall_title').replace('{{0}}', 'Cursor')),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText(tt('ui.settings.tools.purge_config'))).not.toBeInTheDocument();
  });

  it('安装日志出现且完成后重刷工具列表', async () => {
    render(<Settings />);
    goTools();
    const gemini = (await screen.findByText('Gemini')).closest('li')!;
    await act(async () => {
      fireEvent.click(within(gemini).getByRole('button', { name: tt('ui.settings.tools.install') }));
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

  it('安装日志事件文本为 wire key 时逐条翻译渲染', async () => {
    render(<Settings />);
    goTools();
    await screen.findByText('Gemini');
    await act(async () => {
      logCb?.({ toolID: 'gemini', text: 'tool.log.auto_repairing' });
    });
    expect(await screen.findByText(tt('tool.log.auto_repairing'))).toBeInTheDocument();
  });

  it('挂载时未完成任务的 job.Log 逐行翻译后渲染', async () => {
    mocks.getTools.mockResolvedValue([
      {
        ID: 'cursor',
        Name: 'Cursor',
        BinPath: '',
        Version: '',
        Installed: true,
        Broken: true,
        Source: 'config-dir',
      },
    ]);
    mocks.getToolInstallJob.mockResolvedValue({
      ToolID: 'cursor',
      Action: 'install',
      Running: true,
      Log: 'tool.log.auto_repairing\ntool.log.residual_removed|~/.cursor',
      Error: '',
    });
    render(<Settings />);
    goTools();

    const pre = (await screen.findByText('Cursor')).closest('li')!.querySelector('pre');
    expect(pre).not.toBeNull();
    expect(pre!).toHaveTextContent(tt('tool.log.auto_repairing'));
    expect(pre!).toHaveTextContent(
      tt('tool.log.residual_removed').replace('{{0}}', '~/.cursor'),
    );
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
    expect(await screen.findByRole('heading', { name: tt('ui.settings.about.title') })).toBeInTheDocument();
    expect(await screen.findByText(/v0\.1\.0/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: tt('ui.settings.about.check_update') })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: tt('ui.settings.about.feedback') })).toBeInTheDocument();
  });

  it('点击反馈问题打开 GitHub Issues 新建页', async () => {
    const open = vi.fn();
    vi.stubGlobal('runtime', { BrowserOpenURL: open });
    mocks.getAppVersion.mockResolvedValue('v0.1.0');
    render(<Settings />);
    await screen.findByRole('heading', { name: tt('ui.settings.about.title') });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.about.feedback') }));
    expect(open).toHaveBeenCalledWith('https://github.com/kaiys202212/kshell/issues/new');
    vi.unstubAllGlobals();
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
    await screen.findByRole('heading', { name: tt('ui.settings.about.title') });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.about.check_update') }));
    });
    expect(await screen.findByText(/v0\.2\.0/)).toBeInTheDocument();
    expect(
      screen.getByText(tt('ui.settings.about.source').replace('{{0}}', 'ghfast'), { exact: false }),
    ).toBeInTheDocument();
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.about.upgrade_now') }));
    });
    expect(mocks.applyUpdate).toHaveBeenCalledTimes(1);
  });

  it('气泡提示音开关：勾选态绑定 store，切换写入', () => {
    useAppStore.setState({ notifySound: true });
    render(<Settings />);
    const box = screen.getByRole('checkbox', { name: tt('ui.settings.notify.sound') });
    expect(box).toBeChecked();
    fireEvent.click(box);
    expect(useAppStore.getState().notifySound).toBe(false);
    fireEvent.click(box);
    expect(useAppStore.getState().notifySound).toBe(true);
  });
});
