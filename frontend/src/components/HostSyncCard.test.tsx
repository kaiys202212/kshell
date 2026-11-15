// HostSyncCard 测试：WorkBuddy 主动同步收窄 + 其它宿主自助安装（复制指令）。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import HostSyncCard from './HostSyncCard';
import { tt } from '../test/i18n';

const mocks = vi.hoisted(() => ({
  getHostSyncStatuses: vi.fn(),
  syncHosts: vi.fn(),
}));
vi.mock('../lib/api', () => mocks);

const notify = vi.fn();
vi.mock('../state/store', () => ({
  useAppStore: (selector: (s: { notify: (m: string, k: string) => void }) => unknown) =>
    selector({ notify }),
}));

const hostStatus = {
  binaryState: 'present',
  pathState: 'ready',
  binaryPath: 'C:/u/.kshell/bin/kshell.exe',
  skills: [
    { toolID: 'codebuddy', state: 'present', path: 'C:/u/.codebuddy/skills/kshell/SKILL.md' },
    { toolID: 'claude', state: 'absent', path: 'C:/u/.claude/skills/kshell/SKILL.md' },
  ],
};

beforeEach(() => {
  vi.clearAllMocks();
  mocks.getHostSyncStatuses.mockResolvedValue(hostStatus);
  mocks.syncHosts.mockResolvedValue(hostStatus);
});

afterEach(cleanup);

describe('HostSyncCard', () => {
  it('WorkBuddy 主行状态 + 同步按钮只带 codebuddy，无全部同步/其它宿主行', async () => {
    render(<HostSyncCard />);
    const card = await screen.findByText(tt('ui.settings.hostsync.title')).then((el) => el.closest('section')!);
    expect(within(card).getByText('WorkBuddy')).toBeInTheDocument();
    // CLI 行与 WorkBuddy 行均为 present（已安装）
    expect(within(card).getAllByText(tt('ui.settings.hostsync.state_present')).length).toBe(2);
    // 收窄：同步按钮只有 WorkBuddy 一个，其它宿主行不再出现
    expect(within(card).getAllByRole('button', { name: tt('ui.settings.hostsync.sync') }).length).toBe(1);
    expect(within(card).queryByText('Claude Code')).not.toBeInTheDocument();

    fireEvent.click(within(card).getByRole('button', { name: tt('ui.settings.hostsync.sync') }));
    expect(mocks.syncHosts).toHaveBeenCalledWith(['codebuddy']);
  });

  it('自助安装区块：复制指令文本', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    render(<HostSyncCard />);
    const card = await screen.findByText(tt('ui.settings.hostsync.title')).then((el) => el.closest('section')!);
    const instruction = tt('ui.settings.hostsync.selfhelp_instruction');
    expect(within(card).getByText(new RegExp('host-install.md'))).toBeInTheDocument();
    fireEvent.click(within(card).getByRole('button', { name: tt('ui.settings.hostsync.copy') }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(instruction));
    expect(await screen.findByText(tt('ui.settings.hostsync.copied'))).toBeInTheDocument();
  });

  it('同步失败：WorkBuddy 行错误原样展示', async () => {
    mocks.getHostSyncStatuses.mockResolvedValue({
      ...hostStatus,
      skills: [{ toolID: 'codebuddy', state: 'error', err: 'skill boom', path: 'x' }],
    });
    render(<HostSyncCard />);
    expect(await screen.findByText('skill boom')).toBeInTheDocument();
  });
});
