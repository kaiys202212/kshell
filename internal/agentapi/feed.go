package agentapi

import "sync"

// Feed 是会话事件的多播源：desktop 在 chat.Manager 的更新/权限回调链上
// 转发一帧进来，API.Watch 的订阅者按会话领取。
//
// 投递策略是非阻塞：订阅通道缓冲满时丢弃该帧（计数）而不阻塞回调链——
// 回调链是聊天主路径，绝不能被慢消费者拖住。盯输出的消费方用缓冲通道 +
// 持续取用即可，丢帧兜底靠 Messages/LastFullOutput 的拉取原语。
type Feed struct {
	mu       sync.Mutex
	subs     map[string]map[chan Event]struct{} // chatID → 订阅集合
	dropped  map[chan Event]int                 // 各订阅的累计丢帧数
	pending  map[string][]Permission            // chatID → 未处理权限请求（cap pendingCap）
}

// pendingCap 是单个会话记录的权限请求数上限（防旧请求无限堆积）。
const pendingCap = 10

// NewFeed 构造空的多播源。
func NewFeed() *Feed {
	return &Feed{
		subs:    map[string]map[chan Event]struct{}{},
		dropped: map[chan Event]int{},
		pending: map[string][]Permission{},
	}
}

// Publish 广播一帧事件到该会话的所有订阅者，并维护 pending 权限快照。
// sessionID 为空时仅记权限（权限事件在链头归一前可能只有内部 ID，上层保证
// 传入归一后的链头 ID）。
func (f *Feed) Publish(e Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e.Kind == EventPermission {
		p := Permission{ChatID: e.SessionID, RequestID: e.Type, Title: e.Text, Ts: e.Ts}
		list := append(f.pending[e.SessionID], p)
		if len(list) > pendingCap {
			list = list[len(list)-pendingCap:]
		}
		f.pending[e.SessionID] = list
	}
	for ch := range f.subs[e.SessionID] {
		select {
		case ch <- e:
		default:
			f.dropped[ch]++
		}
	}
}

// Subscribe 订阅某会话的事件流，返回只读通道与取消函数。buf<=0 用默认缓冲。
// cancel 后通道关闭，Feed 不再持有该订阅。
func (f *Feed) Subscribe(chatID string, buf int) (<-chan Event, func()) {
	if buf <= 0 {
		buf = 32
	}
	ch := make(chan Event, buf)
	f.mu.Lock()
	defer f.mu.Unlock()
	set, ok := f.subs[chatID]
	if !ok {
		set = map[chan Event]struct{}{}
		f.subs[chatID] = set
	}
	set[ch] = struct{}{}
	cancel := func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if s, ok := f.subs[chatID]; ok {
			delete(s, ch)
			if len(s) == 0 {
				delete(f.subs, chatID)
			}
		}
		delete(f.dropped, ch)
		close(ch)
	}
	return ch, cancel
}

// PendingPermissions 返回某会话记录的未处理权限请求快照（时间正序）。
func (f *Feed) PendingPermissions(chatID string) []Permission {
	f.mu.Lock()
	defer f.mu.Unlock()
	list := f.pending[chatID]
	out := make([]Permission, len(list))
	copy(out, list)
	return out
}

// ClearPermission 从 pending 快照中移除指定请求（GrantPermission 成功后调用，
// 让 PendingPermissions 反映处理后的状态）。
func (f *Feed) ClearPermission(chatID, reqID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	list := f.pending[chatID]
	out := list[:0]
	for _, p := range list {
		if p.RequestID != reqID {
			out = append(out, p)
		}
	}
	f.pending[chatID] = out
}
