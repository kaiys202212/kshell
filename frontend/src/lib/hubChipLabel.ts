// AgentHub 会话胶囊文案：始终带上项目基名前缀（前 2 字）便于跨项目区分。
import { baseName } from './hubAttribution';

const PROJECT_PREFIX_LEN = 2;

// projectPrefix 取 workspace 路径末段的前 n 个字（按 Unicode 码点）。
export function projectPrefix(workspace: string, n = PROJECT_PREFIX_LEN): string {
  const name = workspace ? baseName(workspace) : '';
  if (!name) return '';
  return [...name].slice(0, n).join('');
}

// chipSessionLabel 胶囊主文案：有标题时「前缀 · 标题」；无标题回落完整项目基名。
export function chipSessionLabel(title: string, workspace: string, empty = ''): string {
  const t = title.trim();
  const prefix = projectPrefix(workspace);
  if (t && prefix) return `${prefix} · ${t}`;
  if (t) return t;
  const project = workspace ? baseName(workspace) : '';
  return project || empty;
}
