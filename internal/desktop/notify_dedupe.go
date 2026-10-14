package desktop

import (
	"sync"
	"time"

	"github.com/yangk/kshell/internal/agenthook"
)

// notifyDedupWindow 是完成/出错类通知的去重时间窗：claude 等等待确认期间的
// Notification hook 会连续重发同一条消息，窗口内重复事件直接丢弃，
// 避免气泡与提示音在一秒内连刷。
const notifyDedupWindow = 2 * time.Second

// notifyDedupWaitWindow 是等待确认类通知（Notification / attention）的去重时间窗。
// claude/codebuddy 等待期间会周期性重发同一条通知（如 60s idle 提醒），间隔远超
// 短窗，此前每次都穿透导致气泡与提示音反复出现；拉长到 30s 拦住同一消息的
// 周期性重发，过期后仍放行一次（用户确认的节奏）。完成/出错类不放宽：同一会话
// 30s 内连续两轮完成时 summary 为空、key 相同，长窗会吞掉合法的第二条完成通知。
const notifyDedupWaitWindow = 30 * time.Second

// notifyDeduper 按 (termKey, event, summary) 记录最近一次放行时刻，
// 窗口内的重复 payload 判定为重复。并发安全：收件箱轮询与 OSC 命中
// 分属不同 goroutine，都可能同时到达 handleAgentNotify。
type notifyDeduper struct {
	mu         sync.Mutex
	seen       map[string]time.Time
	now        func() time.Time // 测试注入用
	window     time.Duration    // 完成/出错类窗口
	waitWindow time.Duration    // 等待确认类窗口
}

func newNotifyDeduper(window, waitWindow time.Duration) *notifyDeduper {
	return &notifyDeduper{seen: make(map[string]time.Time), now: time.Now, window: window, waitWindow: waitWindow}
}

// isWaitingEvent 报告事件是否属「等待确认」语义：claude/codebuddy hooks 的
// Notification 与 OSC 扫描的 attention 同义。
func isWaitingEvent(event string) bool {
	return event == "Notification" || event == "attention"
}

// windowFor 按事件语义选窗：等待确认类用长窗，其余保持短窗。
func (d *notifyDeduper) windowFor(event string) time.Duration {
	if isWaitingEvent(event) {
		return d.waitWindow
	}
	return d.window
}

// dedupeKey 用 \x00 拼接判定字段：三类字段都不可能含该字节，无需转义。
func dedupeKey(p agenthook.Payload) string {
	return p.TermKey + "\x00" + p.Event + "\x00" + p.Summary
}

// notifyDedupFor 返回本实例的去重器（懒创建）。挂实例而非包级：测试各自
// new App 天然隔离，不因共享状态在同一窗口期内互相拦截。
func (a *App) notifyDedupFor() *notifyDeduper {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.notifyDedup == nil {
		a.notifyDedup = newNotifyDeduper(notifyDedupWindow, notifyDedupWaitWindow)
	}
	return a.notifyDedup
}

// notifyDedupAllow 是去重器的放行判定薄包装：hook/OSC 通道（handleAgentNotify）
// 与 ACP chat 通道（emitChatNotify）共用同一实例，避免同一条通知从两条通道
// 重复弹出。
func (a *App) notifyDedupAllow(p agenthook.Payload) bool {
	return a.notifyDedupFor().allow(p)
}

// allow 报告该 payload 是否应放行：窗口内重复返回 false，否则记录并返回 true。
// 窗口按事件语义分档（见 windowFor）。每次调用顺手清理过期记录，表大小受窗口内事件数约束。
func (d *notifyDeduper) allow(p agenthook.Payload) bool {
	now := d.now()
	win := d.windowFor(p.Event)
	cutoff := now.Add(-win)
	d.mu.Lock()
	defer d.mu.Unlock()
	for k, ts := range d.seen {
		if ts.Before(cutoff) {
			delete(d.seen, k)
		}
	}
	key := dedupeKey(p)
	// 窗口按左闭右开 [t, t+window)：恰好到达窗界视为过期，重新放行
	if ts, ok := d.seen[key]; ok && ts.After(cutoff) {
		return false
	}
	d.seen[key] = now
	return true
}
