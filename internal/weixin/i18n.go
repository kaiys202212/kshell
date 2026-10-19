package weixin

// 桥的用户可见文案双语表（key → en/zh）。hub 命令层文案已内置中文，
// 这里只放桥自己产生的文案；后续需要新 key 时同步补两列。
type langPair struct{ En, Zh string }

var bridgeTexts = map[string]langPair{
	"msg.truncated": {"…full content in desktop app", "…完整内容见桌面端"},
	// 附件转交文案：%s/%d 由调用方填充（file.received=收到的名字列表与队列总数）。
	"file.received":  {"Received %s. %d attachment(s) queued; send a text to forward them together.", "已收到 %s，共 %d 个待转交，发文字说明需求后一并转交"},
	"file.failed":    {"Failed to fetch attachment %s", "附件 %s 获取失败"},
	"file.line":      {"[Attachment] %s saved at %s", "[附件] %s 已保存至 %s"},
	"file.queued_st": {"%d attachment(s) queued for forwarding", "待转交附件 %d 个"},
	// 语音转写回执。
	"voice.unconfigured": {"Voice received but no ASR model configured (Settings → Model platform → ASR model)", "已收到语音，但未配置语音转写模型（设置 → 模型平台 → 语音转写模型）"},
	"voice.unsupported":  {"Voice received, but the current chat model does not accept audio input. Please configure a dedicated ASR model (Settings → Model platform → ASR model).", "已收到语音，但当前对话模型不支持音频输入，请在设置 → 模型平台 → 语音转写模型（asr_model）中配置专用转写模型"},
	"voice.failed":       {"Voice transcription failed: %v", "语音转写失败：%v"},
	"voice.empty":        {"Empty transcription result", "转写结果为空"},
}

// bridgeText 按语言取文案；zh-CN 用中文，其余用英文；未知 key 原样返回。
func bridgeText(lang, key string) string {
	p, ok := bridgeTexts[key]
	if !ok {
		return key
	}
	if lang == "zh-CN" {
		return p.Zh
	}
	return p.En
}
