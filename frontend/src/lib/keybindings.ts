// 应用内全局快捷键：键位常量表 + 统一分发纯函数。
// 设计要点（docs/plans/2026-10-17-global-shortcuts-design.md）：
// 1. e.defaultPrevented 退让（组件已消费的按键不抢）；
// 2. activeElement 在 .xterm 内分级退让——只有切 tab 类（不写 pty、无双效应）仍生效，
//    Ctrl+K / Ctrl+F / Ctrl+W / F1 让行给终端语义；
// 3. 输入框聚焦不退让：这些键位不产生文本，输入框里 Ctrl+K/切 tab 仍可用。
// actions 由调用方注入（App.tsx），纯函数便于测试与展示层（帮助面板）共用常量。

// 快捷键定义：id 用于 i18n key（ui.help.shortcuts.<id>）与帮助面板渲染，
// keys 为展示键名（帮助表与实现引用同一常量，避免漂移）。
export interface ShortcutDef {
  id: string;
  keys: string;
  matches(e: KeyboardEvent): boolean;
}

// 分发目标动作集合：App.tsx 注入 store/局部 state 操作，测试用桩替换。
export interface GlobalKeyActions {
  switchToTab(n: number): void;
  nextTab(): void;
  prevTab(): void;
  closeActiveTab(): void;
  goHome(): void;
  toggleSwitcher(): void;
  focusSearch(): void;
  toggleHelp(): void;
}

export const SHORTCUTS: ShortcutDef[] = [
  {
    id: 'quick-switcher',
    keys: 'Ctrl+K',
    matches: (e) => e.ctrlKey && !e.shiftKey && !e.altKey && (e.key === 'k' || e.key === 'K'),
  },
  {
    id: 'focus-search',
    keys: 'Ctrl+F',
    matches: (e) => e.ctrlKey && !e.shiftKey && !e.altKey && (e.key === 'f' || e.key === 'F'),
  },
  {
    // Ctrl+1..9 切到第 N 个已开 tab（N=10 表示 Ctrl+0 时不会走到这里，见 go-home）
    id: 'switch-tab-n',
    keys: 'Ctrl+1..9',
    matches: (e) => {
      if (!e.ctrlKey || e.shiftKey || e.altKey) return false;
      const m = /^Digit([1-9])$/.exec(e.code);
      return m !== null;
    },
  },
  { id: 'next-tab', keys: 'Ctrl+Tab', matches: (e) => e.ctrlKey && !e.shiftKey && e.key === 'Tab' },
  {
    id: 'prev-tab',
    keys: 'Ctrl+Shift+Tab',
    matches: (e) => e.ctrlKey && e.shiftKey && e.key === 'Tab',
  },
  {
    id: 'close-tab',
    keys: 'Ctrl+W',
    matches: (e) => e.ctrlKey && !e.shiftKey && !e.altKey && (e.key === 'w' || e.key === 'W'),
  },
  {
    // Ctrl+0 与 Ctrl+T 都映射回首页（首页=新建会话入口）
    id: 'go-home',
    keys: 'Ctrl+0 / Ctrl+T',
    matches: (e) => {
      if (!e.ctrlKey || e.shiftKey || e.altKey) return false;
      return e.code === 'Digit0' || e.key === 't' || e.key === 'T';
    },
  },
  { id: 'toggle-help', keys: 'F1', matches: (e) => e.key === 'F1' },
];

// xterm 聚焦时仍生效的快捷键：切 tab 类（不写 pty、无双效应）
function isTabSwitchShortcut(id: string): boolean {
  return (
    id === 'switch-tab-n' || id === 'next-tab' || id === 'prev-tab' || id === 'go-home'
  );
}

function dispatch(id: string, e: KeyboardEvent, actions: GlobalKeyActions): boolean {
  e.preventDefault();
  switch (id) {
    case 'switch-tab-n':
      actions.switchToTab(Number(/^Digit([1-9])$/.exec(e.code)![1]));
      return true;
    case 'next-tab':
      actions.nextTab();
      return true;
    case 'prev-tab':
      actions.prevTab();
      return true;
    case 'close-tab':
      actions.closeActiveTab();
      return true;
    case 'go-home':
      actions.goHome();
      return true;
    case 'quick-switcher':
      actions.toggleSwitcher();
      return true;
    case 'focus-search':
      actions.focusSearch();
      return true;
    case 'toggle-help':
      actions.toggleHelp();
      return true;
    default:
      return false;
  }
}

// 全局 keydown 分发入口：返回是否消费了该按键（true = 已 preventDefault 并触发动作）。
export function handleGlobalKey(e: KeyboardEvent, actions: GlobalKeyActions): boolean {
  // 组件已处理的按键（CodeMirror 查找面板等）退让
  if (e.defaultPrevented) return false;

  const el = document.activeElement as HTMLElement | null;
  const inXterm = !!(el && el.closest && el.closest('.xterm'));

  for (const s of SHORTCUTS) {
    if (!s.matches(e)) continue;
    // 终端聚焦分级退让：xterm 已把按键写入 pty，再触发就是一次按键双效应；
    // 仅切 tab 类保留可用（不写 pty）
    if (inXterm && !isTabSwitchShortcut(s.id)) return false;
    return dispatch(s.id, e, actions);
  }
  return false;
}
