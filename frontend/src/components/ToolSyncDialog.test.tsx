// ToolSyncDialog 测试：加载回显、claude 槽位/勾选保存载荷、禁用清空、取消不保存。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ToolSyncDialog } from './ToolSyncDialog';
import { useAppStore } from '../state/store';
import { initI18n } from '../i18n';
import { tt } from '../test/i18n';
import type { ModelPlatformView } from '../lib/api';

const mocks = vi.hoisted(() => ({
  getToolSyncConfig: vi.fn(),
  setToolSyncConfig: vi.fn(),
}));
vi.mock('../lib/api', () => mocks);

const plats: ModelPlatformView[] = [
  {
    ID: 'p1',
    Name: 'DeepSeek',
    Preset: 'deepseek',
    OpenAIBaseURL: 'https://oai/',
    AnthropicBaseURL: 'https://ant/',
    Models: ['m1', 'm2'],
    APIKeySet: true,
    ASRModel: '',
  },
  {
    ID: 'p2',
    Name: 'GLM',
    Preset: '',
    OpenAIBaseURL: 'https://glm-oai/',
    AnthropicBaseURL: 'https://glm-ant/',
    Models: ['g1'],
    APIKeySet: true,
    ASRModel: '',
  },
];

beforeEach(async () => {
  await initI18n('en', {});
  vi.clearAllMocks();
  mocks.setToolSyncConfig.mockResolvedValue(undefined);
  useAppStore.setState({ notify: vi.fn() });
});

afterEach(cleanup);

const emptyCfg = {
  Enabled: false,
  Claude: { Model: '', Sonnet: '', Opus: '', Haiku: '' },
  ToolPlatforms: {},
  OpenCode: {},
  CodeBuddy: [],
};

describe('ToolSyncDialog', () => {
  it('加载回显：启用态与已有勾选', async () => {
    mocks.getToolSyncConfig.mockResolvedValue({
      Enabled: true,
      Claude: { Model: 'm1', Sonnet: '', Opus: '', Haiku: '' },
      ToolPlatforms: { claude: 'p1', codebuddy: 'p1' },
      OpenCode: { p1: ['m1'] },
      CodeBuddy: ['m2'],
    });
    render(<ToolSyncDialog toolID="claude" toolName="Claude Code" platforms={plats} onClose={() => {}} />);
    const enable = await screen.findByRole('checkbox', { name: tt('ui.toolsync.enable') });
    expect(enable).toBeChecked();
    const platSel = screen.getByLabelText(tt('ui.toolsync.platform')) as HTMLSelectElement;
    expect(platSel.value).toBe('p1');
    const modelSel = screen.getByLabelText(tt('ui.toolsync.slot_model')) as HTMLSelectElement;
    expect(modelSel.value).toBe('m1');
  });

  it('claude：先选平台，槽位选项来自该平台，保存载荷带平台', async () => {
    mocks.getToolSyncConfig.mockResolvedValue(emptyCfg);
    const onClose = vi.fn();
    render(<ToolSyncDialog toolID="claude" toolName="Claude Code" platforms={plats} onClose={onClose} />);

    fireEvent.click(await screen.findByRole('checkbox', { name: tt('ui.toolsync.enable') }));
    const platSel = screen.getByLabelText(tt('ui.toolsync.platform')) as HTMLSelectElement;
    const modelSel = screen.getByLabelText(tt('ui.toolsync.slot_model')) as HTMLSelectElement;
    // 静态注入没有「未配置」语义：无保存值时渲染期回落到第一个可用平台，
    // 下拉不提供空选项。
    expect(Array.from(platSel.options).map((o) => o.value)).toEqual(['p1', 'p2']);
    expect(platSel.value).toBe('p1');
    expect(Array.from(modelSel.options).map((o) => o.value)).toEqual(['', 'm1', 'm2']);
    // 切到 p2：槽位值清空，选项改为该平台模型
    fireEvent.change(platSel, { target: { value: 'p2' } });
    expect(Array.from(modelSel.options).map((o) => o.value)).toEqual(['', 'g1']);
    fireEvent.change(modelSel, { target: { value: 'g1' } });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.toolsync.save') }));

    await waitFor(() => expect(mocks.setToolSyncConfig).toHaveBeenCalledTimes(1));
    const [toolID, input] = mocks.setToolSyncConfig.mock.calls[0];
    expect(toolID).toBe('claude');
    expect(input.Enabled).toBe(true);
    expect(input.Claude).toEqual({ Model: 'g1', Sonnet: '', Opus: '', Haiku: '' });
    expect(input.ToolPlatforms.claude).toBe('p2');
    expect(onClose).toHaveBeenCalled();
  });

  it('opencode：按平台勾选模型，保存载荷只含勾选平台', async () => {
    mocks.getToolSyncConfig.mockResolvedValue(emptyCfg);
    render(<ToolSyncDialog toolID="opencode" toolName="OpenCode" platforms={plats} onClose={() => {}} />);

    fireEvent.click(await screen.findByRole('checkbox', { name: tt('ui.toolsync.enable') }));
    // p1 的 m1 / p2 的 g1
    fireEvent.click(screen.getByRole('checkbox', { name: 'm1' }));
    fireEvent.click(screen.getByRole('checkbox', { name: 'g1' }));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.toolsync.save') }));

    await waitFor(() => expect(mocks.setToolSyncConfig).toHaveBeenCalledTimes(1));
    const [toolID, input] = mocks.setToolSyncConfig.mock.calls[0];
    expect(toolID).toBe('opencode');
    expect(input.OpenCode).toEqual({ p1: ['m1'], p2: ['g1'] });
  });

  it('codebuddy：选平台后勾该平台模型，载荷带 ToolPlatforms.codebuddy', async () => {
    mocks.getToolSyncConfig.mockResolvedValue(emptyCfg);
    render(<ToolSyncDialog toolID="codebuddy" toolName="CodeBuddy" platforms={plats} onClose={() => {}} />);

    fireEvent.click(await screen.findByRole('checkbox', { name: tt('ui.toolsync.enable') }));
    const platSel = screen.getByLabelText(tt('ui.toolsync.platform')) as HTMLSelectElement;
    fireEvent.change(platSel, { target: { value: 'p2' } });
    fireEvent.click(screen.getByRole('checkbox', { name: 'g1' }));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.toolsync.save') }));

    await waitFor(() => expect(mocks.setToolSyncConfig).toHaveBeenCalledTimes(1));
    const [toolID, input] = mocks.setToolSyncConfig.mock.calls[0];
    expect(toolID).toBe('codebuddy');
    expect(input.CodeBuddy).toEqual(['g1']);
    expect(input.ToolPlatforms.codebuddy).toBe('p2');
  });

  it('claude：悬空平台显示占位并可重选，不静默回落第一个', async () => {
    mocks.getToolSyncConfig.mockResolvedValue({
      ...emptyCfg,
      ToolPlatforms: { claude: 'gone' },
    });
    render(
      <ToolSyncDialog toolID="claude" toolName="Claude Code" platforms={plats} onClose={() => {}} />,
    );

    const platSel = (await screen.findByLabelText(tt('ui.toolsync.platform'))) as HTMLSelectElement;
    expect(platSel.value).toBe('gone');
    expect(platSel.selectedOptions[0].textContent).toBe(tt('ui.toolsync.tool_platform_invalid'));
    // 悬空时只有「已失效」占位 + 现有平台，没有「未配置」空选项。
    expect(Array.from(platSel.options).map((o) => o.value)).toEqual(['gone', 'p1', 'p2']);
    // 候选平台存在时悬空只表示「待重选」：不应误报「保存后不会被注入」。
    expect(screen.queryByText(tt('ui.toolsync.claude_no_endpoint'))).not.toBeInTheDocument();
    // 悬空平台没有模型列表：槽位只剩「不设置」，不会沿用其它平台模型。
    const modelSel = screen.getByLabelText(tt('ui.toolsync.slot_model')) as HTMLSelectElement;
    expect(Array.from(modelSel.options).map((o) => o.value)).toEqual(['']);

    fireEvent.change(platSel, { target: { value: 'p2' } });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.toolsync.save') }));

    await waitFor(() => expect(mocks.setToolSyncConfig).toHaveBeenCalledTimes(1));
    const [toolID, input] = mocks.setToolSyncConfig.mock.calls[0];
    expect(toolID).toBe('claude');
    expect(input.ToolPlatforms.claude).toBe('p2');
  });

  it('codebuddy：悬空平台显示占位并可重选', async () => {
    mocks.getToolSyncConfig.mockResolvedValue({
      ...emptyCfg,
      ToolPlatforms: { codebuddy: 'gone' },
    });
    render(
      <ToolSyncDialog toolID="codebuddy" toolName="CodeBuddy" platforms={plats} onClose={() => {}} />,
    );

    const platSel = (await screen.findByLabelText(tt('ui.toolsync.platform'))) as HTMLSelectElement;
    expect(platSel.value).toBe('gone');
    expect(platSel.selectedOptions[0].textContent).toBe(tt('ui.toolsync.tool_platform_invalid'));
    // 悬空时只有「已失效」占位 + 现有平台，没有「未配置」空选项。
    expect(Array.from(platSel.options).map((o) => o.value)).toEqual(['gone', 'p1', 'p2']);
    // 候选平台存在时不误报 no-endpoint 告警。
    expect(screen.queryByText(tt('ui.toolsync.codebuddy_no_endpoint'))).not.toBeInTheDocument();
    // 悬空平台无模型列表，不应展示任何平台的勾选区。
    expect(screen.queryByText(tt('ui.toolsync.models_empty'))).not.toBeInTheDocument();

    fireEvent.change(platSel, { target: { value: 'p1' } });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.toolsync.save') }));

    await waitFor(() => expect(mocks.setToolSyncConfig).toHaveBeenCalledTimes(1));
    const [toolID, input] = mocks.setToolSyncConfig.mock.calls[0];
    expect(toolID).toBe('codebuddy');
    expect(input.ToolPlatforms.codebuddy).toBe('p1');
  });

  it('codex：只显示平台下拉（无启用开关），候选排除 workbuddy，保存写 ToolPlatforms', async () => {
    mocks.getToolSyncConfig.mockResolvedValue(emptyCfg);
    const withWb: ModelPlatformView[] = [
      ...plats,
      {
        ID: 'wb',
        Name: 'WorkBuddy',
        Preset: 'workbuddy',
        OpenAIBaseURL: '',
        AnthropicBaseURL: '',
        Models: ['hy4free'],
        APIKeySet: false,
        ASRModel: '',
      },
    ];
    render(<ToolSyncDialog toolID="codex" toolName="Codex" platforms={withWb} onClose={() => {}} />);

    const platSel = (await screen.findByLabelText(tt('ui.toolsync.platform'))) as HTMLSelectElement;
    // 无静态注入的目标不显示「启用同步」开关。
    expect(screen.queryByRole('checkbox', { name: tt('ui.toolsync.enable') })).not.toBeInTheDocument();
    expect(Array.from(platSel.options).map((o) => o.value)).toEqual(['', 'p1', 'p2']);
    expect(screen.getByText(tt('ui.toolsync.tool_platform_hint'))).toBeInTheDocument();

    fireEvent.change(platSel, { target: { value: 'p2' } });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.toolsync.save') }));

    await waitFor(() => expect(mocks.setToolSyncConfig).toHaveBeenCalledTimes(1));
    const [toolID, input] = mocks.setToolSyncConfig.mock.calls[0];
    expect(toolID).toBe('codex');
    expect(input.ToolPlatforms.codex).toBe('p2');
    expect(input.Enabled).toBe(false);
  });

  it('gemini：悬空平台显示占位并可重选，保存提交新平台', async () => {
    mocks.getToolSyncConfig.mockResolvedValue({
      ...emptyCfg,
      ToolPlatforms: { gemini: 'gone' },
    });
    render(<ToolSyncDialog toolID="gemini" toolName="Gemini" platforms={plats} onClose={() => {}} />);

    const platSel = (await screen.findByLabelText(tt('ui.toolsync.platform'))) as HTMLSelectElement;
    // 悬空值不被静默清空：选中项仍是原 ID，文案显示「已失效」，不会渲染空白。
    expect(platSel.value).toBe('gone');
    expect(platSel.selectedOptions[0].textContent).toBe(tt('ui.toolsync.tool_platform_invalid'));

    fireEvent.change(platSel, { target: { value: 'p1' } });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.toolsync.save') }));

    await waitFor(() => expect(mocks.setToolSyncConfig).toHaveBeenCalledTimes(1));
    const [toolID, input] = mocks.setToolSyncConfig.mock.calls[0];
    expect(toolID).toBe('gemini');
    expect(input.ToolPlatforms.gemini).toBe('p1');
  });

  it('禁用保存：载荷 Enabled=false 且勾选被清空', async () => {
    mocks.getToolSyncConfig.mockResolvedValue({
      Enabled: true,
      Claude: { Model: 'm1', Sonnet: '', Opus: '', Haiku: '' },
      OpenCode: { p1: ['m1'] },
      CodeBuddy: ['m2'],
    });
    render(<ToolSyncDialog toolID="opencode" toolName="OpenCode" platforms={plats} onClose={() => {}} />);

    const enable = await screen.findByRole('checkbox', { name: tt('ui.toolsync.enable') });
    fireEvent.click(enable); // 关闭
    fireEvent.click(screen.getByRole('button', { name: tt('ui.toolsync.save') }));

    await waitFor(() => expect(mocks.setToolSyncConfig).toHaveBeenCalledTimes(1));
    const input = mocks.setToolSyncConfig.mock.calls[0][1];
    expect(input.Enabled).toBe(false);
    // 禁用时勾选的清理由后端负责（SetToolSyncConfig 禁用即清空并只清不建）
  });

  it('取消：不调用保存直接关闭', async () => {
    mocks.getToolSyncConfig.mockResolvedValue(emptyCfg);
    const onClose = vi.fn();
    render(<ToolSyncDialog toolID="opencode" toolName="OpenCode" platforms={plats} onClose={onClose} />);
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.toolsync.cancel') }));
    expect(mocks.setToolSyncConfig).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  it('加载失败：提示错误并关闭弹窗', async () => {
    const notify = vi.fn();
    useAppStore.setState({ notify });
    mocks.getToolSyncConfig.mockRejectedValue(new Error('err.boom'));
    const onClose = vi.fn();
    render(<ToolSyncDialog toolID="opencode" toolName="OpenCode" platforms={plats} onClose={onClose} />);
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(notify).toHaveBeenCalled();
  });
});
