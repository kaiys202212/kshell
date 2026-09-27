package providers

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"
)

const claudeID = "claude"

var errClaudeNoSessionID = errors.New("claude: 会话文件中未找到 sessionId")

// Claude 对应 Claude Code。会话存于 ~/.claude/projects/<slug>/<sessionId>.jsonl，
// 每行一条 JSON，cwd / sessionId / timestamp 等元信息在任意一条记录里都可能出现。
type Claude struct{}

func (Claude) ID() string          { return claudeID }
func (Claude) DisplayName() string { return "Claude Code" }

func (Claude) DetectSpec(home string) DetectSpec {
	return DetectSpec{
		BinName:     claudeID,
		InstallDirs: []string{"~/.claude/local", "~/.local/bin"},
		ConfigDirs:  []string{"~/.claude"},
	}
}

func (Claude) SessionRoots(home string) []string {
	return []string{filepath.Join(home, ".claude", "projects")}
}

func (Claude) SessionFilePattern() string { return "*.jsonl" }

func (Claude) ParseSession(path string, head []byte) (*Session, error) {
	var (
		id, cwd, title string
		created, updated time.Time
		haveTime        bool
	)

	for _, line := range strings.Split(string(head), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue // 坏行跳过，一条坏数据不该让整个会话消失
		}

		if id == "" {
			id, _ = rec["sessionId"].(string)
		}
		if cwd == "" {
			cwd, _ = rec["cwd"].(string)
		}
		if ts, ok := rec["timestamp"].(string); ok {
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
		if title == "" && rec["type"] == "user" {
			title = oneLine(messageText(rec["message"]), 80)
		}
	}

	if id == "" {
		return nil, errClaudeNoSessionID
	}

	messages := 0
	if n, err := CountLines(path); err == nil {
		messages = n
	}

	return &Session{
		ID:        id,
		ToolID:    claudeID,
		Workspace: cwd,
		Title:     title,
		CreatedAt: created,
		UpdatedAt: updated,
		Messages:  messages,
		Path:      path,
	}, nil
}

func (Claude) NewSessionCmd(ws string, bin string, ctx []string) Launch {
	launch := Launch{Path: bin, Dir: ws}
	if prompt := ContextPrompt(ctx); prompt != "" {
		launch.Args = []string{prompt}
	}
	return launch
}

// ResumeCmd 使用实测确认过的 --resume <sessionId>，并在会话所属工作区启动。
func (Claude) ResumeCmd(s Session, bin string) Launch {
	return Launch{Path: bin, Args: []string{"--resume", s.ID}, Dir: s.Workspace}
}

// WorkspaceToSlug 复刻 Claude 的规则：路径分隔符与盘符冒号都替换成 '-'（D:\data → D--data）。
func (Claude) WorkspaceToSlug(ws string) string {
	r := strings.NewReplacer(":", "-", `\`, "-", "/", "-")
	return r.Replace(ws)
}

// SlugToWorkspace 是 WorkspaceToSlug 的逆运算，用于按工作区反查会话目录。
// 盘符形态（D--xxx）可精确还原；其余情况按当前系统的分隔符还原。
func (Claude) SlugToWorkspace(slug string) string {
	if len(slug) >= 3 && slug[1] == '-' && slug[2] == '-' {
		return slug[0:1] + `:\` + strings.ReplaceAll(slug[3:], "-", `\`)
	}
	return strings.ReplaceAll(slug, "-", string(filepath.Separator))
}
