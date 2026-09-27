package providers

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"
)

const codexID = "codex"

var errCodexNoSessionMeta = errors.New("codex: 会话文件缺少 session_meta 记录")

// Codex 对应 Codex CLI。会话存于 ~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl，
// 首行 type=session_meta，payload 里带 session_id / cwd / timestamp。
type Codex struct{}

func (Codex) ID() string          { return codexID }
func (Codex) DisplayName() string { return "Codex CLI" }

func (Codex) DetectSpec(home string) DetectSpec {
	return DetectSpec{
		BinName:     codexID,
		InstallDirs: []string{"~/.codex/bin"},
		ConfigDirs:  []string{"~/.codex"},
	}
}

func (Codex) SessionRoots(home string) []string {
	return []string{filepath.Join(home, ".codex", "sessions")}
}

func (Codex) SessionFilePattern() string { return "*.jsonl" }

func (Codex) ParseSession(path string, head []byte) (*Session, error) {
	var (
		id, cwd, title   string
		created, updated time.Time
		haveTime         bool
	)

	for _, line := range strings.Split(string(head), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec struct {
			Timestamp string         `json:"timestamp"`
			Type      string         `json:"type"`
			Payload   map[string]any `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}

		if rec.Type == "session_meta" {
			payload := rec.Payload
			if id == "" {
				if v, ok := payload["session_id"].(string); ok {
					id = v
				} else if v, ok := payload["id"].(string); ok {
					id = v
				}
			}
			if cwd == "" {
				cwd, _ = payload["cwd"].(string)
			}
		}

		if title == "" && rec.Type == "event_msg" {
			if item, ok := rec.Payload["item"].(map[string]any); ok {
				if item["type"] == "UserMessage" {
					title = oneLine(messageText(item), 80)
				}
			}
		}

		ts := rec.Timestamp
		if v, ok := rec.Payload["timestamp"].(string); ok && ts == "" {
			ts = v
		}
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			if !haveTime {
				created, updated, haveTime = t, t, true
			}
			if t.Before(created) {
				created = t
			}
			if t.After(updated) {
				updated = t
			}
		}
	}

	if id == "" {
		return nil, errCodexNoSessionMeta
	}

	messages := 0
	if n, err := CountLines(path); err == nil {
		messages = n
	}

	return &Session{
		ID:        id,
		ToolID:    codexID,
		Workspace: cwd,
		Title:     title,
		CreatedAt: created,
		UpdatedAt: updated,
		Messages:  messages,
		Path:      path,
	}, nil
}

func (Codex) NewSessionCmd(ws string, bin string, ctx []string) Launch {
	launch := Launch{Path: bin, Dir: ws}
	if prompt := ContextPrompt(ctx); prompt != "" {
		launch.Args = []string{prompt}
	}
	return launch
}

// ResumeCmd 使用实测确认过的 `codex resume <SESSION_ID>`，并在会话所属工作区启动。
func (Codex) ResumeCmd(s Session, bin string) Launch {
	return Launch{Path: bin, Args: []string{"resume", s.ID}, Dir: s.Workspace}
}
