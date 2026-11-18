package weixin

// 桥的用户可见文案双语表（key → en/zh）。hub 命令层文案已内置中文，
// 这里只放桥自己产生的文案；后续需要新 key 时同步补两列。
type langPair struct{ En, Zh string }

var bridgeTexts = map[string]langPair{
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
	// 管理者循环中途进度（15s 节流单轮一条；%d=当前 LLM 循环轮次——
	// LoopProgress 无累计工具步数字段，一轮多工具时轮次是最低成本近似）。
	"manager.progress": {"Still working: on round %d", "仍在处理：第 %d 轮"},

	// 智能链路失败降级（2026-11-18 真机事故后新增）：路由/管理者循环都未接管
	// 时必须转待选并说明原因，绝不直投当前会话。%s = 失败原因（见下方 err.router.*）。
	"route.pick_degraded":    {"Smart routing unavailable (%s). Pick a target session by number — your message is kept:", "智能路由不可用（%s）。请回复编号选择目标会话，原文已暂存："},
	"route.pick_unavailable": {"Smart routing unavailable (%s), and no session list is available right now. Use #l to list sessions, then resend.", "智能路由不可用（%s），且当前无法列出会话。可用 #l 查看会话后重发。"},

	// 失败原因（稳定 wire key，desktop 分类后写入 ManagerOutcome.FailureKey）。
	"err.router.llm_not_ready": {"no usable model configured", "未配置可用模型"},
	"err.router.loop_failed":   {"model call failed", "模型调用失败"},
	"err.router.no_target":     {"no target recognized", "未识别到投递目标"},
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
