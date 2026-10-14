// workspaceName 取工作区路径的展示名（最后一段），供通知等紧凑场景使用。
// 兼容反斜杠与正斜杠；末尾分隔符忽略；根路径（如 D:\）返回去分隔符后的原串。

export function workspaceName(p: string): string {
  return p.replace(/[\\/]+$/, '').split(/[\\/]/).pop() ?? '';
}
