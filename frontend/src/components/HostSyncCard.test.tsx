// HostSyncCard 测试：宿主行筛选（codebuddy 常显 + 检测到的宿主）、全部同步/
// 单宿主同步的参数与状态合并、错误展示。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import HostSyncCard from './HostSyncCard';
import { initI18n } from '../i18n';
import { tt } from '../test/i18n';
import type { ToolInfo } from '../lib/api';

const mocks = vi.hoisted(() => ({
  getHostSyncStatuses: vi.fn(),
  syncHosts: vi.fn(),
  getTools: vi.fn(),
  onToolsUpdated: vi.fn(() => () => {}),
}));
vi.mock('../lib/api', () => mocks);

vi.mock('../state/store', () => ({
  useAppStore: (selector: (s: { notify: (m: string, k: string) => void }) => unknown) =>
    selector({ notify: vi.fn() }),
}));

const tools: ToolInfo[] = [
  { ID: 'codebuddy', Name: 'CodeBuddy', BinPath: 'x', Version: '2.0.0', Installed: true, Source: 'path' },
  { ID: 'claude', Name: 'Claude Code', BinPath: 'y', Version: '1.0.0', Installed: true, Source: 'path' },
  { ID: 'gemini', Name: 'Gemini', BinPath: '', Version: '', Installed: false, Source: '' },
];

const hostStatus = {
  binaryState: 'absent',
  pathState: 'missing',
  binaryPath: 'C:/u/.kshell/bin/kshell.exe',
  skills: [
    { toolID: 'codebuddy', state: 'absent', path: 'C:/u/.codebuddy/skills/kshell/SKILL.md' },
    { toolID: 'claude', state: 'absent', path: 'C:/u/.claude/skills/kshell/SKILL.md' },
    { toolID: 'gemini', state: 'absent', path: 'C:/u/.gemini/skills/kshell/SKILL.md' },
    { toolID: 'trae', state: 'absent', path: 'C:/u/.traecli/skills/kshell/SKILL.md' },
  ],
};

beforeEach(() => {
  vi.clearAllMocks();
  mocks.getTools.mockResolvedValue(tools);
  mocks.getHostSyncStatuses.mockResolvedValue(hostStatus);
  mocks.syncHosts.mockResolvedValue(hostStatus);
});

afterEach(cleanup);

describe('HostSyncCard', () => {
  it('codebuddy+已装宿主逐行显示，全部同步只带可见宿主', async () => {
    render(<HostSyncCard />);
    const card = await screen.findByText(tt('ui.settings.hostsync.title')).then((el) => el.closest('section')!);
    // gemini/trae 未检测到，不出现（trae 已知宿主但未安装；mytool 不在 skills 里）
    expect(within(card).getByText('CodeBuddy')).toBeInTheDocument();
    expect(within(card).getByText('Claude Code')).toBeInTheDocument();
    expect(within(card).queryByText('Gemini')).not.toBeInTheDocument();

    fireEvent.click(within(card).getByRole('button', { name: tt('ui.settings.hostsync.all') }));
    expect(mocks.syncHosts).toHaveBeenCalledWith(['codebuddy', 'claude']);
  });

  it('单宿主同步按 toolID 合并状态，不顶掉其它宿主行', async () => {
    mocks.syncHosts.mockResolvedValue({
      ...hostStatus,
      binaryState: 'released',
      pathState: 'added',
      skills: [{ toolID: 'claude', state: 'installed', path: 'C:/u/.claude/skills/kshell/SKILL.md' }],
    });
    render(<HostSyncCard />);
    const card = await screen.findByText(tt('ui.settings.hostsync.title')).then((el) => el.closest('section')!);
    const rowButtons = within(card).getAllByRole('button', { name: tt('ui.settings.hostsync.sync') });
    fireEvent.click(rowButtons[1]); // claude 行
    expect(mocks.syncHosts).toHaveBeenCalledWith(['claude']);
    await waitFor(() => {
      expect(within(card).getAllByText(tt('ui.settings.hostsync.state_installed')).length).toBe(1);
    });
    expect(within(card).getByText('CodeBuddy')).toBeInTheDocument();
    expect(within(card).getAllByText(tt('ui.settings.hostsync.state_absent')).length).toBe(1);
  });

  it('错误原样展示', async () => {
    mocks.getHostSyncStatuses.mockResolvedValue({
      binaryState: 'error',
      binaryErr: 'disk full',
      pathState: 'missing',
      skills: [],
    });
    render(<HostSyncCard />);
    expect(await screen.findByText('disk full')).toBeInTheDocument();
  });
});

// initI18n 保证 tt() 有资源可用（与其它组件测试同口径）。
beforeEach(() => {
  void initI18n('en', {});
});
