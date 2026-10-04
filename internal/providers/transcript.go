package providers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const transcriptReadLimit = 512 * 1024

var errTranscriptNoPath = errors.New("会话无本地文件")

// FormatSessionMarkdown 把历史会话抽成只读 Markdown（用户/助手轮次）。
// SQLite（OpenCode）无法按文件展开，返回说明文案；读取超过 transcriptReadLimit 时 truncated=true。
func FormatSessionMarkdown(s Session) (string, bool, error) {
	if s.ToolID == opencodeID || strings.EqualFold(filepath.Ext(s.Path), ".db") {
		return sqliteSessionMarkdown(s), false, nil
	}
	if strings.TrimSpace(s.Path) == "" {
		return "", false, errTranscriptNoPath
	}

	raw, truncated, err := readLimited(s.Path, transcriptReadLimit)
	if err != nil {
		return "", false, err
	}

	turns := turnsFromGeminiJSON(raw)
	if len(turns) == 0 {
		turns = turnsFromJSONL(raw)
	}
	if len(turns) == 0 {
		return fallbackEmptyMarkdown(s), truncated, nil
	}
	return renderTurns(s.Title, turns), truncated, nil
}

func sqliteSessionMarkdown(s Session) string {
	title := strings.TrimSpace(s.Title)
	if title == "" {
		title = s.ID
	}
	return fmt.Sprintf("# %s\n\n该会话保存在数据库中，无法以文件形式展开完整对话。请点击「激活」恢复后再查看。\n", title)
}

func fallbackEmptyMarkdown(s Session) string {
	title := strings.TrimSpace(s.Title)
	if title == "" {
		title = s.ID
	}
	return fmt.Sprintf("# %s\n\n未解析到对话正文。请点击「激活」恢复后查看。\n", title)
}

func readLimited(path string, limit int) ([]byte, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, false, err
	}
	truncated := len(buf) > limit
	if truncated {
		buf = buf[:limit]
	}
	return buf, truncated, nil
}

type previewTurn struct {
	Role string // 用户 | 助手
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
		b.WriteString(t.Role)
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
		role = "用户"
	case "AgentMessage", "AssistantMessage":
		role = "助手"
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
		return "用户"
	case "assistant", "gemini", "ai", "model":
		return "助手"
	}
	switch r {
	case "user", "human":
		return "用户"
	case "assistant", "model", "ai":
		return "助手"
	}
	return ""
}

func stringOr(v any) string {
	s, _ := v.(string)
	return s
}
