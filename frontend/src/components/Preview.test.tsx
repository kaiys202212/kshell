// Preview 组件测试：可编辑文件整读进可写 CodeEditor；图/PDF/二进制只读；Ctrl+S 保存。
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
    expect(mocks.readFileForEdit).not.toHaveBeenCalled();
    expect(mocks.previewFile).not.toHaveBeenCalled();
  });

  it('.ts 整读进可写 CodeEditor', async () => {
    mocks.readFileForEdit.mockResolvedValue({ Text: 'package main\n', EOL: 'lf', Size: 13 });
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\main.ts'} />);

    const editor = await screen.findByRole('textbox', { name: '编辑文件内容' });
    expect(editor).toHaveValue('package main\n');
    expect(mocks.readFileForEdit).toHaveBeenCalledWith('D:\\proj', 'D:\\proj\\main.ts');
    expect(mocks.previewFile).not.toHaveBeenCalled();
  });

  it('.md 也直接可编辑，不渲染 markdown 预览切换', async () => {
    mocks.readFileForEdit.mockResolvedValue({ Text: '# Hi', EOL: 'lf', Size: 4 });
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\readme.md'} />);

    expect(await screen.findByRole('textbox', { name: '编辑文件内容' })).toHaveValue('# Hi');
    expect(screen.queryByRole('button', { name: '预览' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '源码' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '编辑' })).not.toBeInTheDocument();
  });

  it('图片路径直接挂载 ImagePreview，不调 readFileForEdit', async () => {
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\pic.png'} />);
    expect(await screen.findByTestId('image-preview')).toHaveTextContent('D:\\proj\\pic.png');
    expect(mocks.readFileForEdit).not.toHaveBeenCalled();
    expect(mocks.previewFile).not.toHaveBeenCalled();
  });

  it('PDF 路径挂载 PdfPreview', async () => {
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\doc.pdf'} />);
    expect(await screen.findByTestId('pdf-preview')).toHaveTextContent('D:\\proj\\doc.pdf');
    expect(mocks.readFileForEdit).not.toHaveBeenCalled();
  });

  it('路径标题栏固定、内容区可滚动', async () => {
    mocks.readFileForEdit.mockResolvedValue({ Text: 'hello', EOL: 'lf', Size: 5 });
    const { container } = render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\a.ts'} />);
    await screen.findByTestId('code-editor');

    const root = container.firstElementChild as HTMLElement;
    expect(root.className).toMatch(/h-full/);
    expect(root.className).toMatch(/overflow-hidden/);

    const header = root.querySelector('[data-testid="preview-path-header"]') as HTMLElement;
    expect(header.className).toMatch(/shrink-0/);
    const body = root.querySelector('[data-testid="preview-body"]') as HTMLElement;
    expect(body.className).toMatch(/min-h-0/);
    expect(body.className).toMatch(/flex-1/);
  });

  it('整读失败后回退 previewFile：二进制只展示 Info', async () => {
    mocks.readFileForEdit.mockRejectedValue(new Error('二进制文件'));
    mocks.previewFile.mockResolvedValue({
      Lines: [],
      Truncated: false,
      Binary: true,
      Info: '二进制文件 · 1.2 MB',
    });
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\blob.bin'} />);

    expect(await screen.findByText('二进制文件 · 1.2 MB')).toBeInTheDocument();
    expect(screen.queryByTestId('code-editor')).toBeNull();
  });

  it('readFileForEdit 与 previewFile 都失败时显示错误信息', async () => {
    mocks.readFileForEdit.mockRejectedValue(new Error('路径越出工作区范围'));
    mocks.previewFile.mockRejectedValue(new Error('路径越出工作区范围'));
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\secret.ts'} />);

    expect(await screen.findByText(/路径越出工作区范围/)).toBeInTheDocument();
  });

  it('切换文件路径时重新整读', async () => {
    mocks.readFileForEdit
      .mockResolvedValueOnce({ Text: 'a', EOL: 'lf', Size: 1 })
      .mockResolvedValueOnce({ Text: 'b', EOL: 'lf', Size: 1 });
    const { rerender } = render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\a.ts'} />);
    expect(await screen.findByTestId('code-editor')).toHaveValue('a');

    rerender(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\b.ts'} />);
    expect(await screen.findByTestId('code-editor')).toHaveValue('b');
    expect(mocks.readFileForEdit).toHaveBeenCalledTimes(2);
  });

  it('修改出现未保存点，Ctrl+S 保存后仍可编辑', async () => {
    mocks.readFileForEdit.mockResolvedValue({ Text: 'a', EOL: 'lf', Size: 1 });
    mocks.saveFile.mockResolvedValue(undefined);
    const onDirty = vi.fn();
    const onEdited = vi.fn();
    render(
      <Preview wsPath={'D:\\proj'} path={'D:\\proj\\a.ts'} onDirtyChange={onDirty} onEdited={onEdited} />,
    );
    const area = (await screen.findByRole('textbox', { name: '编辑文件内容' })) as HTMLTextAreaElement;
    expect(screen.getByRole('button', { name: '保存' })).toBeDisabled();

    await act(async () => {
      fireEvent.change(area, { target: { value: 'ab' } });
    });
    expect(screen.getByText('●')).toBeInTheDocument();
    expect(onDirty).toHaveBeenCalledWith(true);
    expect(onEdited).toHaveBeenCalled();
    expect(screen.getByRole('button', { name: '保存' })).toBeEnabled();

    await act(async () => {
      fireEvent.keyDown(screen.getByRole('textbox', { name: '编辑文件内容' }), {
        key: 's',
        ctrlKey: true,
      });
    });
    expect(mocks.saveFile).toHaveBeenCalledWith('D:\\proj', 'D:\\proj\\a.ts', 'ab', 'lf');
    expect(await screen.findByRole('textbox', { name: '编辑文件内容' })).toHaveValue('ab');
    expect(useAppStore.getState().toasts.some((t) => t.title === '已保存')).toBe(true);
  });

  it('保存按钮也能保存（crlf），失败提示且留在编辑态', async () => {
    mocks.readFileForEdit.mockResolvedValue({ Text: 'x', EOL: 'crlf', Size: 2 });
    mocks.saveFile.mockRejectedValueOnce(new Error('磁盘已满'));
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\a.ts'} />);
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

  it('整读在途切换文件时丢弃旧结果', async () => {
    let resolveEdit!: (v: { Text: string; EOL: string; Size: number }) => void;
    mocks.readFileForEdit.mockImplementation((_ws: string, p: string) => {
      if (p.endsWith('a.ts')) {
        return new Promise((res) => {
          resolveEdit = res;
        });
      }
      return Promise.resolve({ Text: 'b', EOL: 'lf', Size: 1 });
    });
    const { rerender } = render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\a.ts'} />);
    rerender(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\b.ts'} />);
    expect(await screen.findByRole('textbox', { name: '编辑文件内容' })).toHaveValue('b');
    await act(async () => {
      resolveEdit({ Text: 'A-CONTENT', EOL: 'lf', Size: 9 });
    });
    expect(screen.getByRole('textbox', { name: '编辑文件内容' })).toHaveValue('b');
    expect(mocks.saveFile).not.toHaveBeenCalled();
  });
});
