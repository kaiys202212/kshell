// Settings 页面测试：分区导航、工具检测、providers.yaml、外观/关闭/会话/权限、模型预设。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import Settings from './Settings';
import { useAppStore } from '../state/store';
import { initI18n } from '../i18n';
import { tt } from '../test/i18n';
import type { ModelPlatformView, ToolInfo } from '../lib/api';

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
  setAppearanceShowWhitespace: vi.fn(),
  // 语言接线（getLanguage / onLanguageChanged）在 main.tsx bootstrap，不在此文件测
  setLanguage: vi.fn(),
  getCloseBehavior: vi.fn(),
  setCloseBehavior: vi.fn(),
  getModelConfig: vi.fn(),
  setModelConfig: vi.fn(),
  listModelPresets: vi.fn(),
  fetchAvailableModels: vi.fn(),
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
  checkToolAuth: vi.fn().mockResolvedValue('unknown'),
  openLoginTerminal: vi.fn(),
}));
vi.mock('../lib/api', () => mocks);

// 登录弹窗打桩：Settings 层只验证「登录按钮打开弹窗」；弹窗内部行为见 LoginDialog.test
vi.mock('../components/LoginDialog', () => ({
  default: ({ toolName }: { toolName: string }) => <div data-testid="login-dialog">{toolName}</div>,
}));

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

// platView：构造后端 ModelConfigView.Platforms 的单平台视图
const platView = (over: Partial<ModelPlatformView> = {}): ModelPlatformView => ({
  ID: 'p1',
  Name: 'DeepSeek',
  Preset: 'deepseek',
  OpenAIBaseURL: 'https://oai/',
  AnthropicBaseURL: 'https://ant/',
  Models: ['m1'],
  APIKeySet: false,
  IsDefault: true,
  ASRModel: '',
  ...over,
});

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
  mocks.getAppearance.mockResolvedValue({
    mode: 'dark',
    resolved: 'dark',
    fontSize: 13,
    showWhitespace: false,
  });
  mocks.setAppearanceMode.mockResolvedValue(undefined);
  mocks.setAppearanceFontSize.mockResolvedValue(undefined);
  mocks.setAppearanceShowWhitespace.mockResolvedValue(undefined);
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
    Reason: 'update.reason.dev_build',
  });
  mocks.applyUpdate.mockResolvedValue(undefined);
  mocks.getCloseBehavior.mockResolvedValue('tray');
  mocks.setCloseBehavior.mockResolvedValue(undefined);
  mocks.getSessionMode.mockResolvedValue('tui');
  mocks.setSessionMode.mockResolvedValue(undefined);
  mocks.getPermissionMode.mockResolvedValue('default');
  mocks.setPermissionMode.mockResolvedValue(undefined);
  mocks.listModelPresets.mockResolvedValue([
    { ID: 'custom', Name: 'preset.custom.name', OpenAIBaseURL: '', AnthropicBaseURL: '', RecommendedModel: '', Note: '' },
    {
      ID: 'deepseek',
      Name: 'preset.deepseek.name',
      OpenAIBaseURL: 'https://api.deepseek.com',
      AnthropicBaseURL: 'https://api.deepseek.com/anthropic',
      RecommendedModel: 'deepseek-chat',
      Note: '',
    },
  ]);
  mocks.getModelConfig.mockResolvedValue({
    Enabled: false,
    Platforms: [],
    DefaultPlatform: '',
    ActiveModel: '',
  });
  mocks.setModelConfig.mockResolvedValue(undefined);
  mocks.fetchAvailableModels.mockResolvedValue({ Models: [], Errors: [] });
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

  it('已安装工具按登录态显示徽标：in→已登录、out→未登录+登录按钮、unknown→无徽标', async () => {
    mocks.getTools.mockResolvedValue([
      { ...tools[0], Auth: 'in' },
      { ...tools[3], Auth: 'out' },
      { ...tools[1], Auth: 'unknown' },
    ]);
    render(<Settings />);
    goTools();

    const codebuddy = await screen.findByText('CodeBuddy');
    expect(codebuddy.closest('li')).toHaveTextContent(tt('ui.auth.logged_in'));
    expect(codebuddy.closest('li')).not.toHaveTextContent(tt('ui.auth.not_logged_in'));

    const claude = screen.getByText('Claude Code');
    const claudeRow = claude.closest('li')!;
    expect(claudeRow).toHaveTextContent(tt('ui.auth.not_logged_in'));
    expect(
      within(claudeRow).getByRole('button', { name: tt('ui.auth.login') }),
    ).toBeInTheDocument();

    const mytool = screen.getByText('MyTool');
    expect(mytool.closest('li')).not.toHaveTextContent(tt('ui.auth.logged_in'));
    expect(mytool.closest('li')).not.toHaveTextContent(tt('ui.auth.not_logged_in'));
  });

  it('未登录工具卡片的「登录」按钮打开登录弹窗（传入该工具）', async () => {
    mocks.getTools.mockResolvedValue([{ ...tools[3], Auth: 'out' }]);
    render(<Settings />);
    goTools();

    fireEvent.click(
      await screen.findByRole('button', { name: tt('ui.auth.login') }),
    );

    expect(await screen.findByTestId('login-dialog')).toHaveTextContent('Claude Code');
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

  it('空白字符开关调用 SetAppearanceShowWhitespace', async () => {
    render(<Settings />);
    const cb = await screen.findByLabelText(tt('ui.settings.show_whitespace'));
    expect(cb).not.toBeChecked();
    await act(async () => {
      fireEvent.click(cb);
    });
    expect(mocks.setAppearanceShowWhitespace).toHaveBeenCalledWith(true);
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

  it('平台卡片渲染：名称/默认徽标/密钥状态/模型数，非默认平台显示设为默认按钮', async () => {
    mocks.getModelConfig.mockResolvedValue({
      Enabled: true,
      Platforms: [
        platView({ Models: ['a', 'b'], APIKeySet: true, IsDefault: true }),
        platView({
          ID: 'p2',
          Name: 'GLM',
          Preset: 'mystery',
          OpenAIBaseURL: '',
          AnthropicBaseURL: '',
          Models: [],
          APIKeySet: false,
          IsDefault: false,
        }),
      ],
      DefaultPlatform: 'p1',
      ActiveModel: 'a',
    });
    render(<Settings />);
    goModel();

    // 平台名与已知预设译名同为 DeepSeek：取第一个（平台名 span）
    const p1 = (await screen.findAllByText('DeepSeek'))[0].closest('li')!;
    expect(within(p1).getByText(tt('ui.settings.model.default_badge'))).toBeInTheDocument();
    expect(within(p1).queryByRole('button', { name: tt('ui.settings.model.set_default') })).toBeNull();
    expect(p1).toHaveTextContent(tt('ui.settings.model.api_key_set'));
    expect(p1).toHaveTextContent(tt('ui.settings.model.models_count').replace('{{0}}', '2'));
    // 已知预设经 preset.<id>.name 翻译
    expect(p1).toHaveTextContent(tt('preset.deepseek.name'));

    const p2 = screen.getByText('GLM').closest('li')!;
    expect(within(p2).getByRole('button', { name: tt('ui.settings.model.set_default') })).toBeInTheDocument();
    // 未知预设显示原始标识
    expect(p2).toHaveTextContent('mystery');
  });

  it('平台列表为空时显示空态与添加入口', async () => {
    render(<Settings />);
    goModel();

    expect(await screen.findByText(tt('ui.settings.model.platforms_empty'))).toBeInTheDocument();
    expect(screen.getByRole('button', { name: tt('ui.settings.model.add_platform') })).toBeInTheDocument();
  });

  it('默认模型下拉只列默认平台的模型', async () => {
    mocks.getModelConfig.mockResolvedValue({
      Enabled: true,
      Platforms: [
        platView({ Models: ['m1', 'm2'] }),
        platView({ ID: 'p2', Name: 'GLM', Models: ['m3'], IsDefault: false }),
      ],
      DefaultPlatform: 'p1',
      ActiveModel: 'm1',
    });
    render(<Settings />);
    goModel();

    const sel = (await screen.findByLabelText(tt('ui.settings.model.active_model'))) as HTMLSelectElement;
    await waitFor(() => expect(sel.options).toHaveLength(2));
    expect(Array.from(sel.options).map((o) => o.value)).toEqual(['m1', 'm2']);
  });

  it('添加平台→填写→保存：载荷含新平台（UUID 形态 ID）', async () => {
    vi.stubGlobal('crypto', { randomUUID: () => '123e4567-e89b-12d3-a456-426614174000' });
    render(<Settings />);
    goModel();
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.settings.model.add_platform') }));
    fireEvent.change(screen.getByLabelText(tt('ui.settings.model.platform_name')), {
      target: { value: 'DeepSeek' },
    });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.model.save') }));
    vi.unstubAllGlobals();

    await waitFor(() => expect(mocks.setModelConfig).toHaveBeenCalledTimes(1));
    const arg = mocks.setModelConfig.mock.calls[0][0];
    expect(arg.Enabled).toBe(false);
    expect(arg.Platforms).toHaveLength(1);
    expect(arg.Platforms[0].Name).toBe('DeepSeek');
    expect(arg.Platforms[0].ID).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/);
  });

  it('编辑平台改名保存：沿用原平台 ID', async () => {
    mocks.getModelConfig.mockResolvedValue({
      Enabled: true,
      Platforms: [platView()],
      DefaultPlatform: 'p1',
      ActiveModel: 'm1',
    });
    render(<Settings />);
    goModel();
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.settings.model.edit_platform') }));
    const nameInput = screen.getByLabelText(tt('ui.settings.model.platform_name'));
    expect(nameInput).toHaveValue('DeepSeek');
    fireEvent.change(nameInput, { target: { value: 'DeepSeek2' } });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.model.save') }));

    await waitFor(() => expect(mocks.setModelConfig).toHaveBeenCalledTimes(1));
    const arg = mocks.setModelConfig.mock.calls[0][0];
    expect(arg.Platforms[0]).toEqual(expect.objectContaining({ ID: 'p1', Name: 'DeepSeek2' }));
  });

  it('删除平台需确认，确认后保存载荷不含该平台', async () => {
    const confirm = vi.fn(() => true);
    vi.stubGlobal('confirm', confirm);
    mocks.getModelConfig.mockResolvedValue({
      Enabled: true,
      Platforms: [platView(), platView({ ID: 'p2', Name: 'GLM', IsDefault: false })],
      DefaultPlatform: 'p1',
      ActiveModel: 'm1',
    });
    render(<Settings />);
    goModel();
    const p1 = (await screen.findAllByText('DeepSeek'))[0].closest('li')!;
    fireEvent.click(
      within(p1.closest('li')!).getByRole('button', { name: tt('ui.settings.model.delete_platform') }),
    );
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining('DeepSeek'));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.model.save') }));
    vi.unstubAllGlobals();

    await waitFor(() => expect(mocks.setModelConfig).toHaveBeenCalledTimes(1));
    const arg = mocks.setModelConfig.mock.calls[0][0];
    expect(arg.Platforms.map((p: { ID: string }) => p.ID)).toEqual(['p2']);
  });

  it('取消删除确认时不改动平台列表', async () => {
    vi.stubGlobal('confirm', vi.fn(() => false));
    mocks.getModelConfig.mockResolvedValue({
      Enabled: true,
      Platforms: [platView(), platView({ ID: 'p2', Name: 'GLM', IsDefault: false })],
      DefaultPlatform: 'p1',
      ActiveModel: 'm1',
    });
    render(<Settings />);
    goModel();
    const p1 = (await screen.findAllByText('DeepSeek'))[0].closest('li')!;
    fireEvent.click(
      within(p1.closest('li')!).getByRole('button', { name: tt('ui.settings.model.delete_platform') }),
    );
    vi.unstubAllGlobals();

    expect(screen.getByText('GLM')).toBeInTheDocument();
    expect(screen.getAllByText('DeepSeek').length).toBeGreaterThan(0);
  });

  it('设为默认后保存载荷 DefaultPlatform 更新', async () => {
    mocks.getModelConfig.mockResolvedValue({
      Enabled: true,
      Platforms: [platView(), platView({ ID: 'p2', Name: 'GLM', IsDefault: false })],
      DefaultPlatform: 'p1',
      ActiveModel: 'm1',
    });
    render(<Settings />);
    goModel();
    const p2 = (await screen.findByText('GLM')).closest('li')!;
    fireEvent.click(within(p2).getByRole('button', { name: tt('ui.settings.model.set_default') }));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.model.save') }));

    await waitFor(() => expect(mocks.setModelConfig).toHaveBeenCalledTimes(1));
    expect(mocks.setModelConfig.mock.calls[0][0].DefaultPlatform).toBe('p2');
  });

  it('平台名称必填：为空时前端拦截，不调用保存', async () => {
    render(<Settings />);
    goModel();
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.settings.model.add_platform') }));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.model.save') }));

    expect(await screen.findByText(tt('ui.settings.model.platform_name_required'))).toBeInTheDocument();
    expect(mocks.setModelConfig).not.toHaveBeenCalled();
  });

  it('保存成功后 notify 已保存提示', async () => {
    useAppStore.setState({ toasts: [] });
    mocks.getModelConfig.mockResolvedValue({
      Enabled: true,
      Platforms: [platView()],
      DefaultPlatform: 'p1',
      ActiveModel: 'm1',
    });
    render(<Settings />);
    goModel();
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.settings.model.save') }));

    await waitFor(() => {
      expect(
        useAppStore
          .getState()
          .toasts.some((t) => t.tone === 'info' && t.title === tt('ui.settings.model.saved')),
      ).toBe(true);
    });
  });

  it('拉取模型以编辑中平台 ID 与表单端点/密钥四参调用，结果可加入列表', async () => {
    mocks.getModelConfig.mockResolvedValue({
      Enabled: true,
      Platforms: [platView({ APIKeySet: true })],
      DefaultPlatform: 'p1',
      ActiveModel: 'm1',
    });
    mocks.fetchAvailableModels.mockResolvedValue({
      Models: ['m-a'],
      Errors: ['err.model_fetch.openai_failed'],
    });
    render(<Settings />);
    goModel();
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.settings.model.edit_platform') }));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.model.fetch_models') }));

    expect(mocks.fetchAvailableModels).toHaveBeenCalledWith('p1', 'https://oai/', 'https://ant/', '');
    // 错误 key 经 translateBackend 展示
    expect(await screen.findByText(tt('err.model_fetch.openai_failed'))).toBeInTheDocument();
    const rowA = screen.getAllByRole('listitem').find((r) => r.textContent?.includes('m-a'));
    fireEvent.click(within(rowA!).getByRole('button', { name: tt('ui.settings.model.add') }));

    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.model.save') }));
    await waitFor(() => expect(mocks.setModelConfig).toHaveBeenCalledTimes(1));
    expect(mocks.setModelConfig.mock.calls[0][0].Platforms[0].Models).toEqual(['m1', 'm-a']);
  });

  it('编辑态手动添加模型（去空格去重）并在保存载荷中体现', async () => {
    mocks.getModelConfig.mockResolvedValue({
      Enabled: true,
      Platforms: [platView({ Models: ['m1'] })],
      DefaultPlatform: 'p1',
      ActiveModel: 'm1',
    });
    render(<Settings />);
    goModel();
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.settings.model.edit_platform') }));
    fireEvent.change(screen.getByLabelText(tt('ui.settings.model.add_model')), {
      target: { value: ' m2 ' },
    });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.model.add') }));
    const m2row = screen.getAllByRole('listitem').find((r) => r.textContent?.includes('m2'));
    expect(m2row).toBeDefined();

    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.model.save') }));
    await waitFor(() => expect(mocks.setModelConfig).toHaveBeenCalledTimes(1));
    expect(mocks.setModelConfig.mock.calls[0][0].Platforms[0].Models).toEqual(['m1', 'm2']);
  });

  it('拉取模型列表支持检索过滤', async () => {
    mocks.fetchAvailableModels.mockResolvedValue({
      Models: ['qwen-max', 'qwen-plus', 'gpt-4o'],
      Errors: [],
    });
    mocks.getModelConfig.mockResolvedValue({
      Enabled: true,
      Platforms: [platView({ Models: [] })],
      DefaultPlatform: 'p1',
      ActiveModel: '',
    });
    render(<Settings />);
    goModel();
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.settings.model.edit_platform') }));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.model.fetch_models') }));
    const filter = await screen.findByPlaceholderText(tt('ui.settings.model.fetch_filter_placeholder'));

    // 输入「QUEN」大小写不敏感命中 qwen 前缀，gpt 被过滤掉
    fireEvent.change(filter, { target: { value: 'Qwen' } });
    const rows = screen.getAllByRole('listitem');
    expect(rows.some((r) => r.textContent?.includes('qwen-max'))).toBe(true);
    expect(rows.some((r) => r.textContent?.includes('qwen-plus'))).toBe(true);
    expect(rows.some((r) => r.textContent?.includes('gpt-4o'))).toBe(false);
  });

  it('拉取模型检索无匹配时显示专属空态', async () => {
    mocks.fetchAvailableModels.mockResolvedValue({ Models: ['qwen-max'], Errors: [] });
    mocks.getModelConfig.mockResolvedValue({
      Enabled: true,
      Platforms: [platView({ Models: [] })],
      DefaultPlatform: 'p1',
      ActiveModel: '',
    });
    render(<Settings />);
    goModel();
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.settings.model.edit_platform') }));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.model.fetch_models') }));
    const filter = await screen.findByPlaceholderText(tt('ui.settings.model.fetch_filter_placeholder'));
    fireEvent.change(filter, { target: { value: 'zzz-nomatch' } });

    expect(screen.getByText(tt('ui.settings.model.fetch_filter_empty'))).toBeInTheDocument();
    // 原始 fetch_empty 文案不应出现（并非端点无返回）
    expect(screen.queryByText(tt('ui.settings.model.fetch_empty'))).not.toBeInTheDocument();
  });

  it('拉取结果 Models/Errors 为 null 时不崩溃（旧后端 JSON null 兼容）', async () => {
    // 模拟 Go nil slice 序列化出的 null：前端须兜底为空数组而非读 length 崩溃
    mocks.fetchAvailableModels.mockResolvedValue({ Models: null, Errors: null });
    mocks.getModelConfig.mockResolvedValue({
      Enabled: true,
      Platforms: [platView({ Models: [] })],
      DefaultPlatform: 'p1',
      ActiveModel: '',
    });
    render(<Settings />);
    goModel();
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.settings.model.edit_platform') }));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.model.fetch_models') }));

    // 拉取完成且显示空列表占位，页面未崩溃
    expect(await screen.findByText(tt('ui.settings.model.fetch_empty'))).toBeInTheDocument();
  });

  it('再次拉取会清空上一轮检索词', async () => {
    mocks.fetchAvailableModels.mockResolvedValue({ Models: ['qwen-max', 'gpt-4o'], Errors: [] });
    mocks.getModelConfig.mockResolvedValue({
      Enabled: true,
      Platforms: [platView({ Models: [] })],
      DefaultPlatform: 'p1',
      ActiveModel: '',
    });
    render(<Settings />);
    goModel();
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.settings.model.edit_platform') }));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.model.fetch_models') }));
    const filter = await screen.findByPlaceholderText(tt('ui.settings.model.fetch_filter_placeholder'));
    fireEvent.change(filter, { target: { value: 'qwen' } });
    expect(screen.getByPlaceholderText(tt('ui.settings.model.fetch_filter_placeholder'))).toHaveValue('qwen');

    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.model.fetch_models') }));
    await waitFor(() =>
      expect(screen.getByPlaceholderText(tt('ui.settings.model.fetch_filter_placeholder'))).toHaveValue(''),
    );
  });

  it('拉取结果列表容器带限高滚动', async () => {
    mocks.fetchAvailableModels.mockResolvedValue({ Models: ['a', 'b'], Errors: [] });
    mocks.getModelConfig.mockResolvedValue({
      Enabled: true,
      Platforms: [platView({ Models: [] })],
      DefaultPlatform: 'p1',
      ActiveModel: '',
    });
    render(<Settings />);
    goModel();
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.settings.model.edit_platform') }));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.model.fetch_models') }));
    const list = await screen.findByTestId('model-fetch-list');
    expect(list.className).toContain('overflow-y-auto');
    expect(list.className).toContain('max-h-');
  });

  it('编辑态选择预设填入双 URL 与推荐模型', async () => {
    render(<Settings />);
    goModel();
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.settings.model.add_platform') }));
    const sel = await screen.findByLabelText(tt('ui.settings.model.preset'));
    // 预设 Name 为 wire key，经 translateBackend 翻成当前语言文案
    expect(screen.getByRole('option', { name: tt('preset.deepseek.name') })).toBeInTheDocument();
    await act(async () => {
      fireEvent.change(sel, { target: { value: 'deepseek' } });
    });
    expect(screen.getByLabelText(tt('ui.settings.model.openai_base_url'))).toHaveValue(
      'https://api.deepseek.com',
    );
    expect(screen.getByLabelText(tt('ui.settings.model.anthropic_base_url'))).toHaveValue(
      'https://api.deepseek.com/anthropic',
    );
    // 推荐模型进入编辑中平台的模型列表
    expect(screen.getByText('deepseek-chat')).toBeInTheDocument();
  });

  it('编辑已有平台：密钥留空占位提示保持，勾选清除密钥提交 ClearAPIKey', async () => {
    mocks.getModelConfig.mockResolvedValue({
      Enabled: true,
      Platforms: [platView({ APIKeySet: true })],
      DefaultPlatform: 'p1',
      ActiveModel: 'm1',
    });
    render(<Settings />);
    goModel();
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.settings.model.edit_platform') }));
    expect(screen.getByPlaceholderText(tt('ui.settings.model.api_key_keep_hint'))).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText(tt('ui.settings.model.clear_key')));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.model.save') }));

    await waitFor(() => expect(mocks.setModelConfig).toHaveBeenCalledTimes(1));
    const arg = mocks.setModelConfig.mock.calls[0][0];
    expect(arg.Platforms[0].ClearAPIKey).toBe(true);
    expect(arg.Platforms[0].APIKey).toBe('');
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

  it('开发构建检查更新：跳过原因 wire key 经 translateBackend 翻译展示', async () => {
    render(<Settings />);
    await screen.findByRole('heading', { name: tt('ui.settings.about.title') });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.about.check_update') }));
    });
    expect(await screen.findByText(tt('update.reason.dev_build'))).toBeInTheDocument();
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
