// 工作区文件的受限 URL 通道工具:与 Go 侧 /__kshell-file/ 通道(files_asset.go)
// 对齐。安全边界(工作区根限定 + 扩展名白名单)全部在后端,这里只做 URL 形态拼接。

// base64url(无填充)编码 UTF-8 字符串,与 Go base64.RawURLEncoding 对齐
export function base64UrlUtf8(s: string): string {
  const bytes = new TextEncoder().encode(s);
  let bin = '';
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

// 目录绝对路径(兼容 Windows 分隔符)
export function dirName(abs: string): string {
  const i = Math.max(abs.lastIndexOf('\\'), abs.lastIndexOf('/'));
  return i > 0 ? abs.slice(0, i) : abs;
}

// 工作区文件的子资源 URL:相对资源解析走 <base href="…/"> 同款形态;
// 文件名 percent 编码,Go net/http 的 req.URL.Path 会自动解码。
export function workspaceFileUrl(absPath: string): string {
  const name = absPath.replace(/\\/g, '/').split('/').pop() ?? absPath;
  return `${window.location.origin}/__kshell-file/${base64UrlUtf8(dirName(absPath))}/${encodeURIComponent(name)}`;
}
