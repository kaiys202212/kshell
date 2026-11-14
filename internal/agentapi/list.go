package agentapi

import (
	"fmt"
	"strings"
)

// List 按范围返回会话列表。archived 名单同时作用于活跃与磁盘会话
//（归档的会话若仍活跃，标记为 archived 优先呈现归档语义）。
func (a *API) List(scope Scope) ([]Session, error) {
	switch scope {
	case ScopeActive, ScopeDisk, ScopeArchived, ScopeAll:
	default:
		return nil, fmt.Errorf("agentapi: 未知 scope %q", scope)
	}
	archived := make(map[string]bool)
	for _, id := range a.b.ArchivedIDs() {
		archived[id] = true
	}
	var out []Session
	if scope == ScopeActive || scope == ScopeAll || scope == ScopeArchived {
		for _, s := range a.b.ActiveSessions() {
			if archived[s.SessionID] {
				s.Status = StatusArchived
			}
			if scope == ScopeArchived && s.Status != StatusArchived {
				continue
			}
			out = append(out, s)
		}
	}
	if scope == ScopeDisk || scope == ScopeAll || scope == ScopeArchived {
		for _, s := range a.b.DiskSessions() {
			if archived[s.SessionID] {
				s.Status = StatusArchived
			}
			if scope == ScopeArchived && s.Status != StatusArchived {
				continue
			}
			out = append(out, s)
		}
	}
	return out, nil
}

// resolve 把工具层寻址键归一为会话：优先活跃 chat（按链头 ID 或 SessionID
// 命中），未命中再查磁盘会话（按 SessionID）。找不到返回 ErrNotFound。
func (a *API) resolve(id string) (Session, error) {
	if id == "" {
		return Session{}, ErrNotFound
	}
	for _, s := range a.b.ActiveSessions() {
		if s.ID == id || (s.SessionID != "" && s.SessionID == id) {
			return s, nil
		}
	}
	for _, s := range a.b.DiskSessions() {
		if s.SessionID == id {
			return s, nil
		}
	}
	return Session{}, fmt.Errorf("%w: %s", ErrNotFound, id)
}

// Info 按寻址键查单个会话详情。
func (a *API) Info(id string) (Session, bool) {
	s, err := a.resolve(id)
	if err != nil {
		return Session{}, false
	}
	return s, true
}

// Search 在全部会话（active+disk）里按标题/工作区/工具做大小写不敏感包含匹配。
func (a *API) Search(kw string) []Session {
	kw = strings.ToLower(strings.TrimSpace(kw))
	if kw == "" {
		return nil
	}
	all, err := a.List(ScopeAll)
	if err != nil {
		return nil
	}
	var out []Session
	for _, s := range all {
		hay := strings.ToLower(s.Title + "\n" + s.Workspace + "\n" + s.Tool)
		if strings.Contains(hay, kw) {
			out = append(out, s)
		}
	}
	return out
}
