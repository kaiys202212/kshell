// MarkdownPreview：GFM 渲染 heading 等基础元素 / 渲染结果检索（高亮、计数、跳转）。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { OPEN_FILE_EVENT } from '../lib/openHref';
import { tt } from '../test/i18n';
import MarkdownPreview from './MarkdownPreview';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('MarkdownPreview', () => {
  it('渲染 # Hi 为 heading「Hi」', () => {
    render(<MarkdownPreview markdown="# Hi" />);
    const heading = screen.getByRole('heading', { level: 1, name: 'Hi' });
    expect(heading).toBeInTheDocument();
  });

  it('点击 http 链接调用系统打开且 preventDefault', () => {
    const open = vi.fn();
    vi.stubGlobal('runtime', { BrowserOpenURL: open });
    render(<MarkdownPreview markdown="[x](https://example.com)" />);
    const link = screen.getByRole('link', { name: 'x' });
    const ev = fireEvent.click(link);
    expect(ev).toBe(false); // preventDefault → fireEvent.click 返回 false
    expect(open).toHaveBeenCalledWith('https://example.com');
  });

  it('相对路径派发打开工作区文件', () => {
    const seen: unknown[] = [];
    const onOpen = (e: Event) => seen.push((e as CustomEvent).detail);
    window.addEventListener(OPEN_FILE_EVENT, onOpen);
    render(<MarkdownPreview markdown="[r](./README.md)" workspaceRoot={'D:\\proj'} />);
    fireEvent.click(screen.getByRole('link', { name: 'r' }));
    window.removeEventListener(OPEN_FILE_EVENT, onOpen);
    expect(seen).toEqual([{ workspace: 'D:\\proj', path: 'D:\\proj\\README.md' }]);
  });

  it('渲染检索：输入关键词后高亮命中并计数，大小写不敏感', async () => {
    render(<MarkdownPreview markdown="# alpha Beta\n\nbeta again" />);
    const input = screen.getByPlaceholderText(tt('ui.files.md_search_placeholder'));
    fireEvent.change(input, { target: { value: 'beta' } });

    // 防抖 200ms 后生效
    await waitFor(() => expect(document.querySelectorAll('mark[data-md-hit]').length).toBe(2));
    expect(screen.getByTestId('md-search-count')).toHaveTextContent('2');
  });

  it('渲染检索：清空关键词后移除全部高亮', async () => {
    render(<MarkdownPreview markdown="hello world hello" />);
    const input = screen.getByPlaceholderText(tt('ui.files.md_search_placeholder'));
    fireEvent.change(input, { target: { value: 'hello' } });
    await waitFor(() => expect(document.querySelectorAll('mark[data-md-hit]').length).toBe(2));

    fireEvent.change(input, { target: { value: '' } });
    await waitFor(() => expect(document.querySelectorAll('mark[data-md-hit]').length).toBe(0));
    expect(screen.queryByTestId('md-search-count')).toBeNull();
  });

  it('渲染检索：跨内联标签的长词不命中（单文本节点内匹配）', async () => {
    render(<MarkdownPreview markdown="**关键**词组" />);
    const input = screen.getByPlaceholderText(tt('ui.files.md_search_placeholder'));
    fireEvent.change(input, { target: { value: '关键词' } });
    // 等防抖生效后仍无命中
    await new Promise((r) => setTimeout(r, 320));
    expect(document.querySelectorAll('mark[data-md-hit]').length).toBe(0);
    expect(screen.getByTestId('md-search-count')).toHaveTextContent('0');
  });
});
