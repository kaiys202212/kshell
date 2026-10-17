package weixin

// 桥的用户可见文案双语表（key → en/zh）。hub 命令层文案已内置中文，
// 这里只放桥自己产生的文案；后续需要新 key 时同步补两列。
type langPair struct{ En, Zh string }

var bridgeTexts = map[string]langPair{
	"msg.truncated": {"…full content in desktop app", "…完整内容见桌面端"},
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
