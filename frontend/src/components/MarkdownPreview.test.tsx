// MarkdownPreview：GFM 渲染 heading 等基础元素。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { OPEN_FILE_EVENT } from '../lib/openHref';
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
});
