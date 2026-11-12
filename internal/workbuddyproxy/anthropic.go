package workbuddyproxy

import (
	"encoding/json"
	"fmt"
	"strings"
)

// anthropicToOpenAI 把 Anthropic Messages 请求翻译为 OpenAI Chat Completions 请求体。
// 纯函数、无 I/O，便于表驱动测试。翻译只保证语义等价，不保证 JSON 字段顺序一致
// （ChatOnce 只是把它再交给网关解析，网关按语义消费）。
//
// 一期边界：聚合式翻译；image 尽力透传（base64/url source）；thinking 块丢弃。
func anthropicToOpenAI(body []byte) ([]byte, error) {
	var in map[string]any
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, fmt.Errorf("err.workbuddyproxy.bad_request|%v", err)
	}

	// Anthropic Messages 必须有字符串 model 与 messages 数组；缺失直接视为
	// 非法请求，由 handleMessages 映射为 400 invalid_request_error。
	model, ok := in["model"].(string)
	if !ok || strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("err.workbuddyproxy.bad_request|model 缺失或不是字符串")
	}
	if _, ok := in["messages"].([]any); !ok {
		return nil, fmt.Errorf("err.workbuddyproxy.bad_request|messages 缺失或不是数组")
	}

	out := map[string]any{"model": model}
	// max_tokens / temperature / top_p 语义与 OpenAI 一致，直接透传；
	// 其余 Anthropic 专有字段（stop_sequences、metadata 等）不透传，避免网关拒收。
	for _, key := range []string{"max_tokens", "temperature", "top_p"} {
		if v, ok := in[key]; ok {
			out[key] = v
		}
	}

	msgs := make([]any, 0, 8)
	if sys, ok := systemText(in["system"]); ok && sys != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": sys})
	}
	if rawMsgs, ok := in["messages"].([]any); ok {
		for _, raw := range rawMsgs {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			msgs = append(msgs, translateAnthropicMessage(m)...)
		}
	}
	out["messages"] = msgs

	if tools, ok := in["tools"].([]any); ok && len(tools) > 0 {
		out["tools"] = translateTools(tools)
	}
	if choice, ok := in["tool_choice"]; ok {
		mapped, keep, err := translateToolChoice(choice)
		if err != nil {
			return nil, err
		}
		if keep {
			out["tool_choice"] = mapped
		}
	}

	// 主动剥掉 stream：ChatOnce 内部强制 stream:true 并聚合；代理是否合成 SSE
	// 只看原始 Anthropic 请求 body 的 stream 字段（见 handleMessages），与转发体无关。
	return json.Marshal(out)
}

// systemText 把 Anthropic system 字段归一为文本：字符串原样，blocks 数组拼接 text 块。
func systemText(v any) (string, bool) {
	switch s := v.(type) {
	case string:
		return s, true
	case []any:
		var texts []string
		for _, raw := range s {
			block, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if typ, _ := block["type"].(string); typ != "text" {
				continue
			}
			if text, _ := block["text"].(string); text != "" {
				texts = append(texts, text)
			}
		}
		return strings.Join(texts, "\n"), true
	default:
		return "", false
	}
}

// translateAnthropicMessage 翻译单条消息；content 为 blocks 时可能展开成多条
// OpenAI 消息（tool_result 在 Anthropic 语义里位于 user 消息内，OpenAI 里必须
// 拆成独立的 role:"tool" 消息）。
func translateAnthropicMessage(m map[string]any) []any {
	role, _ := m["role"].(string)
	switch content := m["content"].(type) {
	case string:
		if content == "" {
			return nil
		}
		return []any{map[string]any{"role": role, "content": content}}
	case []any:
		if role == "assistant" {
			return translateAssistantBlocks(content)
		}
		return translateUserBlocks(role, content)
	default:
		return nil
	}
}

// translateAssistantBlocks 把 assistant 的 blocks 合成为一条 OpenAI assistant 消息：
// text 拼接为 content，tool_use 转 tool_calls，thinking/image 丢弃。
func translateAssistantBlocks(blocks []any) []any {
	var texts []string
	var toolCalls []any
	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch typ, _ := block["type"].(string); typ {
		case "text":
			if text, _ := block["text"].(string); text != "" {
				texts = append(texts, text)
			}
		case "tool_use":
			toolCalls = append(toolCalls, toolUseToOpenAI(block))
		}
	}
	if len(texts) == 0 && len(toolCalls) == 0 {
		return nil
	}
	msg := map[string]any{"role": "assistant", "content": strings.Join(texts, "\n")}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}
	return []any{msg}
}

// translateUserBlocks 翻译 user 消息：tool_result 统一先输出为独立的 role:"tool"
// 消息，保证它们紧跟 assistant tool_calls（OpenAI 协议要求 tool 消息紧随其后）；
// 其余 text/image 片段合并成一条 user 消息放在 tool 消息之后。
// Anthropic 允许 tool_result 与文本混排，这里重排是刻意取舍：工具协议正确性优先。
func translateUserBlocks(role string, blocks []any) []any {
	var toolMsgs []any
	var parts []any
	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch typ, _ := block["type"].(string); typ {
		case "text":
			if text, _ := block["text"].(string); text != "" {
				parts = append(parts, map[string]any{"type": "text", "text": text})
			}
		case "image":
			if part, ok := imageBlockToOpenAI(block); ok {
				parts = append(parts, part)
			}
		case "tool_result":
			toolMsgs = append(toolMsgs, toolResultToOpenAIMessage(block))
		default:
			// thinking 等未知块丢弃：一期不还原推理内容。
		}
	}
	out := toolMsgs
	if len(parts) > 0 {
		out = append(out, map[string]any{"role": role, "content": partsToContent(parts)})
	}
	return out
}

// partsToContent 纯文本片段折叠成字符串（贴近多数 OpenAI 客户端习惯），
// 含图片时保留数组形状供网关解析 image_url。
func partsToContent(parts []any) any {
	allText := true
	for _, part := range parts {
		pm, _ := part.(map[string]any)
		if pm["type"] != "text" {
			allText = false
			break
		}
	}
	if !allText {
		return parts
	}
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		pm, _ := part.(map[string]any)
		text, _ := pm["text"].(string)
		texts = append(texts, text)
	}
	return strings.Join(texts, "\n")
}

// toolUseToOpenAI 把 Anthropic tool_use 块转成 OpenAI tool_call；
// input 作为参数 JSON 字符串（语义与原 input 对象一致）。
func toolUseToOpenAI(block map[string]any) map[string]any {
	name, _ := block["name"].(string)
	args := "{}"
	if input, ok := block["input"]; ok && input != nil {
		if raw, err := json.Marshal(input); err == nil {
			args = string(raw)
		}
	}
	id, _ := block["id"].(string)
	return map[string]any{
		"id":   id,
		"type": "function",
		"function": map[string]any{
			"name":      name,
			"arguments": args,
		},
	}
}

// toolResultToOpenAIMessage 把 Anthropic tool_result 块转成 OpenAI role:"tool" 消息，
// tool_call_id 对应先前 tool_use 的 id。
func toolResultToOpenAIMessage(block map[string]any) map[string]any {
	id, _ := block["tool_use_id"].(string)
	return map[string]any{
		"role":         "tool",
		"tool_call_id": id,
		"content":      toolResultText(block["content"]),
	}
}

// toolResultText 抽取 tool_result 的文本内容：字符串原样，blocks 数组拼接 text 块。
func toolResultText(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []any:
		var texts []string
		for _, raw := range c {
			block, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if typ, _ := block["type"].(string); typ != "text" {
				continue
			}
			if text, _ := block["text"].(string); text != "" {
				texts = append(texts, text)
			}
		}
		return strings.Join(texts, "\n")
	default:
		return ""
	}
}

// imageBlockToOpenAI 把 Anthropic image 块转成 OpenAI image_url 内容片段；
// base64 source 拼 data URI，url source 直接透传。
func imageBlockToOpenAI(block map[string]any) (map[string]any, bool) {
	source, ok := block["source"].(map[string]any)
	if !ok {
		return nil, false
	}
	var url string
	switch typ, _ := source["type"].(string); typ {
	case "base64":
		media, _ := source["media_type"].(string)
		data, _ := source["data"].(string)
		// 缺 media_type 无法构造合法 data URI，直接丢弃该图片块（一期尽力透传）。
		if media == "" || data == "" {
			return nil, false
		}
		url = "data:" + media + ";base64," + data
	case "url":
		url, _ = source["url"].(string)
	}
	if url == "" {
		return nil, false
	}
	return map[string]any{
		"type":      "image_url",
		"image_url": map[string]any{"url": url},
	}, true
}

// translateTools 把 Anthropic tools 转成 OpenAI function 工具定义。
func translateTools(tools []any) []any {
	out := make([]any, 0, len(tools))
	for _, raw := range tools {
		tm, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := tm["name"].(string)
		if name == "" {
			continue
		}
		fn := map[string]any{"name": name}
		if desc, ok := tm["description"]; ok {
			fn["description"] = desc
		}
		if schema, ok := tm["input_schema"]; ok && schema != nil {
			fn["parameters"] = schema
		} else {
			fn["parameters"] = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, map[string]any{"type": "function", "function": fn})
	}
	return out
}

// translateToolChoice 映射 Anthropic tool_choice：
// auto→"auto"、any→"required"、{"type":"tool","name":X}→指定 function。
// 返回 (映射值, 是否保留, 错误)：不支持的类型静默丢弃；
// type=tool 缺 name 属于非法请求，必须报错而不是被静默忽略。
func translateToolChoice(v any) (any, bool, error) {
	switch tc := v.(type) {
	case string:
		mapped, ok := mapToolChoiceType(tc)
		return mapped, ok, nil
	case map[string]any:
		typ, _ := tc["type"].(string)
		if typ == "tool" {
			name, _ := tc["name"].(string)
			if strings.TrimSpace(name) == "" {
				return nil, false, fmt.Errorf("err.workbuddyproxy.bad_tool_choice|tool_choice.type=tool 缺少 name")
			}
			return map[string]any{
				"type":     "function",
				"function": map[string]any{"name": name},
			}, true, nil
		}
		mapped, ok := mapToolChoiceType(typ)
		return mapped, ok, nil
	default:
		return nil, false, nil
	}
}

// mapToolChoiceType 映射无参的类型化 tool_choice。
func mapToolChoiceType(typ string) (any, bool) {
	switch typ {
	case "auto":
		return "auto", true
	case "any":
		return "required", true
	case "none":
		return "none", true
	default:
		return nil, false
	}
}
