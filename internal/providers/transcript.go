package providers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yangk/kshell/internal/applang"
)

var errTranscriptNoPath = errors.New("err.transcript.no_path")

// FormatSessionMarkdown 把历史会话抽成只读 Markdown（用户/助手轮次）。
// 全量读入会话文件，不做长度截断——预览必须能看到全部消息内容。
// SQLite（OpenCode）无法按文件展开，返回说明文案。
func FormatSessionMarkdown(s Session) (string, error) {
	if strings.TrimSpace(s.Path) == "" {
		return "", errTranscriptNoPath
	}
	// Cursor ACP store.db：按根 blob 链还原 user/assistant 正文（先于通用 sqlite 占位）。
	if strings.EqualFold(filepath.Ext(s.Path), ".db") && (s.ToolID == cursorID || s.ACPOnly) {
		if turns, _, err := turnsFromCursorStore(s.Path); err == nil && len(turns) > 0 {
			return renderTurns(s.Title, turns), nil
		}
	}
	if s.ToolID == opencodeID || strings.EqualFold(filepath.Ext(s.Path), ".db") {
		return sqliteSessionMarkdown(s), nil
	}

	raw, err := os.ReadFile(s.Path)
	if err != nil {
		return "", err
	}

	turns := turnsFromGeminiJSON(raw)
	if len(turns) == 0 {
		turns = turnsFromJSONL(raw)
	}
	if len(turns) == 0 {
		return fallbackEmptyMarkdown(s), nil
	}
	return renderTurns(s.Title, turns), nil
}

func sqliteSessionMarkdown(s Session) string {
	title := strings.TrimSpace(s.Title)
	if title == "" {
		title = s.ID
	}
	return fmt.Sprintf("# %s\n\n%s\n", title, applang.T("transcript.sqlite_body"))
}

func fallbackEmptyMarkdown(s Session) string {
	title := strings.TrimSpace(s.Title)
	if title == "" {
		title = s.ID
	}
	return fmt.Sprintf("# %s\n\n%s\n", title, applang.T("transcript.empty_body"))
}

type previewTurn struct {
	Role string // user | assistant
	Text string
}

func renderTurns(title string, turns []previewTurn) string {
	var b strings.Builder
	if t := strings.TrimSpace(title); t != "" {
		b.WriteString("# ")
		b.WriteString(t)
		b.WriteString("\n\n")
	}
	for i, t := range turns {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("## ")
		b.WriteString(applang.T("role." + t.Role))
		b.WriteString("\n\n")
		b.WriteString(t.Text)
		b.WriteString("\n")
	}
	return b.String()
}

func turnsFromGeminiJSON(raw []byte) []previewTurn {
	trim := bytes.TrimSpace(raw)
	if len(trim) == 0 || trim[0] != '{' {
		return nil
	}
	var rec struct {
		Messages []struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(trim, &rec); err != nil || len(rec.Messages) == 0 {
		return nil
	}
	var out []previewTurn
	for _, m := range rec.Messages {
		role := previewRole(m.Type, m.Role)
		text := strings.TrimSpace(contentText(m.Content))
		if role == "" || text == "" {
			continue
		}
		out = append(out, previewTurn{Role: role, Text: text})
	}
	return out
}

func turnsFromJSONL(raw []byte) []previewTurn {
	var out []previewTurn
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		role, text := turnFromRecord(rec)
		if role == "" || text == "" {
			continue
		}
		out = append(out, previewTurn{Role: role, Text: text})
	}
	return out
}

func turnFromRecord(rec map[string]any) (role, text string) {
	typ, _ := rec["type"].(string)
	if typ == "progress" || typ == "session_meta" || typ == "event_msg" {
		if typ == "event_msg" {
			return turnFromCodexPayload(rec)
		}
		return "", ""
	}
	role = previewRole(typ, stringOr(rec["role"]))
	if msg, ok := rec["message"].(map[string]any); ok {
		if role == "" {
			role = previewRole("", stringOr(msg["role"]))
		}
		if text = strings.TrimSpace(contentText(msg["content"])); text == "" {
			text = strings.TrimSpace(messageText(msg))
		}
	}
	if text == "" {
		text = strings.TrimSpace(contentText(rec["content"]))
	}
	if text == "" {
		text = strings.TrimSpace(messageText(rec["message"]))
	}
	return role, text
}

func turnFromCodexPayload(rec map[string]any) (role, text string) {
	payload, _ := rec["payload"].(map[string]any)
	item, _ := payload["item"].(map[string]any)
	if item == nil {
		return "", ""
	}
	it, _ := item["type"].(string)
	switch it {
	case "UserMessage":
		role = "user"
	case "AgentMessage", "AssistantMessage":
		role = "assistant"
	default:
		return "", ""
	}
	return role, strings.TrimSpace(contentText(item["content"]))
}

func previewRole(typ, role string) string {
	t := strings.ToLower(strings.TrimSpace(typ))
	r := strings.ToLower(strings.TrimSpace(role))
	switch t {
	case "user", "human":
		return "user"
	case "assistant", "gemini", "ai", "model":
		return "assistant"
	}
	switch r {
	case "user", "human":
		return "user"
	case "assistant", "model", "ai":
		return "assistant"
	}
	return ""
}

func stringOr(v any) string {
	s, _ := v.(string)
	return s
}
