package hubtools

import (
	"context"
	"encoding/json"
	"errors"
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
func mockChoice(text string, calls []map[string]any) map[string]any {
	msg := map[string]any{"role": "assistant"}
	if text != "" {
		msg["content"] = text
	}
	if len(calls) > 0 {
		msg["tool_calls"] = calls
	}
	return map[string]any{"choices": []map[string]any{{"message": msg}}}
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
		writeJSON(t, w, mockChoice("你好", nil))
	}))
	defer srv.Close()

	for _, base := range []string{srv.URL + "/", srv.URL} {
		c := newTestClient(base)
		resp, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
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
		if strings.Contains(gotBody, `"tools"`) {
			t.Fatalf("tools 为空时不应携带字段: %s", gotBody)
		}
		// 两种 BaseURL（含/不含尾部斜杠）都应命中同一端点，重复拼接会导致 404
		if resp.Text != "你好" {
			t.Fatalf("BaseURL=%q 文本 = %q", base, resp.Text)
		}
	}
}

func TestChatToolLoop(t *testing.T) {
	var bodies []string
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		callCount++
		if callCount == 1 {
			writeJSON(t, w, mockChoice("", []map[string]any{{
				"id":   "call-1",
				"type": "function",
				"function": map[string]any{
					"name":      "get_weather",
					"arguments": `{"city":"北京"}`,
				},
			}}))
			return
		}
		writeJSON(t, w, mockChoice("北京晴", nil))
	}))
	defer srv.Close()

	var gotName, gotArgs string
	exec := func(ctx context.Context, name, argsJSON string) (string, error) {
		gotName, gotArgs = name, argsJSON
		return "晴", nil
	}

	c := newTestClient(srv.URL)
	resp, err := c.ChatWithExecutor(context.Background(),
		[]Message{{Role: "user", Content: "天气如何"}},
		[]ToolDef{{Name: "get_weather", Parameters: map[string]any{"type": "object"}}},
		exec)
	if err != nil {
		t.Fatalf("ChatWithExecutor: %v", err)
	}
	if resp.Text != "北京晴" {
		t.Fatalf("文本 = %q", resp.Text)
	}
	if gotName != "get_weather" || gotArgs != `{"city":"北京"}` {
		t.Fatalf("executor 收到 name=%q args=%q", gotName, gotArgs)
	}
	if !strings.Contains(bodies[1], `"role":"tool"`) || !strings.Contains(bodies[1], "call-1") {
		t.Fatalf("第二次请求体缺 tool 消息: %s", bodies[1])
	}
}

func TestChatExecutorError(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			writeJSON(t, w, mockChoice("", []map[string]any{{
				"id":       "call-1",
				"type":     "function",
				"function": map[string]any{"name": "boom", "arguments": "{}"},
			}}))
			return
		}
		writeJSON(t, w, mockChoice("工具失败了", nil))
	}))
	defer srv.Close()

	exec := func(ctx context.Context, name, argsJSON string) (string, error) {
		return "", errors.New("炸了")
	}

	c := newTestClient(srv.URL)
	resp, err := c.ChatWithExecutor(context.Background(),
		[]Message{{Role: "user", Content: "hi"}},
		[]ToolDef{{Name: "boom"}}, exec)
	if err != nil {
		t.Fatalf("executor 报错不应中断循环: %v", err)
	}
	if resp.Text != "工具失败了" {
		t.Fatalf("文本 = %q", resp.Text)
	}
}

func TestChatLoopLimit(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		// 每次不同 ID，避免任何潜在的缓存/去重效应
		writeJSON(t, w, mockChoice("", []map[string]any{{
			"id":       "call-" + strings.Repeat("x", n),
			"type":     "function",
			"function": map[string]any{"name": "loop", "arguments": "{}"},
		}}))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.ChatWithExecutor(context.Background(),
		[]Message{{Role: "user", Content: "hi"}},
		[]ToolDef{{Name: "loop"}},
		func(ctx context.Context, name, args string) (string, error) { return "ok", nil })
	if err == nil || !strings.Contains(err.Error(), "工具调用轮次超限") {
		t.Fatalf("期望轮次超限错误，得到 %v", err)
	}
}

func TestChatServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("期望含 500 的错误，得到 %v", err)
	}
}
