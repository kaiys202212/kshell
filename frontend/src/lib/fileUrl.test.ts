import { describe, expect, it } from 'vitest';
import { base64UrlUtf8, dirName, workspaceFileUrl } from './fileUrl';

describe('base64UrlUtf8', () => {
  it('与 Go RawURLEncoding 对齐（-/_、无填充、UTF-8）', () => {
    expect(base64UrlUtf8('D:\\proj a')).toBe(btoa('D:\\proj a').replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, ''));
    expect(base64UrlUtf8('中文路径')).toBe('5Lit5paH6Lev5b6E');
    expect(base64UrlUtf8('')).toBe('');
  });
});

describe('dirName', () => {
  it('兼容两种分隔符', () => {
    expect(dirName('D:\\ws\\a\\page.html')).toBe('D:\\ws\\a');
    expect(dirName('/home/u/a/page.html')).toBe('/home/u/a');
  });
});

describe('workspaceFileUrl', () => {
  it('Windows 绝对路径 → origin + __kshell-file + b64(目录) + 编码文件名', () => {
    const dir = 'D:\\ws\\a';
    const expected = `http://localhost:3000/__kshell-file/${base64UrlUtf8(dir)}/${encodeURIComponent('img.png')}`;
    expect(workspaceFileUrl('D:\\ws\\a\\img.png')).toBe(expected);
  });

  it('posix 绝对路径同样成立', () => {
    const expected = `http://localhost:3000/__kshell-file/${base64UrlUtf8('/home/u/a')}/${encodeURIComponent('p 1.png')}`;
    expect(workspaceFileUrl('/home/u/a/p 1.png')).toBe(expected);
  });

  it('文件名含中文与空格时 percent 编码,目录原样 b64', () => {
    const url = workspaceFileUrl('D:\\ws\\a\\截图 1.png');
    expect(url).toContain(`/${base64UrlUtf8('D:\\ws\\a')}/`);
    expect(url.endsWith(encodeURIComponent('截图 1.png'))).toBe(true);
  });
});
