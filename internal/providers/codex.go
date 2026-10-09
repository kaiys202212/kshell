package providers

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/appearance"
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

		if title == "" && (rec.Type == "event_msg" || rec.Type == "response_item") {
			title = codexTitle(rec.Payload, title)
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

// codexTitle 从记录里取标题文本，兼容多种历史形状。
// 用户消息：① payload.item.type == "UserMessage"（content 块数组）；
// ② payload.type == "user_message" 且正文在 payload.message（本机实测的主要形状）。
// <manually_attached_skills>/<environment_context>/<external_links> 等包装文本一律跳过；
// resume 续写文件会把原会话历史包装成一条 "The following is the Codex agent history..."
// 的机器引导语，也不能当标题；用户消息全是包装记录时（agent 自主执行的导入会话）
// 退回首条 assistant 内容：payload.item.type == "AgentMessage"、
// payload.type == "agent_message" 或 response_item。
func codexTitle(payload map[string]any, current string) string {
	// 标题候选统一走 cleanTitle：包装标签块被整块剔除、只包着正文的标签被剥掉标记，
	// 清洗后为空即「纯包装记录」，跳过继续找下一条。
	pick := func(raw string) string {
		if isCodexResumeWrapper(raw) {
			return ""
		}
		if text := cleanTitle(raw); text != "" {
			return oneLine(text, 80)
		}
		return ""
	}

	if item, ok := payload["item"].(map[string]any); ok {
		switch item["type"] {
		case "UserMessage", "AgentMessage":
			if title := pick(messageText(item)); title != "" {
				return title
			}
		}
	}
	switch payload["type"] {
	case "user_message", "agent_message":
		if title := pick(contentText(payload["message"])); title != "" {
			return title
		}
	case "message": // response_item
		if role, _ := payload["role"].(string); role == "assistant" {
			if title := pick(contentText(payload["content"])); title != "" {
				return title
			}
		}
	}
	return current
}

// isCodexResumeWrapper 识别 codex resume 续写文件开头的机器引导语：
// 原会话历史被整段塞进这样一条 user 消息里，正文不是用户手写的。
func isCodexResumeWrapper(raw string) bool {
	const marker = "the following is the codex agent history"
	return strings.HasPrefix(strings.ToLower(strings.TrimLeft(raw, " \t\r\n")), marker)
}

func (Codex) NewSessionCmd(ws string, bin string) Launch {
	return Launch{Path: bin, Dir: ws}
}

// ResumeCmd 使用实测确认过的 `codex resume <SESSION_ID>`，并在会话所属工作区启动。
func (Codex) ResumeCmd(s Session, bin string) Launch {
	return Launch{Path: bin, Args: []string{"resume", s.ID}, Dir: s.Workspace}
}

// RemoteNewArgs / RemoteResumeArgs：Codex 无官方远程协议，ssh 远端执行 CLI。
func (Codex) RemoteNewArgs(string) []string { return nil }
func (Codex) RemoteResumeArgs(s Session) []string {
	return []string{"resume", s.ID}
}

// ThemeOverrides 用 -c 覆盖 tui.theme（会话级，不写 config.toml）。
func (Codex) ThemeOverrides(theme appearance.Theme, _ string) ([]string, map[string]string) {
	name := "catppuccin-latte"
	if theme == appearance.ThemeDark {
		name = "catppuccin-mocha"
	}
	return []string{"-c", "tui.theme=" + name}, nil
}

func (Codex) InstallRecipe() InstallRecipe {
	r := npmInstall("@openai/codex")
	r.PurgeDirs = []string{"~/.codex"}
	return r
}
