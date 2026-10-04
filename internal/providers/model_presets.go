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
func ModelPresets() []ModelPreset {
	return []ModelPreset{
		{ID: "custom", Name: "自定义", Note: "不覆盖当前已填 URL"},
		{
			ID: "openai", Name: "OpenAI",
			OpenAIBaseURL: "https://api.openai.com/v1",
			RecommendedModel: "gpt-5",
			Note:             "仅 OpenAI 协议",
		},
		{
			ID: "deepseek", Name: "DeepSeek",
			OpenAIBaseURL:    "https://api.deepseek.com",
			AnthropicBaseURL: "https://api.deepseek.com/anthropic",
			RecommendedModel: "deepseek-chat",
		},
		{
			ID: "minimax", Name: "MiniMax 官方",
			OpenAIBaseURL:    "https://api.minimax.io/v1",
			AnthropicBaseURL: "https://api.minimax.io/anthropic",
			RecommendedModel: "MiniMax-M3",
		},
		{
			ID: "minimax-token-plan", Name: "MiniMax Token Plan",
			OpenAIBaseURL:    "https://api.minimax.io/v1",
			AnthropicBaseURL: "https://api.minimax.io/anthropic",
			RecommendedModel: "MiniMax-M3",
			Note:             "订阅 Key",
		},
		{
			ID: "qwen", Name: "Qwen DashScope 官方",
			OpenAIBaseURL:    "https://dashscope.aliyuncs.com/compatible-mode/v1",
			RecommendedModel: "qwen3-coder-plus",
			Note:             "仅 OpenAI 兼容",
		},
		{
			ID: "qwen-coding-cn", Name: "Qwen Coding Plan（国内）",
			OpenAIBaseURL:    "https://coding.dashscope.aliyuncs.com/v1",
			RecommendedModel: "qwen3-coder-plus",
		},
		{
			ID: "qwen-coding-intl", Name: "Qwen Coding Plan（国际）",
			OpenAIBaseURL:    "https://coding-intl.dashscope.aliyuncs.com/v1",
			RecommendedModel: "qwen3-coder-plus",
		},
		{
			ID: "kimi-coding", Name: "Kimi Coding",
			OpenAIBaseURL:    "https://api.kimi.com/coding/v1",
			AnthropicBaseURL: "https://api.kimi.com/coding/",
			RecommendedModel: "kimi-for-coding",
		},
		{
			ID: "moonshot", Name: "Moonshot（Kimi 平台）",
			OpenAIBaseURL:    "https://api.moonshot.cn/v1",
			RecommendedModel: "kimi-k2.5",
			Note:             "仅 OpenAI 协议",
		},
		{
			ID: "glm-coding", Name: "智谱 GLM Coding Plan",
			OpenAIBaseURL:    "https://open.bigmodel.cn/api/coding/paas/v4",
			AnthropicBaseURL: "https://open.bigmodel.cn/api/anthropic",
			RecommendedModel: "glm-4.7",
		},
		{
			ID: "mimo-token-plan", Name: "小米 MiMo Token Plan",
			OpenAIBaseURL:    "https://token-plan-cn.xiaomimimo.com/v1",
			AnthropicBaseURL: "https://token-plan-cn.xiaomimimo.com/anthropic",
			RecommendedModel: "mimo-v2.5",
		},
		{
			ID: "openrouter", Name: "OpenRouter",
			OpenAIBaseURL:    "https://openrouter.ai/api/v1",
			RecommendedModel: "anthropic/claude-sonnet-4",
			Note:             "仅 OpenAI 协议",
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
