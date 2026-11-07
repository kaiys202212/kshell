// 文件树定位注册表：FileTree 挂载时登记 reveal 能力（按绝对路径逐层展开+选中+滚动），
// 页签联动与聊天文件夹附件点击经 revealInTree 投递（与 chatInputRegistry 同思路）。
interface TreeRevealHandle {
  reveal(absPath: string): void;
}

const registry = new Map<string, TreeRevealHandle>();

// registerTreeRevealer 登记一个文件树；同 wsPath 重复登记时后者覆盖前者。
export function registerTreeRevealer(wsPath: string, handle: TreeRevealHandle): void {
  registry.set(wsPath, handle);
}

// unregisterTreeRevealer 注销文件树（组件卸载时调用）；未知 wsPath 静默忽略。
export function unregisterTreeRevealer(wsPath: string): void {
  registry.delete(wsPath);
}

// revealInTree 请求定位到 absPath；返回 false 表示该工作区没有登记文件树
//（页签未打开或非树视图宿主），调用方静默忽略。
export function revealInTree(wsPath: string, absPath: string): boolean {
  const h = registry.get(wsPath);
  if (!h) return false;
  h.reveal(absPath);
  return true;
}
