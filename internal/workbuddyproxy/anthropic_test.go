package workbuddyproxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/workbuddy"
)

// decodeAny 把 JSON 解为通用结构，便于与期望值做结构比较（忽略字段顺序）。
func decodeAny(t *testing.T, data []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("不是合法 JSON: %v, body=%s", err, data)
	}
	return v
}

func TestAnthropicToOpenAI(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "纯文本与 system 字符串",
			in:   `{"model":"claude-sonnet-4","system":"你是一个助手","max_tokens":1024,"messages":[{"role":"user","content":"你好"}],"stream":true}`,
			want: `{"model":"claude-sonnet-4","max_tokens":1024,"messages":[{"role":"system","content":"你是一个助手"},{"role":"user","content":"你好"}]}`,
		},
		{
			name: "content 为 null 时跳过该消息",
			in:   `{"model":"m","max_tokens":8,"messages":[{"role":"user","content":null}]}`,
			want: `{"model":"m","max_tokens":8,"messages":[]}`,
		},
		{
			name: "system blocks 拼接",
			in:   `{"model":"m","max_tokens":16,"system":[{"type":"text","text":"第一行"},{"type":"text","text":"第二行"}],"messages":[{"role":"user","content":"hi"}]}`,
			want: `{"model":"m","max_tokens":16,"messages":[{"role":"system","content":"第一行\n第二行"},{"role":"user","content":"hi"}]}`,
		},
		{
			name: "assistant tool_use 与 user tool_result",
			in: `{"model":"m","max_tokens":256,"messages":[
				{"role":"user","content":"weather?"},
				{"role":"assistant","content":[
					{"type":"text","text":"checking"},
					{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"city":"Paris"}}
				]},
				{"role":"user","content":[
					{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"sunny"},{"type":"text","text":"25C"}]},
					{"type":"text","text":"thanks"}
				]}
			]}`,
			want: `{"model":"m","max_tokens":256,"messages":[
				{"role":"user","content":"weather?"},
				{"role":"assistant","content":"checking","tool_calls":[{"id":"toolu_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]},
				{"role":"tool","tool_call_id":"toolu_1","content":"sunny\n25C"},
				{"role":"user","content":"thanks"}
			]}`,
		},
		{
			name: "多条 tool_result 合成多条 tool 消息",
			in: `{"model":"m","max_tokens":256,"messages":[
				{"role":"user","content":"go"},
				{"role":"assistant","content":[
					{"type":"tool_use","id":"toolu_a","name":"a","input":{}},
					{"type":"tool_use","id":"toolu_b","name":"b","input":{"n":2}}
				]},
				{"role":"user","content":[
					{"type":"tool_result","tool_use_id":"toolu_a","content":"ra"},
					{"type":"tool_result","tool_use_id":"toolu_b","content":"rb"}
				]}
			]}`,
			want: `{"model":"m","max_tokens":256,"messages":[
				{"role":"user","content":"go"},
				{"role":"assistant","content":"","tool_calls":[
					{"id":"toolu_a","type":"function","function":{"name":"a","arguments":"{}"}},
					{"id":"toolu_b","type":"function","function":{"name":"b","arguments":"{\"n\":2}"}}
				]},
				{"role":"tool","tool_call_id":"toolu_a","content":"ra"},
				{"role":"tool","tool_call_id":"toolu_b","content":"rb"}
			]}`,
		},
		{
			name: "tool_result 与文本混排时 tool 消息优先",
			in: `{"model":"m","max_tokens":8,"messages":[{"role":"user","content":[
				{"type":"text","text":"note"},
				{"type":"tool_result","tool_use_id":"toolu_1","content":"r"}
			]}]}`,
			want: `{"model":"m","max_tokens":8,"messages":[
				{"role":"tool","tool_call_id":"toolu_1","content":"r"},
				{"role":"user","content":"note"}
			]}`,
		},
		{
			name: "tools 与 tool_choice 指定工具",
			in: `{"model":"m","max_tokens":32,"messages":[],
				"tools":[{"name":"get_weather","description":"Get weather","input_schema":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}],
				"tool_choice":{"type":"tool","name":"get_weather"}}`,
			want: `{"model":"m","max_tokens":32,"messages":[],
				"tools":[{"type":"function","function":{"name":"get_weather","description":"Get weather","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}],
				"tool_choice":{"type":"function","function":{"name":"get_weather"}}}`,
		},
		{
			name: "tool_choice auto 映射",
			in:   `{"model":"m","max_tokens":32,"messages":[],"tool_choice":{"type":"auto"}}`,
			want: `{"model":"m","max_tokens":32,"messages":[],"tool_choice":"auto"}`,
		},
		{
			name: "tool_choice any 映射 required",
			in:   `{"model":"m","max_tokens":32,"messages":[],"tool_choice":{"type":"any"}}`,
			want: `{"model":"m","max_tokens":32,"messages":[],"tool_choice":"required"}`,
		},
		{
			name: "image 转 image_url 且 thinking 丢弃",
			in: `{"model":"m","max_tokens":64,"messages":[{"role":"user","content":[
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0KGgo="}},
				{"type":"text","text":"what is this?"},
				{"type":"thinking","thinking":"hmm"}
			]}]}`,
			want: `{"model":"m","max_tokens":64,"messages":[{"role":"user","content":[
				{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgo="}},
				{"type":"text","text":"what is this?"}
			]}]}`,
		},
		{
			name: "temperature 与 top_p 透传",
			in:   `{"model":"m","max_tokens":8,"temperature":0.3,"top_p":0.9,"messages":[{"role":"user","content":"x"}]}`,
			want: `{"model":"m","max_tokens":8,"temperature":0.3,"top_p":0.9,"messages":[{"role":"user","content":"x"}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := anthropicToOpenAI([]byte(tt.in))
			if err != nil {
				t.Fatalf("翻译失败: %v", err)
			}
			got := decodeAny(t, out)
			want := decodeAny(t, []byte(tt.want))
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("翻译结果不符:\n got=%s\nwant=%s", out, tt.want)
			}
		})
	}
}

func TestAnthropicToOpenAIBadRequest(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{name: "非法 JSON", in: "not json"},
		{name: "model 缺失", in: `{"messages":[]}`},
		{name: "model 非字符串", in: `{"model":123,"messages":[]}`},
		{name: "messages 缺失", in: `{"model":"m"}`},
		{name: "messages 非数组", in: `{"model":"m","messages":null}`},
		{name: "tool_choice tool 缺 name", in: `{"model":"m","messages":[],"tool_choice":{"type":"tool"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := anthropicToOpenAI([]byte(tt.in)); err == nil {
				t.Fatalf("应返回翻译错误: %s", tt.in)
			}
		})
	}
}

func TestOpenAIToAnthropic(t *testing.T) {
	tests := []struct {
		name  string
		resp  string
		model string
		want  string
	}{
		{
			name:  "文本与工具调用",
			resp:  `{"id":"chatcmpl-1","object":"chat.completion","model":"workbuddy","choices":[{"index":0,"message":{"role":"assistant","content":"好的","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":11,"completion_tokens":7}}`,
			model: "claude-sonnet-4",
			want:  `{"id":"chatcmpl-1","type":"message","role":"assistant","model":"claude-sonnet-4","content":[{"type":"text","text":"好的"},{"type":"tool_use","id":"call_1","name":"get_weather","input":{"city":"Paris"}}],"stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":11,"output_tokens":7}}`,
		},
		{
			name:  "length 映射 max_tokens",
			resp:  `{"id":"c2","choices":[{"message":{"role":"assistant","content":"截断"},"finish_reason":"length"}],"usage":{"prompt_tokens":3,"completion_tokens":9}}`,
			model: "claude-x",
			want:  `{"id":"c2","type":"message","role":"assistant","model":"claude-x","content":[{"type":"text","text":"截断"}],"stop_reason":"max_tokens","stop_sequence":null,"usage":{"input_tokens":3,"output_tokens":9}}`,
		},
		{
			name:  "stop 映射 end_turn",
			resp:  `{"id":"c3","choices":[{"message":{"role":"assistant","content":"完"},"finish_reason":"stop"}]}`,
			model: "claude-x",
			want:  `{"id":"c3","type":"message","role":"assistant","model":"claude-x","content":[{"type":"text","text":"完"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}`,
		},
		{
			name:  "非法 arguments 回退空对象",
			resp:  `{"id":"c4","choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_x","function":{"name":"f","arguments":"not-json"}}]},"finish_reason":"tool_calls"}]}`,
			model: "claude-x",
			want:  `{"id":"c4","type":"message","role":"assistant","model":"claude-x","content":[{"type":"tool_use","id":"call_x","name":"f","input":{}}],"stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}`,
		},
		{
			name:  "arguments 为 null 回退空对象",
			resp:  `{"id":"c5","choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_y","function":{"name":"f","arguments":"null"}}]},"finish_reason":"tool_calls"}]}`,
			model: "claude-x",
			want:  `{"id":"c5","type":"message","role":"assistant","model":"claude-x","content":[{"type":"tool_use","id":"call_y","name":"f","input":{}}],"stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}`,
		},
		{
			name:  "真实 ChatOnce 形状无 usage",
			resp:  `{"id":"workbuddy-0192abc","object":"chat.completion","created":1,"model":"workbuddy","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`,
			model: "claude-x",
			want:  `{"id":"workbuddy-0192abc","type":"message","role":"assistant","model":"claude-x","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}`,
		},
		{
			name:  "真实 ChatOnce 形状含 usage 映射",
			resp:  `{"id":"workbuddy-0192def","object":"chat.completion","created":1,"model":"workbuddy","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":21,"completion_tokens":13}}`,
			model: "claude-x",
			want:  `{"id":"workbuddy-0192def","type":"message","role":"assistant","model":"claude-x","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":21,"output_tokens":13}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := openAIToAnthropic([]byte(tt.resp), tt.model)
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("序列化失败: %v", err)
			}
			if g, w := decodeAny(t, raw), decodeAny(t, []byte(tt.want)); !reflect.DeepEqual(g, w) {
				t.Fatalf("响应翻译不符:\n got=%s\nwant=%s", raw, tt.want)
			}
		})
	}
}

// TestOpenAIToAnthropicToolIDFallback 覆盖上游缺 tool_call id 的兜底：
// 生成 toolu_ 前缀 id，保证 Anthropic tool_use 的 id 非空。
func TestOpenAIToAnthropicToolIDFallback(t *testing.T) {
	got := openAIToAnthropic([]byte(`{"id":"c","choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"f","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`), "m")
	content, _ := got["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content 数量 = %d，期望 1", len(content))
	}
	block, _ := content[0].(map[string]any)
	id, _ := block["id"].(string)
	if !strings.HasPrefix(id, "toolu_") || len(id) != len("toolu_")+32 {
		t.Fatalf("tool_call id 兜底格式不符: %q", id)
	}
}

// sseFrame 是测试侧解析出的单帧 SSE：event 名 + data JSON。
type sseFrame struct {
	event string
	data  map[string]any
}

// parseSSE 逐行解析 SSE，断言每个 data 帧都是合法 JSON。
func parseSSE(t *testing.T, body string) []sseFrame {
	t.Helper()
	var frames []sseFrame
	var cur sseFrame
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "event:"):
			cur.event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var m map[string]any
			if err := json.Unmarshal([]byte(payload), &m); err != nil {
				t.Fatalf("data 行不是合法 JSON: %q: %v", payload, err)
			}
			cur.data = m
		case line == "":
			if cur.data != nil {
				frames = append(frames, cur)
				cur = sseFrame{}
			}
		}
	}
	if cur.data != nil {
		frames = append(frames, cur)
	}
	return frames
}

// postAnthropic 发一次 Anthropic 形状请求，返回状态码、响应头与响应体。
func postAnthropic(t *testing.T, url, body, key string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	if key != "" {
		req.Header.Set("x-api-key", key)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("发请求失败: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读响应失败: %v", err)
	}
	return resp.StatusCode, resp.Header, data
}

func TestMessagesJSON(t *testing.T) {
	stubCredential(t)
	calls := make(chan chatCall, 1)
	restore := SetChatOnceForTest(func(ctx context.Context, cred workbuddy.Credential, body []byte, hc *http.Client) ([]byte, error) {
		calls <- chatCall{cred: cred, body: append([]byte(nil), body...)}
		return []byte(`{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":"你好"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`), nil
	})
	t.Cleanup(restore)

	s := New()
	t.Cleanup(func() { _ = s.Close() })
	base, key, err := s.Ensure()
	if err != nil {
		t.Fatalf("Ensure 失败: %v", err)
	}

	reqBody := `{"model":"claude-sonnet-4","system":"sys","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`
	status, hdr, data := postAnthropic(t, base+"/v1/messages", reqBody, key)
	if status != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200，body=%s", status, data)
	}
	if ct := hdr.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q，期望 application/json", ct)
	}
	var resp map[string]any
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if resp["type"] != "message" || resp["role"] != "assistant" {
		t.Fatalf("响应不是 Anthropic message 形状: %s", data)
	}
	if resp["model"] != "claude-sonnet-4" {
		t.Fatalf("model 未回填请求模型: %v", resp["model"])
	}
	// usage 必须从聚合响应映射到 Anthropic 字段，避免客户端统计恒为 0。
	usage, _ := resp["usage"].(map[string]any)
	if usage["input_tokens"] != float64(5) || usage["output_tokens"] != float64(2) {
		t.Fatalf("usage 映射不符: %v", resp["usage"])
	}

	// fake 必须收到翻译后的 OpenAI 请求：system 前置、stream 已剥离。
	var call chatCall
	select {
	case call = <-calls:
	case <-time.After(2 * time.Second):
		t.Fatal("ChatOnce 未被调用")
	}
	var sent map[string]any
	if err := json.Unmarshal(call.body, &sent); err != nil {
		t.Fatalf("发送 body 不是合法 JSON: %v", err)
	}
	if _, ok := sent["stream"]; ok {
		t.Fatalf("stream 字段应被剥离: %s", call.body)
	}
	msgs, _ := sent["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages 数量 = %d，期望 2: %s", len(msgs), call.body)
	}
	head, _ := msgs[0].(map[string]any)
	if head["role"] != "system" || head["content"] != "sys" {
		t.Fatalf("首条消息不是 system: %s", call.body)
	}
}

func TestMessagesStream(t *testing.T) {
	stubCredential(t)
	restore := SetChatOnceForTest(func(ctx context.Context, cred workbuddy.Credential, body []byte, hc *http.Client) ([]byte, error) {
		return []byte(`{"id":"chatcmpl-s","choices":[{"message":{"role":"assistant","content":"你好","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":9,"completion_tokens":4}}`), nil
	})
	t.Cleanup(restore)

	s := New()
	t.Cleanup(func() { _ = s.Close() })
	base, key, err := s.Ensure()
	if err != nil {
		t.Fatalf("Ensure 失败: %v", err)
	}

	reqBody := `{"model":"claude-sonnet-4","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hi"}]}`
	status, hdr, data := postAnthropic(t, base+"/v1/messages", reqBody, key)
	if status != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200，body=%s", status, data)
	}
	if ct := hdr.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q，期望 text/event-stream", ct)
	}

	frames := parseSSE(t, string(data))
	wantEvents := []string{
		"message_start",
		"content_block_start", "content_block_delta", "content_block_stop",
		"content_block_start", "content_block_delta", "content_block_stop",
		"message_delta", "message_stop",
	}
	if len(frames) != len(wantEvents) {
		t.Fatalf("SSE 帧数 = %d，期望 %d，body=%s", len(frames), len(wantEvents), data)
	}
	for i, want := range wantEvents {
		if frames[i].event != want {
			t.Fatalf("第 %d 帧 event = %q，期望 %q，body=%s", i, frames[i].event, want, data)
		}
		if frames[i].data["type"] != want {
			t.Fatalf("第 %d 帧 data.type = %v，期望 %q", i, frames[i].data["type"], want)
		}
	}

	// message_start：回填请求模型，content 为空数组。
	startMsg, _ := frames[0].data["message"].(map[string]any)
	if startMsg["model"] != "claude-sonnet-4" {
		t.Fatalf("message_start.model = %v，期望请求模型", startMsg["model"])
	}
	if cs, ok := startMsg["content"].([]any); !ok || len(cs) != 0 {
		t.Fatalf("message_start.message.content 应为空数组: %v", startMsg["content"])
	}

	// 文本块 index=0。
	textStart, _ := frames[1].data["content_block"].(map[string]any)
	if frames[1].data["index"] != float64(0) || textStart["type"] != "text" {
		t.Fatalf("文本块 start 形状不符: %v", frames[1].data)
	}
	textDelta, _ := frames[2].data["delta"].(map[string]any)
	if textDelta["type"] != "text_delta" || textDelta["text"] != "你好" {
		t.Fatalf("文本 delta 不符: %v", frames[2].data)
	}

	// 工具块 index=1：partial_json 为完整 JSON。
	toolStart, _ := frames[4].data["content_block"].(map[string]any)
	if frames[4].data["index"] != float64(1) || toolStart["type"] != "tool_use" ||
		toolStart["id"] != "call_1" || toolStart["name"] != "get_weather" {
		t.Fatalf("工具块 start 形状不符: %v", frames[4].data)
	}
	toolDelta, _ := frames[5].data["delta"].(map[string]any)
	if toolDelta["type"] != "input_json_delta" {
		t.Fatalf("工具 delta 类型不符: %v", frames[5].data)
	}
	partial, _ := toolDelta["partial_json"].(string)
	if got := decodeAny(t, []byte(partial)); !reflect.DeepEqual(got, map[string]any{"city": "Paris"}) {
		t.Fatalf("partial_json 不是完整参数 JSON: %q", partial)
	}

	// message_delta：stop_reason 与 output_tokens。
	delta, _ := frames[7].data["delta"].(map[string]any)
	if delta["stop_reason"] != "tool_use" {
		t.Fatalf("message_delta.stop_reason = %v，期望 tool_use", delta["stop_reason"])
	}
	usage, _ := frames[7].data["usage"].(map[string]any)
	if usage["output_tokens"] != float64(4) {
		t.Fatalf("message_delta.usage.output_tokens = %v，期望 4", usage["output_tokens"])
	}
}

// TestSynthAnthropicSSEThreeToolUses 验证连续 3 个 tool_use 时 content index 递增。
func TestSynthAnthropicSSEThreeToolUses(t *testing.T) {
	resp := `{"id":"workbuddy-3tools","choices":[{"message":{"role":"assistant","content":"","tool_calls":[
		{"id":"call_1","function":{"name":"a","arguments":"{\"i\":1}"}},
		{"id":"call_2","function":{"name":"b","arguments":"{\"i\":2}"}},
		{"id":"call_3","function":{"name":"c","arguments":"{\"i\":3}"}}
	]},"finish_reason":"tool_calls"}]}`
	frames := parseSSE(t, synthAnthropicSSE([]byte(resp), "claude-x"))

	// message_start(1) + 3*(start/delta/stop) + message_delta + message_stop = 12 帧。
	if len(frames) != 12 {
		t.Fatalf("SSE 帧数 = %d，期望 12", len(frames))
	}
	for i := 0; i < 3; i++ {
		start := frames[1+i*3]
		block, _ := start.data["content_block"].(map[string]any)
		if start.data["index"] != float64(i) || block["type"] != "tool_use" {
			t.Fatalf("第 %d 个工具块 start 不符: %v", i, start.data)
		}
		delta := frames[2+i*3]
		if delta.data["index"] != float64(i) {
			t.Fatalf("第 %d 个工具块 delta index 不符: %v", i, delta.data)
		}
		stop := frames[3+i*3]
		if stop.data["index"] != float64(i) {
			t.Fatalf("第 %d 个工具块 stop index 不符: %v", i, stop.data)
		}
	}
	if frames[10].event != "message_delta" || frames[11].event != "message_stop" {
		t.Fatalf("结尾事件序列不符: %q, %q", frames[10].event, frames[11].event)
	}
}

// TestSynthAnthropicSSEEmptyChoices 验证 choices:[] 仍产出合法的最小事件流。
func TestSynthAnthropicSSEEmptyChoices(t *testing.T) {
	frames := parseSSE(t, synthAnthropicSSE([]byte(`{"id":"workbuddy-empty","choices":[]}`), "claude-x"))
	wantEvents := []string{"message_start", "message_delta", "message_stop"}
	if len(frames) != len(wantEvents) {
		t.Fatalf("SSE 帧数 = %d，期望 %d", len(frames), len(wantEvents))
	}
	for i, want := range wantEvents {
		if frames[i].event != want {
			t.Fatalf("第 %d 帧 event = %q，期望 %q", i, frames[i].event, want)
		}
	}
	startMsg, _ := frames[0].data["message"].(map[string]any)
	if startMsg["id"] != "workbuddy-empty" {
		t.Fatalf("message_start.id = %v，期望透传 workbuddy- id", startMsg["id"])
	}
	delta, _ := frames[1].data["delta"].(map[string]any)
	if delta["stop_reason"] != "end_turn" {
		t.Fatalf("空响应 stop_reason = %v，期望 end_turn", delta["stop_reason"])
	}
	usage, _ := frames[1].data["usage"].(map[string]any)
	if usage["output_tokens"] != float64(0) {
		t.Fatalf("空响应 output_tokens = %v，期望 0", usage["output_tokens"])
	}
}

type anthropicErrorBody struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeAnthropicError(t *testing.T, data []byte) anthropicErrorBody {
	t.Helper()
	var e anthropicErrorBody
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatalf("错误响应不是 Anthropic 错误形状: %v, body=%s", err, data)
	}
	if e.Type != "error" || e.Error.Type == "" || e.Error.Message == "" {
		t.Fatalf("错误响应缺少 type/error.type/error.message: %s", data)
	}
	return e
}

func TestMessagesErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantType   string
	}{
		{name: "token 过期 401", err: errors.New("err.workbuddy.token_expired"), wantStatus: http.StatusUnauthorized, wantType: "authentication_error"},
		{name: "网关错误 502", err: errors.New("err.workbuddy.chat_http|500"), wantStatus: http.StatusBadGateway, wantType: "api_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubCredential(t)
			restore := SetChatOnceForTest(func(ctx context.Context, cred workbuddy.Credential, body []byte, hc *http.Client) ([]byte, error) {
				return nil, tt.err
			})
			t.Cleanup(restore)

			s := New()
			t.Cleanup(func() { _ = s.Close() })
			base, key, err := s.Ensure()
			if err != nil {
				t.Fatalf("Ensure 失败: %v", err)
			}

			status, _, data := postAnthropic(t, base+"/v1/messages", `{"model":"m","max_tokens":8,"messages":[]}`, key)
			if status != tt.wantStatus {
				t.Fatalf("状态码 = %d，期望 %d，body=%s", status, tt.wantStatus, data)
			}
			e := decodeAnthropicError(t, data)
			if e.Error.Type != tt.wantType {
				t.Fatalf("错误类型 = %q，期望 %q，body=%s", e.Error.Type, tt.wantType, data)
			}
			if !strings.Contains(e.Error.Message, tt.err.Error()) {
				t.Fatalf("错误 message 未包含底层错误: %s", data)
			}
		})
	}
}

func TestMessagesCredentialUnavailable(t *testing.T) {
	prev := loadCredential
	loadCredential = func() (workbuddy.Credential, error) {
		return workbuddy.Credential{}, errors.New(`open C:\Users\secret\appstate.json: access denied`)
	}
	t.Cleanup(func() { loadCredential = prev })
	restore := SetChatOnceForTest(func(ctx context.Context, cred workbuddy.Credential, body []byte, hc *http.Client) ([]byte, error) {
		return nil, errors.New("ChatOnce 不应被调用")
	})
	t.Cleanup(restore)

	s := New()
	t.Cleanup(func() { _ = s.Close() })
	base, key, err := s.Ensure()
	if err != nil {
		t.Fatalf("Ensure 失败: %v", err)
	}

	status, _, data := postAnthropic(t, base+"/v1/messages", `{"model":"m","max_tokens":8,"messages":[]}`, key)
	if status != http.StatusUnauthorized {
		t.Fatalf("凭据不可用状态码 = %d，期望 401，body=%s", status, data)
	}
	e := decodeAnthropicError(t, data)
	if e.Error.Type != "authentication_error" || e.Error.Message != credentialUnavailableMessage {
		t.Fatalf("凭据错误形状不符: %s", data)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "appstate") {
		t.Fatalf("响应泄露本机路径细节: %s", data)
	}
}

func TestMessagesBadRequestAndAuth(t *testing.T) {
	s := New()
	t.Cleanup(func() { _ = s.Close() })
	base, key, err := s.Ensure()
	if err != nil {
		t.Fatalf("Ensure 失败: %v", err)
	}

	// 鉴权与 /v1/chat/completions 复用同一路径无关中间件：无 token 必须 401。
	// 该中间件按计划保持既有 OpenAI 错误信封不变，这里只断言拒绝语义。
	status, _, data := postAnthropic(t, base+"/v1/messages", `{"model":"m","messages":[]}`, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("无 token 状态码 = %d，期望 401，body=%s", status, data)
	}
	var authErr struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &authErr); err != nil || authErr.Error.Message == "" {
		t.Fatalf("鉴权失败响应缺少错误信息: %s", data)
	}

	stubCredential(t)
	restore := SetChatOnceForTest(func(ctx context.Context, cred workbuddy.Credential, body []byte, hc *http.Client) ([]byte, error) {
		return []byte(`{"choices":[]}`), nil
	})
	t.Cleanup(restore)

	badBodies := []struct {
		name string
		body string
	}{
		{name: "非法 JSON", body: "not json"},
		{name: "model 缺失", body: `{"messages":[]}`},
		{name: "messages 缺失", body: `{"model":"m"}`},
		{name: "tool_choice tool 缺 name", body: `{"model":"m","messages":[],"tool_choice":{"type":"tool"}}`},
	}
	for _, tt := range badBodies {
		t.Run(tt.name, func(t *testing.T) {
			status, _, data := postAnthropic(t, base+"/v1/messages", tt.body, key)
			if status != http.StatusBadRequest {
				t.Fatalf("状态码 = %d，期望 400，body=%s", status, data)
			}
			e := decodeAnthropicError(t, data)
			if e.Error.Type != "invalid_request_error" {
				t.Fatalf("错误类型 = %q，期望 invalid_request_error", e.Error.Type)
			}
		})
	}
}
