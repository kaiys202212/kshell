import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import RemoteDirPicker from './RemoteDirPicker';
import { tt } from '../test/i18n';

const mocks = vi.hoisted(() => ({
  listRemoteDir: vi.fn(),
}));
vi.mock('../lib/api', () => mocks);

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  mocks.listRemoteDir.mockResolvedValue({
    dir: '/home/u',
    entries: [
      { name: 'proj', isDir: true, size: 0, modTime: '2026-01-01T00:00:00Z' },
      { name: 'readme.md', isDir: false, size: 12, modTime: '2026-01-01T00:00:00Z' },
    ],
  });
});

describe('RemoteDirPicker', () => {
  it('加载起始目录并展示目录/文件；确认回传当前路径', async () => {
    const onConfirm = vi.fn();
    const onCancel = vi.fn();
    render(<RemoteDirPicker connID="c1" onConfirm={onConfirm} onCancel={onCancel} />);

    await waitFor(() => expect(mocks.listRemoteDir).toHaveBeenCalledWith('c1', ''));
    expect(await screen.findByText('/home/u')).toBeInTheDocument();
    expect(screen.getByText('proj')).toBeInTheDocument();
    expect(screen.getByText('readme.md')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: tt('ui.home.remote_use_dir') }));
    expect(onConfirm).toHaveBeenCalledWith('/home/u');
  });

  it('进入子目录再 list；上级回到父路径', async () => {
    mocks.listRemoteDir
      .mockResolvedValueOnce({
        dir: '/home/u',
        entries: [{ name: 'proj', isDir: true, size: 0, modTime: '' }],
      })
      .mockResolvedValueOnce({
        dir: '/home/u/proj',
        entries: [{ name: 'src', isDir: true, size: 0, modTime: '' }],
      })
      .mockResolvedValueOnce({
        dir: '/home/u',
        entries: [{ name: 'proj', isDir: true, size: 0, modTime: '' }],
      });

    render(<RemoteDirPicker connID="c1" onConfirm={vi.fn()} onCancel={vi.fn()} />);
    await screen.findByText('proj');

    fireEvent.click(screen.getByRole('button', { name: (n) => n.includes('proj') }));
    await waitFor(() => expect(mocks.listRemoteDir).toHaveBeenCalledWith('c1', '/home/u/proj'));
    expect(await screen.findByText('/home/u/proj')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: tt('ui.home.remote_up') }));
    await waitFor(() => expect(mocks.listRemoteDir).toHaveBeenLastCalledWith('c1', '/home/u'));
  });

  it('取消回调', async () => {
    const onCancel = vi.fn();
    render(<RemoteDirPicker connID="c1" onConfirm={vi.fn()} onCancel={onCancel} />);
    await screen.findByText('/home/u');
    fireEvent.click(screen.getByRole('button', { name: tt('ui.home.create_cancel') }));
    expect(onCancel).toHaveBeenCalled();
  });
});
