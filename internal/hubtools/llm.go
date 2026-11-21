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

// ToolFunc 是提供给模型的工具定义。Parameters 必须是完整 JSON schema：
// 无 omitempty，nil 会 wire 输出 null，严格网关可能拒收。命名避开关键字 tools。
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

// wireTool / wireToolCall 是 OpenAI 严格的嵌套 wire 形状。内部 ToolFunc/ToolCall
// 保持扁平，嵌套映射集中在此，调用方无需关心协议细节。
type wireTool struct {
	Type     string   `json:"type"`
	Function ToolFunc `json:"function"`
}

type wireToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// chatMessage 是 OpenAI 兼容 wire 格式的消息。
type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
}

// chatRequest 是请求体。Tools 为空时 omitempty 不输出字段，旧网关零影响。
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Tools    []wireTool    `json:"tools,omitempty"`
}

// chatResponse 是响应体的最小解析：取 choices[0].message 的 content 与 tool_calls。
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content   string         `json:"content"`
			ToolCalls []wireToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

// toWireTools 把扁平工具定义映射为 OpenAI 嵌套 wire 形状。
func toWireTools(tools []ToolFunc) []wireTool {
	out := make([]wireTool, len(tools))
	for i, t := range tools {
		out[i] = wireTool{Type: "function", Function: t}
	}
	return out
}

// toWireToolCalls 把扁平调用映射为嵌套 wire 形状（assistant 消息用）。
func toWireToolCalls(tcs []ToolCall) []wireToolCall {
	out := make([]wireToolCall, len(tcs))
	for i, tc := range tcs {
		out[i].ID = tc.ID
		out[i].Type = "function"
		out[i].Function.Name = tc.Name
		out[i].Function.Arguments = tc.Arguments
	}
	return out
}

// fromWireToolCalls 把嵌套 wire 形状映射回扁平调用；不按 type 分支，
// 非 function 条目（理论上不出现）自然退化为空名调用而非报错。
// 空/nil 输入返回 nil 而非空切片，避免循环层用 != nil 判「有无调用」误判。
func fromWireToolCalls(ws []wireToolCall) []ToolCall {
	if len(ws) == 0 {
		return nil
	}
	out := make([]ToolCall, len(ws))
	for i, w := range ws {
		out[i] = ToolCall{ID: w.ID, Name: w.Function.Name, Arguments: w.Function.Arguments}
	}
	return out
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
		wireMsgs[i] = chatMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		if len(m.ToolCalls) > 0 {
			wireMsgs[i].ToolCalls = toWireToolCalls(m.ToolCalls)
		}
	}

	var wireTools []wireTool
	if len(tools) > 0 {
		wireTools = toWireTools(tools)
	}
	body, err := json.Marshal(chatRequest{Model: c.cfg.Model, Messages: wireMsgs, Tools: wireTools})
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
	if c.cfg.SessionID != "" {
		req.Header.Set("x-opencode-session", c.cfg.SessionID)
	}

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
	return Response{Text: parsed.Choices[0].Message.Content, ToolCalls: fromWireToolCalls(parsed.Choices[0].Message.ToolCalls)}, nil
}
