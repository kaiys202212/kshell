package hubtools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxToolRounds 是单次 Chat 内允许的最大工具调用轮次，
// 防止模型无限循环调用工具拖垮链路。
const maxToolRounds = 6

// Executor 由上层注入：执行单个工具调用并返回结果文本。llm.go 不关心具体工具。
type Executor func(ctx context.Context, name, argsJSON string) (string, error)

// ToolDef 描述一个暴露给模型的工具，Parameters 是 JSON Schema。
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolCall 是模型发起的一次工具调用，Args 是原始 JSON 参数串（原样透传给执行器）。
type ToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"`
}

// Response 是一轮完整对话的最终结果：要么是文本，要么是待调用方执行的 tool_calls。
type Response struct {
	Text      string
	ToolCalls []ToolCall
}

// Message 是对话消息。role=tool 时 ToolCallID 回传对应 call ID；
// role=assistant 且发起调用时 ToolCalls 携带调用列表。
type Message struct {
	Role       string
	Content    string
	ToolCallID string
	ToolCalls  []ToolCall
}

// chatMessage 是 OpenAI 兼容 wire 格式的消息。
type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
}

// wireToolCall 是 assistant 消息里回传的 tool_calls wire 格式。
type wireToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function wireToolCallFunc `json:"function"`
}

type wireToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// chatRequest 是请求体；tools 为空时整个字段省略（部分网关对空数组报错）。
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Tools    []ToolDef     `json:"tools,omitempty"`
}

// chatResponse 是响应体的最小解析：只取 choices[0].message。
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content   string         `json:"content"`
			ToolCalls []wireToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

// Client 是 OpenAI 兼容 chat/completions 客户端（含工具调用循环）。
type Client struct {
	http *http.Client
	cfg  Config
}

// NewClient 创建客户端，内部 http.Client 带 60s 超时。
func NewClient(cfg Config) *Client {
	return &Client{
		http: &http.Client{Timeout: 60 * time.Second},
		cfg:  cfg,
	}
}

// endpoint 根据 BaseURL 计算请求端点：已以 /chat/completions 结尾则直接用，
// 否则去尾部斜杠后拼接，避免出现重复路径段。
func (c *Client) endpoint() string {
	if strings.HasSuffix(c.cfg.BaseURL, "/chat/completions") {
		return c.cfg.BaseURL
	}
	return strings.TrimSuffix(c.cfg.BaseURL, "/") + "/chat/completions"
}

// Chat 执行一轮完整对话：循环请求直到模型给出纯文本或触达轮次上限。
// exec 为 nil 时收到 tool_calls 直接原样返回给调用方，不进入循环。
func (c *Client) Chat(ctx context.Context, msgs []Message, tools []ToolDef) (Response, error) {
	return c.ChatWithExecutor(ctx, msgs, tools, nil)
}

// ChatWithExecutor 承载工具调用循环：响应带 tool_calls 时执行每个调用，
// 把 assistant 消息与 tool 结果追加进对话继续请求，直到纯文本或超限。
// 执行器报错不中断循环，把错误文本喂回模型，由模型组织回复。
func (c *Client) ChatWithExecutor(ctx context.Context, msgs []Message, tools []ToolDef, exec Executor) (Response, error) {
	// 对话底稿在循环中追加，拷贝一份避免污染调用方切片
	dialog := make([]Message, len(msgs))
	copy(dialog, msgs)

	for round := 0; ; round++ {
		resp, err := c.once(ctx, dialog, tools)
		if err != nil {
			return Response{}, err
		}
		// 无工具调用或未注入执行器：直接返回
		if len(resp.ToolCalls) == 0 || exec == nil {
			return resp, nil
		}
		if round >= maxToolRounds {
			return Response{}, errors.New("工具调用轮次超限")
		}

		dialog = append(dialog, Message{Role: "assistant", ToolCalls: resp.ToolCalls})
		for _, tc := range resp.ToolCalls {
			result, err := exec(ctx, tc.Name, tc.Args)
			if err != nil {
				result = "tool error: " + err.Error()
			}
			dialog = append(dialog, Message{Role: "tool", Content: result, ToolCallID: tc.ID})
		}
	}
}

// once 发送一次请求并解析响应。
func (c *Client) once(ctx context.Context, msgs []Message, tools []ToolDef) (Response, error) {
	wireMsgs := make([]chatMessage, len(msgs))
	for i, m := range msgs {
		wm := chatMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			wm.ToolCalls = append(wm.ToolCalls, wireToolCall{
				ID:       tc.ID,
				Type:     "function",
				Function: wireToolCallFunc{Name: tc.Name, Arguments: tc.Args},
			})
		}
		wireMsgs[i] = wm
	}

	body, err := json.Marshal(chatRequest{Model: c.cfg.Model, Messages: wireMsgs, Tools: tools})
	if err != nil {
		return Response{}, fmt.Errorf("编码请求: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.http.Do(req)
	if err != nil {
		return Response{}, err // 网络错误直接透传
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, 1<<20))
	if err != nil {
		return Response{}, fmt.Errorf("读取响应: %w", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("LLM 请求失败: 状态码 %d, 响应: %.512s", httpResp.StatusCode, respBody)
	}

	var parsed chatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Response{}, fmt.Errorf("解析响应: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return Response{}, errors.New("LLM 响应无 choices")
	}

	msg := parsed.Choices[0].Message
	out := Response{Text: msg.Content}
	for _, tc := range msg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Args: tc.Function.Arguments})
	}
	return out, nil
}
