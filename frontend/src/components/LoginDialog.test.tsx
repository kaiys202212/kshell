// LoginDialog 测试：jsdom 没有真实终端能力，xterm/FitAddon/WebLinks 全部打桩（同 TerminalView）。
// 覆盖：挂载即开登录终端并登记 xterm（全局总线数据可达）、轮询到 in 自动收尾
// （成功提示 + 重扫 + onClose）、不支持登录（err.login.unsupported）展示翻译错误。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { bytesToBase64 } from '../lib/base64';
import { clearTerminalRegistry, dispatchTerminalData } from '../lib/terminalRegistry';
import { useAppStore } from '../state/store';
import LoginDialog from './LoginDialog';
import { tt } from '../test/i18n';

const mocks = vi.hoisted(() => ({
  terminals: [] as any[],
  addons: [] as any[],
}));

// LoginDialog 只用到 api 的这五个方法
const api = vi.hoisted(() => ({
  openLoginTerminal: vi.fn(),
  checkToolAuth: vi.fn(),
  writeTerminal: vi.fn(),
  resizeTerminal: vi.fn(),
  scanSessions: vi.fn(),
  openExternalURL: vi.fn().mockResolvedValue(undefined),
}));
vi.mock('../lib/api', () => api);

vi.mock('@xterm/xterm', () => {
  class MockTerminal {
    options: any;
    dataCb: ((data: string) => void) | null = null;
    resizeCb: ((size: { cols: number; rows: number }) => void) | null = null;
    written: (string | Uint8Array)[] = [];
    disposed = false;
    focusCount = 0;
    constructor(options?: any) {
      this.options = options;
      mocks.terminals.push(this);
    }
    open() {}
    loadAddon() {}
    write(data: string | Uint8Array) {
      this.written.push(data);
    }
    focus() {
      this.focusCount++;
    }
    onData(cb: (data: string) => void) {
      this.dataCb = cb;
      return { dispose: () => (this.dataCb = null) };
    }
    onResize(cb: (size: { cols: number; rows: number }) => void) {
      this.resizeCb = cb;
      return { dispose: () => (this.resizeCb = null) };
    }
    dispose() {
      this.disposed = true;
    }
  }
  return { Terminal: MockTerminal };
});

vi.mock('@xterm/addon-fit', () => ({
  FitAddon: class {
    fit() {}
    dispose() {}
  },
}));

vi.mock('@xterm/addon-web-links', () => ({
  WebLinksAddon: class {
    constructor() {}
    dispose() {}
  },
}));

const OPENED = {
  ID: 'login-term-1',
  Kind: 'login',
  SessionID: '',
  Workspace: '',
  Title: 'Cursor 登录',
  ToolID: 'cursor',
  Status: 'running',
  ExitCode: 0,
  Cols: 80,
  Rows: 24,
};

beforeEach(() => {
  vi.clearAllMocks();
  mocks.terminals.length = 0;
  clearTerminalRegistry();
  api.openLoginTerminal.mockResolvedValue(OPENED);
  api.checkToolAuth.mockResolvedValue('out');
  api.writeTerminal.mockResolvedValue(undefined);
  api.resizeTerminal.mockResolvedValue(undefined);
  api.scanSessions.mockResolvedValue(undefined);
  useAppStore.setState({ toasts: [] });
  useAppStore.getState().setAppearance({ mode: 'system', resolved: 'dark', fontSize: 13 });
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

function renderDialog(onClose = vi.fn()) {
  render(<LoginDialog toolID="cursor" toolName="Cursor" onClose={onClose} />);
  return onClose;
}

describe('LoginDialog', () => {
  it('挂载即开登录终端（login:<toolID> 语义，80x24），标题/说明齐备且 xterm 收到全局总线数据', async () => {
    renderDialog();

    expect(api.openLoginTerminal).toHaveBeenCalledWith('cursor', 80, 24);
    expect(await screen.findByText(tt('ui.auth.login_title').replace('{{name}}', 'Cursor'))).toBeInTheDocument();
    expect(screen.getByText(tt('ui.auth.login_hint'))).toBeInTheDocument();

    // xterm 已登记：App 全局订阅的 terminal:data（base64）经 dispatchTerminalData 投递到实例
    await act(async () => {});
    expect(mocks.terminals).toHaveLength(1);
    const instance = mocks.terminals[0];
    act(() => {
      dispatchTerminalData('login-term-1', bytesToBase64(new TextEncoder().encode('hello-login')));
    });
    // 跨 realm 的 Uint8Array 原型不同，转字符串比较字节内容
    expect(String.fromCharCode(...(instance.written[0] as Uint8Array))).toBe('hello-login');
  });

  it('轮询到 in：成功提示 + 触发重扫（工具表刷新）+ 自动关闭，且不再继续轮询', async () => {
    vi.useFakeTimers();
    let status = 'out';
    api.checkToolAuth.mockImplementation(async () => status);
    const onClose = renderDialog();

    await act(async () => {}); // 挂载立即探测一次（out）
    expect(api.checkToolAuth).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000); // 2s 周期探测（仍 out）
    });
    expect(api.checkToolAuth).toHaveBeenCalledTimes(2);

    status = 'in'; // 模拟用户在终端完成登录
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000);
    });

    expect(api.checkToolAuth).toHaveBeenCalledTimes(3);
    expect(api.scanSessions).toHaveBeenCalledTimes(1);
    expect(onClose).toHaveBeenCalledTimes(1);
    const expected = tt('ui.auth.login_success').replace('{{name}}', 'Cursor');
    expect(useAppStore.getState().toasts.some((t) => t.tone === 'success' && t.title === expected)).toBe(true);

    // 自动关闭后轮询停止：再过多个周期不新增探测
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000);
    });
    expect(api.checkToolAuth).toHaveBeenCalledTimes(3);
  });

  it('不支持登录（err.login.unsupported）：展示翻译后的错误，不开终端、轮询停止', async () => {
    vi.useFakeTimers();
    api.openLoginTerminal.mockRejectedValue(new Error('err.login.unsupported'));
    renderDialog();

    await act(async () => {}); // 让 openLoginTerminal 的拒绝落位
    const alert = screen.getByRole('alert');
    expect(alert).toHaveTextContent(tt('err.login.unsupported'));
    // 打开失败即停轮询：只剩挂载时的立即探测一次，之后不再新增（空转 10 分钟没有意义）
    await act(async () => {
      await vi.advanceTimersByTimeAsync(6000);
    });
    expect(api.checkToolAuth).toHaveBeenCalledTimes(1);
    expect(mocks.terminals).toHaveLength(0);
  });
});
