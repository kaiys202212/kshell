package hub

import "strings"

// IsHubNoise 报告消息是否不应出现在 #h 列表：ui.hub.* 事件/文案、
// hub.welcome 欢迎词、已撤回。与微信镜像跳过口径对齐，避免刷屏。
func IsHubNoise(m Message) bool {
	if m.Status == StatusRecalled {
		return true
	}
	if strings.HasPrefix(m.Event, "ui.hub.") || strings.HasPrefix(m.Text, "ui.hub.") {
		return true
	}
	if m.Text == "hub.welcome" || strings.HasPrefix(m.Text, "hub.welcome|") {
		return true
	}
	return false
}

// VisibleMessages 从时间正序 msgs 中筛掉噪声，再取末尾最多 n 条（仍正序）。
// n<=0 或无可见消息时返回 nil。
func VisibleMessages(msgs []Message, n int) []Message {
	if n <= 0 {
		return nil
	}
	out := make([]Message, 0, n)
	for _, m := range msgs {
		if IsHubNoise(m) {
			continue
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}
