import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  classifyHref,
  handleAnchorClick,
  handleHref,
  OPEN_FILE_EVENT,
  requestOpenWorkspaceFile,
} from './openHref';

describe('classifyHref', () => {
  const ws = 'D:\\proj';

  it('https 走系统打开', () => {
    expect(classifyHref('https://example.com/a')).toEqual({
      kind: 'external',
      url: 'https://example.com/a',
    });
  });

  it('自定义协议走系统打开', () => {
    expect(classifyHref('vscode://file/D:/proj/a.ts')).toEqual({
      kind: 'external',
      url: 'vscode://file/D:/proj/a.ts',
    });
  });

  it('mailto 走系统打开', () => {
    expect(classifyHref('mailto:a@b.com')).toEqual({ kind: 'external', url: 'mailto:a@b.com' });
  });

  it('危险协议忽略', () => {
    expect(classifyHref('javascript:alert(1)')).toEqual({ kind: 'ignore' });
    expect(classifyHref('data:text/html,x')).toEqual({ kind: 'ignore' });
    expect(classifyHref('vbscript:msg')).toEqual({ kind: 'ignore' });
    expect(classifyHref('blob:https://x/1')).toEqual({ kind: 'ignore' });
  });

  it('纯锚点忽略', () => {
    expect(classifyHref('#sec')).toEqual({ kind: 'ignore' });
    expect(classifyHref('  ')).toEqual({ kind: 'ignore' });
  });

  it('相对路径相对工作区根打开', () => {
    expect(classifyHref('./README.md', { workspaceRoot: ws })).toEqual({
      kind: 'workspace',
      path: 'D:\\proj\\README.md',
    });
    expect(classifyHref('docs/a.md', { workspaceRoot: ws })).toEqual({
      kind: 'workspace',
      path: 'D:\\proj\\docs\\a.md',
    });
  });

  it('以 / 开头相对工作区根', () => {
    expect(classifyHref('/src/a.go', { workspaceRoot: ws })).toEqual({
      kind: 'workspace',
      path: 'D:\\proj\\src\\a.go',
    });
  });

  it('相对 sourceFile 所在目录', () => {
    expect(
      classifyHref('../lib/x.ts', {
        workspaceRoot: ws,
        sourceFile: 'D:\\proj\\docs\\n.md',
      }),
    ).toEqual({ kind: 'workspace', path: 'D:\\proj\\lib\\x.ts' });
  });

  it('逃出工作区则忽略', () => {
    expect(classifyHref('../../evil', { workspaceRoot: ws })).toEqual({ kind: 'ignore' });
    expect(classifyHref('../proj-evil/a.md', { workspaceRoot: ws })).toEqual({ kind: 'ignore' });
  });

  it('无工作区上下文的相对路径忽略', () => {
    expect(classifyHref('./README.md')).toEqual({ kind: 'ignore' });
  });

  it('盘符绝对路径且在工作区内', () => {
    expect(classifyHref('D:\\proj\\a.md', { workspaceRoot: ws })).toEqual({
      kind: 'workspace',
      path: 'D:\\proj\\a.md',
    });
  });

  it('盘符路径里多余反斜杠仍视为工作区内', () => {
    expect(classifyHref('./a.md', { workspaceRoot: 'D:\\\\proj' })).toEqual({
      kind: 'workspace',
      path: 'D:\\proj\\a.md',
    });
  });
});

describe('handleHref / 点击', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('external 调用 BrowserOpenURL', () => {
    const open = vi.fn();
    vi.stubGlobal('runtime', { BrowserOpenURL: open });
    handleHref('https://example.com');
    expect(open).toHaveBeenCalledWith('https://example.com');
  });

  it('workspace 派发 kshell:open-file', () => {
    const seen: unknown[] = [];
    const onOpen = (e: Event) => {
      seen.push((e as CustomEvent).detail);
    };
    window.addEventListener(OPEN_FILE_EVENT, onOpen);
    handleHref('./README.md', { workspaceRoot: 'D:\\proj' });
    window.removeEventListener(OPEN_FILE_EVENT, onOpen);
    expect(seen).toEqual([{ workspace: 'D:\\proj', path: 'D:\\proj\\README.md' }]);
  });

  it('多余反斜杠的工作区根在事件里归一化', () => {
    const seen: unknown[] = [];
    const onOpen = (e: Event) => seen.push((e as CustomEvent).detail);
    window.addEventListener(OPEN_FILE_EVENT, onOpen);
    handleHref('./a.md', { workspaceRoot: 'D:\\\\proj' });
    window.removeEventListener(OPEN_FILE_EVENT, onOpen);
    expect(seen).toEqual([{ workspace: 'D:\\proj', path: 'D:\\proj\\a.md' }]);
  });

  it('requestOpenWorkspaceFile 派发事件', () => {
    const seen: unknown[] = [];
    const onOpen = (e: Event) => seen.push((e as CustomEvent).detail);
    window.addEventListener(OPEN_FILE_EVENT, onOpen);
    requestOpenWorkspaceFile('D:\\p', 'D:\\p\\a.ts');
    window.removeEventListener(OPEN_FILE_EVENT, onOpen);
    expect(seen).toEqual([{ workspace: 'D:\\p', path: 'D:\\p\\a.ts' }]);
  });

  it('捕获 a 点击 preventDefault 并用 attribute href', () => {
    const open = vi.fn();
    vi.stubGlobal('runtime', { BrowserOpenURL: open });
    const a = document.createElement('a');
    a.setAttribute('href', 'https://example.com/from-attr');
    Object.defineProperty(a, 'href', { get: () => 'wails://localhost/hijacked' });
    const inner = document.createElement('span');
    a.appendChild(inner);
    document.body.appendChild(a);
    const ev = new MouseEvent('click', { bubbles: true, cancelable: true });
    Object.defineProperty(ev, 'target', { value: inner });
    const handled = handleAnchorClick(ev);
    expect(handled).toBe(true);
    expect(ev.defaultPrevented).toBe(true);
    expect(open).toHaveBeenCalledWith('https://example.com/from-attr');
    a.remove();
  });

  it('从祖先 data-kshell-workspace 解析相对路径', () => {
    const seen: unknown[] = [];
    const onOpen = (e: Event) => seen.push((e as CustomEvent).detail);
    window.addEventListener(OPEN_FILE_EVENT, onOpen);
    const wrap = document.createElement('div');
    wrap.setAttribute('data-kshell-workspace', 'D:\\proj');
    const a = document.createElement('a');
    a.setAttribute('href', './README.md');
    wrap.appendChild(a);
    document.body.appendChild(wrap);
    const ev = new MouseEvent('click', { bubbles: true, cancelable: true });
    Object.defineProperty(ev, 'target', { value: a });
    handleAnchorClick(ev);
    window.removeEventListener(OPEN_FILE_EVENT, onOpen);
    expect(seen).toEqual([{ workspace: 'D:\\proj', path: 'D:\\proj\\README.md' }]);
    wrap.remove();
  });
});
