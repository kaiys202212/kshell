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
// 本包只做「一次请求 → 一次文本回复」：工具调用循环（Executor/ToolDef/
// maxToolRounds）已随 AgentHub 工具化路由下线删除——精确目标直投 ACP，
// 无目标自由文本只走 RouteInput 的单发 JSON 决策，不需要多轮工具会话。
type Message struct {
	Role    string
	Content string
}

// Response 是一轮对话的结果文本。
type Response struct {
	Text string
}

// chatMessage 是 OpenAI 兼容 wire 格式的消息。
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest 是请求体。
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

// chatResponse 是响应体的最小解析：只取 choices[0].message.content。
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
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
	return c.once(ctx, msgs)
}

// wbLoadCredential / wbChatOnce 是 WorkBuddy 网关的注入点：测试可替换，
// 生产路径分别指向 internal/workbuddy 的凭据解密与 SSE 聊天适配。
var (
	wbLoadCredential = workbuddy.LoadCredential
	wbChatOnce       = workbuddy.ChatOnce
)

// once 发送一次请求并解析响应。
func (c *Client) once(ctx context.Context, msgs []Message) (Response, error) {
	wireMsgs := make([]chatMessage, len(msgs))
	for i, m := range msgs {
		wireMsgs[i] = chatMessage{Role: m.Role, Content: m.Content}
	}

	body, err := json.Marshal(chatRequest{Model: c.cfg.Model, Messages: wireMsgs})
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
	return Response{Text: parsed.Choices[0].Message.Content}, nil
}
