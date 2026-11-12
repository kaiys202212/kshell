package workbuddyproxy

import (
	"encoding/json"
	"fmt"
	"strings"
)

// openAICompletion 是 ChatOnce 聚合后的非流式 OpenAI 响应（仅代理关心的字段）。
type openAICompletion struct {
	ID      string `json:"id"`
	Choices []struct {
		Message struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// openAIToAnthropic 把 OpenAI 响应翻译为 Anthropic Messages 响应。
// 纯函数；反序列化失败时返回合法空消息而不是 error，避免代理层 500 掩盖
// 上游异常（调用方仍能看到 end_turn + 空 content）。
func openAIToAnthropic(resp []byte, model string) map[string]any {
	var in openAICompletion
	_ = json.Unmarshal(resp, &in)

	id := in.ID
	if id == "" {
		// 兜底仅防上游缺 id；真实路径透传 ChatOnce 生成的 workbuddy-<id>。
		id = newMessageID()
	}

	content := make([]any, 0, 2)
	finishReason := ""
	if len(in.Choices) > 0 {
		ch := in.Choices[0]
		if ch.Message.Content != "" {
			content = append(content, map[string]any{"type": "text", "text": ch.Message.Content})
		}
		for _, tc := range ch.Message.ToolCalls {
			input := map[string]any{}
			if args := strings.TrimSpace(tc.Function.Arguments); args != "" {
				// 网关偶发截断参数或直接给 "null"：Anthropic 的 input 必须是对象，
				// 反序列化失败或结果为 nil 时都降级为空对象。
				var parsed map[string]any
				if err := json.Unmarshal([]byte(args), &parsed); err != nil || parsed == nil {
					parsed = map[string]any{}
				}
				input = parsed
			}
			callID := tc.ID
			if callID == "" {
				// 上游缺 tool_call id 时本地补充，避免 Anthropic 侧 tool_use id 为空。
				callID = newToolUseID()
			}
			content = append(content, map[string]any{
				"type":  "tool_use",
				"id":    callID,
				"name":  tc.Function.Name,
				"input": input,
			})
		}
		finishReason = ch.FinishReason
	}

	return map[string]any{
		"id":            id,
		"type":          "message",
		"role":          "assistant",
		"model":         model, // 用请求模型回填：ChatOnce 固定返回 "workbuddy"
		"content":       content,
		"stop_reason":   mapStopReason(finishReason),
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  in.Usage.PromptTokens,
			"output_tokens": in.Usage.CompletionTokens,
		},
	}
}

// mapStopReason 映射 OpenAI finish_reason 到 Anthropic stop_reason。
func mapStopReason(finishReason string) string {
	switch finishReason {
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return "end_turn"
	}
}

// newMessageID 生成本地消息 id（msg_ + 32 位 hex）。
func newMessageID() string {
	token, err := randomToken()
	if err != nil {
		return "msg_local"
	}
	return "msg_" + token
}

// newToolUseID 生成本地 tool_use id（toolu_ + 32 位 hex），仅上游缺 id 时兜底。
func newToolUseID() string {
	token, err := randomToken()
	if err != nil {
		return "toolu_local"
	}
	return "toolu_" + token
}

// synthAnthropicSSE 把一次聚合响应合成合法的 Anthropic SSE 事件流。
// 一期是整段合成（非逐 token）：文本块一次性给 full text，工具块一次性给
// 完整参数 JSON；事件序列仍遵循官方协议，Anthropic SDK 可正常消费。
func synthAnthropicSSE(resp []byte, model string) string {
	msg := openAIToAnthropic(resp, model)
	var b strings.Builder

	writeEvent := func(event string, payload map[string]any) {
		data, _ := json.Marshal(payload)
		fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", event, data)
	}

	id, _ := msg["id"].(string)
	usage, _ := msg["usage"].(map[string]any)
	inputTokens := usage["input_tokens"]
	outputTokens := usage["output_tokens"]

	writeEvent("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            id,
			"type":          "message",
			"role":          "assistant",
			"model":         model,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":  inputTokens,
				"output_tokens": 0,
			},
		},
	})

	content, _ := msg["content"].([]any)
	for index, raw := range content {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch block["type"] {
		case "text":
			writeEvent("content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": index,
				"content_block": map[string]any{
					"type": "text",
					"text": "",
				},
			})
			writeEvent("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": index,
				"delta": map[string]any{
					"type": "text_delta",
					"text": block["text"],
				},
			})
			writeEvent("content_block_stop", map[string]any{
				"type":  "content_block_stop",
				"index": index,
			})
		case "tool_use":
			writeEvent("content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": index,
				"content_block": map[string]any{
					"type":  "tool_use",
					"id":    block["id"],
					"name":  block["name"],
					"input": map[string]any{},
				},
			})
			partial, _ := json.Marshal(block["input"])
			writeEvent("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": index,
				"delta": map[string]any{
					"type":         "input_json_delta",
					"partial_json": string(partial),
				},
			})
			writeEvent("content_block_stop", map[string]any{
				"type":  "content_block_stop",
				"index": index,
			})
		}
	}

	writeEvent("message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   msg["stop_reason"],
			"stop_sequence": nil,
		},
		"usage": map[string]any{"output_tokens": outputTokens},
	})
	writeEvent("message_stop", map[string]any{"type": "message_stop"})
	return b.String()
}
