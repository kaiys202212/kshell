import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import FileTabsPane from './FileTabsPane';
import { emptyFileTabs, openPreview, diffTabPath } from '../lib/fileTabs';

vi.mock('./Preview', () => ({
  default: ({ path }: { path: string | null }) => <div data-testid="preview">{path ?? 'empty'}</div>,
}));
vi.mock('./GitDiffView', () => ({
  default: ({ path }: { path: string }) => <div data-testid="git-diff">{path}</div>,
}));

afterEach(cleanup);

describe('FileTabsPane', () => {
  it('空态不渲染文件页签', () => {
    render(
      <FileTabsPane
        wsPath="D:\\proj"
        state={emptyFileTabs()}
        dirty={{}}
        onChange={() => {}}
        onDirty={() => {}}
      />,
    );
    expect(screen.queryByRole('tablist', { name: '文件页签' })?.querySelectorAll('[role="tab"]')).toHaveLength(0);
    expect(screen.getByTestId('preview')).toHaveTextContent('empty');
  });

  it('预览页签斜体，关闭走回调', () => {
    const onChange = vi.fn();
    const state = openPreview(emptyFileTabs(), 'D:\\proj\\a.ts');
    render(
      <FileTabsPane wsPath="D:\\proj" state={state} dirty={{}} onChange={onChange} onDirty={() => {}} />,
    );
    const tab = screen.getByRole('tab', { name: 'a.ts' });
    expect(tab.closest('[data-preview="true"]')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '关闭 a.ts' }));
    expect(onChange).toHaveBeenCalled();
  });

  it('未保存关闭需确认，取消则不关', () => {
    const onChange = vi.fn();
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false);
    const state = openPreview(emptyFileTabs(), 'D:\\proj\\a.ts');
    render(
      <FileTabsPane
        wsPath="D:\\proj"
        state={state}
        dirty={{ 'D:\\proj\\a.ts': true }}
        onChange={onChange}
        onDirty={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: '关闭 a.ts' }));
    expect(confirmSpy).toHaveBeenCalled();
    expect(onChange).not.toHaveBeenCalled();
    confirmSpy.mockRestore();
  });

  it('diff 页签渲染 GitDiffView', () => {
    const state = openPreview(emptyFileTabs(), diffTabPath('working', '', 'a.ts'), 'diff');
    render(
      <FileTabsPane wsPath="D:\\proj" state={state} dirty={{}} onChange={() => {}} onDirty={() => {}} />,
    );
    expect(screen.getByTestId('git-diff')).toHaveTextContent('a.ts');
    expect(screen.queryByTestId('preview')).not.toBeInTheDocument();
  });
});
