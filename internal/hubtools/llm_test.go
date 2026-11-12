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
