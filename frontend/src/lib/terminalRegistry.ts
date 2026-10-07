// 终端注册表：把 Go 侧推来的 terminal:data 事件（base64）分发到当前挂载的 xterm 实例。
// 为什么用模块级 Map 而不是 React store：PTY 输出频率很高，进 store 会引发无谓重渲染；
// 而且页签被隐藏/卸载时数据本就没有接收方——Go 侧每会话保留最近 256 KiB 环形缓冲，
// 重挂载时由上层决定是否回放，所以这里直接丢弃未命中的输出是安全的。
import { base64ToBytes } from './base64';

// TerminalHandle 是 TerminalView 暴露给注册表的最小接口（组件的 xterm 细节不外泄）
export interface TerminalHandle {
  // write 接收已解码的字节，实现方直接交给 xterm.write
  write(bytes: Uint8Array): void;
  focus(): void;
  // fit 重新计算尺寸并同步 Go 侧（resize 上报由实现方内部处理）
  fit(): void;
}

const handles = new Map<string, TerminalHandle>();

// registerTerminal 登记一个已挂载的终端。同 id 重复登记时后者覆盖前者：
// 覆盖必须告警，否则 StrictMode 双挂载、页签重建这类问题会被静默吞掉。
export function registerTerminal(id: string, h: TerminalHandle): void {
  if (!id) {
    console.warn('terminal register failed: empty terminal id');
    return;
  }
  if (handles.has(id)) {
    console.warn(`terminal re-registered; previous instance overwritten: ${id}`);
  }
  handles.set(id, h);
}

// unregisterTerminal 注销终端（组件卸载时调用）；未知 id 静默忽略。
export function unregisterTerminal(id: string): void {
  handles.delete(id);
}

// dispatchTerminalData 把 base64 输出投递给已登记的终端。
// 未登记（页签已卸载）或 base64 解码失败时返回 false 并丢弃数据。
export function dispatchTerminalData(id: string, data: string): boolean {
  if (!id) return false;
  const h = handles.get(id);
  if (!h) return false;
  let bytes: Uint8Array;
  try {
    bytes = base64ToBytes(data);
  } catch {
    console.warn(`terminal output decode failed; dropped: ${id}`);
    return false;
  }
  h.write(bytes);
  return true;
}

// focusTerminal 把焦点交给指定终端，返回是否命中（未登记返回 false）
export function focusTerminal(id: string): boolean {
  const h = handles.get(id);
  if (!h) return false;
  h.focus();
  return true;
}

// fitTerminal 让指定终端重新适配容器尺寸，返回是否命中（未登记返回 false）
export function fitTerminal(id: string): boolean {
  const h = handles.get(id);
  if (!h) return false;
  h.fit();
  return true;
}

// clearTerminalRegistry 清空注册表（仅供测试使用）
export function clearTerminalRegistry(): void {
  handles.clear();
}
