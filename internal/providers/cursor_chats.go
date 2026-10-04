package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type cursorChatMeta struct {
	SchemaVersion   int    `json:"schemaVersion"`
	CreatedAtMs     int64  `json:"createdAtMs"`
	UpdatedAtMs     int64  `json:"updatedAtMs"`
	HasConversation bool   `json:"hasConversation"`
	Title           string `json:"title"`
	Cwd             string `json:"cwd"`
}

func (m cursorChatMeta) CreatedAt() time.Time {
	return time.UnixMilli(m.CreatedAtMs)
}

func loadCursorChatMeta(path string) (cursorChatMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return cursorChatMeta{}, err
	}
	var m cursorChatMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return cursorChatMeta{}, err
	}
	return m, nil
}

func listCursorChatSessions(home string) ([]Session, error) {
	root := filepath.Join(home, ".cursor", "chats")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Session
	for _, hashEnt := range entries {
		if !hashEnt.IsDir() {
			continue
		}
		hashDir := filepath.Join(root, hashEnt.Name())
		agents, err := os.ReadDir(hashDir)
		if err != nil {
			continue
		}
		for _, ag := range agents {
			if !ag.IsDir() {
				continue
			}
			id := ag.Name()
			metaPath := filepath.Join(hashDir, id, "meta.json")
			m, err := loadCursorChatMeta(metaPath)
			if err != nil || !m.HasConversation {
				continue // 损坏 / 无 meta / 空会话 → 跳过
			}
			out = append(out, Session{
				ID:        id,
				ToolID:    cursorID,
				Workspace: m.Cwd,
				Title:     m.Title,
				CreatedAt: time.UnixMilli(m.CreatedAtMs),
				UpdatedAt: time.UnixMilli(m.UpdatedAtMs),
			})
		}
	}
	return out, nil
}
