package providers

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

// cursorUserQueryRe 抽出 Cursor 用户消息里的 <user_query> 正文（对话轮次只关心这个）。
var cursorUserQueryRe = regexp.MustCompile(`(?is)<user_query\b[^>]*>(.*?)</user_query\s*>`)

// cursorStoreMetaJSON 是 acp-sessions/<id>/store.db 里 meta.key=0 的 JSON。
type cursorStoreMetaJSON struct {
	Name             string `json:"name"`
	LatestRootBlobID string `json:"latestRootBlobId"`
	CreatedAtMs      int64  `json:"createdAt"`
}

func (m cursorStoreMetaJSON) CreatedAt() time.Time {
	if m.CreatedAtMs <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(m.CreatedAtMs)
}

// turnsFromCursorStore 按 latestRootBlobId 的 protobuf field1 链抽出可展示轮次。
// 跳过 system/tool，以及无 text 的 assistant（纯 tool-call）；用户正文经 cleanTitle。
func turnsFromCursorStore(storeDB string) ([]previewTurn, cursorStoreMetaJSON, error) {
	db, err := sql.Open("sqlite", storeDB)
	if err != nil {
		return nil, cursorStoreMetaJSON{}, err
	}
	defer db.Close()

	meta, err := loadCursorStoreMeta(db)
	if err != nil {
		return nil, cursorStoreMetaJSON{}, err
	}
	if meta.LatestRootBlobID == "" {
		return nil, meta, nil
	}

	var root []byte
	err = db.QueryRow(`SELECT data FROM blobs WHERE id = ? LIMIT 1`, meta.LatestRootBlobID).Scan(&root)
	if err != nil {
		return nil, meta, err
	}
	ids := cursorRootBlobIDs(root)
	if len(ids) == 0 {
		return nil, meta, nil
	}

	turns := make([]previewTurn, 0, len(ids))
	for _, id := range ids {
		var raw []byte
		if err := db.QueryRow(`SELECT data FROM blobs WHERE id = ? LIMIT 1`, id).Scan(&raw); err != nil {
			continue
		}
		role, text := cursorBlobMessageText(raw)
		if role == "" || text == "" {
			continue
		}
		turns = append(turns, previewTurn{Role: role, Text: text})
	}
	return turns, meta, nil
}

func loadCursorStoreMeta(db *sql.DB) (cursorStoreMetaJSON, error) {
	var val string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = '0' LIMIT 1`).Scan(&val); err != nil {
		return cursorStoreMetaJSON{}, err
	}
	raw := []byte(val)
	if decoded, err := hex.DecodeString(val); err == nil {
		raw = decoded
	}
	var m cursorStoreMetaJSON
	if err := json.Unmarshal(raw, &m); err != nil {
		return cursorStoreMetaJSON{}, err
	}
	return m, nil
}

// cursorRootBlobIDs 解析根 blob：只取顶层 field=1 且长度为 32 的 bytes 为子 blob id（hex）。
func cursorRootBlobIDs(data []byte) []string {
	var out []string
	i := 0
	for i < len(data) {
		tag, n := readProtoVarint(data, i)
		if n == 0 {
			break
		}
		i += n
		field := int(tag >> 3)
		wire := int(tag & 7)
		switch wire {
		case 0: // varint
			_, n = readProtoVarint(data, i)
			if n == 0 {
				return out
			}
			i += n
		case 1: // 64-bit
			if i+8 > len(data) {
				return out
			}
			i += 8
		case 5: // 32-bit
			if i+4 > len(data) {
				return out
			}
			i += 4
		case 2: // length-delimited
			ln, n := readProtoVarint(data, i)
			if n == 0 {
				return out
			}
			i += n
			if i+int(ln) > len(data) {
				return out
			}
			payload := data[i : i+int(ln)]
			i += int(ln)
			if field == 1 && ln == 32 {
				out = append(out, hex.EncodeToString(payload))
			}
		default:
			return out
		}
	}
	return out
}

func readProtoVarint(data []byte, i int) (uint64, int) {
	var x uint64
	var s uint
	start := i
	for i < len(data) {
		b := data[i]
		i++
		x |= uint64(b&0x7f) << s
		if b&0x80 == 0 {
			return x, i - start
		}
		s += 7
		if s >= 64 {
			return 0, 0
		}
	}
	return 0, 0
}

func cursorBlobMessageText(raw []byte) (role, text string) {
	var obj struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", ""
	}
	switch strings.ToLower(strings.TrimSpace(obj.Role)) {
	case "user":
		role = "user"
	case "assistant":
		role = "assistant"
	default:
		return "", ""
	}
	text = strings.TrimSpace(cursorContentText(obj.Content))
	if text == "" {
		return "", ""
	}
	if role == "user" {
		return role, cursorUserPreviewText(text)
	}
	return role, text
}

// cursorUserPreviewText 尽量还原用户真实提问：优先 <user_query>；
// 否则 cleanTitle。纯上下文注入（user_info/git_status 等、无提问）返回空以跳过该轮。
func cursorUserPreviewText(raw string) string {
	if m := cursorUserQueryRe.FindStringSubmatch(raw); len(m) == 2 {
		if body := strings.TrimSpace(m[1]); body != "" {
			return body
		}
	}
	cleaned := cleanTitle(raw)
	if cleaned == "" {
		return ""
	}
	// 无 user_query 的超长/机器上下文块不当对话轮次（Cursor 常把规则/git 状态塞进首条 user）
	if cursorLooksLikeContextDump(raw) || utf8.RuneCountInString(cleaned) > 500 {
		return ""
	}
	return cleaned
}

func cursorLooksLikeContextDump(raw string) bool {
	lower := strings.ToLower(raw)
	for _, mark := range []string{
		"<user_info", "<git_status", "<agent_transcripts",
		"<always_applied_workspace_rules", "<available_skills",
		"<agent_skills", "<user_rules",
	} {
		if strings.Contains(lower, mark) {
			return true
		}
	}
	return false
}

func cursorContentText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			typ, _ := m["type"].(string)
			if typ != "text" {
				continue
			}
			if s, _ := m["text"].(string); strings.TrimSpace(s) != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

// enrichCursorACPSession 用 store.db 补齐 Path / Messages / Title / 时间。
// 无可用对话正文时返回 false（调用方应跳过该空壳）。
//
// 标题优先级（与 ~/.cursor/chats transcript 口径一致）：
//  1. store 首条用户提问（ClipPromptTitle，48 字）——覆盖 Cursor 自动短 meta.title；
//  2. 已有 Title（来自 meta.json）若非空且非 UUID；
//  3. store meta.name。
func enrichCursorACPSession(s *Session, storePath string) bool {
	turns, meta, err := turnsFromCursorStore(storePath)
	if err != nil || len(turns) == 0 {
		return false
	}
	s.Path = storePath
	s.Messages = len(turns)
	if title := firstUserPromptTitle(turns); title != "" {
		s.Title = title
	} else if strings.TrimSpace(s.Title) == "" || s.Title == s.ID {
		if name := strings.TrimSpace(meta.Name); name != "" {
			s.Title = ClipPromptTitle(name)
		}
	}
	if created := meta.CreatedAt(); !created.IsZero() {
		if s.CreatedAt.IsZero() || created.Before(s.CreatedAt) {
			s.CreatedAt = created
		}
	}
	return true
}

// firstUserPromptTitle 从对话轮次取首条可用用户提问作页签标题。
func firstUserPromptTitle(turns []previewTurn) string {
	for _, t := range turns {
		if t.Role != "user" || t.Text == "" {
			continue
		}
		if title := ClipPromptTitle(t.Text); title != "" {
			return title
		}
	}
	return ""
}
