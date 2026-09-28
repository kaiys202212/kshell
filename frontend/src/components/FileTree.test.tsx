// FileTree 组件测试：目录懒加载（展开才调 ListFiles、relPath 用 / 拼接）、
// 点文件回调、Space / 篮子按钮加入篮子并按 Go 返回值同步 store、加载错误提示。
// api 层整体打桩（vi.mock），与 SessionList.test 同一套模式。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import FileTree from './FileTree';
import type { FileNode } from '../lib/api';
import { useAppStore } from '../state/store';

const mocks = vi.hoisted(() => ({
  listFiles: vi.fn(),
  toggleBasket: vi.fn(),
}));
vi.mock('../lib/api', () => mocks);

const node = (name: string, isDir: boolean, rel: string): FileNode => ({
  Name: name,
  Path: `D:\\proj\\${rel.replace(/\//g, '\\')}`,
  IsDir: isDir,
  Expanded: false,
  Loaded: false,
});

const root: FileNode[] = [node('src', true, 'src'), node('README.md', false, 'README.md')];
const srcChildren: FileNode[] = [
  node('lib', true, 'src/lib'),
  node('main.ts', false, 'src/main.ts'),
];

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  useAppStore.setState({ basket: [], toasts: [] });
});

describe('FileTree', () => {
  it('挂载时只请求根层（relPath 为空串），目录未展开前不发起子层请求', async () => {
    mocks.listFiles.mockResolvedValue(root);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);

    expect(await screen.findByText('README.md')).toBeInTheDocument();
    expect(mocks.listFiles).toHaveBeenCalledTimes(1);
    expect(mocks.listFiles).toHaveBeenCalledWith('D:\\proj', '');
    expect(screen.queryByText('main.ts')).not.toBeInTheDocument();
  });

  it('点击目录懒加载子层，嵌套目录 relPath 用 / 拼接', async () => {
    mocks.listFiles.mockResolvedValueOnce(root).mockResolvedValueOnce(srcChildren);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);

    fireEvent.click(await screen.findByText('src'));
    expect(await screen.findByText('main.ts')).toBeInTheDocument();
    expect(mocks.listFiles).toHaveBeenLastCalledWith('D:\\proj', 'src');

    mocks.listFiles.mockResolvedValueOnce([node('tree.go', false, 'src/lib/tree.go')]);
    fireEvent.click(screen.getByText('lib'));
    expect(await screen.findByText('tree.go')).toBeInTheDocument();
    expect(mocks.listFiles).toHaveBeenLastCalledWith('D:\\proj', 'src/lib');
  });

  it('收起再展开已加载的目录不重复请求（子层缓存）', async () => {
    mocks.listFiles.mockResolvedValueOnce(root).mockResolvedValueOnce(srcChildren);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);

    fireEvent.click(await screen.findByText('src'));
    await screen.findByText('main.ts');
    fireEvent.click(screen.getByText('src')); // 收起
    expect(screen.queryByText('main.ts')).not.toBeInTheDocument();
    fireEvent.click(screen.getByText('src')); // 再展开
    expect(await screen.findByText('main.ts')).toBeInTheDocument();
    expect(mocks.listFiles).toHaveBeenCalledTimes(2);
  });

  it('点文件回调 onOpenFile（绝对路径）', async () => {
    mocks.listFiles.mockResolvedValue(root);
    const onOpen = vi.fn();
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={onOpen} />);

    fireEvent.click(await screen.findByText('README.md'));
    expect(onOpen).toHaveBeenCalledWith('D:\\proj\\README.md');
  });

  it('Space 键把文件加入篮子，并按 Go 返回值（true）同步 store', async () => {
    mocks.listFiles.mockResolvedValue(root);
    mocks.toggleBasket.mockResolvedValue(true);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);

    const name = await screen.findByText('README.md');
    await act(async () => {
      fireEvent.keyDown(name, { key: ' ' });
    });

    expect(mocks.toggleBasket).toHaveBeenCalledWith('D:\\proj\\README.md');
    expect(useAppStore.getState().basket).toContain('D:\\proj\\README.md');
    expect(screen.getByRole('button', { name: '移出篮子 README.md' })).toBeInTheDocument();
  });

  it('篮子已满（Go 返回 false 且原本不在篮中）时不加入 store', async () => {
    mocks.listFiles.mockResolvedValue(root);
    mocks.toggleBasket.mockResolvedValue(false);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);

    const name = await screen.findByText('README.md');
    await act(async () => {
      fireEvent.keyDown(name, { key: ' ' });
    });

    expect(useAppStore.getState().basket).not.toContain('D:\\proj\\README.md');
    expect(screen.getByRole('button', { name: '加入篮子 README.md' })).toBeInTheDocument();
  });

  it('篮子按钮点击加入，再次点击移出（Go 返回 false）', async () => {
    mocks.listFiles.mockResolvedValue(root);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    await screen.findByText('README.md');

    mocks.toggleBasket.mockResolvedValueOnce(true);
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '加入篮子 README.md' }));
    });
    expect(useAppStore.getState().basket).toContain('D:\\proj\\README.md');

    mocks.toggleBasket.mockResolvedValueOnce(false);
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '移出篮子 README.md' }));
    });
    expect(useAppStore.getState().basket).not.toContain('D:\\proj\\README.md');
  });

  it('ListFiles 失败时显示错误提示', async () => {
    mocks.listFiles.mockRejectedValue(new Error('路径越出工作区范围'));
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);

    expect(await screen.findByText(/路径越出工作区范围/)).toBeInTheDocument();
  });

  it('工作区没有可显示的文件时给空态文案', async () => {
    mocks.listFiles.mockResolvedValue([]);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);

    expect(await screen.findByText('没有可显示的文件')).toBeInTheDocument();
  });

  it('子目录加载失败：只在目标目录行内提示 + 重试入口，不整树替换', async () => {
    mocks.listFiles
      .mockResolvedValueOnce(root)
      .mockRejectedValueOnce(new Error('子目录读取失败'));
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);

    fireEvent.click(await screen.findByText('src'));
    // 错误出现在 src 目录行内（带重试按钮），根层其他条目仍在
    expect(await screen.findByText(/子目录读取失败/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '重试加载 src' })).toBeInTheDocument();
    expect(screen.getByText('README.md')).toBeInTheDocument();

    // 重试成功后子层正常加载，错误消失
    mocks.listFiles.mockResolvedValueOnce(srcChildren);
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '重试加载 src' }));
    });
    expect(await screen.findByText('main.ts')).toBeInTheDocument();
    expect(screen.queryByText(/子目录读取失败/)).not.toBeInTheDocument();
  });

  it('加入篮子被拒（篮满，Go 返回 false 且原本不在篮中）时提示篮满', async () => {
    mocks.listFiles.mockResolvedValue(root);
    mocks.toggleBasket.mockResolvedValue(false);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);

    const name = await screen.findByText('README.md');
    await act(async () => {
      fireEvent.keyDown(name, { key: ' ' });
    });

    expect(useAppStore.getState().toasts.some((t) => t.title.includes('篮子已满'))).toBe(true);
  });

  it('篮子操作抛错时提示失败，不打断浏览', async () => {
    mocks.listFiles.mockResolvedValue(root);
    mocks.toggleBasket.mockRejectedValue(new Error('绑定异常'));
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);

    const name = await screen.findByText('README.md');
    await act(async () => {
      fireEvent.keyDown(name, { key: ' ' });
    });

    expect(useAppStore.getState().toasts.some((t) => t.title.includes('篮子操作失败'))).toBe(true);
  });
});
