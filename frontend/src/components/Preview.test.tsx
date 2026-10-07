// Preview 组件测试：可编辑文件整读进可写 CodeEditor；图/PDF/二进制只读；Ctrl+S 保存。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import i18next from 'i18next';
import Preview from './Preview';
import { useAppStore } from '../state/store';
import { tt } from '../test/i18n';

// 测试用可写编辑器的稳定标签（真实 CodeEditor 的 aria 由本 mock 替代，避免依赖 UI 语言）
const EDITOR_LABEL = 'code-editor';

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
    showWhitespace,
  }: {
    value: string;
    onChange?: (v: string) => void;
    readOnly?: boolean;
    showWhitespace?: boolean;
  }) {
    return (
      <textarea
        data-testid="code-editor"
        aria-label={EDITOR_LABEL}
        data-show-whitespace={showWhitespace ? '1' : '0'}
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

vi.mock('./MarkdownPreview', () => ({
  default: function MockMarkdownPreview({ markdown }: { markdown: string }) {
    return <div data-testid="markdown-preview">{markdown}</div>;
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
    expect(screen.getByText(tt('ui.files.pick_from_tree'))).toBeInTheDocument();
    expect(mocks.readFileForEdit).not.toHaveBeenCalled();
    expect(mocks.previewFile).not.toHaveBeenCalled();
  });

  it('.ts 整读进可写 CodeEditor', async () => {
    mocks.readFileForEdit.mockResolvedValue({ Text: 'package main\n', EOL: 'lf', Size: 13 });
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\main.ts'} />);

    const editor = await screen.findByRole('textbox', { name: EDITOR_LABEL });
    expect(editor).toHaveValue('package main\n');
    expect(mocks.readFileForEdit).toHaveBeenCalledWith('D:\\proj', 'D:\\proj\\main.ts');
    expect(mocks.previewFile).not.toHaveBeenCalled();
  });

  it('从 appearance 传入 showWhitespace', async () => {
    useAppStore.setState({
      appearance: { mode: 'dark', resolved: 'dark', fontSize: 13, showWhitespace: true },
    });
    mocks.readFileForEdit.mockResolvedValue({ Text: 'x', EOL: 'lf', Size: 1 });
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\a.ts'} />);
    const editor = await screen.findByTestId('code-editor');
    expect(editor).toHaveAttribute('data-show-whitespace', '1');
  });

  it('.md 默认预览模式：可见切换与 MarkdownPreview，不整读', async () => {
    mocks.previewFile.mockResolvedValue({
      Text: '# Hi',
      Lines: ['# Hi'],
      Truncated: false,
      Binary: false,
      Info: '',
    });
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\readme.md'} />);

    expect(await screen.findByTestId('markdown-preview')).toHaveTextContent('# Hi');
    expect(screen.getByRole('button', { name: tt('ui.files.md_preview') })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: tt('ui.files.md_source') })).toBeInTheDocument();
    expect(screen.queryByRole('textbox', { name: EDITOR_LABEL })).toBeNull();
    expect(mocks.previewFile).toHaveBeenCalledWith('D:\\proj', 'D:\\proj\\readme.md');
    expect(mocks.readFileForEdit).not.toHaveBeenCalled();
  });

  it('.md 点源码后显示可写 CodeEditor 并整读', async () => {
    mocks.previewFile.mockResolvedValue({
      Text: '# Hi',
      Lines: ['# Hi'],
      Truncated: false,
      Binary: false,
      Info: '',
    });
    mocks.readFileForEdit.mockResolvedValue({ Text: '# Hi full', EOL: 'lf', Size: 9 });
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\readme.md'} />);
    await screen.findByTestId('markdown-preview');

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: tt('ui.files.md_source') }));
    });

    const editor = await screen.findByRole('textbox', { name: EDITOR_LABEL });
    expect(editor).toHaveValue('# Hi full');
    expect(editor).not.toHaveAttribute('readonly');
    expect(mocks.readFileForEdit).toHaveBeenCalledWith('D:\\proj', 'D:\\proj\\readme.md');
    expect(screen.queryByTestId('markdown-preview')).toBeNull();
  });

  it('.go 无预览/源码切换', async () => {
    mocks.readFileForEdit.mockResolvedValue({ Text: 'package main\n', EOL: 'lf', Size: 13 });
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\main.go'} />);

    expect(await screen.findByRole('textbox', { name: EDITOR_LABEL })).toHaveValue('package main\n');
    expect(screen.queryByRole('button', { name: tt('ui.files.md_preview') })).toBeNull();
    expect(screen.queryByRole('button', { name: tt('ui.files.md_source') })).toBeNull();
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

  it('无路径栏，内容区可占满', async () => {
    mocks.readFileForEdit.mockResolvedValue({ Text: 'hello', EOL: 'lf', Size: 5 });
    const { container } = render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\a.ts'} />);
    await screen.findByTestId('code-editor');

    const root = container.firstElementChild as HTMLElement;
    expect(root.className).toMatch(/h-full/);
    expect(root.className).toMatch(/overflow-hidden/);
    expect(root.querySelector('[data-testid="preview-path-header"]')).toBeNull();
    const body = root.querySelector('[data-testid="preview-body"]') as HTMLElement;
    expect(body.className).toMatch(/min-h-0/);
    expect(body.className).toMatch(/flex-1/);
  });

  it('整读失败后回退 previewFile：二进制只展示 Info（wire key 经 translateBackend）', async () => {
    mocks.readFileForEdit.mockRejectedValue(new Error('二进制文件'));
    mocks.previewFile.mockResolvedValue({
      Lines: [],
      Truncated: false,
      Binary: true,
      Info: 'preview.binary_file|1234567|2026-10-07 10:00',
    });
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\blob.bin'} />);

    const info = tt('preview.binary_file')
      .replace('{{0}}', '1234567')
      .replace('{{1}}', '2026-10-07 10:00');
    expect(await screen.findByText(info)).toBeInTheDocument();
    expect(screen.queryByTestId('code-editor')).toBeNull();
  });

  it('readFileForEdit 与 previewFile 都失败时显示错误信息（wire key 经 backendError）', async () => {
    mocks.readFileForEdit.mockRejectedValue(new Error('err.files.out_of_workspace'));
    mocks.previewFile.mockRejectedValue(new Error('err.files.out_of_workspace'));
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\secret.ts'} />);

    expect(await screen.findByText(tt('err.files.out_of_workspace'))).toBeInTheDocument();
  });

  it('切换语言不重跑整读加载（未保存草稿不被丢弃）', async () => {
    mocks.readFileForEdit.mockResolvedValue({ Text: 'draft', EOL: 'lf', Size: 5 });
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\a.ts'} />);
    const area = (await screen.findByRole('textbox', { name: EDITOR_LABEL })) as HTMLTextAreaElement;
    await act(async () => {
      fireEvent.change(area, { target: { value: 'draft-edited' } });
    });
    const before = mocks.readFileForEdit.mock.calls.length;
    expect(before).toBe(1);
    expect(screen.getByRole('textbox', { name: EDITOR_LABEL })).toHaveValue('draft-edited');

    try {
      await act(async () => {
        await i18next.changeLanguage('zh-CN');
      });
      // 加载 effect 未因 t 变化重跑：调用次数不变、草稿仍在
      expect(mocks.readFileForEdit.mock.calls.length).toBe(before);
      expect(screen.getByRole('textbox', { name: EDITOR_LABEL })).toHaveValue('draft-edited');
    } finally {
      await act(async () => {
        await i18next.changeLanguage('en');
      });
    }
  });

  it('切换文件路径时重新整读', async () => {    mocks.readFileForEdit
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
    const area = (await screen.findByRole('textbox', { name: EDITOR_LABEL })) as HTMLTextAreaElement;

    await act(async () => {
      fireEvent.change(area, { target: { value: 'ab' } });
    });
    expect(onDirty).toHaveBeenCalledWith(true);
    expect(onEdited).toHaveBeenCalled();

    await act(async () => {
      fireEvent.keyDown(screen.getByRole('textbox', { name: EDITOR_LABEL }), {
        key: 's',
        ctrlKey: true,
      });
    });
    expect(mocks.saveFile).toHaveBeenCalledWith('D:\\proj', 'D:\\proj\\a.ts', 'ab', 'lf');
    expect(await screen.findByRole('textbox', { name: EDITOR_LABEL })).toHaveValue('ab');
    expect(useAppStore.getState().toasts.some((t) => t.title === tt('ui.files.saved'))).toBe(true);
  });

  it('Ctrl+S 保存失败（crlf）提示且留在编辑态', async () => {
    mocks.readFileForEdit.mockResolvedValue({ Text: 'x', EOL: 'crlf', Size: 2 });
    mocks.saveFile.mockRejectedValueOnce(new Error('磁盘已满'));
    render(<Preview wsPath={'D:\\proj'} path={'D:\\proj\\a.ts'} />);
    const area = await screen.findByRole('textbox', { name: EDITOR_LABEL });
    await act(async () => {
      fireEvent.change(area, { target: { value: 'y' } });
    });
    await act(async () => {
      fireEvent.keyDown(area, { key: 's', ctrlKey: true });
    });

    expect(mocks.saveFile).toHaveBeenCalledWith('D:\\proj', 'D:\\proj\\a.ts', 'y', 'crlf');
    expect(useAppStore.getState().toasts.some((t) => t.title.includes('磁盘已满'))).toBe(true);
    expect(screen.getByRole('textbox', { name: EDITOR_LABEL })).toHaveValue('y');
  });

  it('改后还原为原文则取消脏标记', async () => {
    mocks.readFileForEdit.mockResolvedValue({ Text: 'hello', EOL: 'lf', Size: 5 });
    const onDirty = vi.fn();
    render(
      <Preview wsPath={'D:\\proj'} path={'D:\\proj\\a.ts'} onDirtyChange={onDirty} />,
    );
    const area = await screen.findByRole('textbox', { name: EDITOR_LABEL });
    await act(async () => {
      fireEvent.change(area, { target: { value: 'hello!' } });
    });
    expect(onDirty).toHaveBeenLastCalledWith(true);
    await act(async () => {
      fireEvent.change(area, { target: { value: 'hello' } });
    });
    expect(onDirty).toHaveBeenLastCalledWith(false);
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
    expect(await screen.findByRole('textbox', { name: EDITOR_LABEL })).toHaveValue('b');
    await act(async () => {
      resolveEdit({ Text: 'A-CONTENT', EOL: 'lf', Size: 9 });
    });
    expect(screen.getByRole('textbox', { name: EDITOR_LABEL })).toHaveValue('b');
    expect(mocks.saveFile).not.toHaveBeenCalled();
  });
});
