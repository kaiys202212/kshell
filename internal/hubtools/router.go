package hubtools

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RouteAgentJSON 是 auto 模式下模型表示「与本地数据无关」的固定输出：
// 桌面层据此把原文转投 ACP agent 原链路。
const RouteAgentJSON = `{"route":"agent"}`

// BuildSystemPrompt 构造工具化链路的系统提示词。routeAware=true（auto 模式）
// 时附带意图路由规则；false（force_auto）为纯工具人设，跳过路由判断。
// recent 是最近的聊天记录摘要（可为空）：auto 模式据此渲染编号列表与派发
// 规则（模型可把明确任务派发到记录中的会话）；force_auto 仅注入记录本身。
func BuildSystemPrompt(tools []ToolDef, routeAware bool, recent []ChatLogEntry) string {
	var b strings.Builder
	b.WriteString("你是 Capybara 桌面助手的工具化执行器，通过调用工具操作本机数据（需求、工作区、运行中任务）。回复用简洁中文。\n\n可用工具：\n")
	for _, t := range tools {
		b.WriteString("- " + t.Name + "：" + t.Description + "\n")
	}
	if len(recent) > 0 {
		b.WriteString("\n近期聊天记录（旧→新，编号 [n] 供 dispatch_task 的 ref 参数引用）：\n")
		for i, it := range recent {
			// 折叠换行保持一行一条，编号行结构不被多行消息破坏。
			text := strings.ReplaceAll(it.Text, "\n", " ")
			b.WriteString(fmt.Sprintf("[%d] 会话=%s 方向=%s 内容=%s\n", i+1, it.Agent, it.Sender, text))
		}
	}
	if routeAware {
		b.WriteString("\n路由规则：\n" +
			"1. 用户意图与本地数据操作有关时，调用对应工具；\n" +
			"2. 意图明确该由某个 agent 会话执行的任务（如让某工具修代码、继续某个会话），调用 dispatch_task 派发，ref 从上方聊天记录编号中选择；记录里没有合适会话、或内容就是要交给编程 agent 的，只输出一行 JSON：" + RouteAgentJSON + "，不要输出任何其他内容；\n" +
			"3. 意图不明确时不要猜测性调用工具或派发：用自然语言给出你对意图的理解与建议，请用户确认；用户在后续输入中确认后再执行。\n" +
			"除路由转投外不要输出 JSON。\n")
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
