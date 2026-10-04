// CodeEditor 烟测：只读挂载、testid、变更 value 不抛错。
import '@testing-library/jest-dom/vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import CodeEditor from './CodeEditor';

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
});
