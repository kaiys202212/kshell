// Preview 组件测试：按 previewKind 调度 CodeEditor / Markdown / 图 / PDF，
// 截断提示、二进制元信息、错误与空态、
// 编辑模式（整读 → CodeEditor → Ctrl+S/保存 → 刷新预览与 git 状态）。
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
  readFileBytes: vi.fn(),
}));
vi.mock('../lib/api', () => mocks);

vi.mock('./CodeEditor', () => ({
  default: function MockCodeEditor({
    value,
    onChange,
    readOnly,
  }: {
    value: string;
    onChange?: (v: string) => void;
    readOnly?: boolean;
  }) {
    return (
      <textarea
        data-testid="code-editor"
        aria-label={readOnly ? '文件内容' : '编辑文件内容'}
        readOnly={!!readOnly}
        value={value}
        onChange={(e) => onChange?.(e.target.value)}
      />
    );
  },
}));

vi.mock('./MarkdownPreview', () => ({
  default: function MockMarkdownPreview({ markdown }: { markdown: string }) {
    return <div data-testid="markdown-preview">{markdown}</div>;
  },
}));

vi.mock('./ImagePreview', () => ({
  default: function MockImagePreview({ path }: { path: string }) {
    return <div data-testid="image-preview">{path}</div>;
  },
}));

vi.mock('./PdfPreview', () => ({
  default: function MockPdfPreview({ path }: { path: string }) {
    return <div data-testid="pdf-preview">{path}</div>;
  },
}));

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

  it('.ts 挂载 CodeEditor，优先用 Text，无行号前缀', async () => {
    mocks.previewFile.mockResolvedValue({
      Lines: ['   1 │ package main', '   2 │ ', '   3 │ func main() {}'],
      Text: 'package main\n\nfunc main() {}',
      Truncated: false,
      Binary: false,
      Info: 'text/plain · 3 行',
    });
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\main.ts'} />);

    await screen.findByText(/text\/plain/);
    const editor = await screen.findByTestId('code-editor');
    expect(editor).toHaveValue('package main\n\nfunc main() {}');
    expect(mocks.previewFile).toHaveBeenCalledWith('D:\\proj', 'D:\\proj\\main.ts');
  });

  it('无 Text 时用 stripLinePrefix(Lines) 作为 CodeEditor 内容', async () => {
    mocks.previewFile.mockResolvedValue({
      Lines: ['   1 │ hello', '   2 │ world'],
      Truncated: false,
      Binary: false,
      Info: '',
    });
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\a.ts'} />);

    const editor = await screen.findByTestId('code-editor');
    expect(editor).toHaveValue('hello\nworld');
  });

  it('.md 默认预览模式：出现「预览」按钮并渲染 markdown', async () => {
    mocks.previewFile.mockResolvedValue({
      Lines: ['   1 │ # Hi'],
      Text: '# Hi',
      Truncated: false,
      Binary: false,
      Info: '',
    });
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\readme.md'} />);

    expect(await screen.findByRole('button', { name: '预览' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '源码' })).toBeInTheDocument();
    expect(await screen.findByTestId('markdown-preview')).toHaveTextContent('# Hi');
    expect(screen.queryByTestId('code-editor')).not.toBeInTheDocument();
  });

  it('.md 切换源码显示 CodeEditor', async () => {
    mocks.previewFile.mockResolvedValue({
      Lines: [],
      Text: '# Hi',
      Truncated: false,
      Binary: false,
      Info: '',
    });
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\readme.md'} />);
    await screen.findByTestId('markdown-preview');

    fireEvent.click(screen.getByRole('button', { name: '源码' }));
    expect(await screen.findByTestId('code-editor')).toHaveValue('# Hi');
    expect(screen.queryByTestId('markdown-preview')).not.toBeInTheDocument();
  });

  it('图片路径直接挂载 ImagePreview，不调 previewFile', async () => {
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\pic.png'} />);
    expect(await screen.findByTestId('image-preview')).toHaveTextContent('D:\\proj\\pic.png');
    expect(mocks.previewFile).not.toHaveBeenCalled();
  });

  it('PDF 路径挂载 PdfPreview，不调 previewFile', async () => {
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\doc.pdf'} />);
    expect(await screen.findByTestId('pdf-preview')).toHaveTextContent('D:\\proj\\doc.pdf');
    expect(mocks.previewFile).not.toHaveBeenCalled();
  });

  it('Truncated 时显示截断提示', async () => {
    mocks.previewFile.mockResolvedValue({
      Lines: ['   1 │ x'],
      Text: 'x',
      Truncated: true,
      Binary: false,
      Info: '',
    });
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\big.log'} />);

    expect(await screen.findByText(/内容已截断/)).toBeInTheDocument();
  });

  it('二进制文件（非图/PDF）只展示 Info 元信息，不渲染内容', async () => {
    mocks.previewFile.mockResolvedValue({
      Lines: [],
      Truncated: false,
      Binary: true,
      Info: '二进制文件 · 1.2 MB',
    });
    const { container } = render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\blob.bin'} />);

    expect(await screen.findByText('二进制文件 · 1.2 MB')).toBeInTheDocument();
    expect(screen.queryByTestId('code-editor')).toBeNull();
    expect(container.querySelector('pre')).toBeNull();
  });

  it('PreviewFile 失败时显示错误信息', async () => {
    mocks.previewFile.mockRejectedValue(new Error('路径越出工作区范围'));
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\secret.ts'} />);

    expect(await screen.findByText(/路径越出工作区范围/)).toBeInTheDocument();
  });

  it('切换文件路径时重新加载', async () => {
    mocks.previewFile
      .mockResolvedValueOnce({
        Lines: ['   1 │ a'],
        Text: 'a',
        Truncated: false,
        Binary: false,
        Info: '',
      })
      .mockResolvedValueOnce({
        Lines: ['   1 │ b'],
        Text: 'b',
        Truncated: false,
        Binary: false,
        Info: '',
      });
    const { rerender } = render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\a.ts'} />);
    expect(await screen.findByTestId('code-editor')).toHaveValue('a');

    rerender(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\b.ts'} />);
    expect(await screen.findByTestId('code-editor')).toHaveValue('b');
    expect(mocks.previewFile).toHaveBeenCalledTimes(2);
    expect(mocks.previewFile).toHaveBeenLastCalledWith('D:\\proj', 'D:\\proj\\b.ts');
  });

  it('编辑：整读进 CodeEditor，修改出现未保存点，Ctrl+S 保存后刷新预览与 git 状态', async () => {
    mocks.previewFile
      .mockResolvedValueOnce({
        Lines: ['   1 │ a'],
        Text: 'a',
        Truncated: false,
        Binary: false,
        Info: '',
      })
      .mockResolvedValueOnce({
        Lines: ['   1 │ ab'],
        Text: 'ab',
        Truncated: false,
        Binary: false,
        Info: '',
      });
    mocks.readFileForEdit.mockResolvedValue({ Text: 'a', EOL: 'lf', Size: 1 });
    mocks.saveFile.mockResolvedValue(undefined);
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\a.ts'} />);
    expect(await screen.findByTestId('code-editor')).toHaveValue('a');

    fireEvent.click(screen.getByRole('button', { name: '编辑' }));
    const area = (await screen.findByRole('textbox', { name: '编辑文件内容' })) as HTMLTextAreaElement;
    expect(area.value).toBe('a');

    expect(screen.getByRole('button', { name: '保存' })).toBeDisabled();

    await act(async () => {
      fireEvent.change(area, { target: { value: 'ab' } });
    });
    expect(screen.getByText('●')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '保存' })).toBeEnabled();

    await act(async () => {
      fireEvent.keyDown(screen.getByRole('textbox', { name: '编辑文件内容' }), {
        key: 's',
        ctrlKey: true,
      });
    });
    expect(mocks.saveFile).toHaveBeenCalledWith('D:\\proj', 'D:\\proj\\a.ts', 'ab', 'lf');
    expect(await screen.findByTestId('code-editor')).toHaveValue('ab');
    expect(useAppStore.getState().toasts.some((t) => t.title === '已保存')).toBe(true);
    expect(screen.queryByRole('textbox', { name: '编辑文件内容' })).not.toBeInTheDocument();
  });

  it('保存按钮也能保存（crlf 按 EOL 还原），保存失败提示错误且留在编辑态', async () => {
    mocks.previewFile.mockResolvedValue({
      Lines: ['   1 │ x'],
      Text: 'x',
      Truncated: false,
      Binary: false,
      Info: '',
    });
    mocks.readFileForEdit.mockResolvedValue({ Text: 'x', EOL: 'crlf', Size: 2 });
    mocks.saveFile.mockRejectedValueOnce(new Error('磁盘已满'));
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\a.ts'} />);
    await screen.findByTestId('code-editor');

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
    expect(screen.getByRole('textbox', { name: '编辑文件内容' })).toHaveValue('y');
  });

  it('取消：有修改时经确认放弃，无修改直接退出', async () => {
    mocks.previewFile.mockResolvedValue({
      Lines: ['   1 │ x'],
      Text: 'x',
      Truncated: false,
      Binary: false,
      Info: '',
    });
    mocks.readFileForEdit.mockResolvedValue({ Text: 'x', EOL: 'lf', Size: 1 });
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\a.ts'} />);
    await screen.findByTestId('code-editor');

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
      .mockResolvedValueOnce({
        Lines: ['   1 │ a'],
        Text: 'a',
        Truncated: false,
        Binary: false,
        Info: '',
      })
      .mockResolvedValue({
        Lines: ['   1 │ b'],
        Text: 'b',
        Truncated: false,
        Binary: false,
        Info: '',
      });
    let resolveEdit!: (v: { Text: string; EOL: string; Size: number }) => void;
    mocks.readFileForEdit.mockReturnValue(
      new Promise((res) => {
        resolveEdit = res;
      }),
    );
    const { rerender } = render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\a.ts'} />);
    await screen.findByTestId('code-editor');

    fireEvent.click(screen.getByRole('button', { name: '编辑' }));
    rerender(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\b.ts'} />);
    await act(async () => {
      resolveEdit({ Text: 'A-CONTENT', EOL: 'lf', Size: 9 });
    });

    expect(screen.queryByRole('textbox', { name: '编辑文件内容' })).not.toBeInTheDocument();
    expect(mocks.saveFile).not.toHaveBeenCalled();
  });

  it('二进制与加载失败不显示编辑按钮', async () => {
    mocks.previewFile.mockResolvedValue({
      Lines: [],
      Truncated: false,
      Binary: true,
      Info: '二进制',
    });
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\blob.bin'} />);
    await screen.findByText('二进制');
    expect(screen.queryByRole('button', { name: '编辑' })).not.toBeInTheDocument();
  });
});
