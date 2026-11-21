package hubtools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient 构造指向 mock 服务端的客户端。
func newTestClient(baseURL string) *Client {
	return NewClient(Config{BaseURL: baseURL, APIKey: "sk-test", Model: "test-model"})
}

// mockChoice 是 mock 响应的便捷构造。
func mockChoice(text string) map[string]any {
	return map[string]any{"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": text}}}}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("编码响应: %v", err)
	}
}

func TestChatOnce(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		writeJSON(t, w, mockChoice("你好"))
	}))
	defer srv.Close()

	for _, base := range []string{srv.URL + "/", srv.URL} {
		c := newTestClient(base)
		resp, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}})
		if err != nil {
			t.Fatalf("Chat: %v", err)
		}
		if resp.Text != "你好" {
			t.Fatalf("文本 = %q", resp.Text)
		}
		// 无工具调用的路径 ToolCalls 必须是 nil（而非空切片），供循环层安全判空
		if resp.ToolCalls != nil {
			t.Fatalf("无工具调用时 ToolCalls 应为 nil, 得到 %#v", resp.ToolCalls)
		}
		if gotAuth != "Bearer sk-test" {
			t.Fatalf("Authorization = %q", gotAuth)
		}
		if !strings.Contains(gotBody, `"model":"test-model"`) || !strings.Contains(gotBody, `"messages"`) {
			t.Fatalf("请求体缺失字段: %s", gotBody)
		}
		// Chat 路径不带 tools：请求体不得出现 tools 字段（omitempty 向后兼容回归）
		if strings.Contains(gotBody, `"tools"`) {
			t.Fatalf("请求体不应携带 tools: %s", gotBody)
		}
	}
}

// mockToolCalls 是带 tool_calls 的 mock 响应（OpenAI 嵌套 wire 形状，
// arguments 保持原始字符串）。
func mockToolCalls() map[string]any {
	return map[string]any{"choices": []map[string]any{{"message": map[string]any{
		"role":    "assistant",
		"content": "",
		"tool_calls": []map[string]any{
			{"id": "call_1", "type": "function", "function": map[string]any{
				"name":      "get_weather",
				"arguments": `{"city":"北京"}`,
			}},
		},
	}}}}
}

func TestChatServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err == nil || !strings.Contains(err.Error(), "err.hubtools.request_failed") {
		t.Fatalf("期望含 request_failed 的错误，得到 %v", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("期望含 500 的错误，得到 %v", err)
	}
}

// TestChatWithToolsRequestBody 验证带工具的请求体含 tools 数组且形状正确。
func TestChatWithToolsRequestBody(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		writeJSON(t, w, mockChoice("好"))
	}))
	defer srv.Close()

	tools := []ToolFunc{{
		Name:        "get_weather",
		Description: "查询天气",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}},
	}}
	c := newTestClient(srv.URL)
	if _, err := c.ChatWithTools(context.Background(), []Message{{Role: "user", Content: "hi"}}, tools); err != nil {
		t.Fatalf("ChatWithTools: %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("请求体非 JSON: %v\n%s", err, gotBody)
	}
	arr, ok := body["tools"].([]any)
	if !ok || len(arr) != 1 {
		t.Fatalf("请求体 tools 数组缺失或长度不符: %s", gotBody)
	}
	// OpenAI 严格 wire 形状：{"type":"function","function":{name,description,parameters}}
	t0, ok := arr[0].(map[string]any)
	if !ok || t0["type"] != "function" {
		t.Fatalf("tools[0].type 缺失或不符: %s", gotBody)
	}
	fn, ok := t0["function"].(map[string]any)
	if !ok || fn["name"] != "get_weather" {
		t.Fatalf("tools[0].function.name 缺失或不符: %s", gotBody)
	}
	if _, ok := fn["parameters"].(map[string]any); !ok {
		t.Fatalf("tools[0].function.parameters 缺失: %s", gotBody)
	}
}

// TestChatWithToolsResponseParsing 验证响应含 tool_calls 时解析到 Response.ToolCalls，
// arguments 原样保留字符串（解析归循环层）。
func TestChatWithToolsResponseParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, mockToolCalls())
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	resp, err := c.ChatWithTools(context.Background(), []Message{{Role: "user", Content: "hi"}}, []ToolFunc{{Name: "get_weather"}})
	if err != nil {
		t.Fatalf("ChatWithTools: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls 数量 = %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_1" || tc.Name != "get_weather" {
		t.Fatalf("ToolCalls 解析不符: %+v", tc)
	}
	if tc.Arguments != `{"city":"北京"}` {
		t.Fatalf("arguments 应原样保留字符串, 得到 %q", tc.Arguments)
	}
}

// TestChatMultiTurnToolMessagesWireShape 验证多轮 tool 消息的 wire 装配：
// assistant 的扁平 ToolCalls 输出嵌套 tool_calls，tool 角色输出 tool_call_id。
func TestChatMultiTurnToolMessagesWireShape(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		writeJSON(t, w, mockChoice("晴"))
	}))
	defer srv.Close()

	msgs := []Message{
		{Role: "user", Content: "查天气"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1", Name: "get_weather", Arguments: `{"city":"北京"}`}}},
		{Role: "tool", ToolCallID: "call_1", Content: "晴"},
	}
	tools := []ToolFunc{{Name: "get_weather"}}
	c := newTestClient(srv.URL)
	if _, err := c.ChatWithTools(context.Background(), msgs, tools); err != nil {
		t.Fatalf("ChatWithTools: %v", err)
	}

	var body struct {
		Messages []struct {
			Role       string `json:"role"`
			ToolCallID string `json:"tool_call_id"`
			ToolCalls  []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("请求体非 JSON: %v\n%s", err, gotBody)
	}
	if len(body.Messages) != 3 {
		t.Fatalf("消息数 = %d: %s", len(body.Messages), gotBody)
	}
	// 第二条：assistant 携带嵌套 tool_calls
	m2 := body.Messages[1]
	if m2.Role != "assistant" || len(m2.ToolCalls) != 1 {
		t.Fatalf("第二条消息应携带嵌套 tool_calls: %+v", m2)
	}
	tc := m2.ToolCalls[0]
	if tc.ID != "call_1" || tc.Type != "function" || tc.Function.Name != "get_weather" || tc.Function.Arguments != `{"city":"北京"}` {
		t.Fatalf("tool_calls 解析不符: %+v", tc)
	}
	// 第三条：role=tool 且带 tool_call_id
	m3 := body.Messages[2]
	if m3.Role != "tool" || m3.ToolCallID != "call_1" {
		t.Fatalf("第三条消息应为 role=tool 且带 tool_call_id: %+v", m3)
	}
}

// TestChatSessionHeader 验证 SessionID 与 x-opencode-session 头的对应关系：
// 非空必带（opencode GO 网关缺失该头直接 400），空值不发（其它网关零影响）。
func TestChatSessionHeader(t *testing.T) {
	var gotSession string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSession = r.Header.Get("x-opencode-session")
		writeJSON(t, w, mockChoice("好"))
	}))
	defer srv.Close()

	// 非空：必须带上该头
	c := NewClient(Config{BaseURL: srv.URL, APIKey: "k", Model: "m", SessionID: "sess-1"})
	if _, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gotSession != "sess-1" {
		t.Fatalf("x-opencode-session = %q, 期望 sess-1", gotSession)
	}

	// 空：不得发该头
	gotSession = ""
	c2 := newTestClient(srv.URL)
	if _, err := c2.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gotSession != "" {
		t.Fatalf("SessionID 为空时不应发送 x-opencode-session, got %q", gotSession)
	}
}
