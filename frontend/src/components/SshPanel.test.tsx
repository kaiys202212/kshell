// SshPanel：连接列表、新建/编辑、双击打开远程、命令执行。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import SshPanel from './SshPanel';
import type { RemoteResult, SshConnection } from '../lib/api';
import { useAppStore } from '../state/store';

const mocks = vi.hoisted(() => ({
  listConnections: vi.fn(),
  upsertConnection: vi.fn(),
  deleteConnection: vi.fn(),
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
  mocks.upsertConnection.mockImplementation(async (c: SshConnection) => ({
    ...c,
    ID: c.ID || 'new-id',
    Source: c.Source || 'manual',
  }));
  mocks.deleteConnection.mockResolvedValue(undefined);
  mocks.execRemote.mockResolvedValue(result({ Stdout: 'ok' }));
  useAppStore.setState({ windowStatus: {}, toasts: [] });
});

async function findRow(name: string): Promise<HTMLElement> {
  const text = await screen.findByText(name);
  const row = text.closest('li');
  if (!row) throw new Error(`找不到连接行: ${name}`);
  return row;
}

describe('SshPanel', () => {
  it('渲染连接列表：名称 + user@host(:port) + 来源中文标注 + 已验证标记', async () => {
    render(<SshPanel wsPath="D:\\proj-a" />);

    const row1 = await findRow('生产机');
    expect(within(row1).getByText('root@10.0.0.1')).toBeInTheDocument();
    const src1 = within(row1).getByText('ssh 配置');
    expect(src1).toHaveAttribute('title', 'C:\\Users\\me\\.ssh\\config');
    expect(within(row1).getByText('✓')).toBeInTheDocument();

    const row2 = await findRow('跳板机');
    expect(within(row2).getByText('jump.example.com:2222')).toBeInTheDocument();
    expect(within(row2).getByText('部署脚本')).toBeInTheDocument();
  });

  it('没有连接时给空态文案', async () => {
    mocks.listConnections.mockResolvedValue([]);
    render(<SshPanel wsPath="D:\\proj-a" />);
    expect(await screen.findByText('没有可用的 SSH 连接')).toBeInTheDocument();
  });

  it('列表加载中显示骨架屏', () => {
    mocks.listConnections.mockReturnValue(new Promise(() => {}));
    const { container } = render(<SshPanel wsPath="D:\\proj-a" />);
    expect(container.querySelectorAll('.animate-pulse')).toHaveLength(3);
  });

  it('列表加载失败时面板级 error 呈现错误信息', async () => {
    mocks.listConnections.mockRejectedValue(new Error('绑定不可用'));
    render(<SshPanel wsPath="D:\\proj-a" />);
    expect(await screen.findByText(/绑定不可用/)).toBeInTheDocument();
  });

  it('双击连接行调用 onOpenRemote', async () => {
    const onOpenRemote = vi.fn();
    render(<SshPanel wsPath="D:\\proj-a" onOpenRemote={onOpenRemote} />);
    const row = await findRow('生产机');
    fireEvent.doubleClick(row);
    expect(onOpenRemote).toHaveBeenCalledWith(expect.objectContaining({ ID: 'c1', Name: '生产机' }));
  });

  it('点「编辑」打开表单并可保存', async () => {
    render(<SshPanel wsPath="D:\\proj-a" />);
    const row = await findRow('生产机');
    fireEvent.click(within(row).getByRole('button', { name: '编辑' }));
    expect(await screen.findByText('编辑 SSH 连接')).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('用户名'), { target: { value: 'ops' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '保存' }));
    });
    expect(mocks.upsertConnection).toHaveBeenCalledWith(
      expect.objectContaining({ ID: 'c1', User: 'ops', Host: '10.0.0.1' }),
    );
  });

  it('点「新建」保存时 Source 走手动且绑定工作区', async () => {
    render(<SshPanel wsPath="D:\\proj-a" />);
    await findRow('生产机');
    fireEvent.click(screen.getByRole('button', { name: '新建' }));
    expect(await screen.findByText('新建 SSH 连接')).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('主机'), { target: { value: '10.0.0.9' } });
    fireEvent.change(screen.getByLabelText('连接名称'), { target: { value: '手动机' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '保存' }));
    });
    expect(mocks.upsertConnection).toHaveBeenCalled();
    const arg = mocks.upsertConnection.mock.calls[0][0] as SshConnection;
    expect(arg.ID).toBe('');
    expect(arg.Host).toBe('10.0.0.9');
    expect(arg.Name).toBe('手动机');
    expect(arg.Workspace.replace(/\\/g, '/')).toMatch(/proj-a$/i);
  });

  it('命令执行：默认选中第一个连接，展示 Stdout 尾部', async () => {
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
    const out = screen.getByLabelText('命令输出');
    expect(out.textContent).toContain('line-11');
    expect(screen.getByText(/退出码 0/)).toBeInTheDocument();
  });

  it('非 0 退出码展示 Stderr 尾部', async () => {
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

    expect(screen.getByLabelText('错误输出').textContent).toContain('err-6');
    expect(screen.getByText(/退出码 1/)).toBeInTheDocument();
  });

  it('ExecRemote 调用失败时显示错误提示', async () => {
    mocks.execRemote.mockRejectedValue(new Error('连接超时'));
    render(<SshPanel wsPath="D:\\proj-a" />);
    await findRow('生产机');

    fireEvent.change(screen.getByLabelText('执行命令'), { target: { value: 'echo hi' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '执行' }));
    });

    expect(await screen.findByText(/连接超时/)).toBeInTheDocument();
  });

  it('命令历史：↑↓ 回填与 chip 清空', async () => {
    render(<SshPanel wsPath="D:\\proj-a" />);
    await findRow('生产机');
    const input = screen.getByLabelText('执行命令') as HTMLInputElement;

    fireEvent.change(input, { target: { value: 'uname -a' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '执行' }));
    });
    fireEvent.change(input, { target: { value: 'uptime' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '执行' }));
    });

    fireEvent.change(input, { target: { value: '' } });
    fireEvent.keyDown(input, { key: 'ArrowUp' });
    expect(input.value).toBe('uptime');
    fireEvent.keyDown(input, { key: 'ArrowUp' });
    expect(input.value).toBe('uname -a');

    const hist = screen.getByLabelText('命令历史');
    fireEvent.click(within(hist).getByRole('button', { name: '清空命令历史' }));
    expect(screen.queryByLabelText('命令历史')).not.toBeInTheDocument();
  });
});
