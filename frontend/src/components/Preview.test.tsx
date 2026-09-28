// Preview 组件测试：Lines 原样渲染（Go 已带行号前缀，前端不加行号）、
// 截断提示、二进制元信息、错误与空态、加入/移出篮子按钮。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import Preview from './Preview';
import { useAppStore } from '../state/store';

const mocks = vi.hoisted(() => ({
  previewFile: vi.fn(),
  // Preview 引入 lib/basket（toggleAndSync）后需要该导出存在，避免打桩代理报缺导出
  toggleBasket: vi.fn(),
}));
vi.mock('../lib/api', () => mocks);

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  useAppStore.setState({ basket: [], toasts: [] });
});

describe('Preview', () => {
  it('未选择文件时显示占位文案，不发起请求', () => {
    render(<Preview wsPath="D:\\proj" path={null} />);
    expect(screen.getByText('从右侧文件树选择文件查看预览')).toBeInTheDocument();
    expect(mocks.previewFile).not.toHaveBeenCalled();
  });

  it('渲染 Go 返回的 Lines（行号前缀原样保留，前端不再加行号）', async () => {
    mocks.previewFile.mockResolvedValue({
      Lines: ['   1 │ package main', '   2 │ ', '   3 │ func main() {}'],
      Truncated: false,
      Binary: false,
      Info: 'text/plain · 3 行',
    });
    const { container } = render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\main.go'} />);

    await screen.findByText(/text\/plain/);
    const pre = container.querySelector('pre');
    expect(pre).not.toBeNull();
    expect(pre?.textContent).toBe('   1 │ package main\n   2 │ \n   3 │ func main() {}');
    expect(mocks.previewFile).toHaveBeenCalledWith('D:\\proj', 'D:\\proj\\main.go');
  });

  it('Truncated 时显示截断提示', async () => {
    mocks.previewFile.mockResolvedValue({
      Lines: ['   1 │ x'],
      Truncated: true,
      Binary: false,
      Info: '',
    });
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\big.log'} />);

    expect(await screen.findByText(/内容已截断/)).toBeInTheDocument();
  });

  it('二进制文件只展示 Info 元信息，不渲染内容', async () => {
    mocks.previewFile.mockResolvedValue({
      Lines: [],
      Truncated: false,
      Binary: true,
      Info: '二进制文件 · 1.2 MB',
    });
    const { container } = render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\img.png'} />);

    expect(await screen.findByText('二进制文件 · 1.2 MB')).toBeInTheDocument();
    expect(container.querySelector('pre')).toBeNull();
  });

  it('PreviewFile 失败时显示错误信息', async () => {
    mocks.previewFile.mockRejectedValue(new Error('路径越出工作区范围'));
    render(<Preview wsPath={'D:\\proj'} path={'D:\\outside\\secret'} />);

    expect(await screen.findByText(/路径越出工作区范围/)).toBeInTheDocument();
  });

  it('文本加载完成后显示篮子按钮（篮中为移出态），点击走 toggleAndSync 同步 store；加载中与错误态不显示', async () => {
    useAppStore.setState({ basket: [], toasts: [] });
    mocks.previewFile.mockResolvedValue({
      Lines: ['   1 │ x'],
      Truncated: false,
      Binary: false,
      Info: '',
    });
    mocks.toggleBasket.mockResolvedValue(true);
    const { rerender } = render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\a.ts'} />);

    // 加载中不显示按钮
    expect(screen.queryByRole('button', { name: '加入篮子' })).not.toBeInTheDocument();

    // 加载完成显示「加入篮子」，点击后按 Go 返回值同步并翻转按钮
    const btn = await screen.findByRole('button', { name: '加入篮子' });
    await act(async () => {
      fireEvent.click(btn);
    });
    expect(mocks.toggleBasket).toHaveBeenCalledWith('D:\\proj\\a.ts');
    expect(useAppStore.getState().basket).toContain('D:\\proj\\a.ts');
    expect(screen.getByRole('button', { name: '移出篮子' })).toBeInTheDocument();

    // 错误态不显示按钮
    mocks.previewFile.mockRejectedValueOnce(new Error('读取失败'));
    rerender(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\b.ts'} />);
    await screen.findByText(/读取失败/);
    expect(screen.queryByRole('button', { name: /篮子/ })).not.toBeInTheDocument();
  });

  it('切换文件路径时重新加载', async () => {
    mocks.previewFile
      .mockResolvedValueOnce({ Lines: ['   1 │ a'], Truncated: false, Binary: false, Info: '' })
      .mockResolvedValueOnce({ Lines: ['   1 │ b'], Truncated: false, Binary: false, Info: '' });
    const { rerender } = render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\a.ts'} />);
    await screen.findByText(/1 │ a/);

    rerender(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\b.ts'} />);
    // findByText 的默认 normalizer 会折叠空白，正则不要再带行号前导空格
    await screen.findByText(/1 │ b/);
    expect(mocks.previewFile).toHaveBeenCalledTimes(2);
    expect(mocks.previewFile).toHaveBeenLastCalledWith('D:\\proj', 'D:\\proj\\b.ts');
  });
});
