// Preview 组件测试：Lines 原样渲染（Go 已带行号前缀，前端不加行号）、
// 截断提示、二进制元信息、错误与空态、
// 编辑模式（整读 → textarea → Ctrl+S/保存 → 刷新预览与 git 状态）。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import Preview from './Preview';
import { useAppStore } from '../state/store';

const mocks = vi.hoisted(() => ({
  previewFile: vi.fn(),
  readFileForEdit: vi.fn(),
  saveFile: vi.fn(),
  gitStatus: vi.fn(),
}));
vi.mock('../lib/api', () => mocks);

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  useAppStore.setState({ toasts: [], gitStatus: {} });
  mocks.gitStatus.mockResolvedValue({ Status: {}, IsRepo: false });
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

  it('编辑：整读进 textarea，修改出现未保存点，Ctrl+S 保存后刷新预览与 git 状态', async () => {
    mocks.previewFile
      .mockResolvedValueOnce({ Lines: ['   1 │ a'], Truncated: false, Binary: false, Info: '' })
      .mockResolvedValueOnce({ Lines: ['   1 │ ab'], Truncated: false, Binary: false, Info: '' });
    mocks.readFileForEdit.mockResolvedValue({ Text: 'a', EOL: 'lf', Size: 1 });
    mocks.saveFile.mockResolvedValue(undefined);
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\a.ts'} />);
    await screen.findByText(/1 │ a/);

    // 进入编辑态
    fireEvent.click(screen.getByRole('button', { name: '编辑' }));
    const area = (await screen.findByRole('textbox', { name: '编辑文件内容' })) as HTMLTextAreaElement;
    expect(area.value).toBe('a');

    // 未修改时保存按钮禁用
    expect(screen.getByRole('button', { name: '保存' })).toBeDisabled();

    // 修改 → 未保存点 + 保存可用
    await act(async () => {
      fireEvent.change(area, { target: { value: 'ab' } });
    });
    expect(screen.getByText('●')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '保存' })).toBeEnabled();

    // Ctrl+S 保存 → saveFile 按整读的 EOL 落盘，重新预览
    await act(async () => {
      fireEvent.keyDown(screen.getByRole('textbox', { name: '编辑文件内容' }), {
        key: 's',
        ctrlKey: true,
      });
    });
    expect(mocks.saveFile).toHaveBeenCalledWith('D:\\proj', 'D:\\proj\\a.ts', 'ab', 'lf');
    expect(await screen.findByText(/1 │ ab/)).toBeInTheDocument();
    expect(useAppStore.getState().toasts.some((t) => t.title === '已保存')).toBe(true);
    expect(screen.queryByRole('textbox', { name: '编辑文件内容' })).not.toBeInTheDocument();
  });

  it('保存按钮也能保存（crlf 按 EOL 还原），保存失败提示错误且留在编辑态', async () => {
    mocks.previewFile.mockResolvedValue({ Lines: ['   1 │ x'], Truncated: false, Binary: false, Info: '' });
    mocks.readFileForEdit.mockResolvedValue({ Text: 'x', EOL: 'crlf', Size: 2 });
    mocks.saveFile.mockRejectedValueOnce(new Error('磁盘已满'));
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\a.ts'} />);
    await screen.findByText(/1 │ x/);

    fireEvent.click(screen.getByRole('button', { name: '编辑' }));
    const area = await screen.findByRole('textbox', { name: '编辑文件内容' });
    await act(async () => {
      fireEvent.change(area, { target: { value: 'y' } });
    });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '保存' }));
    });

    expect(mocks.saveFile).toHaveBeenCalledWith('D:\\proj', 'D:\\proj\\a.ts', 'y', 'crlf');
    expect(useAppStore.getState().toasts.some((t) => t.title.includes('磁盘已满'))).toBe(true);
    // 失败后留在编辑态，内容不丢
    expect(screen.getByRole('textbox', { name: '编辑文件内容' })).toHaveValue('y');
  });

  it('取消：有修改时经确认放弃，无修改直接退出', async () => {
    mocks.previewFile.mockResolvedValue({ Lines: ['   1 │ x'], Truncated: false, Binary: false, Info: '' });
    mocks.readFileForEdit.mockResolvedValue({ Text: 'x', EOL: 'lf', Size: 1 });
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\a.ts'} />);
    await screen.findByText(/1 │ x/);

    fireEvent.click(screen.getByRole('button', { name: '编辑' }));
    const area = await screen.findByRole('textbox', { name: '编辑文件内容' });
    await act(async () => {
      fireEvent.change(area, { target: { value: 'xy' } });
    });
    fireEvent.click(screen.getByRole('button', { name: '取消' }));
    expect(confirmSpy).toHaveBeenCalled();
    expect(screen.queryByRole('textbox', { name: '编辑文件内容' })).not.toBeInTheDocument();
    confirmSpy.mockRestore();
  });

  it('编辑在途切换文件时丢弃结果，不会把旧内容带进新文件的编辑态', async () => {
    mocks.previewFile
      .mockResolvedValueOnce({ Lines: ['   1 │ a'], Truncated: false, Binary: false, Info: '' })
      .mockResolvedValue({ Lines: ['   1 │ b'], Truncated: false, Binary: false, Info: '' });
    let resolveEdit!: (v: { Text: string; EOL: string; Size: number }) => void;
    mocks.readFileForEdit.mockReturnValue(
      new Promise((res) => {
        resolveEdit = res;
      }),
    );
    const { rerender } = render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\a.ts'} />);
    await screen.findByText(/1 │ a/);

    fireEvent.click(screen.getByRole('button', { name: '编辑' }));
    // 整读在途时切到 b.ts
    rerender(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\b.ts'} />);
    await act(async () => {
      resolveEdit({ Text: 'A-CONTENT', EOL: 'lf', Size: 9 });
    });

    // 不应出现编辑态（更不可能把 a 的内容写进 b）
    expect(screen.queryByRole('textbox', { name: '编辑文件内容' })).not.toBeInTheDocument();
    expect(mocks.saveFile).not.toHaveBeenCalled();
  });

  it('二进制与加载失败不显示编辑按钮', async () => {
    mocks.previewFile.mockResolvedValue({ Lines: [], Truncated: false, Binary: true, Info: '二进制' });
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\img.png'} />);
    await screen.findByText('二进制');
    expect(screen.queryByRole('button', { name: '编辑' })).not.toBeInTheDocument();
  });
});
