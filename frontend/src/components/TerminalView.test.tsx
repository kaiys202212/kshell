// TerminalView 测试：jsdom 没有真实终端能力，xterm 与 FitAddon 全部打桩。
// 桩把「写入口 / onData / onResize 回调 / fit 次数」暴露出来供断言；
// rAF 也替换为可控队列，避免测试依赖真实帧时序。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { TerminalInfo } from '../lib/api';
import { bytesToBase64, encodeTerminalInput } from '../lib/base64';
import { clearTerminalRegistry, dispatchTerminalData } from '../lib/terminalRegistry';
import TerminalView from './TerminalView';

// 桩实例的收集箱（vi.hoisted：mock 工厂先于 import 执行）
const mocks = vi.hoisted(() => ({
  terminals: [] as any[],
  addons: [] as any[],
  observers: [] as any[],
}));

// TerminalView 只用到 api 的这两个方法
const api = vi.hoisted(() => ({ writeTerminal: vi.fn(), resizeTerminal: vi.fn() }));
vi.mock('../lib/api', () => api);

vi.mock('@xterm/xterm', () => {
  class MockTerminal {
    options: any;
    host: HTMLElement | null = null;
    dataCb: ((data: string) => void) | null = null;
    resizeCb: ((size: { cols: number; rows: number }) => void) | null = null;
    written: (string | Uint8Array)[] = [];
    disposed = false;
    focusCount = 0;

    constructor(options?: any) {
      this.options = options;
      mocks.terminals.push(this);
    }
    open(host: HTMLElement) {
      this.host = host;
    }
    loadAddon(addon: any) {
      // 真实 xterm 的 loadAddon 会调 addon.activate(terminal)，桩里只回填宿主以便 fit 触发 onResize
      addon.activatedOn = this;
    }
    write(data: string | Uint8Array) {
      this.written.push(data);
    }
    focus() {
      this.focusCount++;
    }
    onData(cb: (data: string) => void) {
      this.dataCb = cb;
      return {
        dispose: () => {
          this.dataCb = null;
        },
      };
    }
    onResize(cb: (size: { cols: number; rows: number }) => void) {
      this.resizeCb = cb;
      return {
        dispose: () => {
          this.resizeCb = null;
        },
      };
    }
    dispose() {
      this.disposed = true;
    }
  }
  return { Terminal: MockTerminal };
});

vi.mock('@xterm/addon-fit', () => {
  class MockFitAddon {
    fitCount = 0;
    dims: { cols: number; rows: number } | null = null;
    activatedOn: { resizeCb: ((size: { cols: number; rows: number }) => void) | null } | null = null;

    constructor() {
      mocks.addons.push(this);
    }
    // 真实 FitAddon.fit 在行列变化时会让 xterm 触发 onResize，桩里显式模拟这一步
    fit() {
      this.fitCount++;
      if (this.dims) this.activatedOn?.resizeCb?.(this.dims);
    }
    dispose() {}
  }
  return { FitAddon: MockFitAddon };
});

const TERM: TerminalInfo = {
  ID: 'term-1',
  Kind: 'session',
  SessionID: 'sess-1',
  Workspace: 'D:\\proj',
  Title: '修复登录',
  ToolID: 'claude',
  Status: 'running',
  ExitCode: 0,
  Cols: 80,
  Rows: 24,
};

// 可控 rAF：回调入队，由 flushRaf 在 act 内统一执行（固定测试的时序）
let rafQueue: FrameRequestCallback[] = [];
function flushRaf() {
  const pending = rafQueue;
  rafQueue = [];
  act(() => {
    pending.forEach((cb) => cb(0));
  });
}

function observer(index = 0) {
  return mocks.observers[index] as {
    cb: ResizeObserverCallback;
    observed: number;
    disconnected: boolean;
  };
}

// 桩实例的断言视图（mocks 里存的是 any[]，这里收窄成可断言的形状）
interface StubTerminal {
  options: Record<string, unknown> & { theme: { background: string; foreground: string } };
  host: HTMLElement | null;
  dataCb: ((data: string) => void) | null;
  resizeCb: ((size: { cols: number; rows: number }) => void) | null;
  written: (string | Uint8Array)[];
  disposed: boolean;
  focusCount: number;
}

interface StubFitAddon {
  fitCount: number;
  dims: { cols: number; rows: number } | null;
}

function term(index = 0): StubTerminal {
  return mocks.terminals[index] as StubTerminal;
}

function fitAddon(index = 0): StubFitAddon {
  return mocks.addons[index] as StubFitAddon;
}

// jsdom 的 window.matchMedia 恒返回 matches:false；组件读的是 window 上的属性，
// 所以这里直接改写 window（stubGlobal 只改 globalThis，不适合这种场景），
// 才能分别验证深浅色与「matchMedia 缺失」的兜底
const realMatchMedia = window.matchMedia;
function setMatchMedia(value: unknown) {
  Object.defineProperty(window, 'matchMedia', { value, configurable: true, writable: true });
}

beforeEach(() => {
  rafQueue = [];
  vi.clearAllMocks();
  mocks.terminals.length = 0;
  mocks.addons.length = 0;
  mocks.observers.length = 0;
  clearTerminalRegistry();
  api.writeTerminal.mockResolvedValue(undefined);
  api.resizeTerminal.mockResolvedValue(undefined);

  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
    rafQueue.push(cb);
    return rafQueue.length;
  });
  vi.stubGlobal('cancelAnimationFrame', () => {});
  vi.stubGlobal(
    'ResizeObserver',
    class MockResizeObserver {
      cb: ResizeObserverCallback;
      observed = 0;
      disconnected = false;
      constructor(cb: ResizeObserverCallback) {
        this.cb = cb;
        mocks.observers.push(this);
      }
      observe() {
        this.observed++;
      }
      unobserve() {}
      disconnect() {
        this.disconnected = true;
      }
    },
  );
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  setMatchMedia(realMatchMedia);
  clearTerminalRegistry();
});

describe('TerminalView', () => {
  it('挂载即登记到注册表，并把 dispatch 来的 base64 输出写进终端', () => {
    render(<TerminalView term={TERM} active />);
    const instance = term();

    // open 到带 xterm-host 的容器（样式由 style.css 提供）
    expect(instance.host).not.toBeNull();
    expect(instance.host?.className).toContain('xterm-host');
    // 初始构造参数：关闭 convertEol、开启光标闪烁、5000 行回滚
    expect(instance.options).toMatchObject({
      convertEol: false,
      cursorBlink: true,
      fontSize: 13,
      scrollback: 5000,
    });

    const bytes = new TextEncoder().encode('\x1b[32m你好\x1b[0m\r\n');
    expect(dispatchTerminalData(TERM.ID, bytesToBase64(bytes))).toBe(true);
    expect(instance.written).toHaveLength(1);
    expect(Array.from(instance.written[0] as Uint8Array)).toEqual(Array.from(bytes));
  });

  it('键盘输入按 UTF-8 → base64 交给 writeTerminal（中文与转义序列各一条）', () => {
    render(<TerminalView term={TERM} active />);
    const instance = term();

    instance.dataCb?.('中文');
    instance.dataCb?.('\x1b[A'); // 方向键上

    expect(api.writeTerminal).toHaveBeenNthCalledWith(1, TERM.ID, encodeTerminalInput('中文'));
    expect(api.writeTerminal).toHaveBeenNthCalledWith(2, TERM.ID, encodeTerminalInput('\x1b[A'));
    // 断死具体 base64，避免「编解码自洽但取值错误」被漏掉
    expect(api.writeTerminal.mock.calls[0][1]).toBe('5Lit5paH'); // 中文
    expect(api.writeTerminal.mock.calls[1][1]).toBe('G1tB'); // ESC [ A
  });

  it('容器尺寸变化 → 下一帧 fit，fit 造成的行列变化同步给 Go（resizeTerminal）', () => {
    render(<TerminalView term={TERM} active />);
    flushRaf(); // 消化挂载时的初始 fit
    const addon = fitAddon();

    expect(observer().observed).toBe(1);
    addon.dims = { cols: 120, rows: 40 };
    const before = addon.fitCount;
    act(() => observer().cb([], observer() as unknown as ResizeObserver));
    expect(addon.fitCount).toBe(before); // 尺寸变化当帧不 fit，等下一帧
    flushRaf();

    expect(addon.fitCount).toBe(before + 1);
    expect(api.resizeTerminal).toHaveBeenCalledWith(TERM.ID, 120, 40);
  });

  it('active 由 false 变 true 时适配尺寸并取焦点；隐藏期间不动终端', () => {
    const { rerender } = render(<TerminalView term={TERM} active={false} />);
    flushRaf();
    const instance = term();
    const addon = fitAddon();
    expect(addon.fitCount).toBe(0);
    expect(instance.focusCount).toBe(0);

    rerender(<TerminalView term={TERM} active />);
    expect(addon.fitCount).toBe(0); // 仍在等下一帧
    flushRaf();
    expect(addon.fitCount).toBe(1);
    expect(instance.focusCount).toBe(1);

    // 再隐藏不做任何销毁：实例与登记都还在
    rerender(<TerminalView term={TERM} active={false} />);
    flushRaf();
    expect(instance.disposed).toBe(false);
    expect(dispatchTerminalData(TERM.ID, bytesToBase64(new Uint8Array([65])))).toBe(true);
  });

  it('非激活页签的尺寸变化不 fit（隐藏宿主会被 FitAddon 钳成退化尺寸）', () => {
    const { rerender } = render(<TerminalView term={TERM} active={false} />);
    flushRaf();
    const addon = fitAddon();

    // 隐藏态容器尺寸为 0，真实 FitAddon 会算出 cols=2/rows=1：这里正是必须跳过的时刻
    addon.dims = { cols: 2, rows: 1 };
    act(() => observer().cb([], observer() as unknown as ResizeObserver));
    flushRaf();

    expect(addon.fitCount).toBe(0);
    expect(api.resizeTerminal).not.toHaveBeenCalled();

    // 切回可见页签后照常 fit，并把真实尺寸同步给 Go
    addon.dims = { cols: 120, rows: 40 };
    rerender(<TerminalView term={TERM} active />);
    flushRaf();

    expect(addon.fitCount).toBe(1);
    expect(api.resizeTerminal).toHaveBeenCalledWith(TERM.ID, 120, 40);
  });

  it('退化尺寸（行列过小）绝不推给 PTY，恢复有效尺寸后再同步', () => {
    render(<TerminalView term={TERM} active />);
    flushRaf();
    const instance = term();

    // 实测：把 2x1、6x3 这类尺寸推给伪终端会让 opencode 直接崩溃退出（退出码 3）
    instance.resizeCb?.({ cols: 2, rows: 1 });
    instance.resizeCb?.({ cols: 6, rows: 3 });
    expect(api.resizeTerminal).not.toHaveBeenCalled();

    instance.resizeCb?.({ cols: 80, rows: 24 });
    expect(api.resizeTerminal).toHaveBeenCalledTimes(1);
    expect(api.resizeTerminal).toHaveBeenCalledWith(TERM.ID, 80, 24);
  });

  it('卸载会注销注册表、取消订阅并销毁终端；ResizeObserver 断开', () => {
    const { unmount } = render(<TerminalView term={TERM} active />);
    const instance = term();
    expect(dispatchTerminalData(TERM.ID, bytesToBase64(new Uint8Array([65])))).toBe(true);

    unmount();

    expect(instance.disposed).toBe(true);
    expect(instance.dataCb).toBeNull();
    expect(instance.resizeCb).toBeNull();
    expect(observer().disconnected).toBe(true);
    expect(dispatchTerminalData(TERM.ID, bytesToBase64(new Uint8Array([65])))).toBe(false);
  });

  it('Status=exited 时渲染退出条，并只往终端里写一次退出提示', () => {
    const { rerender } = render(<TerminalView term={TERM} active />);
    const instance = term();
    expect(screen.queryByText(/会话已退出/)).toBeNull();

    rerender(<TerminalView term={{ ...TERM, Status: 'exited', ExitCode: 3 }} active />);
    expect(screen.getByText('会话已退出（退出码 3）')).toBeInTheDocument();
    const hintCount = () =>
      instance.written.filter((w) => typeof w === 'string' && w.includes('会话已退出')).length;
    expect(hintCount()).toBe(1);
    expect(String(instance.written.find((w) => typeof w === 'string'))).toContain('\x1b[90m'); // 灰色提示

    // store 再次 upsert（对象换新）不重复写提示
    rerender(<TerminalView term={{ ...TERM, Status: 'exited', ExitCode: 3 }} active />);
    expect(hintCount()).toBe(1);
  });

  it('终端主题跟随系统深浅色；matchMedia 缺失时兜底浅色且不抛异常', () => {
    setMatchMedia(() => ({ matches: true }));
    const dark = render(<TerminalView term={TERM} active />);
    expect(term(0).options.theme).toEqual({ background: '#0d1117', foreground: '#e6edf3' });
    dark.unmount();

    setMatchMedia(() => ({ matches: false }));
    const light = render(<TerminalView term={TERM} active />);
    expect(term(1).options.theme).toEqual({ background: '#ffffff', foreground: '#1f2328' });
    light.unmount();

    setMatchMedia(undefined);
    const fallback = render(<TerminalView term={TERM} active />);
    expect(term(2).options.theme).toEqual({ background: '#ffffff', foreground: '#1f2328' });
    fallback.unmount();
  });
});
