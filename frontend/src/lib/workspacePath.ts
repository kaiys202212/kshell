// 工作区路径归一化与比较。
// 必须集中一处的原因：同一个目录在不同来源里的写法并不一致——
//   Claude/Codex 的会话文件里是 `D:\data\ws`，而 opencode 的 SQLite 里存的是 `D:/data/ws`；
// 大小写也可能漂移（盘符）。只按原样比较就会把同一工作区的会话过滤掉
// （表现为「会话列表里看不到 opencode 的会话」）。
export function normalizeWorkspacePath(p: string): string {
  if (!p) return '';
  // 分隔符统一成 '/'（保留盘符后的冒号），去掉多余尾分隔符，再统一小写
  const slashed = p.replace(/\\/g, '/').replace(/\/+$/, '');
  return slashed.toLowerCase();
}

// 两个路径是否指向同一个工作区。
export function sameWorkspacePath(a: string, b: string): boolean {
  return normalizeWorkspacePath(a) === normalizeWorkspacePath(b);
}
