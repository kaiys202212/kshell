package providers

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const cursorID = "cursor"

// errCursorEmpty 表示头部没有任何真实用户消息（纯工具调用的空壳会话），列表里只会是噪音。
var errCursorEmpty = errors.New("cursor: 会话无实际用户消息")

// Cursor 对应 Cursor 官方 CLI（cursor-agent）。CLI 会话存于
// ~/.cursor/projects/<工作区slug>/agent-transcripts/<uuid>/<uuid>.jsonl，
// 每行 {"role":...,"message":{"content":[...]}}，与 Claude 格式相近但**没有**
// sessionId / timestamp / cwd 字段，ID 从路径取、时间取文件 mtime、工作区从 slug 逆解码。
type Cursor struct{}

func (Cursor) ID() string          { return cursorID }
func (Cursor) DisplayName() string { return "Cursor" }

func (Cursor) DetectSpec(home string) DetectSpec {
	return DetectSpec{
		BinName:    "cursor-agent",
		ConfigDirs: []string{"~/.cursor"},
	}
}

func (Cursor) SessionRoots(home string) []string {
	return []string{filepath.Join(home, ".cursor", "projects")}
}

func (Cursor) SessionFilePattern() string { return "*.jsonl" }

// MatchSessionRel 按目录深度过滤：projects/<slug>/agent-transcripts/<uuid>/<uuid>.jsonl
// 才是会话文件，同 slug 下的 terminals/ 等其它产物不能拿去 resume。
func (Cursor) MatchSessionRel(rel string) bool {
	rel = filepath.ToSlash(rel)
	rel = strings.TrimPrefix(rel, "./")
	parts := strings.Split(rel, "/")
	return len(parts) == 4 && parts[1] == "agent-transcripts" && strings.HasSuffix(rel, ".jsonl")
}

// stripUserQuery 剥掉 Cursor 首条用户消息的 <user_query>…</user_query> 包装。
// 头部只读有限字节，标签不闭合是常态，按前缀存在与否尽力剥。
func stripUserQuery(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "<user_query>") {
		return s
	}
	s = strings.TrimPrefix(s, "<user_query>")
	s = strings.TrimSuffix(strings.TrimSpace(s), "</user_query>")
	return strings.TrimSpace(s)
}

func (Cursor) ParseSession(path string, head []byte) (*Session, error) {
	// 记录里没有 sessionId，uuid 只能从 <uuid>/<uuid>.jsonl 的目录段取
	id := filepath.Base(filepath.Dir(path))
	title := ""
	for _, line := range strings.Split(string(head), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue // 坏行跳过
		}
		if role, _ := rec["role"].(string); role == "user" {
			if text := stripUserQuery(messageText(rec["message"])); text != "" {
				title = oneLine(text, 80)
				break
			}
		}
	}
	if title == "" {
		return nil, errCursorEmpty
	}

	// 无时间戳记录，文件 mtime 是唯一可靠的时间来源（两者同源）
	var updated time.Time
	if st, err := os.Stat(path); err == nil {
		updated = st.ModTime()
	}

	messages := 0
	if n, err := CountLines(path); err == nil {
		messages = n
	}

	slug := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path))))
	return &Session{
		ID:        id,
		ToolID:    cursorID,
		Workspace: Cursor{}.SlugToWorkspace(slug),
		Title:     title,
		CreatedAt: updated,
		UpdatedAt: updated,
		Messages:  messages,
		Path:      path,
	}, nil
}

func (Cursor) NewSessionCmd(ws string, bin string) Launch {
	return Launch{Path: bin, Dir: ws}
}

// ResumeCmd 走实测确认过的 --resume <chatId>；启动目录取 slug 逆解码的工作区。
func (Cursor) ResumeCmd(s Session, bin string) Launch {
	return Launch{Path: bin, Args: []string{"--resume", s.ID}, Dir: s.Workspace}
}

// WorkspaceToSlug 复刻 Cursor CLI 的规则：盘符冒号直接删除，路径分隔符替换成 '-'
// （d:\data\ws → d-data-ws；注意与 Claude 的 D--data-... 不同）。
func (Cursor) WorkspaceToSlug(ws string) string {
	r := strings.NewReplacer(":", "", `\`, "-", "/", "-")
	return r.Replace(ws)
}

// SlugToWorkspace 是 WorkspaceToSlug 的逆运算，仅用于展示与 resume 启动目录的
// 尽力而为，**是有损的**：路径里自带的 '-' 会被误还原成分隔符。Cursor 的会话
// 记录里没有 cwd 字段，没有更权威的来源可用。
func (Cursor) SlugToWorkspace(slug string) string {
	if slug == "" {
		return ""
	}
	// 首段是单字符盘符（d-data-... → d:\data\...）
	if i := strings.Index(slug, "-"); i == 1 && len(slug) > 2 {
		return slug[0:1] + `:\` + strings.ReplaceAll(slug[2:], "-", `\`)
	}
	return strings.ReplaceAll(slug, "-", string(filepath.Separator))
}
