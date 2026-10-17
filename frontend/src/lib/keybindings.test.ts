// 全局快捷键分发模块测试：表驱动覆盖各键位匹配、三级退让（defaultPrevented /
// xterm 聚焦分级）、输入框聚焦仍可切 tab、越界场景由 actions 桩验证。
// jsdom 合成 KeyboardEvent，actions 全部用 vi.fn() 打桩。
import { afterEach, describe, expect, it, vi } from 'vitest';
import { handleGlobalKey, SHORTCUTS, type GlobalKeyActions } from './keybindings';

function makeActions(): GlobalKeyActions {
  return {
    switchToTab: vi.fn(),
    nextTab: vi.fn(),
    prevTab: vi.fn(),
    closeActiveTab: vi.fn(),
    goHome: vi.fn(),
    toggleSwitcher: vi.fn(),
    focusSearch: vi.fn(),
    toggleHelp: vi.fn(),
  };
}

function key(
  init: { key: string; code?: string; ctrl?: boolean; shift?: boolean },
): KeyboardEvent {
  return new KeyboardEvent('keydown', {
    key: init.key,
    code: init.code ?? '',
    ctrlKey: !!init.ctrl,
    shiftKey: !!init.shift,
    cancelable: true,
    bubbles: true,
  });
}

// 把 activeElement 放进/移出 .xterm 容器
function focusInXterm(): void {
  const wrap = document.createElement('div');
  wrap.className = 'xterm';
  const inner = document.createElement('input');
  wrap.appendChild(inner);
  document.body.appendChild(wrap);
  inner.focus();
}
function blurAll(): void {
  (document.activeElement as HTMLElement | null)?.blur?.();
  document.body.focus();
  document.querySelectorAll('.xterm').forEach((n) => n.remove());
}

afterEach(() => {
  blurAll();
});

describe('SHORTCUTS 表', () => {
  it('包含全部 8 个快捷键定义，且均有展示键名与匹配函数', () => {
    const ids = SHORTCUTS.map((s) => s.id);
    expect(ids).toEqual([
      'quick-switcher',
      'focus-search',
      'switch-tab-n',
      'next-tab',
      'prev-tab',
      'close-tab',
      'go-home',
      'toggle-help',
    ]);
    for (const s of SHORTCUTS) {
      expect(s.keys.length).toBeGreaterThan(0); // 展示键名非空
      expect(typeof s.matches).toBe('function');
    }
  });
});

describe('handleGlobalKey：基础匹配', () => {
  it('Ctrl+K 触发 toggleSwitcher 并消费（大小写兼容）', () => {
    for (const k of ['k', 'K']) {
      const actions = makeActions();
      const e = key({ key: k, ctrl: true });
      expect(handleGlobalKey(e, actions)).toBe(true);
      expect(actions.toggleSwitcher).toHaveBeenCalledTimes(1);
      expect(e.defaultPrevented).toBe(true);
    }
  });

  it('Ctrl+F 触发 focusSearch', () => {
    for (const k of ['f', 'F']) {
      const actions = makeActions();
      const e = key({ key: k, ctrl: true });
      expect(handleGlobalKey(e, actions)).toBe(true);
      expect(actions.focusSearch).toHaveBeenCalledTimes(1);
    }
  });

  it('Ctrl+1..9 触发 switchToTab(n)，Ctrl+0 触发 goHome', () => {
    for (let n = 1; n <= 9; n++) {
      const actions = makeActions();
      const e = key({ key: String(n), code: `Digit${n}`, ctrl: true });
      expect(handleGlobalKey(e, actions)).toBe(true);
      expect(actions.switchToTab).toHaveBeenCalledWith(n);
    }
    const actions = makeActions();
    const e = key({ key: '0', code: 'Digit0', ctrl: true });
    expect(handleGlobalKey(e, actions)).toBe(true);
    expect(actions.goHome).toHaveBeenCalledTimes(1);
    expect(actions.switchToTab).not.toHaveBeenCalled();
  });

  it('Ctrl+T 也映射 go-home（回首页=新建会话入口）', () => {
    const actions = makeActions();
    const e = key({ key: 't', ctrl: true });
    expect(handleGlobalKey(e, actions)).toBe(true);
    expect(actions.goHome).toHaveBeenCalledTimes(1);
  });

  it('Ctrl+Tab 触发 nextTab，Ctrl+Shift+Tab 触发 prevTab（用 e.key 判定）', () => {
    const a1 = makeActions();
    expect(handleGlobalKey(key({ key: 'Tab', ctrl: true }), a1)).toBe(true);
    expect(a1.nextTab).toHaveBeenCalledTimes(1);
    expect(a1.prevTab).not.toHaveBeenCalled();

    const a2 = makeActions();
    expect(handleGlobalKey(key({ key: 'Tab', ctrl: true, shift: true }), a2)).toBe(true);
    expect(a2.prevTab).toHaveBeenCalledTimes(1);
    expect(a2.nextTab).not.toHaveBeenCalled();
  });

  it('Ctrl+W 触发 closeActiveTab', () => {
    const actions = makeActions();
    expect(handleGlobalKey(key({ key: 'w', ctrl: true }), actions)).toBe(true);
    expect(actions.closeActiveTab).toHaveBeenCalledTimes(1);
  });

  it('F1（无修饰）触发 toggleHelp', () => {
    const actions = makeActions();
    expect(handleGlobalKey(key({ key: 'F1' }), actions)).toBe(true);
    expect(actions.toggleHelp).toHaveBeenCalledTimes(1);
  });

  it('未匹配的按键返回 false 且不消费', () => {
    const actions = makeActions();
    const e = key({ key: 'x', ctrl: true });
    expect(handleGlobalKey(e, actions)).toBe(false);
    expect(e.defaultPrevented).toBe(false);
    for (const a of Object.values(actions)) expect(a).not.toHaveBeenCalled();
  });

  it('无 Ctrl 修饰的字母键不触发（F1 除外）', () => {
    const actions = makeActions();
    expect(handleGlobalKey(key({ key: 'k' }), actions)).toBe(false);
    expect(handleGlobalKey(key({ key: 'w', shift: true }), actions)).toBe(false);
    expect(handleGlobalKey(key({ key: '1', code: 'Digit1' }), actions)).toBe(false);
    expect(actions.toggleSwitcher).not.toHaveBeenCalled();
  });
});

describe('handleGlobalKey：退让规则', () => {
  it('e.defaultPrevented 时不处理任何键', () => {
    const actions = makeActions();
    const e = new KeyboardEvent('keydown', { key: 'k', ctrlKey: true, cancelable: true });
    e.preventDefault();
    expect(handleGlobalKey(e, actions)).toBe(false);
    expect(actions.toggleSwitcher).not.toHaveBeenCalled();
  });

  it('xterm 聚焦时：切 tab 类（1..9 / 0 / Ctrl+Tab / Ctrl+T）仍生效，其余退让', () => {
    focusInXterm();
    // 仍生效：switch-tab-n / go-home（0 与 T）/ next / prev
    const a1 = makeActions();
    expect(handleGlobalKey(key({ key: '3', code: 'Digit3', ctrl: true }), a1)).toBe(true);
    expect(a1.switchToTab).toHaveBeenCalledWith(3);

    const a2 = makeActions();
    expect(handleGlobalKey(key({ key: '0', code: 'Digit0', ctrl: true }), a2)).toBe(true);
    expect(a2.goHome).toHaveBeenCalledTimes(1);

    const a3 = makeActions();
    expect(handleGlobalKey(key({ key: 'Tab', ctrl: true }), a3)).toBe(true);
    expect(a3.nextTab).toHaveBeenCalledTimes(1);

    const a4 = makeActions();
    expect(handleGlobalKey(key({ key: 't', ctrl: true }), a4)).toBe(true);
    expect(a4.goHome).toHaveBeenCalledTimes(1);

    // 退让：Ctrl+K / Ctrl+F / Ctrl+W / F1
    for (const ev of [
      key({ key: 'k', ctrl: true }),
      key({ key: 'f', ctrl: true }),
      key({ key: 'w', ctrl: true }),
      key({ key: 'F1' }),
    ]) {
      const actions = makeActions();
      expect(handleGlobalKey(ev, actions)).toBe(false);
      expect(ev.defaultPrevented).toBe(false);
      for (const a of Object.values(actions)) expect(a).not.toHaveBeenCalled();
    }
  });

  it('输入框聚焦（非 xterm）时全部快捷键仍生效（不产生文本的键）', () => {
    const input = document.createElement('input');
    document.body.appendChild(input);
    input.focus();

    const actions = makeActions();
    expect(handleGlobalKey(key({ key: 'k', ctrl: true }), actions)).toBe(true);
    expect(actions.toggleSwitcher).toHaveBeenCalledTimes(1);
    const a2 = makeActions();
    expect(handleGlobalKey(key({ key: '2', code: 'Digit2', ctrl: true }), a2)).toBe(true);
    expect(a2.switchToTab).toHaveBeenCalledWith(2);
    input.remove();
  });
});
