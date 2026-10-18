// agenthubDefaults：新建 agent 弹窗的默认值持久化。
// 记住上次创建的项目/工具/模型与各工具的模型列表（下次打开直接预填），
// 全部存 localStorage；隐私模式或损坏 JSON 一律静默降级（null / 空数组）。
const LAST_CREATE_KEY = 'kshell.agenthub.lastCreate';
const DEFAULT_CREATE_KEY = 'kshell.agenthub.defaultCreate';
const MODELS_KEY_PREFIX = 'kshell.agenthub.models.';
const ROUTE_MODE_KEY = 'kshell.agenthub.routeMode';

// Hub 路由模式：auto=自动意图路由（默认）、agent=全部走 ACP agent、
// force_auto=跳过意图判断直接进工具循环。
export type HubRouteMode = 'auto' | 'agent' | 'force_auto';

export interface LastAgentCreate {
  wsPath: string;
  toolId: string;
  modelId: string | null;
}

// DefaultAgentCreate：用户显式设置的「默认 agent 工具 + 模型」，
// 供快速新建（easynew）与需求派发使用；modelId 为 null 即用该工具默认模型。
export interface DefaultAgentCreate {
  toolId: string;
  modelId: string | null;
}

function readJSON<T>(key: string): T | null {
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return null;
    return JSON.parse(raw) as T;
  } catch {
    return null;
  }
}

function writeJSON(key: string, value: unknown): void {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // 写失败（隐私模式/配额）静默：默认值是辅助功能
  }
}

// loadLastCreate 读取上次创建记录；记录缺失、损坏或结构不完整时返回 null。
export function loadLastCreate(): LastAgentCreate | null {
  const v = readJSON<Partial<LastAgentCreate>>(LAST_CREATE_KEY);
  if (!v || typeof v.wsPath !== 'string' || typeof v.toolId !== 'string') return null;
  return { wsPath: v.wsPath, toolId: v.toolId, modelId: typeof v.modelId === 'string' ? v.modelId : null };
}

// saveLastCreate 保存上次创建记录。
export function saveLastCreate(v: LastAgentCreate): void {
  writeJSON(LAST_CREATE_KEY, v);
}

// loadDefaultCreate 读取用户显式设置的默认 agent 配置；缺失、损坏或结构不完整时返回 null。
export function loadDefaultCreate(): DefaultAgentCreate | null {
  const v = readJSON<Partial<DefaultAgentCreate>>(DEFAULT_CREATE_KEY);
  if (!v || typeof v.toolId !== 'string') return null;
  return { toolId: v.toolId, modelId: typeof v.modelId === 'string' ? v.modelId : null };
}

// saveDefaultCreate 保存用户显式设置的默认 agent 配置。
export function saveDefaultCreate(v: DefaultAgentCreate): void {
  writeJSON(DEFAULT_CREATE_KEY, v);
}

// loadToolModels 读取某工具缓存的模型 ID 列表；损坏或非数组返回空数组，非字符串项过滤。
export function loadToolModels(toolId: string): string[] {
  const v = readJSON<unknown>(MODELS_KEY_PREFIX + toolId);
  if (!Array.isArray(v)) return [];
  return v.filter((x): x is string => typeof x === 'string');
}

// saveToolModels 缓存某工具的模型 ID 列表（成功拉到 listChatModels 后调用）。
export function saveToolModels(toolId: string, modelIds: string[]): void {
  writeJSON(MODELS_KEY_PREFIX + toolId, modelIds);
}

// loadHubRouteMode 读取路由模式；缺失/非法回落 auto（与后端 Normalize 同语义）。
export function loadHubRouteMode(): HubRouteMode {
  const v = localStorage.getItem(ROUTE_MODE_KEY);
  if (v === 'agent' || v === 'force_auto') return v;
  return 'auto';
}

// saveHubRouteMode 保存路由模式到 localStorage（跨端副本由调用方写穿后端）。
export function saveHubRouteMode(mode: HubRouteMode): void {
  try {
    localStorage.setItem(ROUTE_MODE_KEY, mode);
  } catch {
    // 写失败（隐私模式）静默：模式是辅助功能
  }
}
