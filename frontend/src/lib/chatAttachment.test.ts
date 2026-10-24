import { describe, expect, it, vi } from 'vitest';
import { attachmentAction, fileUriToPath } from './chatAttachment';

// openHref 依赖 api 的 openExternalURL；此处只 mock 掉避免牵入 wailsjs 运行时
const api = vi.hoisted(() => ({
  openExternalURL: vi.fn().mockResolvedValue(undefined),
}));
vi.mock('./api', () => api);

describe('fileUriToPath', () => {
  it('盘符 URI 还原 Windows 反斜杠并解码空格', () => {
    expect(fileUriToPath('file:///D:/a/b%20z')).toBe('D:\\a\\b z');
  });

  it('POSIX 形态路径保持斜杠', () => {
    expect(fileUriToPath('file:///home/u/x.png')).toBe('/home/u/x.png');
  });

  it('字面 #/? 截断 fragment/query，%23/%3F 还原为字面字符', () => {
    expect(fileUriToPath('file:///D:/a/b%20z#c')).toBe('D:\\a\\b z');
    expect(fileUriToPath('file:///D:/a/b%23c')).toBe('D:\\a\\b#c');
    expect(fileUriToPath('file:///D:/a/b%3Fc')).toBe('D:\\a\\b?c');
  });

  it('非法 percent-encoding 保持原串', () => {
    expect(fileUriToPath('file:///D:/a/b%zz')).toBe('D:\\a\\b%zz');
  });
});

describe('attachmentAction', () => {
  const cases: Array<{
    name: string;
    meta: Parameters<typeof attachmentAction>[0];
    root: string;
    want: ReturnType<typeof attachmentAction>;
  }> = [
    {
      name: 'image 带 Path → preview',
      meta: { Kind: 'image', Path: 'D:\\c\\1.png' },
      root: 'D:\\ws',
      want: { kind: 'preview', path: 'D:\\c\\1.png' },
    },
    {
      name: '工作区内 file URI → default',
      meta: { Kind: 'file', URI: 'file:///D:/ws/doc.pdf' },
      root: 'D:\\ws',
      want: { kind: 'default', wsPath: 'D:\\ws', rel: 'doc.pdf' },
    },
    {
      name: '工作区内嵌套路径 file URI → default 带 relative 子路径',
      meta: { Kind: 'file', URI: 'file:///D:/ws/sub/doc.pdf' },
      root: 'D:\\ws',
      want: { kind: 'default', wsPath: 'D:\\ws', rel: 'sub\\doc.pdf' },
    },
    // 两斜杠盘符 URI（file://D:/...，少见但存在）：fileUriToPath 的 /? 可选斜杠须兼容
    {
      name: '两斜杠盘符 file URI → default',
      meta: { Kind: 'file', URI: 'file://D:/x/y.png' },
      root: 'D:\\x',
      want: { kind: 'default', wsPath: 'D:\\x', rel: 'y.png' },
    },
    {
      name: '工作区外 file URI → local',
      meta: { Kind: 'file', URI: 'file:///D:/other/x.exe' },
      root: 'D:\\ws',
      want: { kind: 'local', abs: 'D:\\other\\x.exe' },
    },
    {
      name: 'http 外链 → external',
      meta: { Kind: 'file', URI: 'https://x/y' },
      root: 'D:\\ws',
      want: { kind: 'external', url: 'https://x/y' },
    },
    {
      name: '无 URI 有 Path（内嵌资源落盘）→ local',
      meta: { Kind: 'file', Path: 'D:\\c\\2.bin' },
      root: 'D:\\ws',
      want: { kind: 'local', abs: 'D:\\c\\2.bin' },
    },
    {
      name: '空 meta → none',
      meta: {},
      root: 'D:\\ws',
      want: { kind: 'none' },
    },
    {
      name: 'image 无 Path → none',
      meta: { Kind: 'image' },
      root: 'D:\\ws',
      want: { kind: 'none' },
    },
    {
      name: 'workspaceRoot 为空时 file URI 一律 local',
      meta: { Kind: 'file', URI: 'file:///D:/ws/doc.pdf' },
      root: '',
      want: { kind: 'local', abs: 'D:\\ws\\doc.pdf' },
    },
  ];

  for (const c of cases) {
    it(c.name, () => {
      expect(attachmentAction(c.meta, c.root)).toEqual(c.want);
    });
  }
});
