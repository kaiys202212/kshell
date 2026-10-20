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

// rememberLastModel 把一次成功的模型切换记为该工具的「上次模型」：
// lastCreate 同工具时原地更新 modelId（保留 wsPath），否则以传入 wsPath/toolId
// 新建记录。toolId/modelId 缺失直接忽略——无法定位工具，或语义是回落默认模型
// （切回默认不应把「默认」当成一个被记住的模型）。失败静默（辅助功能）。
export function rememberLastModel(toolId: string, wsPath: string, modelId: string): void {
  if (!toolId || !modelId) return;
  const last = loadLastCreate();
  if (last && last.toolId === toolId) {
    saveLastCreate({ ...last, modelId });
    return;
  }
  saveLastCreate({ wsPath, toolId, modelId });
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

// ToolModelOption：模型下拉一项；id 用于 SetModel，name 用于展示（Cursor Auto 等）。
export interface ToolModelOption {
  id: string;
  name: string;
}

function normalizeToolModelOption(x: unknown): ToolModelOption | null {
  if (typeof x === 'string') {
    return x ? { id: x, name: x } : null;
  }
  if (!x || typeof x !== 'object') return null;
  const id = (x as { id?: unknown }).id;
  if (typeof id !== 'string' || !id) return null;
  const name = (x as { name?: unknown }).name;
  return { id, name: typeof name === 'string' && name ? name : id };
}

// loadToolModelOptions 读取某工具缓存的模型选项（id+展示名）。
// 兼容旧版 string[]（升格为 name=id）；损坏或非数组返回空数组。
export function loadToolModelOptions(toolId: string): ToolModelOption[] {
  const v = readJSON<unknown>(MODELS_KEY_PREFIX + toolId);
  if (!Array.isArray(v)) return [];
  const out: ToolModelOption[] = [];
  for (const x of v) {
    const opt = normalizeToolModelOption(x);
    if (opt) out.push(opt);
  }
  return out;
}

// loadToolModels 读取某工具缓存的模型 ID 列表（兼容旧调用方）。
export function loadToolModels(toolId: string): string[] {
  return loadToolModelOptions(toolId).map((m) => m.id);
}

// saveToolModels 缓存某工具的模型列表（成功拉到 listChatModels 后调用）。
// 接受 string（旧）或 {id,name}；一律落盘为对象形态。
export function saveToolModels(toolId: string, models: Array<string | ToolModelOption>): void {
  const opts: ToolModelOption[] = [];
  for (const m of models) {
    const opt = normalizeToolModelOption(m);
    if (opt) opts.push(opt);
  }
  writeJSON(MODELS_KEY_PREFIX + toolId, opts);
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
