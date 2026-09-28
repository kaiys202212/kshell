// SshPanel 组件测试：连接列表渲染（来源中文标注 + SourceFile title 提示 + Verified ✓）、
// 「连接」→ OpenSSH 且 open 状态复用聚焦（Go 侧幂等，前端保持状态）、
// 命令执行 → ExecRemote、输出尾部展示（Stdout/Stderr 各取尾部）、ExitCode/Duration 展示。
// api 层整体打桩（vi.mock），与 SessionList.test 同一套模式。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import SshPanel from './SshPanel';
import type { RemoteResult, SshConnection } from '../lib/api';
import { useAppStore } from '../state/store';

const mocks = vi.hoisted(() => ({
  listConnections: vi.fn(),
  openSSH: vi.fn(),
  execRemote: vi.fn(),
}));
vi.mock('../lib/api', () => mocks);

const conns: SshConnection[] = [
  {
    ID: 'c1',
    Name: '生产机',
    Host: '10.0.0.1',
    User: 'root',
    Port: 22,
    IdentityFile: '',
    Workspace: '',
    Source: 'sshconfig',
    SourceFile: 'C:\\Users\\me\\.ssh\\config',
    Verified: true,
  },
  {
    ID: 'c2',
    Name: '跳板机',
    Host: 'jump.example.com',
    User: '',
    Port: 2222,
    IdentityFile: '',
    Workspace: '',
    Source: 'deploy',
    SourceFile: '',
    Verified: false,
  },
];

function result(over: Partial<RemoteResult>): RemoteResult {
  return { Stdout: '', Stderr: '', ExitCode: 0, Duration: 0, ...over };
}

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  mocks.listConnections.mockResolvedValue(conns);
  mocks.openSSH.mockResolvedValue(undefined);
  mocks.execRemote.mockResolvedValue(result({ Stdout: 'ok' }));
  useAppStore.setState({ windowStatus: {}, toasts: [] });
});

// 按连接名找列表行（li 元素）
async function findRow(name: string): Promise<HTMLElement> {
  const text = await screen.findByText(name);
  const row = text.closest('li');
  if (!row) throw new Error(`找不到连接行: ${name}`);
  return row;
}

describe('SshPanel', () => {
  it('渲染连接列表：名称 + user@host(:port) + 来源中文标注（SourceFile 作 title 提示）+ 已验证标记', async () => {
    render(<SshPanel wsPath="D:\\proj-a" />);

    const row1 = await findRow('生产机');
    expect(within(row1).getByText('root@10.0.0.1')).toBeInTheDocument();
    const src1 = within(row1).getByText('ssh 配置');
    expect(src1).toHaveAttribute('title', 'C:\\Users\\me\\.ssh\\config');
    expect(within(row1).getByText('✓')).toBeInTheDocument(); // Verified

    const row2 = await findRow('跳板机');
    expect(within(row2).getByText('jump.example.com:2222')).toBeInTheDocument();
    expect(within(row2).getByText('部署脚本')).toBeInTheDocument();
    expect(within(row2).queryByText('✓')).not.toBeInTheDocument();
  });

  it('没有连接时给空态文案', async () => {
    mocks.listConnections.mockResolvedValue([]);
    render(<SshPanel wsPath="D:\\proj-a" />);
    expect(await screen.findByText('没有可用的 SSH 连接')).toBeInTheDocument();
  });

  it('列表加载中显示骨架屏，不闪错误/空态', () => {
    mocks.listConnections.mockReturnValue(new Promise(() => {})); // 永不 resolve
    const { container } = render(<SshPanel wsPath="D:\\proj-a" />);
    expect(container.querySelectorAll('.animate-pulse')).toHaveLength(3);
    expect(screen.queryByText('没有可用的 SSH 连接')).not.toBeInTheDocument();
  });

  it('列表加载失败时面板级 error 呈现错误信息', async () => {
    mocks.listConnections.mockRejectedValue(new Error('绑定不可用'));
    render(<SshPanel wsPath="D:\\proj-a" />);
    expect(await screen.findByText(/绑定不可用/)).toBeInTheDocument();
  });

  it('点击「连接」调用 OpenSSH，并按窗口标题把 open 状态置 true', async () => {
    render(<SshPanel wsPath="D:\\proj-a" />);
    const row = await findRow('生产机');

    await act(async () => {
      fireEvent.click(within(row).getByRole('button', { name: '连接' }));
    });

    expect(mocks.openSSH).toHaveBeenCalledWith('c1');
    expect(useAppStore.getState().windowStatus['kshell · 生产机']).toBe(true);
    expect(row).toHaveClass('bg-primary/5');
  });

  it('已 open 的连接再点「连接」仍调 OpenSSH（Go 侧幂等转聚焦），状态保持 open', async () => {
    useAppStore.setState({ windowStatus: { 'kshell · 生产机': true } });
    render(<SshPanel wsPath="D:\\proj-a" />);
    const row = await findRow('生产机');
    expect(row).toHaveClass('bg-primary/5');

    await act(async () => {
      fireEvent.click(within(row).getByRole('button', { name: '连接' }));
    });

    expect(mocks.openSSH).toHaveBeenCalledWith('c1');
    expect(useAppStore.getState().windowStatus['kshell · 生产机']).toBe(true);
    expect(row).toHaveClass('bg-primary/5');
  });

  it('OpenSSH 失败时不置 open 状态，走轻量提示（notify）而不炸面板', async () => {
    mocks.openSSH.mockRejectedValue(new Error('未找到 ssh 可执行文件'));
    render(<SshPanel wsPath="D:\\proj-a" />);
    const row = await findRow('生产机');

    await act(async () => {
      fireEvent.click(within(row).getByRole('button', { name: '连接' }));
    });

    expect(useAppStore.getState().windowStatus['kshell · 生产机']).toBeUndefined();
    expect(row).not.toHaveClass('bg-primary/5');
    // 操作失败走 store 的轻量提示，面板本身保持完整渲染（列表仍在）
    expect(
      useAppStore.getState().toasts.some((t) => t.title.includes('未找到 ssh 可执行文件')),
    ).toBe(true);
    expect(screen.getByLabelText('SSH 连接列表')).toBeInTheDocument();
  });

  it('命令执行：默认选中第一个连接，输入命令点「执行」调 ExecRemote，展示 Stdout 尾部 + ExitCode + Duration', async () => {
    const lines = Array.from({ length: 60 }, (_, i) => `line-${i + 1}`);
    mocks.execRemote.mockResolvedValue(
      result({ Stdout: lines.join('\n'), ExitCode: 0, Duration: 1_500_000_000 }),
    );
    render(<SshPanel wsPath="D:\\proj-a" />);
    await findRow('生产机');

    fireEvent.change(screen.getByLabelText('执行命令'), {
      target: { value: 'uname -a' },
    });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '执行' }));
    });

    expect(mocks.execRemote).toHaveBeenCalledWith('c1', 'uname -a');
    // 尾部 50 行：line-11 起可见，line-1 不展示
    const out = screen.getByLabelText('命令输出');
    expect(out.textContent).toContain('line-11');
    expect(out.textContent).toContain('line-60');
    expect(out.textContent).not.toMatch(/line-1\n/);
    expect(screen.getByText(/退出码 0/)).toBeInTheDocument();
    expect(screen.getByText(/1.50s/)).toBeInTheDocument();
  });

  it('非 0 退出码不是异常：展示 Stderr 尾部与退出码', async () => {
    const errLines = Array.from({ length: 55 }, (_, i) => `err-${i + 1}`);
    mocks.execRemote.mockResolvedValue(
      result({ Stderr: errLines.join('\n'), ExitCode: 1, Duration: 20_000_000 }),
    );
    render(<SshPanel wsPath="D:\\proj-a" />);
    await findRow('生产机');

    fireEvent.change(screen.getByLabelText('执行命令'), { target: { value: 'ls /nope' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '执行' }));
    });

    expect(screen.getByLabelText('错误输出').textContent).toContain('err-6'); // 55 行取尾部 50 → err-6 起
    expect(screen.getByLabelText('错误输出').textContent).not.toMatch(/err-5\n/);
    expect(screen.getByText(/退出码 1/)).toBeInTheDocument();
  });

  it('ExecRemote 调用失败（网络/绑定异常）时显示错误提示', async () => {
    mocks.execRemote.mockRejectedValue(new Error('连接超时'));
    render(<SshPanel wsPath="D:\\proj-a" />);
    await findRow('生产机');

    fireEvent.change(screen.getByLabelText('执行命令'), { target: { value: 'echo hi' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '执行' }));
    });

    expect(await screen.findByText(/连接超时/)).toBeInTheDocument();
  });

  it('切换选中连接后，命令针对新选中连接执行', async () => {
    render(<SshPanel wsPath="D:\\proj-a" />);
    const row2 = await findRow('跳板机');

    fireEvent.click(within(row2).getByText('跳板机'));
    fireEvent.change(screen.getByLabelText('执行命令'), { target: { value: 'uptime' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '执行' }));
    });

    expect(mocks.execRemote).toHaveBeenCalledWith('c2', 'uptime');
  });
});
