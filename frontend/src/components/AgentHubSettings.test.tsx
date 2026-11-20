// AgentHubSettings 测试：mock api，覆盖初载回显、启用开关即时落盘、保存传参（trim 后列表）。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { tt } from '../test/i18n';
import { AgentHubSettings } from './AgentHubSettings';

const mocks = vi.hoisted(() => ({
  getAgentHubSettings: vi.fn(),
  setAgentHubEnabled: vi.fn(),
  saveAgentHubSettings: vi.fn(),
}));

vi.mock('../lib/api', () => mocks);
// 微信区自身有绑定/轮询逻辑，这里只验证门控显隐，用占位组件隔离
vi.mock('./WeixinSettings', () => ({
  WeixinSettings: () => <div data-testid="weixin-settings" />,
}));
// 本地语音区自带安装事件订阅与 api 调用，同样用占位组件隔离
vi.mock('./ASRSettings', () => ({
  ASRSettings: () => <div data-testid="asr-settings" />,
}));

const settings = (over: Record<string, unknown> = {}) => ({
  enabled: true,
  heartbeat_minutes: 5,
  direct_patterns: null,
  default_tool: '',
  default_model: '',
  route_mode: '',
  ...over,
});

beforeEach(() => {
  vi.clearAllMocks();
  mocks.getAgentHubSettings.mockResolvedValue(settings());
  mocks.setAgentHubEnabled.mockResolvedValue(undefined);
  mocks.saveAgentHubSettings.mockResolvedValue(undefined);
});

afterEach(() => cleanup());

describe('AgentHubSettings', () => {
  it('初载按设置回显：勾选态、心跳阈值、直通关键词逐行', async () => {
    render(<AgentHubSettings />);
    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: tt('ui.settings.agenthub.enabled') })).toBeChecked();
    });
    expect(
      screen.getByRole('spinbutton', { name: tt('ui.settings.agenthub.heartbeat') }),
    ).toHaveValue(5);
    expect(screen.getByRole('textbox', { name: tt('ui.settings.agenthub.patterns') })).toHaveValue('');
  });

  it('直通关键词按设置的行回显', async () => {
    mocks.getAgentHubSettings.mockResolvedValue(settings({ direct_patterns: ['#fix', 'review'], heartbeat_minutes: 7 }));
    render(<AgentHubSettings />);
    await waitFor(() => {
      expect(screen.getByRole('textbox', { name: tt('ui.settings.agenthub.patterns') })).toHaveValue('#fix\nreview');
    });
    expect(screen.getByRole('spinbutton', { name: tt('ui.settings.agenthub.heartbeat') })).toHaveValue(7);
  });

  it('关闭开关先弹确认框，确认后落盘并隐藏微信区', async () => {
    render(<AgentHubSettings />);
    const box = await screen.findByRole('checkbox', { name: tt('ui.settings.agenthub.enabled') });
    expect(await screen.findByTestId('weixin-settings')).toBeInTheDocument();
    fireEvent.click(box);
    // 未确认前不落盘
    expect(mocks.setAgentHubEnabled).not.toHaveBeenCalled();
    expect(screen.getByText(tt('ui.settings.agenthub.disable_confirm_title'))).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole('button', { name: tt('ui.settings.agenthub.disable_confirm_ok') }),
    );
    await waitFor(() => expect(mocks.setAgentHubEnabled).toHaveBeenCalledWith(false));
    await waitFor(() => expect(screen.queryByTestId('weixin-settings')).toBeNull());
  });

  it('确认框取消：不调接口且开关保持勾选', async () => {
    render(<AgentHubSettings />);
    const box = await screen.findByRole('checkbox', { name: tt('ui.settings.agenthub.enabled') });
    fireEvent.click(box);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.agenthub.cancel') }));
    await waitFor(() => {
      expect(
        screen.queryByText(tt('ui.settings.agenthub.disable_confirm_title')),
      ).toBeNull();
    });
    expect(mocks.setAgentHubEnabled).not.toHaveBeenCalled();
    expect(box).toBeChecked();
  });

  it('关闭状态下点开关直接开启，不弹确认框', async () => {
    mocks.getAgentHubSettings.mockResolvedValue(settings({ enabled: false }));
    render(<AgentHubSettings />);
    const box = await screen.findByRole('checkbox', { name: tt('ui.settings.agenthub.enabled') });
    expect(box).not.toBeChecked();
    expect(screen.queryByTestId('weixin-settings')).toBeNull();
    fireEvent.click(box);
    await waitFor(() => expect(mocks.setAgentHubEnabled).toHaveBeenCalledWith(true));
    expect(screen.queryByText(tt('ui.settings.agenthub.disable_confirm_title'))).toBeNull();
    expect(await screen.findByTestId('weixin-settings')).toBeInTheDocument();
  });

  it('保存按钮 dirty 前禁用，编辑后点击按 trim 后列表传参', async () => {
    render(<AgentHubSettings />);
    const saveBtn = await screen.findByRole('button', { name: tt('ui.settings.agenthub.save') });
    expect(saveBtn).toBeDisabled();
    const area = screen.getByRole('textbox', { name: tt('ui.settings.agenthub.patterns') });
    fireEvent.change(area, { target: { value: ' #fix \n\nreview\n' } });
    expect(saveBtn).toBeEnabled();
    fireEvent.click(saveBtn);
    await waitFor(() => {
      expect(mocks.saveAgentHubSettings).toHaveBeenCalledWith(5, ['#fix', 'review']);
    });
  });
});
