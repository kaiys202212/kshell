package workbuddy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestPrepareChatBody(t *testing.T) {
	in := `{"model":"hy3","messages":[{"role":"developer","content":"dev"},{"role":"user","content":"hi"}]}`
	out, err := prepareChatBody([]byte(in))
	if err != nil {
		t.Fatalf("预处理失败: %v", err)
	}
	var body struct {
		Stream   bool `json:"stream"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatal(err)
	}
	if !body.Stream {
		t.Fatal("必须强制 stream:true")
	}
	// developer 在头部：转 system 后即为 system 头，无需再补兜底。
	if len(body.Messages) != 2 {
		t.Fatalf("developer 转换后应 2 条: %d", len(body.Messages))
	}
	if body.Messages[0].Role != "system" {
		t.Fatalf("首条应为 system: %q", body.Messages[0].Role)
	}
	if body.Messages[1].Role != "user" {
		t.Fatalf("developer 应转为 system: %+v", body.Messages)
	}
}

func TestPrepareChatBodyPrependsSystem(t *testing.T) {
	in := `{"model":"hy3","messages":[{"role":"user","content":"hi"}]}`
	out, err := prepareChatBody([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Messages) != 2 || body.Messages[0].Role != "system" ||
		body.Messages[0].Content != fallbackSystemPrompt || body.Messages[1].Role != "user" {
		t.Fatalf("应前置兜底 system: %+v", body.Messages)
	}
}

func TestChatOnceHeadersAndAggregation(t *testing.T) {
	sse := "data: " + `{"choices":[{"delta":{"content":"he"}}]}` + "\n\n" +
		"data: " + `{"choices":[{"delta":{"content":"llo"}}]}` + "\n\n" +
		"data: [DONE]\n\n"
	var got ConversationHeaders
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = captureConversationHeaders(r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()

	body, err := prepareChatBody([]byte(`{"model":"hy3","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	hc := &http.Client{Transport: rewriteTransport{base: srv.URL}}
	out, err := ChatOnce(context.Background(), Credential{
		AccessToken: "tok", UID: "uid-1", Domain: "www.codebuddy.cn",
	}, body, hc)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	// 头部断言
	if got.userAgent != clientUA {
		t.Fatalf("chat UA 应为 CLI 频道: %q", got.userAgent)
	}
	if got.userID != "uid-1" {
		t.Fatalf("X-User-Id 不符: %q", got.userID)
	}
	if got.noEnterprise != "1" {
		t.Fatalf("X-No-Enterprise-Id 不符: %q", got.noEnterprise)
	}
	if got.product != "WorkBuddy" || got.agentPurpose != "conversation" {
		t.Fatalf("归属头不符: %+v", got)
	}
	hex32 := regexp.MustCompile(`^[0-9a-f]{32}$`)
	if !hex32.MatchString(got.conversationID) {
		t.Fatalf("会话 ID 应为 32 位小写 hex: %q", got.conversationID)
	}
	if got.rootRequestID != got.requestID {
		t.Fatalf("X-Root-Request-ID 应与 X-Conversation-Request-ID 相同")
	}
	if got.messageID == got.conversationID {
		t.Fatal("消息 ID 与会话 ID 不应相同")
	}
	if got.origin != "https://www.codebuddy.cn" {
		t.Fatalf("Origin 不符: %q", got.origin)
	}
	// 聚合断言
	var resp struct {
		Choices []struct {
			Message struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("聚合响应非法: %v", err)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "hello" {
		t.Fatalf("内容聚合不符: %s", out)
	}
}

func TestChatOnceToolCallAggregation(t *testing.T) {
	sse := "data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":""}}]}}]}` + "\n\n" +
		"data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\""}}]}}]}` + "\n\n" +
		"data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"a.txt\"}"}}]},"finish_reason":null}]}` + "\n\n" +
		"data: " + `{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()

	body, _ := prepareChatBody([]byte(`{"model":"hy3","messages":[{"role":"user","content":"read it"}]}`))
	hc := &http.Client{Transport: rewriteTransport{base: srv.URL}}
	out, err := ChatOnce(context.Background(), Credential{AccessToken: "t", Domain: "www.codebuddy.cn"}, body, hc)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	var resp struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatal(err)
	}
	tcs := resp.Choices[0].Message.ToolCalls
	if len(tcs) != 1 {
		t.Fatalf("应聚合为 1 个工具调用: %s", out)
	}
	if tcs[0].ID != "call_1" || tcs[0].Function.Name != "read_file" {
		t.Fatalf("工具调用头不符: %+v", tcs[0])
	}
	if tcs[0].Function.Arguments != `{"path":"a.txt"}` {
		t.Fatalf("参数分片未拼接: %q", tcs[0].Function.Arguments)
	}
	if resp.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("finish_reason 不符: %q", resp.Choices[0].FinishReason)
	}
}

func TestChatOnceChannelReject(t *testing.T) {
	sse := "data: " + `{"code":11128,"msg":"Illegal API invocation from an unapproved channel"}` + "\n\n" +
		"data: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()
	body, _ := prepareChatBody([]byte(`{"model":"hy3","messages":[{"role":"user","content":"x"}]}`))
	hc := &http.Client{Transport: rewriteTransport{base: srv.URL}}
	_, err := ChatOnce(context.Background(), Credential{AccessToken: "t", Domain: "www.codebuddy.cn"}, body, hc)
	if err == nil || !strings.Contains(err.Error(), "err.workbuddy.channel_reject") {
		t.Fatalf("应报渠道拒绝: %v", err)
	}
}

func TestChatOnceQueued(t *testing.T) {
	sse := "data: " + `{"code":6020,"msg":"排队中","position":8}` + "\n\n" +
		"data: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()
	body, _ := prepareChatBody([]byte(`{"model":"hy3","messages":[{"role":"user","content":"x"}]}`))
	hc := &http.Client{Transport: rewriteTransport{base: srv.URL}}
	_, err := ChatOnce(context.Background(), Credential{AccessToken: "t", Domain: "www.codebuddy.cn"}, body, hc)
	if err == nil || !strings.Contains(err.Error(), "err.workbuddy.queued") {
		t.Fatalf("应报排队: %v", err)
	}
}

func TestChatOnceExpiredToken(t *testing.T) {
	cred := Credential{AccessToken: "t", ExpiresAtMs: 1}
	if _, err := ChatOnce(context.Background(), cred, []byte(`{}`), nil); err == nil ||
		!strings.Contains(err.Error(), "err.workbuddy.token_expired") {
		t.Fatalf("过期 token 应提前报错: %v", err)
	}
}

// TestAggregateSSEUsage 验证用量从流式 chunk 聚合进非流式响应：
// 顶层与 choices 内两种位置都要兼容；取不到时省略字段。
func TestAggregateSSEUsage(t *testing.T) {
	tests := []struct {
		name           string
		sse            string
		wantUsage      bool
		wantPrompt     int
		wantCompletion int
	}{
		{
			name: "chunk 顶层 usage",
			sse: "data: " + `{"choices":[{"delta":{"content":"hi"}}]}` + "\n\n" +
				"data: " + `{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":3}}` + "\n\n" +
				"data: [DONE]\n\n",
			wantUsage:      true,
			wantPrompt:     12,
			wantCompletion: 3,
		},
		{
			name: "choices 内 usage",
			sse: "data: " + `{"choices":[{"delta":{"content":"hi"},"usage":{"prompt_tokens":7,"completion_tokens":2}}]}` + "\n\n" +
				"data: [DONE]\n\n",
			wantUsage:      true,
			wantPrompt:     7,
			wantCompletion: 2,
		},
		{
			name: "无 usage 时省略",
			sse: "data: " + `{"choices":[{"delta":{"content":"hi"},"finish_reason":"stop"}]}` + "\n\n" +
				"data: [DONE]\n\n",
			wantUsage: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := aggregateSSE(strings.NewReader(tt.sse))
			if err != nil {
				t.Fatalf("聚合失败: %v", err)
			}
			var resp struct {
				Usage *struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal(out, &resp); err != nil {
				t.Fatalf("聚合响应非法: %v", err)
			}
			if !tt.wantUsage {
				if resp.Usage != nil {
					t.Fatalf("无 usage 时应省略字段: %s", out)
				}
				return
			}
			if resp.Usage == nil {
				t.Fatalf("缺少 usage 字段: %s", out)
			}
			if resp.Usage.PromptTokens != tt.wantPrompt || resp.Usage.CompletionTokens != tt.wantCompletion {
				t.Fatalf("usage 映射不符: %+v", resp.Usage)
			}
		})
	}
}
