package hubtools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// chatTranscribePrompt 是多模态对话模型转写语音的约束：要求逐字转写、
// 只输出文本，防止模型寒暄或自行发挥意图。
const chatTranscribePrompt = "请逐字转写这段语音的内容，只输出转写文本本身，不要任何解释或额外说明，原样保留语音内容。"

// timeoutChatTranscribe 是单次多模态 chat 转写请求的超时。取 90s 对齐 hub
// 工具循环上限（hubPromptTimeout）而非 ASR 的 60s：音频 base64 后请求体大、
// 多模态推理慢，勿擅自统一成 60s。
const timeoutChatTranscribe = 90 * time.Second

// ErrChatAudioUnsupported 表示默认对话模型不支持音频输入（chat 转写回落链的
// 常态而非异常）：主流 coding 模型（Claude 全系 / GPT 标准版 / GLM / DeepSeek /
// 豆包 coding 系列）均不接受 input_audio，网关以 400/404/422 拒绝。上层据此
// 给出「去配置 asr_model」的行动指引，而不是透传网关原始报错。
var ErrChatAudioUnsupported = errors.New("err.hubtools.chat_audio_unsupported")

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
	if strings.TrimSpace(base) == "" {
		return "", fmt.Errorf("err.hubtools.base_url_required")
	}
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
		if isAudioUnsupportedStatus(httpResp.StatusCode) && isAudioUnsupportedBody(string(data)) {
			// 保留原始报错于日志可读的包装里，但 errors.Is 可归因到哨兵。
			return "", fmt.Errorf("%w: 状态码 %d, 响应: %.512s", ErrChatAudioUnsupported, httpResp.StatusCode, data)
		}
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
	content := out.Choices[0].Message.Content
	if isTranscriptRejection(content) {
		// 200 + 拒答：网关吞掉了音频，模型只能回「没收到语音」。绝不能把
		// 这段文本当转写结果投递（2026-11-20 真机缺陷），归因为音频不支持。
		return "", fmt.Errorf("%w: 模型返回拒答而非转写（%q）",
			ErrChatAudioUnsupported, headRunes(strings.TrimSpace(content), 80))
	}
	return content, nil
}

// maxRejectionRunes 是拒答识别的长度上限：真机拒答都很短（数十字符），
// 长文本即使命中关键词也按正常转写放行，避免把长篇陈述误判成拒绝。
const maxRejectionRunes = 160

// rejectionPhrases 是「模型声称没收到/听不了音频」的高置信短语。只收录
// 指向「输入缺失、无法转写」的表述，不收裸「上传/重新发送」这类正常口语
// （用户口述任务时完全可能说「帮我重新上传一下」）。
var rejectionPhrases = []string{
	"没有收到语音", "没有收到音频", "未收到语音", "未收到音频",
	"没有提供语音", "没有提供音频", "还没有提供语音", "你还没有提供",
	"没有语音内容", "没有音频内容",
	"看不到任何音频", "看不到音频附件", "看不到语音", "无法看到音频",
	"没有附带音频", "没有附带语音", "未附带音频", "没有附带任何音频",
	"请把语音文件发上来", "把语音文件发上来", "请重新上传一次", "请重新发送语音",
	// 英文（统一小写比较）。
	"no audio", "no voice", "no speech",
	"didn't receive", "did not receive", "haven't received", "have not received",
	"cannot transcribe", "can't transcribe", "unable to transcribe",
	"please upload the file", "please resend",
}

// rejectionVoiceWords 是「无法转写」类泛化表述的限定词：单独出现可能只是
// 转述内容（如「讨论中出现了『无法转写』这个词」），必须同时提到语音/音频
// 等音频载体时才判定为拒答。
var rejectionVoiceWords = []string{"语音", "音频", "附件", "这段", "此", "内容"}

// rejectionAbsence 是「没收到/没提供」类否定词，与音频载体词共现即判为拒答
// （真机 200 拒答的真实措辞每次略有差异，靠固定短语穷举不可靠）。
// 特意不收「没看到」：用户口述「我没看到你的语音消息」是正常指示，不是拒答。
var rejectionAbsence = []string{"没有收到", "未收到", "还没有收到", "没有提供", "还没有提供", "没有附带", "未附带"}

// rejectionAudioWords 是音频载体词（中英），供否定词共现规则使用。
var rejectionAudioWords = []string{"语音", "音频", "audio", "voice"}

// isTranscriptRejection 判断多模态 chat 返回的文本是不是「模型拒答」而非
// 转写结果。判定条件：短文本（≤maxRejectionRunes）+ 命中拒答短语（或
// 「无法转写」与音频载体词共现）。
func isTranscriptRejection(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if len([]rune(t)) > maxRejectionRunes {
		return false
	}
	lower := strings.ToLower(t)
	for _, p := range rejectionPhrases {
		if strings.Contains(lower, p) {
			return true
		}
	}
	if strings.Contains(lower, "无法转写") || strings.Contains(lower, "无法转录") {
		for _, w := range rejectionVoiceWords {
			if strings.Contains(lower, w) {
				return true
			}
		}
	}
	// 否定词 + 音频载体：覆盖「我还没有收到需要转写的语音内容…没有附带
	// 音频文件」这类真实措辞变体。
	hasAbsence := false
	for _, a := range rejectionAbsence {
		if strings.Contains(lower, a) {
			hasAbsence = true
			break
		}
	}
	if hasAbsence {
		for _, w := range rejectionAudioWords {
			if strings.Contains(lower, w) {
				return true
			}
		}
	}
	return false
}

// headRunes 取文本前 n 个字符（按字符而非字节，中文不被截半），用于错误与
// 日志前缀。
func headRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// isAudioUnsupportedStatus 判定状态码是否可能是「模型不支持音频」：主流网关
// 对不认识的 input_audio 字段报 400，个别报 404/422，其余状态码不猜。
func isAudioUnsupportedStatus(code int) bool {
	return code == http.StatusBadRequest || code == http.StatusNotFound || code == http.StatusUnprocessableEntity
}

// isAudioUnsupportedBody 判定响应体是否指向音频输入被拒：各家报错都带
// input_audio 字段名或明确的 audio not supported 文案，双条件覆盖大小写差异。
func isAudioUnsupportedBody(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "input_audio") ||
		(strings.Contains(lower, "audio") && strings.Contains(lower, "not supported"))
}
