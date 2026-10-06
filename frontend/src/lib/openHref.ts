// 超链接点击：带协议交给系统默认程序，相对路径在工作区内打开文件。
// 分类必须用元素上的 href 属性，不能用 a.href（WebView 会按自身基址改写）。

export type HrefAction =
  | { kind: 'ignore' }
  | { kind: 'external'; url: string }
  | { kind: 'workspace'; path: string };

export type ClassifyOpts = {
  workspaceRoot?: string;
  sourceFile?: string;
};

export const OPEN_FILE_EVENT = 'kshell:open-file';

const DANGEROUS_SCHEMES = new Set(['javascript', 'data', 'vbscript', 'blob']);

function posixNorm(p: string): string {
  return p.replace(/\\/g, '/');
}

function toNative(posixPath: string, workspaceRoot: string): string {
  if (workspaceRoot.includes('\\') || /^[a-zA-Z]:/.test(workspaceRoot)) {
    return posixPath.replace(/\//g, '\\');
  }
  return posixPath;
}

function splitDrive(p: string): { drive: string; rest: string } {
  const n = posixNorm(p);
  const m = n.match(/^([a-zA-Z]:)(\/.*)?$/);
  if (m) return { drive: m[1], rest: m[2] || '/' };
  return { drive: '', rest: n.startsWith('/') ? n : `/${n}` };
}

function joinPosix(base: string, rel: string): string {
  const { drive, rest } = splitDrive(base);
  const parts = rest.split('/').filter((s) => s && s !== '.');
  for (const seg of posixNorm(rel).split('/')) {
    if (!seg || seg === '.') continue;
    if (seg === '..') {
      if (parts.length > 0) parts.pop();
      continue;
    }
    parts.push(seg);
  }
  const body = parts.join('/');
  if (drive) return `${drive}/${body}`;
  return `/${body}`;
}

function isInsideWorkspace(root: string, abs: string): boolean {
  const r = posixNorm(root).replace(/\/+$/, '').toLowerCase();
  const a = posixNorm(abs).replace(/\/+$/, '').toLowerCase();
  return a === r || a.startsWith(`${r}/`);
}

function stripHashQuery(href: string): string {
  const i = href.search(/[?#]/);
  return i >= 0 ? href.slice(0, i) : href;
}

function resolveWorkspace(href: string, opts: ClassifyOpts): HrefAction {
  const root = opts.workspaceRoot?.trim();
  if (!root) return { kind: 'ignore' };

  let raw = stripHashQuery(href.trim());
  try {
    raw = decodeURI(raw);
  } catch {
    // 非法 percent-encoding 时保持原串
  }

  const posixRoot = posixNorm(root);
  const posixRaw = posixNorm(raw);
  const isAbsWin = /^[a-zA-Z]:/.test(posixRaw);
  const rootedAtWs = posixRaw.startsWith('/') && !isAbsWin;

  let abs: string;
  if (isAbsWin) {
    abs = posixRaw;
  } else if (rootedAtWs) {
    abs = joinPosix(posixRoot, posixRaw.replace(/^\/+/, ''));
  } else {
    const base = opts.sourceFile
      ? posixNorm(opts.sourceFile).replace(/\/[^/]+$/, '')
      : posixRoot;
    abs = joinPosix(base, posixRaw);
  }

  if (!isInsideWorkspace(root, abs)) return { kind: 'ignore' };
  return { kind: 'workspace', path: toNative(abs, root) };
}

export function classifyHref(href: string, opts?: ClassifyOpts): HrefAction {
  const raw = href.trim();
  if (!raw || raw.startsWith('#')) return { kind: 'ignore' };

  const m = raw.match(/^([a-zA-Z][a-zA-Z0-9+.-]*):/);
  if (m) {
    const scheme = m[1].toLowerCase();
    if (scheme.length === 1) return resolveWorkspace(raw, opts ?? {});
    if (DANGEROUS_SCHEMES.has(scheme)) return { kind: 'ignore' };
    return { kind: 'external', url: raw };
  }
  return resolveWorkspace(raw, opts ?? {});
}

type RuntimeHost = { BrowserOpenURL?: (url: string) => void };

export function openExternal(url: string): void {
  const rt = (globalThis as { runtime?: RuntimeHost }).runtime;
  rt?.BrowserOpenURL?.(url);
}

export function requestOpenWorkspaceFile(workspace: string, path: string): void {
  window.dispatchEvent(new CustomEvent(OPEN_FILE_EVENT, { detail: { workspace, path } }));
}

export function handleHref(href: string, opts?: ClassifyOpts): void {
  const action = classifyHref(href, opts);
  if (action.kind === 'external') {
    openExternal(action.url);
    return;
  }
  if (action.kind === 'workspace' && opts?.workspaceRoot) {
    requestOpenWorkspaceFile(opts.workspaceRoot, action.path);
  }
}

export function handleAnchorClick(ev: MouseEvent, opts?: ClassifyOpts): boolean {
  const target = ev.target;
  if (!(target instanceof Element)) return false;
  const a = target.closest('a');
  if (!a) return false;
  const href = a.getAttribute('href');
  if (href == null) return false;

  const wrap = a.closest('[data-kshell-workspace]');
  const workspaceRoot =
    opts?.workspaceRoot ?? wrap?.getAttribute('data-kshell-workspace') ?? undefined;
  const sourceFile =
    opts?.sourceFile ?? wrap?.getAttribute('data-kshell-source-file') ?? undefined;
  const merged: ClassifyOpts = { workspaceRoot, sourceFile };

  ev.preventDefault();
  handleHref(href, merged);
  return true;
}
