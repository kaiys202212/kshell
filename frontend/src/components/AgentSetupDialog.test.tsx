// 首次 Agent 安装向导：扫描结果、默认勾选未装、跳过落盘、串行一键安装。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import AgentSetupDialog from './AgentSetupDialog';
import type { InstallRecipeView, ToolInfo } from '../lib/api';

const mocks = vi.hoisted(() => ({
  needsAgentSetup: vi.fn(),
  dismissAgentSetup: vi.fn(),
  getTools: vi.fn(),
  getToolInstallRecipe: vi.fn(),
  installBuiltinTool: vi.fn(),
  onToolInstallDone: vi.fn(),
  onToolInstallLog: vi.fn(),
}));

vi.mock('../lib/api', () => mocks);

const tools: ToolInfo[] = [
  {
    ID: 'claude',
    Name: 'Claude Code',
    BinPath: 'claude',
    Version: '1.0',
    Installed: true,
    Source: 'path',
  },
  {
    ID: 'gemini',
    Name: 'Gemini CLI',
    BinPath: '',
    Version: '',
    Installed: false,
    Source: '',
  },
  {
    ID: 'opencode',
    Name: 'OpenCode',
    BinPath: '',
    Version: '',
    Installed: false,
    Source: '',
  },
];

const recipes: Record<string, InstallRecipeView> = {
  claude: {
    ToolID: 'claude',
    Name: 'Claude Code',
    InstallCmd: 'npm i -g @anthropic-ai/claude-code',
    UninstallCmd: 'npm uninstall -g @anthropic-ai/claude-code',
    PurgeDirs: [],
    CanPurge: true,
  },
  gemini: {
    ToolID: 'gemini',
    Name: 'Gemini CLI',
    InstallCmd: 'npm i -g @google/gemini-cli',
    UninstallCmd: 'npm uninstall -g @google/gemini-cli',
    PurgeDirs: [],
    CanPurge: true,
  },
  opencode: {
    ToolID: 'opencode',
    Name: 'OpenCode',
    InstallCmd: 'npm i -g opencode-ai',
    UninstallCmd: 'npm uninstall -g opencode-ai',
    PurgeDirs: [],
    CanPurge: true,
  },
};

let doneCb: ((p: { toolID: string; action: string; ok: boolean; error?: string }) => void) | undefined;

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  doneCb = undefined;
  mocks.needsAgentSetup.mockResolvedValue(true);
  mocks.dismissAgentSetup.mockResolvedValue(undefined);
  mocks.getTools.mockResolvedValue(tools);
  mocks.getToolInstallRecipe.mockImplementation(async (id: string) => {
    const r = recipes[id];
    if (!r) throw new Error('该工具不支持一键安装');
    return r;
  });
  mocks.onToolInstallLog.mockImplementation(() => () => {});
  mocks.onToolInstallDone.mockImplementation((cb: NonNullable<typeof doneCb>) => {
    doneCb = cb;
    return () => {
      doneCb = undefined;
    };
  });
  mocks.installBuiltinTool.mockImplementation(async (id: string) => {
    queueMicrotask(() => {
      doneCb?.({ toolID: id, action: 'install', ok: true });
    });
  });
});

describe('AgentSetupDialog', () => {
  it('NeedsAgentSetup 为 false 时不渲染向导', async () => {
    mocks.needsAgentSetup.mockResolvedValue(false);
    render(<AgentSetupDialog />);
    await waitFor(() => {
      expect(mocks.needsAgentSetup).toHaveBeenCalled();
    });
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('列出已安装与未安装，未安装默认勾选', async () => {
    render(<AgentSetupDialog />);
    expect(await screen.findByRole('dialog')).toBeInTheDocument();
    expect(screen.getByText('Claude Code')).toBeInTheDocument();
    expect(screen.getByText('已安装')).toBeInTheDocument();
    const gemini = screen.getByRole('checkbox', { name: /Gemini CLI/ });
    const opencode = screen.getByRole('checkbox', { name: /OpenCode/ });
    expect(gemini).toBeChecked();
    expect(opencode).toBeChecked();
    expect(screen.queryByRole('checkbox', { name: /Claude Code/ })).not.toBeInTheDocument();
  });

  it('点跳过调用 DismissAgentSetup 并关闭', async () => {
    render(<AgentSetupDialog />);
    fireEvent.click(await screen.findByRole('button', { name: '跳过' }));
    await waitFor(() => {
      expect(mocks.dismissAgentSetup).toHaveBeenCalled();
    });
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    });
  });

  it('Escape 不关闭向导、不 dismiss', async () => {
    render(<AgentSetupDialog />);
    expect(await screen.findByRole('dialog')).toBeInTheDocument();
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(mocks.dismissAgentSetup).not.toHaveBeenCalled();
  });

  it('一键安装等上一枚 done 后再装下一枚；失败后继续；全部结束后 dismiss', async () => {
    const order: string[] = [];
    let releaseGemini: () => void = () => {};
    const geminiHold = new Promise<void>((resolve) => {
      releaseGemini = resolve;
    });
    mocks.installBuiltinTool.mockImplementation(async (id: string) => {
      order.push(`start:${id}`);
      if (id === 'gemini') await geminiHold;
      queueMicrotask(() => {
        doneCb?.({
          toolID: id,
          action: 'install',
          ok: id !== 'gemini',
          error: id === 'gemini' ? 'npm missing' : undefined,
        });
      });
    });
    render(<AgentSetupDialog />);
    fireEvent.click(await screen.findByRole('button', { name: '一键安装' }));
    await waitFor(() => {
      expect(order).toEqual(['start:gemini']);
    });
    expect(mocks.installBuiltinTool).toHaveBeenCalledTimes(1);
    releaseGemini();
    await waitFor(() => {
      expect(order).toEqual(['start:gemini', 'start:opencode']);
    });
    await waitFor(() => {
      expect(mocks.dismissAgentSetup).toHaveBeenCalled();
    });
    expect(mocks.installBuiltinTool).not.toHaveBeenCalledWith('claude');
  });
});
