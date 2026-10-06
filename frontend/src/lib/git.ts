// git 状态刷新的共用封装：FileTree（挂载/重命名）与 Preview（保存）共用，
// 保证两条入口对 gitStatus 镜像的更新行为一致。
import { gitStatus } from './api';
import { useAppStore } from '../state/store';

const DIRTY = new Set(['modified', 'added', 'deleted', 'renamed', 'untracked', 'conflicted']);

// 子树是否有未提交改动（忽略 ignored）。relPath 为空表示工作区虚拟根。
// 嵌套 git 仓库的改动只算在该仓库根及其内部，不冒泡到外层虚拟根。
export function subtreeDirty(
  gitMap: Record<string, string> | undefined,
  relPath: string,
  dirBranches?: Record<string, string>,
): boolean {
  if (!gitMap) return false;
  const prefix = relPath === '' ? '' : `${relPath}/`;
  const queryRoot = nestedGitRootOf(dirBranches, relPath);
  for (const [p, code] of Object.entries(gitMap)) {
    if (!DIRTY.has(code)) continue;
    const owner = nestedGitRootOf(dirBranches, p);
    if (owner && owner !== queryRoot) continue;
    if (relPath === '') return true;
    if (p === relPath || p.startsWith(prefix)) return true;
  }
  return false;
}

// 覆盖 relPath 的最长嵌套 git 根（DirBranches 非空键）；虚拟根 '' 不计。
function nestedGitRootOf(
  dirBranches: Record<string, string> | undefined,
  relPath: string,
): string | undefined {
  if (!dirBranches || !relPath) return undefined;
  let best: string | undefined;
  for (const key of Object.keys(dirBranches)) {
    if (!key) continue;
    if (relPath === key || relPath.startsWith(`${key}/`)) {
      if (!best || key.length > best.length) best = key;
    }
  }
  return best;
}

// 仅当祖先在 porcelain 中显式为 ignored（!!）时才继承；不因「子项全是 ignored」推断父目录。
// git status 不含干净已跟踪文件，那种推断会把 website 这类目录误标成 I。
// fromExclusive 为嵌套 git 根时，不把该根及其更外层的 ignored 算作祖先。
function ancestorIgnored(
  gitMap: Record<string, string>,
  relPath: string,
  fromExclusive?: string,
): boolean {
  if (!relPath) return false;
  const parts = relPath.split('/');
  const minLen = fromExclusive ? fromExclusive.split('/').length : 0;
  for (let i = 1; i < parts.length; i++) {
    if (i <= minLen) continue;
    if (gitMap[parts.slice(0, i).join('/')] === 'ignored') return true;
  }
  return false;
}

// 解析文件树行应展示的 git 码：与 porcelain 对齐，祖先仅在显式 ignored 时继承。
// 嵌套 git 根及其内部不受外层忽略目录影响。
export function resolveGitCode(
  gitMap: Record<string, string> | undefined,
  relPath: string,
  _isDir: boolean,
  dirBranches?: Record<string, string>,
): string | undefined {
  if (!gitMap || relPath === '') return undefined;
  const nestedRoot = nestedGitRootOf(dirBranches, relPath);
  if (nestedRoot && relPath === nestedRoot) return undefined;
  if (ancestorIgnored(gitMap, relPath, nestedRoot)) return 'ignored';
  return gitMap[relPath];
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
