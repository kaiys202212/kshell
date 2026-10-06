// git 状态刷新的共用封装：FileTree（挂载/重命名）与 Preview（保存）共用，
// 保证两条入口对 gitStatus 镜像的更新行为一致。
import { gitStatus } from './api';
import { useAppStore } from '../state/store';

const DIRTY = new Set(['modified', 'added', 'deleted', 'renamed', 'untracked', 'conflicted']);
const EXPLICIT = new Set(['modified', 'added', 'deleted', 'renamed', 'conflicted']);

// 子树是否有未提交改动（忽略 ignored）。relPath 为空表示工作区虚拟根。
export function subtreeDirty(gitMap: Record<string, string> | undefined, relPath: string): boolean {
  if (!gitMap) return false;
  const prefix = relPath === '' ? '' : `${relPath}/`;
  for (const [p, code] of Object.entries(gitMap)) {
    if (!DIRTY.has(code)) continue;
    if (relPath === '') return true;
    if (p === relPath || p.startsWith(prefix)) return true;
  }
  return false;
}

function ancestorIgnored(gitMap: Record<string, string>, relPath: string): boolean {
  if (!relPath) return false;
  const parts = relPath.split('/');
  for (let i = 1; i < parts.length; i++) {
    const anc = parts.slice(0, i).join('/');
    if (gitMap[anc] === 'ignored') return true;
  }
  return false;
}

function childrenAllIgnored(gitMap: Record<string, string>, relPath: string): boolean {
  const prefix = `${relPath}/`;
  let any = false;
  for (const [p, code] of Object.entries(gitMap)) {
    if (p === relPath || !p.startsWith(prefix)) continue;
    any = true;
    if (code !== 'ignored') return false;
  }
  return any;
}

// 解析文件树行应展示的 git 码：祖先继承 ignored；目录可在子全 ignored 时推断（含覆盖 untracked）。
export function resolveGitCode(
  gitMap: Record<string, string> | undefined,
  relPath: string,
  isDir: boolean,
): string | undefined {
  if (!gitMap || relPath === '') return undefined;
  if (ancestorIgnored(gitMap, relPath)) return 'ignored';
  const own = gitMap[relPath];
  if (own && EXPLICIT.has(own)) return own;
  if (isDir && childrenAllIgnored(gitMap, relPath) && (!own || own === 'untracked' || own === 'ignored')) {
    return 'ignored';
  }
  return own;
}

// 目录是否为「嵌套 git 根」的严格路径前缀（中间层）；嵌套根本身与虚拟根不算。
export function isNestedGitParent(
  dirBranches: Record<string, string> | undefined,
  relPath: string,
): boolean {
  if (!dirBranches || !relPath) return false;
  if (Object.prototype.hasOwnProperty.call(dirBranches, relPath) && relPath !== '') {
    return false; // 自身是嵌套根
  }
  const needle = `${relPath}/`;
  for (const key of Object.keys(dirBranches)) {
    if (!key) continue;
    if (key.startsWith(needle)) return true;
  }
  return false;
}

// 拉取并写入 store；失败静默（git 不在 PATH、超时等都不该打断文件浏览），
// 非仓库时写入空映射（与「未加载」区分：前端按 relPath 查不到即不渲染标记）。
export async function refreshGitStatus(wsPath: string): Promise<void> {
  try {
    const res = await gitStatus(wsPath);
    useAppStore
      .getState()
      .setGitStatus(wsPath, res?.Status ?? {}, res?.Branch ?? '', res?.DirBranches ?? {});
  } catch {
    // 静默：状态标记属于锦上添花，失败不影响主流程
  }
}
