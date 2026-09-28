// 终端注册表测试：登记/注销/覆盖、输出分发、非法输入容错、focus/fit 路由。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Mock } from 'vitest';
import { bytesToBase64 } from './base64';
import {
  clearTerminalRegistry,
  dispatchTerminalData,
  fitTerminal,
  focusTerminal,
  registerTerminal,
  unregisterTerminal,
} from './terminalRegistry';
import type { TerminalHandle } from './terminalRegistry';

interface FakeHandle extends TerminalHandle {
  write: Mock<(bytes: Uint8Array) => void>;
  focus: Mock<() => void>;
  fit: Mock<() => void>;
}

function fakeHandle(): FakeHandle {
  return {
    write: vi.fn<(bytes: Uint8Array) => void>(),
    focus: vi.fn<() => void>(),
    fit: vi.fn<() => void>(),
  };
}

function spyWarn() {
  return vi.spyOn(console, 'warn').mockImplementation(() => {});
}

let warn: ReturnType<typeof spyWarn>;

beforeEach(() => {
  clearTerminalRegistry();
  warn = spyWarn();
});

afterEach(() => {
  warn.mockRestore();
  clearTerminalRegistry();
});

describe('terminalRegistry', () => {
  it('登记后能把 base64 输出解码成字节投递给对应终端', () => {
    const h = fakeHandle();
    registerTerminal('t1', h);

    const bytes = new TextEncoder().encode('\x1b[32m你好\x1b[0m\r\n');
    expect(dispatchTerminalData('t1', bytesToBase64(bytes))).toBe(true);

    expect(h.write).toHaveBeenCalledTimes(1);
    expect(Array.from(h.write.mock.calls[0][0])).toEqual(Array.from(bytes));
  });

  it('空 base64 串按空字节投递，不报错', () => {
    const h = fakeHandle();
    registerTerminal('t1', h);

    expect(dispatchTerminalData('t1', '')).toBe(true);
    expect(Array.from(h.write.mock.calls[0][0])).toEqual([]);
  });

  it('未登记的 id（页签已卸载）丢弃数据并返回 false', () => {
    const h = fakeHandle();
    registerTerminal('t1', h);

    expect(dispatchTerminalData('t2', bytesToBase64(new Uint8Array([1, 2])))).toBe(false);
    expect(h.write).not.toHaveBeenCalled();
  });

  it('空 id 不抛异常，也不投递（含 registerTerminal 的空 id 保护）', () => {
    const h = fakeHandle();
    registerTerminal('t1', h);

    expect(() => registerTerminal('', h)).not.toThrow();
    expect(() => dispatchTerminalData('', 'AQ==')).not.toThrow();
    expect(dispatchTerminalData('', 'AQ==')).toBe(false);
    expect(focusTerminal('')).toBe(false);
    expect(fitTerminal('')).toBe(false);
    expect(h.write).not.toHaveBeenCalled();
    expect(h.focus).not.toHaveBeenCalled();
    expect(h.fit).not.toHaveBeenCalled();
  });

  it('非法 base64 返回 false 并丢弃（不抛异常）', () => {
    const h = fakeHandle();
    registerTerminal('t1', h);

    expect(() => dispatchTerminalData('t1', '@@@not-base64@@@')).not.toThrow();
    expect(dispatchTerminalData('t1', '@@@not-base64@@@')).toBe(false);
    expect(h.write).not.toHaveBeenCalled();
  });

  it('同 id 重复登记：后者覆盖前者，且告警一次', () => {
    const oldHandle = fakeHandle();
    const newHandle = fakeHandle();
    registerTerminal('t1', oldHandle);
    registerTerminal('t1', newHandle);
    expect(warn).toHaveBeenCalledTimes(1);

    expect(dispatchTerminalData('t1', 'AQ==')).toBe(true);
    expect(newHandle.write).toHaveBeenCalledTimes(1);
    expect(oldHandle.write).not.toHaveBeenCalled();
  });

  it('注销后分发返回 false，注销未知 id 静默忽略', () => {
    const h = fakeHandle();
    registerTerminal('t1', h);
    unregisterTerminal('t1');

    expect(dispatchTerminalData('t1', 'AQ==')).toBe(false);
    expect(() => unregisterTerminal('t1')).not.toThrow();
    expect(() => unregisterTerminal('')).not.toThrow();
    expect(h.write).not.toHaveBeenCalled();
  });

  it('focusTerminal / fitTerminal 只路由到命中的终端', () => {
    const a = fakeHandle();
    const b = fakeHandle();
    registerTerminal('a', a);
    registerTerminal('b', b);

    expect(focusTerminal('a')).toBe(true);
    expect(fitTerminal('b')).toBe(true);
    expect(a.focus).toHaveBeenCalledTimes(1);
    expect(a.fit).not.toHaveBeenCalled();
    expect(b.fit).toHaveBeenCalledTimes(1);
    expect(b.focus).not.toHaveBeenCalled();

    expect(focusTerminal('missing')).toBe(false);
    expect(fitTerminal('missing')).toBe(false);
  });

  it('clearTerminalRegistry 清空全部登记项', () => {
    const a = fakeHandle();
    const b = fakeHandle();
    registerTerminal('a', a);
    registerTerminal('b', b);
    clearTerminalRegistry();

    expect(dispatchTerminalData('a', 'AQ==')).toBe(false);
    expect(dispatchTerminalData('b', 'AQ==')).toBe(false);
    expect(focusTerminal('a')).toBe(false);
    expect(fitTerminal('b')).toBe(false);
  });
});
