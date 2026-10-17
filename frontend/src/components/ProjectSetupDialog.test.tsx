// 首次项目选择弹窗：等首个 scan:done 后判定弹出、默认全选、确认隐藏未选项、关闭不隐藏。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import ProjectSetupDialog from './ProjectSetupDialog';
import type { Workspace } from '../lib/api';
import { tt } from '../test/i18n';

const mocks = vi.hoisted(() => ({
  needsAgentSetup: vi.fn(),
  needsProjectSetup: vi.fn(),
  dismissProjectSetup: vi.fn(),
  getWorkspaces: vi.fn(),
  hideProject: vi.fn(),
  onScanDone: vi.fn(),
}));

vi.mock('../lib/api', () => mocks);

const workspaces: Workspace[] = [
  { Path: 'D:\\ws-a', Name: 'ws-a', LastUsed: '', SessionCount: 0, ToolCounts: {}, Source: 'sessions' },
  { Path: 'D:\\ws-b', Name: 'ws-b', LastUsed: '', SessionCount: 0, ToolCounts: {}, Source: 'sessions' },
  { Path: 'D:\\ws-c', Name: 'ws-c', LastUsed: '', SessionCount: 0, ToolCounts: {}, Source: 'sessions' },
];

let scanDoneCb: (() => void) | undefined;

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  scanDoneCb = undefined;
  mocks.onScanDone.mockImplementation((cb: () => void) => {
    scanDoneCb = cb;
    return () => {
      scanDoneCb = undefined;
    };
  });
  mocks.needsAgentSetup.mockResolvedValue(false);
  mocks.needsProjectSetup.mockResolvedValue(true);
  mocks.getWorkspaces.mockResolvedValue(workspaces);
  mocks.dismissProjectSetup.mockResolvedValue(undefined);
  mocks.hideProject.mockResolvedValue(undefined);
});

// 触发 scan:done 并等弹窗出现
async function openDialog() {
  render(<ProjectSetupDialog />);
  await waitFor(() => {
    expect(mocks.needsProjectSetup).not.toHaveBeenCalled();
  });
  scanDoneCb?.();
  await screen.findByRole('dialog');
  expect(mocks.needsProjectSetup).toHaveBeenCalled();
}

describe('ProjectSetupDialog', () => {
  it('scan:done 前不判定、不弹窗', async () => {
    render(<ProjectSetupDialog />);
    await waitFor(() => {
      expect(mocks.onScanDone).toHaveBeenCalled();
    });
    expect(mocks.needsProjectSetup).not.toHaveBeenCalled();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('needsProjectSetup 为 false 不弹窗', async () => {
    mocks.needsProjectSetup.mockResolvedValue(false);
    render(<ProjectSetupDialog />);
    scanDoneCb?.();
    await waitFor(() => {
      expect(mocks.needsProjectSetup).toHaveBeenCalled();
    });
    expect(mocks.getWorkspaces).not.toHaveBeenCalled();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('列表为空时落盘标记且不弹窗', async () => {
    mocks.getWorkspaces.mockResolvedValue([]);
    render(<ProjectSetupDialog />);
    scanDoneCb?.();
    await waitFor(() => {
      expect(mocks.dismissProjectSetup).toHaveBeenCalled();
    });
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('needsAgentSetup 为 true 时本次不弹（让位安装向导）', async () => {
    mocks.needsAgentSetup.mockResolvedValue(true);
    render(<ProjectSetupDialog />);
    scanDoneCb?.();
    await waitFor(() => {
      expect(mocks.needsAgentSetup).toHaveBeenCalled();
    });
    expect(mocks.needsProjectSetup).not.toHaveBeenCalled();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('弹出后默认全选', async () => {
    await openDialog();
    for (const ws of workspaces) {
      expect(screen.getByRole('checkbox', { name: ws.Path })).toBeChecked();
    }
  });

  it('取消勾选后确认：仅隐藏未选项并落盘标记', async () => {
    await openDialog();
    fireEvent.click(screen.getByRole('checkbox', { name: 'D:\\ws-b' }));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.project_setup.confirm') }));
    await waitFor(() => {
      expect(mocks.dismissProjectSetup).toHaveBeenCalled();
    });
    expect(mocks.hideProject).toHaveBeenCalledTimes(1);
    expect(mocks.hideProject).toHaveBeenCalledWith('D:\\ws-b');
  });

  it('确认时全部保留：不调 hideProject，仅落盘标记', async () => {
    await openDialog();
    fireEvent.click(screen.getByRole('button', { name: tt('ui.project_setup.confirm') }));
    await waitFor(() => {
      expect(mocks.dismissProjectSetup).toHaveBeenCalled();
    });
    expect(mocks.hideProject).not.toHaveBeenCalled();
  });

  it('关闭按钮：不隐藏任何项目，仅落盘标记', async () => {
    await openDialog();
    fireEvent.click(screen.getByRole('button', { name: tt('ui.project_setup.close') }));
    await waitFor(() => {
      expect(mocks.dismissProjectSetup).toHaveBeenCalled();
    });
    expect(mocks.hideProject).not.toHaveBeenCalled();
  });

  it('hideProject 单项失败不中断其余处理，仍落盘标记', async () => {
    mocks.hideProject.mockRejectedValueOnce(new Error('boom'));
    await openDialog();
    fireEvent.click(screen.getByRole('checkbox', { name: 'D:\\ws-a' }));
    fireEvent.click(screen.getByRole('checkbox', { name: 'D:\\ws-b' }));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.project_setup.confirm') }));
    await waitFor(() => {
      expect(mocks.dismissProjectSetup).toHaveBeenCalled();
    });
    expect(mocks.hideProject).toHaveBeenCalledTimes(2);
  });

  it('弹窗打开期间再收 scan:done 不重置勾选状态', async () => {
    await openDialog();
    fireEvent.click(screen.getByRole('checkbox', { name: 'D:\\ws-b' }));
    expect(screen.getByRole('checkbox', { name: 'D:\\ws-b' })).not.toBeChecked();
    scanDoneCb?.(); // 设置页刷新等触发第二轮 scan:done
    await waitFor(() => {
      expect(mocks.needsProjectSetup).toHaveBeenCalledTimes(1);
    });
    expect(screen.getByRole('checkbox', { name: 'D:\\ws-b' })).not.toBeChecked();
  });
});
