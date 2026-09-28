// WorkspaceTab 集成测试：新建会话按钮调 NewSession、文件树点文件联动预览区。
// api 层整体打桩；子组件（SessionList/FileTree/Preview/BasketBar）随之一起真渲染。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import WorkspaceTabView from '../pages/WorkspaceTab';
import { useAppStore } from '../state/store';

const mocks = vi.hoisted(() => ({
  getSessions: vi.fn(),
  resumeSession: vi.fn(),
  focusSession: vi.fn(),
  onScanDone: vi.fn(),
  onWindowClosed: vi.fn(),
  listFiles: vi.fn(),
  previewFile: vi.fn(),
  toggleBasket: vi.fn(),
  newSession: vi.fn(),
  listConnections: vi.fn(),
  openSSH: vi.fn(),
  execRemote: vi.fn(),
}));
vi.mock('../lib/api', () => mocks);

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  mocks.onScanDone.mockReturnValue(() => {});
  mocks.onWindowClosed.mockReturnValue(() => {});
  mocks.getSessions.mockResolvedValue([]);
  mocks.listFiles.mockResolvedValue([]);
  useAppStore.setState({ basket: [], windowStatus: {}, scanState: 'done' });
});

describe('WorkspaceTab', () => {
  it('「新建会话」按钮调用 NewSession 并带上当前工作区路径', async () => {
    mocks.newSession.mockResolvedValue(undefined);
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} />);

    fireEvent.click(screen.getByRole('button', { name: '新建会话' }));

    await waitFor(() => {
      expect(mocks.newSession).toHaveBeenCalledWith('D:\\proj-a');
    });
  });

  it('新建会话失败时显示错误提示', async () => {
    mocks.newSession.mockRejectedValue(new Error('工作区不存在'));
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} />);

    fireEvent.click(screen.getByRole('button', { name: '新建会话' }));

    expect(await screen.findByText(/新建会话失败/)).toBeInTheDocument();
  });

  it('文件树点文件后预览区加载并渲染内容', async () => {
    mocks.listFiles.mockResolvedValue([
      {
        Name: 'main.ts',
        Path: 'D:\\proj-a\\main.ts',
        IsDir: false,
        Expanded: false,
        Loaded: false,
      },
    ]);
    mocks.previewFile.mockResolvedValue({
      Lines: ['   1 │ const a = 1;'],
      Truncated: false,
      Binary: false,
      Info: '',
    });
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} />);

    fireEvent.click(await screen.findByText('main.ts'));

    expect(await screen.findByText(/const a = 1;/)).toBeInTheDocument();
    expect(mocks.previewFile).toHaveBeenCalledWith('D:\\proj-a', 'D:\\proj-a\\main.ts');
  });

  it('SSH 页签渲染 SshPanel 连接列表，文件页签渲染文件树', async () => {
    mocks.listConnections.mockResolvedValue([
      {
        ID: 'c1',
        Name: '生产机',
        Host: '10.0.0.1',
        User: 'root',
        Port: 22,
        IdentityFile: '',
        Workspace: '',
        Source: 'sshconfig',
        SourceFile: '',
        Verified: true,
      },
    ]);
    render(<WorkspaceTabView tab={{ id: 'D:\\proj-a', name: 'proj-a' }} />);

    expect(await screen.findByText('没有可显示的文件')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'SSH' }));
    expect(await screen.findByText('生产机')).toBeInTheDocument();
    expect(mocks.listConnections).toHaveBeenCalledWith('D:\\proj-a');
  });
});
