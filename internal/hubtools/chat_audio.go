package hubtools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// chatTranscribePrompt 是多模态对话模型转写语音的约束：要求逐字转写、
// 只输出文本，防止模型寒暄或自行发挥意图。
const chatTranscribePrompt = "请逐字转写这段语音的内容，只输出转写文本本身，不要任何解释、标点修饰或额外说明。"

// timeoutChatTranscribe 是单次多模态 chat 转写请求的超时。
const timeoutChatTranscribe = 90 * time.Second

// chatAudioLimit 是 chat 转写响应体的读入上限。
const chatAudioLimit = 1 << 20

// TranscribeViaChat 用多模态对话模型（OpenAI 兼容 chat/completions +
// input_audio 内容块）把 WAV 语音转成文字——供未配置专用 ASR 模型、但默认
// 对话模型支持音频输入的场景。独立 wire 结构，不影响既有纯文本 chat 链路；
// input_audio 字段名按 OpenAI 规范，个别网关有差异时真机联调再修。
func TranscribeViaChat(ctx context.Context, cfg Config, model string, wav []byte) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", fmt.Errorf("err.hubtools.chat_model_required")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return "", fmt.Errorf("err.hubtools.base_url_required")
	}
	// 端点派生与 chat 客户端同口径：BaseURL 已带 /chat/completions 尾巴则直接用。
	base := strings.TrimSuffix(strings.TrimSuffix(cfg.BaseURL, "/"), "/chat/completions")
	endpoint := base + "/chat/completions"

	reqBody := map[string]any{
		"model": model,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": chatTranscribePrompt},
					{"type": "input_audio", "input_audio": map[string]string{
						"data":   base64.StdEncoding.EncodeToString(wav),
						"format": "wav",
					}},
				},
			},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("编码请求: %w", err)
	}

	actx, cancel := context.WithTimeout(ctx, timeoutChatTranscribe)
	defer cancel()
	req, err := http.NewRequestWithContext(actx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	httpResp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求 chat 转写: %w", err)
	}
	defer httpResp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(httpResp.Body, chatAudioLimit))
	if err != nil {
		return "", fmt.Errorf("读取 chat 转写响应: %w", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("err.hubtools.chat_transcribe_failed: 状态码 %d, 响应: %.512s", httpResp.StatusCode, data)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("解析 chat 转写响应: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("chat 转写响应无 choices")
	}
	return out.Choices[0].Message.Content, nil
}
