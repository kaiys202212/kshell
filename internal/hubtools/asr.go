package hubtools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// LocalTranscriber 是本地转写实现（desktop 注入 asrlocal），nil 表示未装配。
type LocalTranscriber func(ctx context.Context, wav []byte) (string, error)

// TranscribeOptions 是转写优先级链的输入。
type TranscribeOptions struct {
	// ASRModel 非空走专用 /audio/transcriptions 端点。
	ASRModel string
	// ChatModel 非空走多模态 chat（input_audio）转写。
	ChatModel string
	// Local 非空时作为本地引擎来源（未安装时返回 asrlocal.ErrNotInstalled）。
	Local LocalTranscriber
	// PreferCloud 为真且 ASRModel 非空时，云端排在本地之前。
	PreferCloud bool
	// Logf 可选：记录每次来源尝试的结果（成功来源、失败原因）。转写是
	// 「静默降级」的高发路径，没有来源归因时排障只能靠猜。
	Logf func(string)
}

// TranscribeAuto 是语音转写的优先级链聚合（desktop 装配唯一入口）。
// 默认顺序：本地 → 云端 ASR → 多模态 chat；PreferCloud 时云端优先。
// 单个来源失败不中断：继续下一个来源；本地未安装（ErrNotInstalled）静默跳过。
// 全部不可用/失败返回聚合错误（单来源失败原样返回，保留哨兵可归因）。
func TranscribeAuto(ctx context.Context, cfg Config, opts TranscribeOptions, wav []byte) (string, error) {
	type source struct {
		name string
		fn   func() (string, error)
	}
	var cloud, chat, local *source
	if m := strings.TrimSpace(opts.ASRModel); m != "" {
		cloud = &source{"asr", func() (string, error) { return TranscribeAudio(ctx, cfg, m, wav) }}
	}
	if m := strings.TrimSpace(opts.ChatModel); m != "" {
		chat = &source{"chat", func() (string, error) { return TranscribeViaChat(ctx, cfg, m, wav) }}
	}
	if opts.Local != nil {
		local = &source{"local", func() (string, error) { return opts.Local(ctx, wav) }}
	}

	var order []*source
	if opts.PreferCloud && cloud != nil {
		order = append(order, cloud)
	}
	if local != nil {
		order = append(order, local)
	}
	if !opts.PreferCloud || cloud == nil {
		if cloud != nil {
			order = append(order, cloud)
		}
	}
	if chat != nil {
		order = append(order, chat)
	}
	if len(order) == 0 {
		return "", fmt.Errorf("err.hubtools.voice_no_source")
	}

	logf := opts.Logf
	if logf == nil {
		logf = func(string) {}
	}
	var errs []error
	for _, s := range order {
		text, err := s.fn()
		if err == nil {
			logf("语音转写来源=" + s.name)
			return text, nil
		}
		if errors.Is(err, ErrLocalNotInstalled) {
			// 本地引擎未安装是正常形态（用户没装），不作为失败累积。
			logf("语音转写跳过 local: 未安装")
			continue
		}
		logf(fmt.Sprintf("语音转写失败 source=%s err=%v", s.name, err))
		errs = append(errs, fmt.Errorf("%s: %w", s.name, err))
	}
	if len(errs) == 1 {
		return "", errs[0]
	}
	if len(errs) == 0 {
		return "", fmt.Errorf("err.hubtools.voice_no_source")
	}
	return "", errors.Join(errs...)
}

// ErrLocalNotInstalled 与 asrlocal.ErrNotInstalled 同义：避免 hubtools 依赖
// asrlocal（本地引擎包较重），desktop 注入时做 errors.Is 透传即可。
var ErrLocalNotInstalled = errors.New("err.hubtools.local_not_installed")

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
