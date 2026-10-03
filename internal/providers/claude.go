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

// errClaudeEmpty 表示整段记录里没有任何真实对话内容（只有 /clear、hook 进度、
// 系统旁白这类记录留下的空壳会话），列表里只会是噪音，扫描阶段直接剔除。
var errClaudeEmpty = errors.New("claude: 会话无实际内容（仅命令/系统记录）")

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

// MatchSessionRel 按目录深度过滤：~/.claude/projects/<slug>/<sessionId>.jsonl 才是会话文件。
// <sessionId>/subagents/agent-*.jsonl 是子代理的执行记录（不能拿去 resume），
// 没有 PathMatcher 时会被整体当会话收录，列表里出现一大批同名条目。
func (Claude) MatchSessionRel(rel string) bool {
	rel = filepath.ToSlash(rel)
	rel = strings.TrimPrefix(rel, "./")
	// 恰好两层：<工作区slug>/<会话文件>.jsonl
	return len(strings.Split(rel, "/")) == 2 && strings.HasSuffix(rel, ".jsonl")
}

func (Claude) ParseSession(path string, head []byte) (*Session, error) {
	var (
		id, cwd, title   string
		assistantTitle   string
		created, updated time.Time
		haveTime         bool
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
		// isMeta 记录是 CLI 自己插的旁白（<local-command-caveat>、斜杠命令回显等），
		// 一律不参与标题；剩下的候选再过 cleanTitle，纯包装文本会变成空串被跳过。
		if meta, _ := rec["isMeta"].(bool); !meta {
			text := cleanTitle(messageText(rec["message"]))
			if title == "" && text != "" && rec["type"] == "user" {
				title = oneLine(text, 80)
			}
			if assistantTitle == "" && text != "" && rec["type"] == "assistant" {
				assistantTitle = oneLine(text, 80)
			}
		}
	}

	if id == "" {
		return nil, errClaudeNoSessionID
	}

	// 用户消息全是包装记录时退回首条 assistant 文本；两者都没有说明整个头部
	// 只有 /clear、hook 进度、系统旁白这类记录，属空壳会话，直接剔除。
	if title == "" {
		title = assistantTitle
	}
	if title == "" {
		return nil, errClaudeEmpty
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

func (Claude) NewSessionCmd(ws string, bin string) Launch {
	return Launch{Path: bin, Dir: ws}
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

// SlugToWorkspace 是 WorkspaceToSlug 的逆运算，仅用于展示与粗筛，**不可作为权威来源**。
// 它是有损的：路径里自带的 '-' 会被误还原成分隔符（真实 slug 如 D--data-wps-cache-303217782）。
// 会话的工作区一律以记录里的 cwd 为准。
func (Claude) SlugToWorkspace(slug string) string {
	if len(slug) >= 3 && slug[1] == '-' && slug[2] == '-' {
		return slug[0:1] + `:\` + strings.ReplaceAll(slug[3:], "-", `\`)
	}
	return strings.ReplaceAll(slug, "-", string(filepath.Separator))
}
