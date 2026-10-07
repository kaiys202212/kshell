package providers

// ModelPreset 是设置页可选的模型提供商基础配置（双协议 URL + 推荐模型名）。
type ModelPreset struct {
	ID               string `json:"ID"`
	Name             string `json:"Name"`
	OpenAIBaseURL    string `json:"OpenAIBaseURL"`
	AnthropicBaseURL string `json:"AnthropicBaseURL"`
	RecommendedModel string `json:"RecommendedModel"`
	Note             string `json:"Note"`
}

// ModelPresets 内置常见提供商/Coding Plan 端点（数据来自各厂商公开文档，可手改）。
// Name/Note 为 wire key（`preset.<id>.name` / `preset.<id>.note`），由前端 translateBackend 翻；
// 逻辑（选择/匹配）一律按稳定的 ID，勿依赖展示文案。
func ModelPresets() []ModelPreset {
	return []ModelPreset{
		{ID: "custom", Name: "preset.custom.name", Note: "preset.custom.note"},
		{
			ID: "openai", Name: "preset.openai.name",
			OpenAIBaseURL: "https://api.openai.com/v1",
			RecommendedModel: "gpt-5",
			Note:             "preset.openai.note",
		},
		{
			ID: "deepseek", Name: "preset.deepseek.name",
			OpenAIBaseURL:    "https://api.deepseek.com",
			AnthropicBaseURL: "https://api.deepseek.com/anthropic",
			RecommendedModel: "deepseek-chat",
		},
		{
			ID: "minimax", Name: "preset.minimax.name",
			OpenAIBaseURL:    "https://api.minimax.io/v1",
			AnthropicBaseURL: "https://api.minimax.io/anthropic",
			RecommendedModel: "MiniMax-M3",
		},
		{
			ID: "minimax-token-plan", Name: "preset.minimax-token-plan.name",
			OpenAIBaseURL:    "https://api.minimax.io/v1",
			AnthropicBaseURL: "https://api.minimax.io/anthropic",
			RecommendedModel: "MiniMax-M3",
			Note:             "preset.minimax-token-plan.note",
		},
		{
			ID: "qwen", Name: "preset.qwen.name",
			OpenAIBaseURL:    "https://dashscope.aliyuncs.com/compatible-mode/v1",
			RecommendedModel: "qwen3-coder-plus",
			Note:             "preset.qwen.note",
		},
		{
			ID: "qwen-coding-cn", Name: "preset.qwen-coding-cn.name",
			OpenAIBaseURL:    "https://coding.dashscope.aliyuncs.com/v1",
			RecommendedModel: "qwen3-coder-plus",
		},
		{
			ID: "qwen-coding-intl", Name: "preset.qwen-coding-intl.name",
			OpenAIBaseURL:    "https://coding-intl.dashscope.aliyuncs.com/v1",
			RecommendedModel: "qwen3-coder-plus",
		},
		{
			ID: "kimi-coding", Name: "preset.kimi-coding.name",
			OpenAIBaseURL:    "https://api.kimi.com/coding/v1",
			AnthropicBaseURL: "https://api.kimi.com/coding/",
			RecommendedModel: "kimi-for-coding",
		},
		{
			ID: "moonshot", Name: "preset.moonshot.name",
			OpenAIBaseURL:    "https://api.moonshot.cn/v1",
			RecommendedModel: "kimi-k2.5",
			Note:             "preset.moonshot.note",
		},
		{
			ID: "glm-coding", Name: "preset.glm-coding.name",
			OpenAIBaseURL:    "https://open.bigmodel.cn/api/coding/paas/v4",
			AnthropicBaseURL: "https://open.bigmodel.cn/api/anthropic",
			RecommendedModel: "glm-4.7",
		},
		{
			ID: "mimo-token-plan", Name: "preset.mimo-token-plan.name",
			OpenAIBaseURL:    "https://token-plan-cn.xiaomimimo.com/v1",
			AnthropicBaseURL: "https://token-plan-cn.xiaomimimo.com/anthropic",
			RecommendedModel: "mimo-v2.5",
		},
		{
			ID: "openrouter", Name: "preset.openrouter.name",
			OpenAIBaseURL:    "https://openrouter.ai/api/v1",
			RecommendedModel: "anthropic/claude-sonnet-4",
			Note:             "preset.openrouter.note",
		},
	}
}

// FindModelPreset 按 id 查找预设；未找到返回 false。
func FindModelPreset(id string) (ModelPreset, bool) {
	for _, p := range ModelPresets() {
		if p.ID == id {
			return p, true
		}
	}
	return ModelPreset{}, false
}
