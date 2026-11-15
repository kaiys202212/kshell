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
		if gotAuth != "Bearer sk-test" {
			t.Fatalf("Authorization = %q", gotAuth)
		}
		if !strings.Contains(gotBody, `"model":"test-model"`) || !strings.Contains(gotBody, `"messages"`) {
			t.Fatalf("请求体缺失字段: %s", gotBody)
		}
		// 工具循环已下线：请求体不得再携带 tools 字段（旧网关对空数组会报错）
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
