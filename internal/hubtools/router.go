package hubtools

import (
	"encoding/json"
	"strings"
)

// RouteAgentJSON 是 auto 模式下模型表示「与本地数据无关」的固定输出：
// 桌面层据此把原文转投 ACP agent 原链路。
const RouteAgentJSON = `{"route":"agent"}`

// BuildSystemPrompt 构造工具化链路的系统提示词。routeAware=true（auto 模式）
// 时附带意图路由规则；false（force_auto）为纯工具人设，跳过路由判断。
func BuildSystemPrompt(tools []ToolDef, routeAware bool) string {
	var b strings.Builder
	b.WriteString("你是 kshell 桌面助手的工具化执行器，通过调用工具操作本机数据（需求、工作区、运行中任务）。回复用简洁中文。\n\n可用工具：\n")
	for _, t := range tools {
		b.WriteString("- " + t.Name + "：" + t.Description + "\n")
	}
	if routeAware {
		b.WriteString("\n路由规则：仅当用户输入与上述本地数据操作有关时才调用工具；" +
			"若与本地数据无关（写代码、技术问答等应交给编程 agent 的内容），" +
			"只输出一行 JSON：" + RouteAgentJSON + "，不要输出任何其他内容。\n")
	}
	return b.String()
}

// ParseRouteAgent 判断模型回复是否为 route=agent 路由指令。JSON 可能混在
// 其他文本中：从首个 { 起用流式解码取首个 JSON 值解析。
func ParseRouteAgent(text string) bool {
	i := strings.Index(text, "{")
	if i < 0 {
		return false
	}
	var v struct {
		Route string `json:"route"`
	}
	if err := json.NewDecoder(strings.NewReader(text[i:])).Decode(&v); err != nil {
		return false
	}
	return v.Route == "agent"
}
