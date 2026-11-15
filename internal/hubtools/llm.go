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

	"github.com/yangk/kshell/internal/workbuddy"
)

// Message 是对话消息（OpenAI 兼容的最小形态）。
// 除单发文本外，支持 function calling 协议：role=tool 用 ToolCallID 回填
// 调用结果，role=assistant 用 ToolCalls 携带模型发起的调用（多轮循环归上层）。
type Message struct {
	Role       string
	Content    string
	ToolCallID string     // role=tool 时对应被答复的调用 ID
	ToolCalls  []ToolCall // role=assistant 发起工具调用时携带
}

// ToolFunc 是提供给模型的工具定义（JSON Schema 形式）。命名避开关键字 tools。
type ToolFunc struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"` // JSON schema
}

// ToolCall 是一次工具调用：arguments 保持原始 JSON 字符串，解析归循环层。
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Response 是一轮对话的结果：文本回复与（可选）模型发起的工具调用。
type Response struct {
	Text      string
	ToolCalls []ToolCall
}

// chatMessage 是 OpenAI 兼容 wire 格式的消息。
type chatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// chatRequest 是请求体。Tools 为空时 omitempty 不输出字段，旧网关零影响。
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Tools    []ToolFunc    `json:"tools,omitempty"`
}

// chatResponse 是响应体的最小解析：取 choices[0].message 的 content 与 tool_calls。
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

// Client 是 OpenAI 兼容 chat/completions 客户端。
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

// Chat 发一次对话请求并返回文本回复。
func (c *Client) Chat(ctx context.Context, msgs []Message) (Response, error) {
	return c.once(ctx, msgs, nil)
}

// ChatWithTools 发一次带工具定义的对话请求，模型可返回工具调用。
// 是否执行调用与多轮循环由上层处理，本方法只做单次请求-响应。
func (c *Client) ChatWithTools(ctx context.Context, msgs []Message, tools []ToolFunc) (Response, error) {
	return c.once(ctx, msgs, tools)
}

// wbLoadCredential / wbChatOnce 是 WorkBuddy 网关的注入点：测试可替换，
// 生产路径分别指向 internal/workbuddy 的凭据解密与 SSE 聊天适配。
var (
	wbLoadCredential = workbuddy.LoadCredential
	wbChatOnce       = workbuddy.ChatOnce
)

// once 发送一次请求并解析响应。tools 为 nil 时请求体不带 tools 字段（向后兼容）。
func (c *Client) once(ctx context.Context, msgs []Message, tools []ToolFunc) (Response, error) {
	wireMsgs := make([]chatMessage, len(msgs))
	for i, m := range msgs {
		wireMsgs[i] = chatMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID, ToolCalls: m.ToolCalls}
	}

	body, err := json.Marshal(chatRequest{Model: c.cfg.Model, Messages: wireMsgs, Tools: tools})
	if err != nil {
		return Response{}, fmt.Errorf("编码请求: %w", err)
	}

	// WorkBuddy 平台：端点/凭据由适配器内置，body 复用同一 OpenAI 形状，
	// 响应已聚合为普通 chat/completion JSON，走同一解析路径。
	if strings.TrimSpace(c.cfg.Preset) == workbuddy.PresetID {
		cred, err := wbLoadCredential()
		if err != nil {
			return Response{}, err
		}
		respBody, err := wbChatOnce(ctx, cred, body, nil)
		if err != nil {
			return Response{}, err
		}
		return parseChatResponse(respBody)
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
		return Response{}, fmt.Errorf("err.hubtools.request_failed: 状态码 %d, 响应: %.512s", httpResp.StatusCode, respBody)
	}
	return parseChatResponse(respBody)
}

// parseChatResponse 解析 OpenAI 形状响应为 Response。
func parseChatResponse(respBody []byte) (Response, error) {
	var parsed chatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Response{}, fmt.Errorf("解析响应: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return Response{}, errors.New("LLM 响应无 choices")
	}
	return Response{Text: parsed.Choices[0].Message.Content, ToolCalls: parsed.Choices[0].Message.ToolCalls}, nil
}
