package hubtools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// asrResponseLimit 是 ASR 响应体的读入上限。
const asrResponseLimit = 1 << 20

// timeoutASR 是单次语音转写请求的超时。
const timeoutASR = 60 * time.Second

// TranscribeAuto 是语音转写的优先级链聚合（desktop 装配唯一入口）：
// asrModel 非空走专用 ASR 端点；否则 chatModel 非空走多模态 chat 转写；
// 两者皆空报错（desktop 层通常先行判断哨兵，不走到这里）。
// 返回错误用 err.hubtools.voice_no_source 标识双空，便于上层归因。
func TranscribeAuto(ctx context.Context, cfg Config, asrModel, chatModel string, wav []byte) (string, error) {
	if strings.TrimSpace(asrModel) != "" {
		return TranscribeAudio(ctx, cfg, asrModel, wav)
	}
	if strings.TrimSpace(chatModel) != "" {
		return TranscribeViaChat(ctx, cfg, chatModel, wav)
	}
	return "", fmt.Errorf("err.hubtools.voice_no_source")
}

// TranscribeAudio 调 OpenAI 兼容 /audio/transcriptions 端点把 WAV 转成文字。
// 复用 chat 的平台配置：BaseURL 带 /chat/completions 尾巴时剥掉再拼端点。
// 响应缺 text 字段返回空串（语义：转写为空，由调用方决定回执）。
func TranscribeAudio(ctx context.Context, cfg Config, model string, wav []byte) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", fmt.Errorf("err.hubtools.asr_model_required")
	}
	base := strings.TrimSuffix(strings.TrimSuffix(cfg.BaseURL, "/"), "/chat/completions")
	if base == "" {
		return "", fmt.Errorf("err.hubtools.base_url_required")
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("model", model); err != nil {
		return "", err
	}
	fw, err := w.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", err
	}
	if _, err := fw.Write(wav); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	actx, cancel := context.WithTimeout(ctx, timeoutASR)
	defer cancel()
	req, err := http.NewRequestWithContext(actx, http.MethodPost, base+"/audio/transcriptions", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求转写端点: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, asrResponseLimit))
	if err != nil {
		return "", fmt.Errorf("读取转写响应: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("err.hubtools.asr_request_failed: 状态码 %d, 响应: %.512s", resp.StatusCode, data)
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("解析转写响应: %w", err)
	}
	return out.Text, nil
}
