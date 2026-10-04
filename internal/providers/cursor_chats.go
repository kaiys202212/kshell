package providers

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
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

// cursorStoreHasSubagentInfo 读 meta key=0：value 先尝试 hex 解码再 JSON 解析；subagentInfo 为非空 JSON 对象时返回 true，缺库/SQL 错误返回 err。
func cursorStoreHasSubagentInfo(storeDB string) (bool, error) {
	db, err := sql.Open("sqlite", storeDB)
	if err != nil {
		return false, err
	}
	defer db.Close()
	var val string
	err = db.QueryRow(`SELECT value FROM meta WHERE key = '0' LIMIT 1`).Scan(&val)
	if err != nil {
		return false, err
	}
	raw := []byte(val)
	if decoded, derr := hex.DecodeString(val); derr == nil {
		raw = decoded
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false, err
	}
	rawSub, ok := obj["subagentInfo"]
	if !ok {
		return false, nil
	}
	trimmed := strings.TrimSpace(string(rawSub))
	if trimmed == "" || trimmed == "null" || !strings.HasPrefix(trimmed, "{") {
		return false, nil
	}
	return true, nil
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

func enumerateCursorTranscriptFallback(home string) ([]Session, error) {
	return nil, nil
}

func enrichCursorSession(home string, s *Session) {
	path := resolveCursorTranscript(home, s.Workspace, s.ID)
	if path == "" {
		return
	}
	s.Path = path
	if n, err := CountLines(path); err == nil {
		s.Messages = n
	}
	if s.Title == "" {
		if head, err := ReadHead(path, 256*1024); err == nil {
			if parsed, err := (Cursor{}).ParseSession(path, head); err == nil && parsed.Title != "" {
				s.Title = parsed.Title
			}
		}
	}
}

func resolveCursorTranscript(home, cwd, id string) string {
	slug := (Cursor{}).WorkspaceToSlug(cwd)
	p := filepath.Join(home, ".cursor", "projects", slug, "agent-transcripts", id, id+".jsonl")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	root := filepath.Join(home, ".cursor", "projects")
	matches, _ := filepath.Glob(filepath.Join(root, "*", "agent-transcripts", id, id+".jsonl"))
	if len(matches) == 1 {
		return matches[0]
	}
	return ""
}
