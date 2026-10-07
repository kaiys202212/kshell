// CodeEditor 烟测：只读挂载、testid、变更 value 不抛错；空白符与折叠扩展。
import '@testing-library/jest-dom/vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import CodeEditor, { buildWhitespaceFoldExtensions } from './CodeEditor';

afterEach(cleanup);

describe('CodeEditor', () => {
  it('渲染只读编辑器并暴露 data-testid="code-editor"', () => {
    render(
      <CodeEditor
        value="const x = 1;"
        path="D:\\proj\\app.ts"
        readOnly
        theme="dark"
      />,
    );
    expect(screen.getByTestId('code-editor')).toBeInTheDocument();
  });

  it('变更 value 不抛错', () => {
    const { rerender } = render(
      <CodeEditor
        value="line1"
        path="D:\\proj\\readme.md"
        readOnly
        theme="light"
      />,
    );
    expect(() => {
      rerender(
        <CodeEditor
          value="line1\nline2"
          path="D:\\proj\\readme.md"
          readOnly
          theme="light"
        />,
      );
    }).not.toThrow();
    expect(screen.getByTestId('code-editor')).toBeInTheDocument();
  });

  it('挂载 .ps1 与 .sh 不抛错', () => {
    expect(() => {
      const { unmount } = render(
        <CodeEditor value="$x = 1" path="D:\\proj\\build.ps1" readOnly theme="dark" />,
      );
      unmount();
      render(<CodeEditor value="echo hi" path="D:\\proj\\run.sh" readOnly theme="light" />);
    }).not.toThrow();
  });

  it('始终挂载 fold gutter', () => {
    render(
      <CodeEditor value={'function f() {\n  return 1;\n}\n'} path="a.js" readOnly theme="dark" />,
    );
    const root = screen.getByTestId('code-editor');
    expect(root.querySelector('.cm-foldGutter')).toBeTruthy();
  });

  it('showWhitespace 默认不渲染空白高亮类', () => {
    render(
      <CodeEditor value={'a b\tc'} path="a.txt" readOnly theme="dark" />,
    );
    const root = screen.getByTestId('code-editor');
    expect(root.querySelector('.cm-highlightSpace')).toBeNull();
    expect(root.querySelector('.cm-highlightTab')).toBeNull();
  });

  it('showWhitespace true 时渲染空白高亮', () => {
    render(
      <CodeEditor value={'a b\tc'} path="a.txt" readOnly theme="dark" showWhitespace />,
    );
    const root = screen.getByTestId('code-editor');
    expect(
      root.querySelector('.cm-highlightSpace') || root.querySelector('.cm-highlightTab'),
    ).toBeTruthy();
  });

  it('切换 showWhitespace 用 reconfigure 不销毁编辑器根节点', () => {
    const { rerender } = render(
      <CodeEditor value={'a b'} path="a.txt" readOnly theme="dark" showWhitespace={false} />,
    );
    const root = screen.getByTestId('code-editor');
    const cmBefore = root.querySelector('.cm-editor');
    expect(cmBefore).toBeTruthy();
    rerender(
      <CodeEditor value={'a b'} path="a.txt" readOnly theme="dark" showWhitespace />,
    );
    expect(root.querySelector('.cm-editor')).toBe(cmBefore);
    expect(root.querySelector('.cm-highlightSpace')).toBeTruthy();
  });
});

describe('buildWhitespaceFoldExtensions', () => {
  it('始终含折叠扩展；showWhitespace 为 true 时多一段空白扩展', () => {
    const off = buildWhitespaceFoldExtensions(false);
    const on = buildWhitespaceFoldExtensions(true);
    expect(off.length).toBe(2); // foldGutter + foldKeymap
    expect(on.length).toBe(3); // + highlightWhitespace
  });
});
