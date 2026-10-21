package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// cursorACPSessionMeta 是 ~/.cursor/acp-sessions/<id>/meta.json。
// 与 IDE chats 的 meta 不同：无 createdAt/hasConversation，只有 cwd + title。
type cursorACPSessionMeta struct {
	SchemaVersion int    `json:"schemaVersion"`
	Cwd           string `json:"cwd"`
	Title         string `json:"title"`
}

// listCursorACPSessions 枚举 Cursor 官方 ACP 会话目录。
// kshell / cursor-acp 创建的会话落在这里，不进 ~/.cursor/chats/，
// 若不枚举则 AttachSession 与 AgentHub 快捷回复永远绑不上 SessionID。
func listCursorACPSessions(home string) ([]Session, error) {
	root := filepath.Join(home, ".cursor", "acp-sessions")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Session
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		id := ent.Name()
		metaPath := filepath.Join(root, id, "meta.json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var m cursorACPSessionMeta
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if m.Cwd == "" {
			continue
		}
		title := m.Title
		if title == "" {
			title = id
		}
		updated := time.Time{}
		if st, err := os.Stat(metaPath); err == nil {
			updated = st.ModTime()
		}
		// store.db 往往比 meta 更新更勤（对话进行中），优先作 UpdatedAt
		storePath := filepath.Join(root, id, "store.db")
		if st, err := os.Stat(storePath); err == nil && st.ModTime().After(updated) {
			updated = st.ModTime()
		}
		out = append(out, Session{
			ID:        id,
			ToolID:    cursorID,
			Workspace: m.Cwd,
			Title:     title,
			CreatedAt: updated,
			UpdatedAt: updated,
			Path:      metaPath,
			ACPOnly:   true, // 仅能以 ACP 激活；TUI resume 会静默丢上下文
		})
	}
	return out, nil
}

// mergeCursorSessions 合并 chats 与 acp-sessions：同 ID 保留已有（chats 优先传入）。
func mergeCursorSessions(primary, extra []Session) []Session {
	if len(extra) == 0 {
		return primary
	}
	seen := make(map[string]bool, len(primary)+len(extra))
	out := make([]Session, 0, len(primary)+len(extra))
	for _, s := range primary {
		if s.ID == "" || seen[s.ID] {
			continue
		}
		seen[s.ID] = true
		out = append(out, s)
	}
	for _, s := range extra {
		if s.ID == "" || seen[s.ID] {
			continue
		}
		seen[s.ID] = true
		out = append(out, s)
	}
	return out
}
