// git 状态刷新的共用封装：FileTree（挂载/重命名）与 Preview（保存）共用，
// 保证两条入口对 gitStatus 镜像的更新行为一致。
import { gitStatus } from './api';
import { useAppStore } from '../state/store';

// 拉取并写入 store；失败静默（git 不在 PATH、超时等都不该打断文件浏览），
// 非仓库时写入空映射（与「未加载」区分：前端按 relPath 查不到即不渲染标记）。
export async function refreshGitStatus(wsPath: string): Promise<void> {
  try {
    const res = await gitStatus(wsPath);
    useAppStore.getState().setGitStatus(wsPath, res?.Status ?? {});
  } catch {
    // 静默：状态标记属于锦上添花，失败不影响主流程
  }
}
