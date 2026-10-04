// 聊天输入框注册表：ChatView 挂载时登记 append 回调，
// 文件拖入聊天页签时经 appendChatInput 投递文本（与 terminalRegistry 同思路）。
interface ChatInputHandle {
  append(text: string): void;
}

const registry = new Map<string, ChatInputHandle>();

// registerChatInput 登记一个聊天输入框；同 id 重复登记时后者覆盖前者。
export function registerChatInput(id: string, handle: ChatInputHandle): void {
  registry.set(id, handle);
}

// unregisterChatInput 注销聊天输入框（组件卸载时调用）；未知 id 静默忽略。
export function unregisterChatInput(id: string): void {
  registry.delete(id);
}

// appendChatInput 把文本投递给已登记的聊天输入框；
// 返回 false 表示该聊天没有注册（已关闭或非聊天页签）。
export function appendChatInput(id: string, text: string): boolean {
  const h = registry.get(id);
  if (!h) return false;
  h.append(text);
  return true;
}
