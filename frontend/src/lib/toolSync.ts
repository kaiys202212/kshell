// 设置页「模型设置」支持的工具集合：与后端 settings.go 的 modelSyncTargets /
// platformOnlyTargets 对齐，避免入口显隐与弹窗分支各维护一份。
export const MODEL_SYNC_TARGETS = new Set(['opencode', 'codebuddy', 'claude', 'codex', 'gemini']);

// PLATFORM_ONLY_TARGETS 是没有 agentsync 静态注入、只保存每工具平台的目标：
// 弹窗不显示「启用同步」，平台选择仅供启动端点解析。
export const PLATFORM_ONLY_TARGETS = new Set(['codex', 'gemini']);
