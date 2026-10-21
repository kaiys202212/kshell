package hub

import (
	"strings"
	"time"

	"github.com/yangk/kshell/internal/discovery"
)

// DiskSession 是 orphan 回填用的磁盘会话摘要（desktop 从 providers.Session 投影）。
// 与命令层 SessionRef（可切换会话编号）不同名，避免冲突。
type DiskSession struct {
	ID        string
	ToolID    string
	Workspace string
	Title     string
	UpdatedAt time.Time
}

// 与前端 resolveSessionByProximity 对齐：45 分钟窗口、前两名距离差 <5 分钟视为歧义。
const (
	proximityWindow = 45 * time.Minute
	proximityAmbig  = 5 * time.Minute
)

// BackfillMissingSessionIDs 给 live 中缺 SessionID 的消息尽量补上磁盘会话 ID：
//  1. termKey 以 session: 开头 → 派生；
//  2. tool+workspace+sessionTitle 在 refs 中唯一命中；
//  3. 同 tool+workspace 下 UpdatedAt 邻近唯一（窗口/歧义规则见常量）。
// 已有快照不覆盖；返回更新条数。
func (s *Store) BackfillMissingSessionIDs(sessions []DiskSession) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := 0
	for i := range s.live {
		if s.live[i].SessionID != "" {
			continue
		}
		id := resolveSessionID(s.live[i], sessions)
		if id == "" {
			continue
		}
		s.live[i].SessionID = id
		changed++
	}
	if changed > 0 {
		_ = s.writeLive()
	}
	return changed
}

func resolveSessionID(m Message, sessions []DiskSession) string {
	if strings.HasPrefix(m.TermKey, "session:") {
		return strings.TrimPrefix(m.TermKey, "session:")
	}
	if m.Tool != "" && m.Workspace != "" && m.SessionTitle != "" {
		var hit string
		n := 0
		ws := discovery.NormalizePath(m.Workspace)
		for _, r := range sessions {
			if r.ToolID != m.Tool || discovery.NormalizePath(r.Workspace) != ws {
				continue
			}
			if r.Title != m.SessionTitle {
				continue
			}
			hit = r.ID
			n++
			if n > 1 {
				hit = ""
				break
			}
		}
		if hit != "" {
			return hit
		}
	}
	return resolveByProximity(m, sessions)
}

func resolveByProximity(m Message, sessions []DiskSession) string {
	if m.Tool == "" || m.Workspace == "" || m.Ts <= 0 {
		return ""
	}
	msgMs := hubTsToMs(m.Ts)
	if msgMs <= 0 {
		return ""
	}
	ws := discovery.NormalizePath(m.Workspace)
	type cand struct {
		id   string
		dist time.Duration
	}
	var ranked []cand
	msgT := time.UnixMilli(msgMs)
	for _, r := range sessions {
		if r.ToolID != m.Tool || discovery.NormalizePath(r.Workspace) != ws {
			continue
		}
		if r.UpdatedAt.IsZero() || r.ID == "" {
			continue
		}
		d := r.UpdatedAt.Sub(msgT)
		if d < 0 {
			d = -d
		}
		if d > proximityWindow {
			continue
		}
		ranked = append(ranked, cand{id: r.ID, dist: d})
	}
	if len(ranked) == 0 {
		return ""
	}
	for i := 1; i < len(ranked); i++ {
		for j := i; j > 0 && ranked[j].dist < ranked[j-1].dist; j-- {
			ranked[j], ranked[j-1] = ranked[j-1], ranked[j]
		}
	}
	if len(ranked) >= 2 {
		gap := ranked[1].dist - ranked[0].dist
		if gap < 0 {
			gap = -gap
		}
		if gap < proximityAmbig {
			return ""
		}
	}
	return ranked[0].id
}

// hubTsToMs 把 Hub Ts（UnixNano，或已是 ms）转为毫秒；过小的夹具值返回 0。
func hubTsToMs(ts int64) int64 {
	if ts <= 0 {
		return 0
	}
	if ts > 1e14 {
		return ts / 1e6
	}
	if ts > 1e11 {
		return ts
	}
	return 0
}
