// HtmlBrowserPreview：blob iframe 沙箱渲染 + head 注入（polyfill/导航拦截/<base>）
// + 系统浏览器打开 + 重载。api 层打桩；URL.createObjectURL 在 jsdom 缺失，打桩验证。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { tt } from '../test/i18n';
import HtmlBrowserPreview, { base64UrlUtf8, dirName, injectBase } from './HtmlBrowserPreview';

const mocks = vi.hoisted(() => ({
  readFileBytes: vi.fn(),
  openInDefaultApp: vi.fn().mockResolvedValue(undefined),
}));
vi.mock('../lib/api', () => mocks);

const b64 = (s: string) => btoa(String.fromCharCode(...new TextEncoder().encode(s)));

beforeEach(() => {
  vi.clearAllMocks();
  // jsdom 未实现 createObjectURL/revokeObjectURL
  URL.createObjectURL = vi.fn(() => 'blob:mock-url');
  URL.revokeObjectURL = vi.fn();
});

afterEach(cleanup);

describe('HtmlBrowserPreview', () => {
  it('读取文件后渲染沙箱 iframe：允许脚本与表单、禁止同源，src 为 blob', async () => {
    mocks.readFileBytes.mockResolvedValue({
      Base64: b64('<html><head></head><body>hi</body></html>'),
      Mime: 'application/octet-stream',
      Size: 10,
      AbsPath: 'D:\\proj\\page.html',
    });
    render(<HtmlBrowserPreview wsPath="D:\\proj" path="page.html" />);
    const frame = await screen.findByTestId('html-browser-frame');
    expect(frame).toHaveAttribute('sandbox', 'allow-scripts allow-forms');
    expect(frame).toHaveAttribute('src', 'blob:mock-url');
  });

  it('读取失败显示错误态，iframe 不渲染', async () => {
    mocks.readFileBytes.mockRejectedValue(new Error('boom'));
    render(<HtmlBrowserPreview wsPath="D:\\proj" path="page.html" />);
    await screen.findByText('boom');
    expect(screen.queryByTestId('html-browser-frame')).toBeNull();
  });

  it('重载按钮重新拉取文件', async () => {
    mocks.readFileBytes.mockResolvedValue({
      Base64: b64('<p>x</p>'),
      Mime: '',
      Size: 3,
      AbsPath: 'D:\\proj\\page.html',
    });
    render(<HtmlBrowserPreview wsPath="D:\\proj" path="page.html" />);
    await screen.findByTestId('html-browser-frame');
    fireEvent.click(screen.getByRole('button', { name: tt('ui.files.html_preview_reload') }));
    await waitFor(() => expect(mocks.readFileBytes).toHaveBeenCalledTimes(2));
  });

  it('「浏览器打开」按钮经 Go 绑定用默认应用打开本地文件（不走 file:// BrowserOpenURL）', async () => {
    mocks.readFileBytes.mockResolvedValue({
      Base64: b64('<p>x</p>'),
      Mime: '',
      Size: 3,
      AbsPath: 'D:\\proj\\a b.html',
    });
    const ws = String.raw`D:\proj`;
    render(<HtmlBrowserPreview wsPath={ws} path="a b.html" />);
    await screen.findByTestId('html-browser-frame');
    const btn = screen.getByRole('button', { name: tt('ui.files.html_preview_open_browser') });
    expect(btn).not.toBeDisabled();
    fireEvent.click(btn);
    expect(mocks.openInDefaultApp).toHaveBeenCalledWith(ws, 'a b.html');
  });

  it('iframe postMessage open-external → OpenInDefaultApp', async () => {
    mocks.readFileBytes.mockResolvedValue({
      Base64: b64('<p>x</p>'),
      Mime: '',
      Size: 3,
      AbsPath: 'D:\\proj\\page.html',
    });
    const ws = String.raw`D:\proj`;
    render(<HtmlBrowserPreview wsPath={ws} path="page.html" />);
    await screen.findByTestId('html-browser-frame');
    window.dispatchEvent(new MessageEvent('message', { data: { kshell: 'open-external' } }));
    expect(mocks.openInDefaultApp).toHaveBeenCalledWith(ws, 'page.html');
  });

  it('base64UrlUtf8 与 Go RawURLEncoding 对齐（-/_、无填充、UTF-8）', () => {
    expect(base64UrlUtf8('D:\\proj a')).toBe(btoa('D:\\proj a').replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, ''));
    expect(base64UrlUtf8('中文路径')).toBe('5Lit5paH6Lev5b6E');
    expect(base64UrlUtf8('')).toBe('');
  });

  it('dirName 兼容两种分隔符', () => {
    expect(dirName('D:\\ws\\a\\page.html')).toBe('D:\\ws\\a');
    expect(dirName('/home/u/a/page.html')).toBe('/home/u/a');
  });

  it('injectBase：head 最前注入导航拦截脚本与 <base>；无 head 时插到 doctype 后', () => {
    const withHead = injectBase('<html><head><title>t</title></head><body></body></html>', 'http://x/b/');
    expect(withHead.startsWith('<html><head><script>')).toBe(true);
    expect(withHead).toContain("kshell: 'open-external'");
    expect(withHead).toContain('localStorage');
    expect(withHead).toContain('<base href="http://x/b/"><title>');

    const noHead = injectBase('<!DOCTYPE html><html><body></body></html>', 'http://x/b/');
    expect(noHead.startsWith('<!DOCTYPE html>')).toBe(true);
    expect(noHead).toContain('<base href=');

    const bare = injectBase('<p>hi</p>', 'http://x/b/');
    expect(bare.startsWith('<script>')).toBe(true);
    expect(bare).toContain('<base href=');
  });
});
