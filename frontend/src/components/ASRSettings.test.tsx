// ASRSettings 测试：mock api，覆盖未安装/已安装/安装中日志流/保存传参/
// 卸载二次确认/平台不支持各态。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { tt } from '../test/i18n';
import { useAppStore } from '../state/store';
import { ASRSettings } from './ASRSettings';

type LogCb = (text: string) => void;
type DoneCb = (p: { ok: boolean; error?: string }) => void;
type ProgressCb = (p: { phase: string; pct: number; overall: number }) => void;

const mocks = vi.hoisted(() => ({
  getASRStatus: vi.fn(),
  getASRInstallLog: vi.fn(),
  installASRLocal: vi.fn(),
  uninstallASRLocal: vi.fn(),
  saveASRSettings: vi.fn(),
  onASRInstallLog: vi.fn(),
  onASRInstallProgress: vi.fn(),
  onASRInstallDone: vi.fn(),
  logCbs: [] as LogCb[],
  progressCbs: [] as ProgressCb[],
  doneCbs: [] as DoneCb[],
}));

vi.mock('../lib/api', () => mocks);

const baseStatus = (over: Record<string, unknown> = {}) => ({
  installed: false,
  partial: false,
  enginePath: '',
  modelPath: '',
  version: '',
  sizeMB: 0,
  supported: true,
  platform: 'windows/amd64',
  enabled: false,
  preferCloud: false,
  language: 'auto',
  itn: true,
  mirrors: [],
  command: '',
  running: false,
  error: '',
  ...over,
});

function mockStatus(over: Record<string, unknown> = {}) {
  const view = baseStatus(over);
  mocks.getASRStatus.mockResolvedValue(view);
  return view;
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.logCbs.length = 0;
  mocks.progressCbs.length = 0;
  mocks.doneCbs.length = 0;
  mocks.onASRInstallLog.mockImplementation((cb: LogCb) => {
    mocks.logCbs.push(cb);
    return () => {};
  });
  mocks.onASRInstallProgress.mockImplementation((cb: ProgressCb) => {
    mocks.progressCbs.push(cb);
    return () => {};
  });
  mocks.onASRInstallDone.mockImplementation((cb: DoneCb) => {
    mocks.doneCbs.push(cb);
    return () => {};
  });
  mocks.getASRInstallLog.mockResolvedValue('');
  mocks.installASRLocal.mockResolvedValue(undefined);
  mocks.uninstallASRLocal.mockResolvedValue(undefined);
  mockStatus();
});

afterEach(() => cleanup());

describe('ASRSettings', () => {
  it('未安装时渲染安装按钮，点击后调用安装并刷新状态', async () => {
    render(<ASRSettings />);
    const btn = await screen.findByRole('button', { name: tt('ui.asr.install') });
    fireEvent.click(btn);
    expect(mocks.installASRLocal).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(mocks.getASRStatus).toHaveBeenCalledTimes(2));
  });

  it('已安装时展示版本/占用并只给卸载按钮', async () => {
    mockStatus({ installed: true, version: 'v2.0.2', sizeMB: 229 });
    render(<ASRSettings />);
    expect(await screen.findByText(/v2\.0\.2/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: tt('ui.asr.install') })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: tt('ui.asr.uninstall') })).toBeInTheDocument();
  });

  it('安装中回填历史日志并追加事件行，done 后刷新状态', async () => {
    mockStatus({ running: true });
    mocks.getASRInstallLog.mockResolvedValue('下载语音引擎 10%');
    render(<ASRSettings />);
    expect(await screen.findByText(/下载语音引擎 10%/)).toBeInTheDocument();
    act(() => mocks.logCbs[0]('下载语音模型 50%'));
    expect(await screen.findByText(/下载语音模型 50%/)).toBeInTheDocument();
    expect(screen.getByText(tt('ui.asr.installing'))).toBeInTheDocument();
    const before = mocks.getASRStatus.mock.calls.length;
    act(() => mocks.doneCbs[0]({ ok: true }));
    await waitFor(() => expect(mocks.getASRStatus.mock.calls.length).toBeGreaterThan(before));
  });

  it('安装中按事件更新进度条与阶段，done 后弹出完成提示', async () => {
    mockStatus({ running: true, progress: { phase: 'extract', pct: 0, overall: 99 } });
    render(<ASRSettings />);
    const bar = await screen.findByRole('progressbar', { name: tt('ui.asr.progress') });
    expect(bar).toHaveAttribute('aria-valuenow', '99');
    expect(screen.getByText(tt('ui.asr.phase_extract'))).toBeInTheDocument();
    act(() => mocks.progressCbs[0]({ phase: 'runtime', pct: 10, overall: 1 }));
    expect(bar).toHaveAttribute('aria-valuenow', '1');
    expect(screen.getByText(tt('ui.asr.phase_runtime'))).toBeInTheDocument();
    act(() => mocks.progressCbs[0]({ phase: 'model.int8.onnx', pct: 50, overall: 66 }));
    expect(bar).toHaveAttribute('aria-valuenow', '66');
    expect(screen.getByText('66%')).toBeInTheDocument();
    act(() => mocks.doneCbs[0]({ ok: true }));
    expect(useAppStore.getState().toasts.some((x) => x.title === tt('ui.asr.install_done'))).toBe(true);
  });

  it('保存把镜像按行拆分并传归一化配置', async () => {
    render(<ASRSettings />);
    const mirrors = await screen.findByRole('textbox', { name: tt('ui.asr.mirrors') });
    fireEvent.change(mirrors, { target: { value: ' https://m.example/{url} \n\n https://gh-proxy.com/{url} ' } });
    fireEvent.change(screen.getByRole('textbox', { name: tt('ui.asr.command') }), {
      target: { value: ' my-asr {wav} ' },
    });
    fireEvent.click(screen.getByRole('checkbox', { name: tt('ui.asr.enabled') }));
    fireEvent.click(screen.getByRole('checkbox', { name: tt('ui.asr.prefer_cloud') }));
    fireEvent.change(screen.getByRole('combobox', { name: tt('ui.asr.language') }), {
      target: { value: 'zh' },
    });
    mocks.saveASRSettings.mockResolvedValue(
      baseStatus({ enabled: true, preferCloud: true, language: 'zh', command: 'my-asr {wav}' }),
    );
    fireEvent.click(screen.getByRole('button', { name: tt('ui.asr.save') }));
    await waitFor(() =>
      expect(mocks.saveASRSettings).toHaveBeenCalledWith({
        enabled: true,
        preferCloud: true,
        language: 'zh',
        itn: true,
        mirrors: ['https://m.example/{url}', 'https://gh-proxy.com/{url}'],
        command: 'my-asr {wav}',
      }),
    );
  });

  it('卸载需二次点击确认', async () => {
    mockStatus({ installed: true, version: 'v2.0.2', sizeMB: 229 });
    render(<ASRSettings />);
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.asr.uninstall') }));
    expect(mocks.uninstallASRLocal).not.toHaveBeenCalled();
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.asr.uninstall_confirm') }));
    await waitFor(() => expect(mocks.uninstallASRLocal).toHaveBeenCalledTimes(1));
  });

  it('平台不支持时给出提示且不显示安装按钮', async () => {
    mockStatus({ supported: false, platform: 'freebsd/amd64' });
    render(<ASRSettings />);
    expect(await screen.findByText(tt('ui.asr.unsupported'))).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: tt('ui.asr.install') })).not.toBeInTheDocument();
    expect(screen.getByRole('textbox', { name: tt('ui.asr.command') })).toBeInTheDocument();
  });
});