// MarkdownPreview：GFM 渲染 heading 等基础元素 / 渲染结果检索（高亮、计数、跳转）。
// 检索工具条默认隐藏：搜索按钮或 Ctrl+F 事件（预览可见时优先消费）呼出。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { OPEN_FILE_EVENT } from '../lib/openHref';
import { tt } from '../test/i18n';
import MarkdownPreview from './MarkdownPreview';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  delete (HTMLElement.prototype as { offsetParent?: unknown }).offsetParent;
});

// jsdom 无布局，offsetParent 恒 undefined（视为不可见）；测试可见态用它打开
function makeVisible() {
  Object.defineProperty(HTMLElement.prototype, 'offsetParent', {
    get() {
      return document.body;
    },
    configurable: true,
  });
}

// 呼出检索工具条：派发 Ctrl+F 事件（预览可见时消费）；act 保证状态同步落地
async function openBar() {
  makeVisible();
  await act(async () => {
    window.dispatchEvent(new CustomEvent('kshell:focus-search', { cancelable: true }));
  });
}

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

  it('检索工具条默认不渲染，Ctrl+F 呼出并聚焦', async () => {
    render(<MarkdownPreview markdown="hello" />);
    expect(screen.queryByTestId('md-search-bar')).toBeNull();
    await openBar();
    await waitFor(() =>
      expect(screen.getByPlaceholderText(tt('ui.files.md_search_placeholder'))).toBeInTheDocument(),
    );
  });

  it('Ctrl+F：预览可见时消费事件并呼出工具条（preventDefault 阻断会话过滤框）', async () => {
    makeVisible();
    render(<MarkdownPreview markdown="hello" />);
    const ev = new CustomEvent('kshell:focus-search', { cancelable: true });
    window.dispatchEvent(ev);
    expect(ev.defaultPrevented).toBe(true);
    await waitFor(() =>
      expect(screen.getByPlaceholderText(tt('ui.files.md_search_placeholder'))).toBeInTheDocument(),
    );
  });

  it('Ctrl+F：预览不可见（jsdom 默认）时不消费，交给会话过滤框', () => {
    render(<MarkdownPreview markdown="hello" />);
    const ev = new CustomEvent('kshell:focus-search', { cancelable: true });
    window.dispatchEvent(ev);
    expect(ev.defaultPrevented).toBe(false);
    expect(screen.queryByTestId('md-search-bar')).toBeNull();
  });

  it('渲染检索：输入关键词后高亮命中并计数，大小写不敏感', async () => {
    render(<MarkdownPreview markdown="# alpha Beta\n\nbeta again" />);
    await openBar();
    const input = screen.getByPlaceholderText(tt('ui.files.md_search_placeholder'));
    fireEvent.change(input, { target: { value: 'beta' } });

    // 防抖 200ms 后生效
    await waitFor(() => expect(document.querySelectorAll('mark[data-md-hit]').length).toBe(2));
    expect(screen.getByTestId('md-search-count')).toHaveTextContent('2');
  });

  it('渲染检索：清空关键词后移除全部高亮', async () => {
    render(<MarkdownPreview markdown="hello world hello" />);
    await openBar();
    const input = screen.getByPlaceholderText(tt('ui.files.md_search_placeholder'));
    fireEvent.change(input, { target: { value: 'hello' } });
    await waitFor(() => expect(document.querySelectorAll('mark[data-md-hit]').length).toBe(2));

    fireEvent.change(input, { target: { value: '' } });
    await waitFor(() => expect(document.querySelectorAll('mark[data-md-hit]').length).toBe(0));
    expect(screen.queryByTestId('md-search-count')).toBeNull();
  });

  it('渲染检索：关闭工具条清空检索态', async () => {
    render(<MarkdownPreview markdown="hello hello world" />);
    await openBar();
    fireEvent.change(screen.getByPlaceholderText(tt('ui.files.md_search_placeholder')), {
      target: { value: 'hello' },
    });
    await waitFor(() => expect(document.querySelectorAll('mark[data-md-hit]').length).toBe(2));
    fireEvent.click(screen.getByRole('button', { name: tt('ui.files.md_search_close') }));
    // 关闭后防抖把 query 清空，高亮随之移除
    await waitFor(() => expect(document.querySelectorAll('mark[data-md-hit]').length).toBe(0));
    expect(screen.queryByPlaceholderText(tt('ui.files.md_search_placeholder'))).toBeNull();
  });

  it('渲染检索：命中超过上限 500 时计数显示 500+', async () => {
    render(<MarkdownPreview markdown={'word '.repeat(600)} />);
    await openBar();
    const input = screen.getByPlaceholderText(tt('ui.files.md_search_placeholder'));
    fireEvent.change(input, { target: { value: 'word' } });
    await waitFor(() => expect(screen.getByTestId('md-search-count')).toHaveTextContent('500+'));
    // 高亮标记数仍以上限为界
    expect(document.querySelectorAll('mark[data-md-hit]').length).toBe(500);
  });

  it('渲染检索：跨内联标签的长词不命中（单文本节点内匹配）', async () => {
    render(<MarkdownPreview markdown="**关键**词组" />);
    await openBar();
    const input = screen.getByPlaceholderText(tt('ui.files.md_search_placeholder'));
    fireEvent.change(input, { target: { value: '关键词' } });
    // 等防抖生效后仍无命中
    await new Promise((r) => setTimeout(r, 320));
    expect(document.querySelectorAll('mark[data-md-hit]').length).toBe(0);
    expect(screen.getByTestId('md-search-count')).toHaveTextContent('0');
  });
});
