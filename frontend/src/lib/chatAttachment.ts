// 聊天附件点击分派：纯函数，供 chip 点击与 lightbox 前置判断使用。
// 分类只依赖 meta 与 workspaceRoot，不触任何后端绑定，方便单测。

import type { ChatAttachmentMeta } from './api';
import { isInsideWorkspace, posixNorm, toNative } from './openHref';

export type AttachmentAction =
  | { kind: 'preview'; path: string } // 图片 → lightbox
  | { kind: 'default'; wsPath: string; rel: string } // 工作区内文件 → OpenInDefaultApp
  | { kind: 'local'; abs: string } // 工作区外本地文件 → OpenLocalPath
  | { kind: 'external'; url: string } // http(s) 等外链
  | { kind: 'none' };

// fileUriToPath 把 file:///D:/a/b%20z#c 换算成本地路径。字面 # 与 ? 在 file URI
// 中须写作 %23/%3F，因此先按字面 #/? 截掉 query/fragment，再解码剩余部分
// （decodeURI 不会还原保留字符，这里用 decodeURIComponent；非法 percent-encoding
// 保持原串）。盘符形态（file:///D:/...，参照 toNative 的盘符判断）还原为
// Windows 反斜杠，POSIX 形态保持斜杠。
export function fileUriToPath(uri: string): string {
  const u = uri.trim();
  const m = u.match(/^file:\/\/\/?(.*)$/i);
  if (!m) return u;
  let p = m[1];
  const cut = p.search(/[?#]/);
  if (cut >= 0) p = p.slice(0, cut);
  try {
    p = decodeURIComponent(p);
  } catch {
    // 非法 percent-encoding 保持原串
  }
  const dm = p.match(/^([a-zA-Z]:)(\/.*)?$/);
  if (dm) return dm[1] + (dm[2] || '/').replace(/\//g, '\\');
  return '/' + p.replace(/^\/+/, '');
}

// attachmentAction 按 meta 分派点击行为：
//   图片带落盘 Path → lightbox 预览；file:// URI 按是否落在工作区内分流
//   （区内 → OpenInDefaultApp，rel 为原生分隔符相对路径；区外 → OpenLocalPath）；
//   非 file 协议 URI 走外链；无 URI 的落盘 Path（内嵌 resource）按工作区外
//   本地文件打开；其余不响应。workspaceRoot 为空串时无法判断归属，file URI
//   一律按工作区外本地文件处理。
export function attachmentAction(meta: ChatAttachmentMeta, workspaceRoot: string): AttachmentAction {
  if (meta.Kind === 'image' && meta.Path) return { kind: 'preview', path: meta.Path };
  const uri = meta.URI?.trim();
  if (uri) {
    if (!/^file:\/\//i.test(uri)) return { kind: 'external', url: uri };
    const abs = fileUriToPath(uri);
    const root = workspaceRoot.trim();
    if (!root || !isInsideWorkspace(root, abs)) return { kind: 'local', abs };
    const posixRoot = posixNorm(root).replace(/\/+$/, '');
    const rel = posixNorm(abs).slice(posixRoot.length).replace(/^\/+/, '');
    return { kind: 'default', wsPath: toNative(posixRoot, root), rel: toNative(rel, root) };
  }
  if (meta.Path) return { kind: 'local', abs: meta.Path };
  return { kind: 'none' };
}
