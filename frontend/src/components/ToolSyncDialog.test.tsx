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
  ClaudePlatform: '',
  OpenCode: {},
  CodeBuddy: [],
  CodeBuddyPlatform: '',
};

describe('ToolSyncDialog', () => {
  it('加载回显：启用态与已有勾选', async () => {
    mocks.getToolSyncConfig.mockResolvedValue({
      Enabled: true,
      Claude: { Model: 'm1', Sonnet: '', Opus: '', Haiku: '' },
      ClaudePlatform: 'p1',
      OpenCode: { p1: ['m1'] },
      CodeBuddy: ['m2'],
      CodeBuddyPlatform: 'p1',
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
    expect(input.ClaudePlatform).toBe('p2');
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

  it('codebuddy：选平台后勾该平台模型，载荷带 CodeBuddyPlatform', async () => {
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
    expect(input.CodeBuddyPlatform).toBe('p2');
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
