package workbuddy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// clientUA 是 chat 频道的 CLI UA（照抄官方 CLI 形状；版本惰性，频道即身份）。
const clientUA = "CLI/2.63.2 CodeBuddy/2.63.2"

// fallbackSystemPrompt 是请求缺 system 头时前置的兜底系统提示：
// 两个区的网关都要求对话以 system 开头，国际区强制校验。
const fallbackSystemPrompt = "You are a helpful assistant."

// ConversationHeaders 汇集一次 chat 请求的会话身份头，测试用。
type ConversationHeaders struct {
	userAgent       string
	userID          string
	noEnterprise    string
	product         string
	agentPurpose    string
	origin          string
	conversationID  string
	requestID       string
	rootRequestID   string
	messageID       string
	acceptLanguage  string
	codebuddyReq    string
	noDepartment    string
	ideName         string
	conversationReq string
}

func captureConversationHeaders(r *http.Request) ConversationHeaders {
	return ConversationHeaders{
		userAgent:       r.Header.Get("User-Agent"),
		userID:          r.Header.Get("X-User-Id"),
		noEnterprise:    r.Header.Get("X-No-Enterprise-Id"),
		product:         r.Header.Get("X-Product"),
		agentPurpose:    r.Header.Get("X-Agent-Purpose"),
		origin:          r.Header.Get("Origin"),
		conversationID:  r.Header.Get("X-Conversation-ID"),
		requestID:       r.Header.Get("X-Conversation-Request-ID"),
		rootRequestID:   r.Header.Get("X-Root-Request-ID"),
		messageID:       r.Header.Get("X-Conversation-Message-ID"),
		acceptLanguage:  r.Header.Get("Accept-Language"),
		codebuddyReq:    r.Header.Get("X-CodeBuddy-Request"),
		noDepartment:    r.Header.Get("X-No-Department-Info"),
		ideName:         r.Header.Get("X-IDE-Name"),
		conversationReq: r.Header.Get("X-Conversation-Request-ID"),
	}
}

// uuidv7Hex 生成 32 位无连字符小写 UUIDv7：官方客户端的请求 id 形状，
// 用量页以该 id 展示，格式不一致会被上游识别为两种客户端。
func uuidv7Hex() string {
	var b [16]byte
	ms := uint64(time.Now().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	b[6] = 0x70 | (b[6] & 0x0f) // version 7
	_, _ = rand.Read(b[8:])
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return hex.EncodeToString(b[:])
}

// prepareChatBody 预处理 OpenAI 形状请求体：强制 stream:true（网关只回 SSE）、
// developer 角色转 system、缺 system 头时前置兜底提示。
func prepareChatBody(body []byte) ([]byte, error) {
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, fmt.Errorf("err.workbuddy.bad_request|%v", err)
	}
	obj["stream"] = true
	if msgs, ok := obj["messages"].([]any); ok {
		for i, mv := range msgs {
			m, ok := mv.(map[string]any)
			if !ok {
				continue
			}
			if r, _ := m["role"].(string); r == "developer" {
				m["role"] = "system"
				msgs[i] = m
			}
		}
		if len(msgs) > 0 {
			head, _ := msgs[0].(map[string]any)
			role := ""
			if head != nil {
				role, _ = head["role"].(string)
			}
			if !strings.EqualFold(role, "system") {
				msgs = append([]any{map[string]any{"role": "system", "content": fallbackSystemPrompt}}, msgs...)
				obj["messages"] = msgs
			}
		}
	}
	return json.Marshal(obj)
}

// chatHeaders 组装 balanced 档的 chat 请求头（不含 Authorization）。
func chatHeaders(cred Credential) (map[string]string, error) {
	convID, reqID, msgID := uuidv7Hex(), uuidv7Hex(), uuidv7Hex()
	lang := "zh-CN"
	if cred.Region() == "global" {
		lang = "en-US"
	}
	h := map[string]string{
		"Accept":                    "application/json, text/plain, */*",
		"X-Requested-With":          "XMLHttpRequest",
		"Origin":                    originBase(cred),
		"Referer":                   originBase(cred) + "/",
		"User-Agent":                clientUA,
		"X-CodeBuddy-Request":       "1",
		"Accept-Language":           lang,
		"X-Agent-Purpose":           "conversation",
		"X-IDE-Name":                "WorkBuddy",
		"X-IDE-Type":                "WorkBuddy",
		"X-IDE-Version":             "5.8.3",
		"X-Product":                 "WorkBuddy",
		"X-Conversation-ID":         convID,
		"X-Conversation-Request-ID": reqID,
		"X-Root-Request-ID":         reqID,
		"X-Conversation-Message-ID": msgID,
		"X-Request-ID":              msgID,
		"X-No-Enterprise-Id":        "1",
	}
	if cred.UID == "" {
		h["X-No-User-Id"] = "1"
	} else {
		h["X-User-Id"] = cred.UID
	}
	if cred.Domain == "" {
		h["X-No-Department-Info"] = "1"
	} else {
		h["X-Domain"] = cred.Domain
	}
	return h, nil
}

// ChatOnce 把 OpenAI 形状请求体经 WorkBuddy 网关执行一次，SSE 聚合为
// 非流式 OpenAI 形状响应（choices[].message{content,tool_calls}），供
// hubtools 现有解析器直接消费。设备头按「盐缺席整对不发」的约定省略。
func ChatOnce(ctx context.Context, cred Credential, body []byte, hc *http.Client) ([]byte, error) {
	if cred.expired(time.Now()) {
		return nil, errors.New("err.workbuddy.token_expired")
	}
	prepared, err := prepareChatBody(body)
	if err != nil {
		return nil, err
	}
	headers, err := chatHeaders(cred)
	if err != nil {
		return nil, err
	}
	if hc == nil {
		hc = &http.Client{Timeout: 120 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatGatewayBase(cred)+"/v2/chat/completions", bytes.NewReader(prepared))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cred.AccessToken)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("err.workbuddy.chat_unreachable|%v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("err.workbuddy.chat_http|%d", resp.StatusCode)
	}
	return aggregateSSE(resp.Body)
}

// usageTokens 是网关流式 chunk 里可能携带的用量；OpenAI 惯例是只在末尾
// 带 usage 的 chunk 里给出一次。
type usageTokens struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// sseAgg 聚合一次 SSE 会话的增量。
type sseAgg struct {
	content      strings.Builder
	finishReason string
	toolOrders   []int
	toolCalls    map[int]*sseToolCall
	usage        *usageTokens
}

type sseToolCall struct {
	ID        string
	Type      string
	Name      string
	Arguments strings.Builder
}

func (a *sseAgg) result() ([]byte, error) {
	msg := map[string]any{"role": "assistant", "content": a.content.String()}
	if len(a.toolCalls) > 0 {
		calls := make([]map[string]any, 0, len(a.toolOrders))
		for _, idx := range a.toolOrders {
			tc := a.toolCalls[idx]
			args := tc.Arguments.String()
			if args == "" {
				args = "{}"
			}
			calls = append(calls, map[string]any{
				"id": tc.ID,
				"type": func() string {
					if tc.Type != "" {
						return tc.Type
					}
					return "function"
				}(),
				"function": map[string]string{"name": tc.Name, "arguments": args},
			})
		}
		msg["tool_calls"] = calls
	}
	if a.finishReason == "" {
		if len(a.toolCalls) > 0 {
			a.finishReason = "tool_calls"
		} else {
			a.finishReason = "stop"
		}
	}
	out := map[string]any{
		"id":      "workbuddy-" + uuidv7Hex(),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "workbuddy",
		"choices": []any{map[string]any{
			"index":         0,
			"message":       msg,
			"finish_reason": a.finishReason,
		}},
	}
	// 取不到 usage 时省略字段（OpenAI 非流式响应惯例），由调用方按 0 处理。
	if a.usage != nil {
		out["usage"] = map[string]any{
			"prompt_tokens":     a.usage.PromptTokens,
			"completion_tokens": a.usage.CompletionTokens,
		}
	}
	return json.Marshal(out)
}

// aggregateSSE 逐行读 SSE：data 帧为 OpenAI 增量 chunk 则聚合，
// 为错误信封（code != 0）则映射稳定错误 key；[DONE] 结束。
func aggregateSSE(r io.Reader) ([]byte, error) {
	agg := &sseAgg{toolCalls: map[int]*sseToolCall{}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			if payload == "[DONE]" {
				break
			}
			continue
		}
		var chunk struct {
			Code     *int `json:"code"`
			Position *int `json:"position"`
			// usage 可能出现在 chunk 顶层（OpenAI 标准位置），也可能被网关
			// 塞进 choices 内；两处都尝试解析，取到为准。
			Usage   *usageTokens `json:"usage"`
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    *int   `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string      `json:"finish_reason"`
				Usage        *usageTokens `json:"usage"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue // 忽略无法解析的心跳/注释帧
		}
		if chunk.Code != nil && *chunk.Code != 0 {
			return nil, mapGatewayError(*chunk.Code)
		}
		if chunk.Usage != nil {
			agg.usage = chunk.Usage
		}
		for _, ch := range chunk.Choices {
			if ch.Usage != nil {
				agg.usage = ch.Usage
			}
			if ch.Delta.Content != "" {
				agg.content.WriteString(ch.Delta.Content)
			}
			for _, tc := range ch.Delta.ToolCalls {
				idx := 0
				if tc.Index != nil {
					idx = *tc.Index
				}
				cur, ok := agg.toolCalls[idx]
				if !ok {
					cur = &sseToolCall{}
					agg.toolCalls[idx] = cur
					agg.toolOrders = append(agg.toolOrders, idx)
				}
				if tc.ID != "" {
					cur.ID = tc.ID
				}
				if tc.Type != "" {
					cur.Type = tc.Type
				}
				if tc.Function.Name != "" {
					cur.Name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					cur.Arguments.WriteString(tc.Function.Arguments)
				}
			}
			if ch.FinishReason != nil && *ch.FinishReason != "" {
				agg.finishReason = *ch.FinishReason
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("err.workbuddy.chat_stream|%v", err)
	}
	return agg.result()
}

// mapGatewayError 把网关业务错误码映射为稳定错误 key。
func mapGatewayError(code int) error {
	switch code {
	case 11128:
		return errors.New("err.workbuddy.channel_reject|11128 网关渠道校验拒绝")
	case 6020, 6021:
		return fmt.Errorf("err.workbuddy.queued|%d", code)
	default:
		return fmt.Errorf("err.workbuddy.gateway_code|%d", code)
	}
}
