package workbuddyproxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/workbuddy"
)

// stubCredential 让测试绕过真实的 WorkBuddy 桌面端凭据文件；
// 生产路径仍由 workbuddy.LoadCredential 提供凭据，这里只替换注入点。
func stubCredential(t *testing.T) {
	t.Helper()
	prev := loadCredential
	loadCredential = func() (workbuddy.Credential, error) {
		return workbuddy.Credential{AccessToken: "test-token"}, nil
	}
	t.Cleanup(func() { loadCredential = prev })
}

// postJSON 向测试服务发一次 JSON POST，返回状态码与响应体。
func postJSON(t *testing.T, url, body string, headers map[string]string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("发请求失败: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读响应失败: %v", err)
	}
	return resp.StatusCode, data
}

type openAIErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func decodeErrorBody(t *testing.T, data []byte) openAIErrorBody {
	t.Helper()
	var e openAIErrorBody
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatalf("错误响应不是 OpenAI 错误形状: %v, body=%s", err, data)
	}
	if e.Error.Message == "" || e.Error.Type == "" {
		t.Fatalf("错误响应缺少 message/type: %s", data)
	}
	return e
}

func TestEnsureIdempotentAndAuth(t *testing.T) {
	s := New()
	t.Cleanup(func() { _ = s.Close() })

	base1, key1, err := s.Ensure()
	if err != nil {
		t.Fatalf("首次 Ensure 失败: %v", err)
	}
	base2, key2, err := s.Ensure()
	if err != nil {
		t.Fatalf("二次 Ensure 失败: %v", err)
	}
	if base1 != base2 || key1 != key2 {
		t.Fatalf("Ensure 非幂等: 首次 (%q,%q)，二次 (%q,%q)", base1, key1, base2, key2)
	}
	if !strings.HasPrefix(base1, "http://127.0.0.1:") {
		t.Fatalf("base 不是本地监听地址: %q", base1)
	}
	if len(key1) != 32 {
		t.Fatalf("token 应为 32 位 hex，实际 %d 位: %q", len(key1), key1)
	}

	// 无 token 与错误 token 都必须被拒。
	status, data := postJSON(t, base1+"/v1/chat/completions", `{"model":"m","messages":[]}`, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("无 token 状态码 = %d，期望 401", status)
	}
	decodeErrorBody(t, data)

	status, data = postJSON(t, base1+"/v1/chat/completions", `{"model":"m","messages":[]}`, map[string]string{
		"Authorization": "Bearer wrong-token",
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("错误 token 状态码 = %d，期望 401", status)
	}
	decodeErrorBody(t, data)
}

func TestOpenAIPassthrough(t *testing.T) {
	stubCredential(t)
	var gotBody []byte
	restore := SetChatOnceForTest(func(ctx context.Context, cred workbuddy.Credential, body []byte, hc *http.Client) ([]byte, error) {
		gotBody = append([]byte(nil), body...)
		if cred.AccessToken != "test-token" {
			t.Fatalf("ChatOnce 收到的凭据 = %q，期望 test-token", cred.AccessToken)
		}
		return []byte(`{"id":"chatcmpl-test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}]}`), nil
	})
	t.Cleanup(restore)

	s := New()
	t.Cleanup(func() { _ = s.Close() })
	base, key, err := s.Ensure()
	if err != nil {
		t.Fatalf("Ensure 失败: %v", err)
	}

	reqBody := `{"model":"glm-5.3","messages":[{"role":"user","content":"ping"}]}`
	status, data := postJSON(t, base+"/v1/chat/completions", reqBody, map[string]string{
		"Authorization": "Bearer " + key,
	})
	if status != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200，body=%s", status, data)
	}
	want := `{"id":"chatcmpl-test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}]}`
	if string(data) != want {
		t.Fatalf("响应未原样直通:\n got=%s\nwant=%s", data, want)
	}

	// fake 必须真的收到请求体，且 model/messages 保持 OpenAI 形状。
	if len(gotBody) == 0 {
		t.Fatal("ChatOnce 未收到请求体")
	}
	var sent map[string]any
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("透传 body 不是合法 JSON: %v", err)
	}
	if sent["model"] != "glm-5.3" {
		t.Fatalf("model 未保留: %v", sent["model"])
	}
	if _, ok := sent["messages"].([]any); !ok {
		t.Fatalf("messages 未保留为数组: %v", sent["messages"])
	}
}

func TestAuthXApiKey(t *testing.T) {
	stubCredential(t)
	restore := SetChatOnceForTest(func(ctx context.Context, cred workbuddy.Credential, body []byte, hc *http.Client) ([]byte, error) {
		return []byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`), nil
	})
	t.Cleanup(restore)

	s := New()
	t.Cleanup(func() { _ = s.Close() })
	base, key, err := s.Ensure()
	if err != nil {
		t.Fatalf("Ensure 失败: %v", err)
	}

	status, data := postJSON(t, base+"/v1/chat/completions", `{"model":"m","messages":[]}`, map[string]string{
		"x-api-key": key,
	})
	if status != http.StatusOK {
		t.Fatalf("x-api-key 鉴权状态码 = %d，期望 200，body=%s", status, data)
	}
}

func TestChatOnceErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "网关错误 502", err: errors.New("err.workbuddy.chat_http|500"), wantStatus: http.StatusBadGateway},
		{name: "token 过期 401", err: errors.New("err.workbuddy.token_expired"), wantStatus: http.StatusUnauthorized},
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

			status, data := postJSON(t, base+"/v1/chat/completions", `{"model":"m","messages":[]}`, map[string]string{
				"Authorization": "Bearer " + key,
			})
			if status != tt.wantStatus {
				t.Fatalf("状态码 = %d，期望 %d，body=%s", status, tt.wantStatus, data)
			}
			e := decodeErrorBody(t, data)
			if !strings.Contains(e.Error.Message, tt.err.Error()) {
				t.Fatalf("错误 message 未包含底层错误: %s", data)
			}
		})
	}
}

func TestUnknownRoute404(t *testing.T) {
	s := New()
	t.Cleanup(func() { _ = s.Close() })
	base, key, err := s.Ensure()
	if err != nil {
		t.Fatalf("Ensure 失败: %v", err)
	}

	status, _ := postJSON(t, base+"/v1/unknown", `{}`, map[string]string{
		"Authorization": "Bearer " + key,
	})
	if status != http.StatusNotFound {
		t.Fatalf("未知路由状态码 = %d，期望 404", status)
	}
}
