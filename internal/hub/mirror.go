package hub

import (
	"strings"
	"sync"
	"time"
)

// MirrorPolicy 是汇报聚合的共享策略状态：群发成功后进入聚合,单会话精确
// 投递即退出。桌面 Console 与微信桥 Console 可共享同一实例(SetMirrorPolicy),
// 让任一渠道发起的群发对另一渠道的出站镜像同样生效。非活跃(目标集不足两
// 个)时一切现状行为。并发安全;消费方为 desktop 的出站镜像分派与 Console 自身。
type MirrorPolicy struct {
	mu      sync.Mutex
	targets map[string]MirrorTarget // 主键优先 SessionID,缺 SessionID 用 ChatID
	sinceTs int64                   // 群发时刻(UnixNano):「会话有回复」以它为界
}

// MirrorTarget 是聚合目标的一行登记。
type MirrorTarget struct {
	ChatID    string // 链头/前端 ID 空间的 chat ID(群发目标必为 ACP 可投递会话)
	SessionID string // 磁盘会话 ID:跨 chat 重建仍稳定,匹配优先用
	Title     string
}

func NewMirrorPolicy() *MirrorPolicy {
	return &MirrorPolicy{targets: map[string]MirrorTarget{}}
}

// Enter 登记群发目标集,替换旧目标集并重置时间基准。无稳定标识
// (ChatID 与 SessionID 皆空)的引用不登记,防误命中。
// 单目标(len<2)即清空、不登记:单目标传入是非聚合(单会话精确投递),
// 与 Discard 等价。
func (p *MirrorPolicy) Enter(refs []SessionRef) {
	if len(refs) < 2 {
		p.Discard()
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	m := make(map[string]MirrorTarget, len(refs))
	for _, r := range refs {
		key := r.SessionID
		if key == "" {
			key = r.ChatID
		}
		if key == "" {
			continue
		}
		m[key] = MirrorTarget{ChatID: r.ChatID, SessionID: r.SessionID, Title: r.Title}
	}
	p.targets = m
	if len(m) >= 2 {
		p.sinceTs = time.Now().UnixNano()
	} else {
		p.sinceTs = 0
	}
}

// EnterSinceTs 重设群发时间基准(测试注入用)。
func (p *MirrorPolicy) EnterSinceTs(ts int64) {
	p.mu.Lock()
	p.sinceTs = ts
	p.mu.Unlock()
}

// Discard 清空聚合(任何单会话精确投递触发)。
func (p *MirrorPolicy) Discard() {
	p.mu.Lock()
	p.targets = nil
	p.sinceTs = 0
	p.mu.Unlock()
}

// Active 报告是否处于聚合模式(单目标(len<2)即清空、不登记,故不可达)。
func (p *MirrorPolicy) Active() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.targets) >= 2
}

// Count 返回当前已登记目标数(单目标(len<2)即清空、不登记)。
func (p *MirrorPolicy) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.targets)
}

// SinceTs 返回群发时间基准(0 表示未设置)。
func (p *MirrorPolicy) SinceTs() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sinceTs
}

// Targets 返回目标快照(拷贝,调用方可安全持有)。
func (p *MirrorPolicy) Targets() []MirrorTarget {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]MirrorTarget, 0, len(p.targets))
	for _, t := range p.targets {
		out = append(out, t)
	}
	return out
}

// Match 按归因三元组命中目标:sessionId 冗余快照 > chatId > termKey 的
// session: 前缀派生。空参数即未提供该维度,跳过。
func (p *MirrorPolicy) Match(chatID, termKey, sessionID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, t := range p.targets {
		if sessionID != "" && t.SessionID != "" && sessionID == t.SessionID {
			return true
		}
		if chatID != "" && t.ChatID != "" && chatID == t.ChatID {
			return true
		}
		if termKey == "" || t.SessionID == "" {
			continue
		}
		if sid, ok := strings.CutPrefix(termKey, "session:"); ok && sid == t.SessionID {
			return true
		}
	}
	return false
}
