// SshPanel：连接列表、新建/编辑、双击打开远程、命令执行。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import SshPanel from './SshPanel';
import type { RemoteResult, SshConnection } from '../lib/api';
import { formatDuration } from '../lib/format';
import { useAppStore } from '../state/store';
import { tt } from '../test/i18n';

const exitSummary = (code: number, ns: number) =>
  tt('ui.ssh.exit_summary')
    .replace('{{code}}', String(code))
    .replace('{{duration}}', formatDuration(ns));

const mocks = vi.hoisted(() => ({
  listConnections: vi.fn(),
  upsertConnection: vi.fn(),
  deleteConnection: vi.fn(),
  execRemote: vi.fn(),
  pickFile: vi.fn(),
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
    Password: '',
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
    Password: '',
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
  mocks.pickFile.mockResolvedValue('');
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
    const src1 = within(row1).getByText(tt('ui.ssh.source_sshconfig'));
    expect(src1).toHaveAttribute('title', 'C:\\Users\\me\\.ssh\\config');
    expect(within(row1).getByText('✓')).toBeInTheDocument();

    const row2 = await findRow('跳板机');
    expect(within(row2).getByText('jump.example.com:2222')).toBeInTheDocument();
    expect(within(row2).getByText(tt('ui.ssh.source_deploy'))).toBeInTheDocument();
  });

  it('没有连接时给空态文案', async () => {
    mocks.listConnections.mockResolvedValue([]);
    render(<SshPanel wsPath="D:\\proj-a" />);
    expect(await screen.findByText(tt('ui.ssh.empty'))).toBeInTheDocument();
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

  it('查看全部用空 wsID 拉全局连接，切回当前项目再带工作区路径', async () => {
    const ws = 'D:\\proj-a';
    const globalConns: SshConnection[] = [
      ...conns,
      {
        ID: 'c3',
        Name: '其他项目机',
        Host: '10.0.0.9',
        User: 'ops',
        Port: 22,
        IdentityFile: '',
        Password: '',
        Workspace: 'D:\\proj-b',
        Source: 'manual',
        SourceFile: '',
        Verified: false,
      },
    ];
    mocks.listConnections.mockImplementation(async (wsID: string) => (wsID === '' ? globalConns : conns));
    render(<SshPanel wsPath={ws} />);
    await findRow('生产机');
    expect(mocks.listConnections.mock.calls[0][0]).toBe(ws);

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.ssh.view_all') }));
    });
    expect(mocks.listConnections).toHaveBeenCalledWith('');
    expect(await screen.findByText('其他项目机')).toBeInTheDocument();
    expect(screen.getByText('proj-b')).toBeInTheDocument();

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.ssh.view_current') }));
    });
    expect(mocks.listConnections.mock.calls.at(-1)?.[0]).toBe(ws);
    expect(screen.queryByText('其他项目机')).not.toBeInTheDocument();
  });

  it('点「编辑」打开表单并可保存', async () => {
    render(<SshPanel wsPath="D:\\proj-a" />);
    const row = await findRow('生产机');
    fireEvent.click(within(row).getByRole('button', { name: tt('ui.ssh.edit') }));
    expect(await screen.findByText(tt('ui.ssh.edit_title'))).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText(tt('ui.ssh.user_aria')), { target: { value: 'ops' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.ssh.save') }));
    });
    expect(mocks.upsertConnection).toHaveBeenCalledWith(
      expect.objectContaining({ ID: 'c1', User: 'ops', Host: '10.0.0.1' }),
    );
  });

  it('点「新建」保存时 Source 走手动且绑定工作区', async () => {
    render(<SshPanel wsPath="D:\\proj-a" />);
    await findRow('生产机');
    fireEvent.click(screen.getByRole('button', { name: tt('ui.ssh.new') }));
    expect(await screen.findByText(tt('ui.ssh.new_title'))).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText(tt('ui.ssh.host_aria')), { target: { value: '10.0.0.9' } });
    fireEvent.change(screen.getByLabelText(tt('ui.ssh.name_aria')), { target: { value: '手动机' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.ssh.save') }));
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

    fireEvent.change(screen.getByLabelText(tt('ui.ssh.exec_aria')), {
      target: { value: 'uname -a' },
    });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.ssh.exec') }));
    });

    expect(mocks.execRemote).toHaveBeenCalledWith('c1', 'uname -a');
    const out = screen.getByLabelText(tt('ui.ssh.stdout_aria'));
    expect(out.textContent).toContain('line-11');
    expect(screen.getByText(exitSummary(0, 1_500_000_000))).toBeInTheDocument();
  });

  it('非 0 退出码展示 Stderr 尾部', async () => {
    const errLines = Array.from({ length: 55 }, (_, i) => `err-${i + 1}`);
    mocks.execRemote.mockResolvedValue(
      result({ Stderr: errLines.join('\n'), ExitCode: 1, Duration: 20_000_000 }),
    );
    render(<SshPanel wsPath="D:\\proj-a" />);
    await findRow('生产机');

    fireEvent.change(screen.getByLabelText(tt('ui.ssh.exec_aria')), { target: { value: 'ls /nope' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.ssh.exec') }));
    });

    expect(screen.getByLabelText(tt('ui.ssh.stderr_aria')).textContent).toContain('err-6');
    expect(screen.getByText(exitSummary(1, 20_000_000))).toBeInTheDocument();
  });

  it('ExecRemote 调用失败时显示错误提示', async () => {
    mocks.execRemote.mockRejectedValue(new Error('连接超时'));
    render(<SshPanel wsPath="D:\\proj-a" />);
    await findRow('生产机');

    fireEvent.change(screen.getByLabelText(tt('ui.ssh.exec_aria')), { target: { value: 'echo hi' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.ssh.exec') }));
    });

    expect(await screen.findByText(/连接超时/)).toBeInTheDocument();
  });

  it('命令历史：↑↓ 回填与 chip 清空', async () => {
    render(<SshPanel wsPath="D:\\proj-a" />);
    await findRow('生产机');
    const input = screen.getByLabelText(tt('ui.ssh.exec_aria')) as HTMLInputElement;

    fireEvent.change(input, { target: { value: 'uname -a' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.ssh.exec') }));
    });
    fireEvent.change(input, { target: { value: 'uptime' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.ssh.exec') }));
    });

    fireEvent.change(input, { target: { value: '' } });
    fireEvent.keyDown(input, { key: 'ArrowUp' });
    expect(input.value).toBe('uptime');
    fireEvent.keyDown(input, { key: 'ArrowUp' });
    expect(input.value).toBe('uname -a');

    const hist = screen.getByLabelText(tt('ui.ssh.history_aria'));
    fireEvent.click(within(hist).getByRole('button', { name: tt('ui.ssh.clear_history_aria') }));
    expect(screen.queryByLabelText(tt('ui.ssh.history_aria'))).not.toBeInTheDocument();
  });

  it('编辑表单含密码字段（type=password）与明文保存提示', async () => {
    render(<SshPanel wsPath="D:\\proj-a" />);
    const row = await findRow('生产机');
    fireEvent.click(within(row).getByRole('button', { name: tt('ui.ssh.edit') }));
    expect(await screen.findByText(tt('ui.ssh.edit_title'))).toBeInTheDocument();

    const pwd = screen.getByLabelText(tt('ui.ssh.password_aria')) as HTMLInputElement;
    expect(pwd).toHaveAttribute('type', 'password');
    expect(screen.getByText(tt('ui.ssh.password_plaintext_hint'))).toBeInTheDocument();
  });

  it('保存时把 Password 传给 upsertConnection', async () => {
    render(<SshPanel wsPath="D:\\proj-a" />);
    const row = await findRow('生产机');
    fireEvent.click(within(row).getByRole('button', { name: tt('ui.ssh.edit') }));
    await screen.findByText(tt('ui.ssh.edit_title'));

    fireEvent.change(screen.getByLabelText(tt('ui.ssh.password_aria')), {
      target: { value: 's3cret' },
    });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.ssh.save') }));
    });

    expect(mocks.upsertConnection).toHaveBeenCalledWith(
      expect.objectContaining({ ID: 'c1', Password: 's3cret' }),
    );
  });

  it('点浏览私钥按钮调用 PickFile 并回填路径', async () => {
    mocks.pickFile.mockResolvedValueOnce('C:\\Users\\me\\.ssh\\id_ed25519');
    render(<SshPanel wsPath="D:\\proj-a" />);
    const row = await findRow('生产机');
    fireEvent.click(within(row).getByRole('button', { name: tt('ui.ssh.edit') }));
    await screen.findByText(tt('ui.ssh.edit_title'));

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.ssh.browse_key') }));
    });

    expect(mocks.pickFile).toHaveBeenCalledWith(tt('ui.ssh.pick_key_title'));
    expect(screen.getByLabelText(tt('ui.ssh.key_aria'))).toHaveValue(
      'C:\\Users\\me\\.ssh\\id_ed25519',
    );
  });
});
